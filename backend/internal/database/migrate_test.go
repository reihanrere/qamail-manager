package database

import (
	"os"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	"github.com/reihanrere/qamail-manager/internal/auth"
	"github.com/reihanrere/qamail-manager/internal/models"
)

// These tests need a disposable PostgreSQL database (see services/integration_test.go)
// and DROP every application table first. Skipped when TEST_DATABASE_DSN is not set.
func emptyDB(t *testing.T) *gorm.DB {
	t.Helper()
	dsn := os.Getenv("TEST_DATABASE_DSN")
	if dsn == "" {
		t.Skip("TEST_DATABASE_DSN not set")
	}
	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Exec("DROP TABLE IF EXISTS messages, retired_addresses, sessions, mail_accounts, users, setting_overrides CASCADE").Error; err != nil {
		t.Fatal(err)
	}
	return db
}

// legacyMailAccount is mail_accounts as GORM created it before logins existed (no user_id).
type legacyMailAccount struct {
	ID        uuid.UUID `gorm:"type:uuid;default:gen_random_uuid();primaryKey"`
	Provider  string    `gorm:"not null;default:mailtm;index"`
	AccountID string    `gorm:"uniqueIndex"`
	Email     string    `gorm:"uniqueIndex;not null"`
	Password  string    `gorm:"not null"`
	Domain    string    `gorm:"not null"`
	Status    string    `gorm:"default:AVAILABLE"`
	CreatedAt time.Time
	UpdatedAt time.Time
}

func (legacyMailAccount) TableName() string { return "mail_accounts" }

func legacySchema(t *testing.T, db *gorm.DB, rows int) {
	t.Helper()
	if err := db.AutoMigrate(&legacyMailAccount{}); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < rows; i++ {
		account := legacyMailAccount{Email: strings.Repeat("a", i+1) + "@x.test", AccountID: uuid.NewString(), Password: "pw", Domain: "x.test"}
		if err := db.Create(&account).Error; err != nil {
			t.Fatal(err)
		}
	}
}

var seed = AdminSeed{Email: "admin@example.com", Password: "a-long-enough-password"}

func userIDNullable(t *testing.T, db *gorm.DB) bool {
	t.Helper()
	var nullable string
	if err := db.Raw(`SELECT is_nullable FROM information_schema.columns
		WHERE table_name = 'mail_accounts' AND column_name = 'user_id'`).Scan(&nullable).Error; err != nil {
		t.Fatal(err)
	}
	return nullable == "YES"
}

func TestMigrateAssignsExistingAccountsToAdmin(t *testing.T) {
	db := emptyDB(t)
	legacySchema(t, db, 3)

	if err := Migrate(db, seed); err != nil {
		t.Fatal(err)
	}

	var admin models.User
	if err := db.Where("email = ?", seed.Email).First(&admin).Error; err != nil {
		t.Fatal(err)
	}
	if !admin.IsAdmin() || admin.Disabled {
		t.Fatalf("admin = %+v", admin)
	}
	if ok, err := auth.VerifyPassword(seed.Password, admin.PasswordHash); err != nil || !ok {
		t.Fatalf("admin password does not verify: ok=%v err=%v", ok, err)
	}
	var owned, total int64
	db.Model(&models.MailAccount{}).Where("user_id = ?", admin.ID).Count(&owned)
	db.Model(&models.MailAccount{}).Count(&total)
	if total != 3 || owned != 3 {
		t.Fatalf("owned %d of %d accounts", owned, total)
	}
	if userIDNullable(t, db) {
		t.Fatal("user_id must be NOT NULL after migration")
	}
}

func TestMigrateIsIdempotent(t *testing.T) {
	db := emptyDB(t)
	legacySchema(t, db, 2)
	if err := Migrate(db, seed); err != nil {
		t.Fatal(err)
	}
	var first models.User
	db.First(&first)

	// A second start (even without the admin env) changes nothing, including the NOT NULL.
	for i := 0; i < 2; i++ {
		if err := Migrate(db, AdminSeed{}); err != nil {
			t.Fatalf("start %d: %v", i+2, err)
		}
	}
	var users, owned int64
	db.Model(&models.User{}).Count(&users)
	db.Model(&models.MailAccount{}).Where("user_id = ?", first.ID).Count(&owned)
	if users != 1 || owned != 2 {
		t.Fatalf("users=%d owned=%d", users, owned)
	}
	var again models.User
	db.First(&again)
	if again.ID != first.ID || again.PasswordHash != first.PasswordHash || !again.UpdatedAt.Equal(first.UpdatedAt) {
		t.Fatal("admin was modified by a later start")
	}
	if userIDNullable(t, db) {
		t.Fatal("a later start loosened user_id")
	}
}

func TestMigrateRefusesOwnerlessAccountsWithoutAdminEnv(t *testing.T) {
	db := emptyDB(t)
	legacySchema(t, db, 2)

	err := Migrate(db, AdminSeed{})
	if err == nil || !strings.Contains(err.Error(), "ADMIN_EMAIL") {
		t.Fatalf("expected an error naming ADMIN_EMAIL, got %v", err)
	}
	// The whole step rolled back: no admin and no user_id column were left behind.
	var users int64
	db.Model(&models.User{}).Count(&users)
	if users != 0 || db.Migrator().HasColumn(&models.MailAccount{}, "user_id") {
		t.Fatalf("failed start must change nothing: users=%d", users)
	}
}

func TestMigrateUsesExistingAdminForOrphans(t *testing.T) {
	db := emptyDB(t)
	if err := Migrate(db, seed); err != nil {
		t.Fatal(err)
	}
	var admin models.User
	db.First(&admin)
	// Pretend the column was added by hand and a row slipped in without an owner.
	db.Exec("ALTER TABLE mail_accounts ALTER COLUMN user_id DROP NOT NULL")
	db.Exec(`INSERT INTO mail_accounts (email, password, domain) VALUES ('o@x.test', 'pw', 'x.test')`)

	if err := Migrate(db, AdminSeed{}); err != nil {
		t.Fatal(err)
	}
	var owner string
	db.Raw("SELECT user_id::text FROM mail_accounts WHERE email = 'o@x.test'").Scan(&owner)
	if owner != admin.ID.String() {
		t.Fatalf("owner = %s, want %s", owner, admin.ID)
	}
}

func TestMigrateFreshDatabase(t *testing.T) {
	db := emptyDB(t)
	if err := Migrate(db, AdminSeed{}); err != nil {
		t.Fatalf("fresh database without admin env must start: %v", err)
	}
	var users int64
	db.Model(&models.User{}).Count(&users)
	if users != 0 {
		t.Fatalf("users = %d", users)
	}
	if err := Migrate(db, seed); err != nil {
		t.Fatal(err)
	}
	db.Model(&models.User{}).Count(&users)
	if users != 1 {
		t.Fatalf("first admin not created: users = %d", users)
	}
	// The env is ignored once a user exists.
	if err := Migrate(db, AdminSeed{Email: "other@example.com", Password: "another-long-password"}); err != nil {
		t.Fatal(err)
	}
	db.Model(&models.User{}).Count(&users)
	if users != 1 {
		t.Fatalf("second admin created from env: users = %d", users)
	}
}

func TestAccountsRequireAnExistingOwner(t *testing.T) {
	db := emptyDB(t)
	if err := Migrate(db, seed); err != nil {
		t.Fatal(err)
	}
	err := db.Exec(`INSERT INTO mail_accounts (email, password, domain, user_id)
		VALUES ('x@x.test', 'pw', 'x.test', gen_random_uuid())`).Error
	if err == nil {
		t.Fatal("an account with an unknown user_id must be rejected")
	}
	// A user who still owns accounts cannot be deleted.
	var admin models.User
	db.First(&admin)
	db.Exec(`INSERT INTO mail_accounts (email, password, domain, user_id) VALUES ('y@x.test', 'pw', 'x.test', ?)`, admin.ID)
	if err := db.Delete(&admin).Error; err == nil {
		t.Fatal("deleting a user who owns accounts must be refused")
	}
}

func TestDeletingUserRemovesSessions(t *testing.T) {
	db := emptyDB(t)
	if err := Migrate(db, seed); err != nil {
		t.Fatal(err)
	}
	var admin models.User
	db.First(&admin)
	if err := db.Create(&models.Session{UserID: admin.ID, TokenHash: "h"}).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Delete(&admin).Error; err != nil {
		t.Fatal(err)
	}
	var sessions int64
	db.Model(&models.Session{}).Count(&sessions)
	if sessions != 0 {
		t.Fatalf("sessions left: %d", sessions)
	}
}
