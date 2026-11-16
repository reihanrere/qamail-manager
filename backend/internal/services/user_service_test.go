package services

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"github.com/reihanrere/qamail-manager/internal/auth"
	"github.com/reihanrere/qamail-manager/internal/models"
)

func TestUserCreateValidation(t *testing.T) {
	db := testDB(t)
	users := NewUserService(db, 10)
	ctx := context.Background()

	if _, err := users.Create(ctx, "not-an-email", testPassword, models.RoleUser); !errors.Is(err, auth.ErrInvalidEmail) {
		t.Errorf("bad email: %v", err)
	}
	if _, err := users.Create(ctx, "a@example.com", "short", models.RoleUser); !errors.Is(err, auth.ErrPasswordTooShort) {
		t.Errorf("short password: %v", err)
	}
	if _, err := users.Create(ctx, "a@example.com", testPassword, "root"); err == nil {
		t.Error("unknown role accepted")
	}
	if _, err := users.Create(ctx, "A@Example.com", testPassword, models.RoleUser); err != nil {
		t.Fatal(err)
	}
	if _, err := users.Create(ctx, "a@example.com", testPassword, models.RoleUser); !errors.Is(err, ErrUserExists) {
		t.Errorf("duplicate: %v", err)
	}
}

func TestUserListCountsAccounts(t *testing.T) {
	db := testDB(t)
	users := NewUserService(db, 10)
	ctx := context.Background()
	u, _ := users.Create(ctx, "a@example.com", testPassword, models.RoleUser)
	if _, err := users.Create(ctx, "b@example.com", testPassword, models.RoleUser); err != nil {
		t.Fatal(err)
	}
	seedAccount(t, db, u.ID, "one@inbox.example.com")
	seedAccount(t, db, u.ID, "two@inbox.example.com")

	list, err := users.List(ctx)
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]int64{}
	for _, s := range list {
		got[s.Email] = s.Accounts
	}
	if got["a@example.com"] != 2 || got["b@example.com"] != 0 {
		t.Fatalf("counts = %v", got)
	}
}

func TestSetPasswordSignsOutEverywhere(t *testing.T) {
	svc, users, _ := newAuthFixture(t)
	ctx := context.Background()
	res, _ := svc.Login(ctx, "alice@example.com", testPassword, "1.1.1.1")

	if err := users.SetPassword(ctx, "alice@example.com", "a-brand-new-password"); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Authenticate(ctx, res.Token); !errors.Is(err, ErrUnauthenticated) {
		t.Fatalf("old session still valid: %v", err)
	}
	if _, err := svc.Login(ctx, "alice@example.com", testPassword, "1.1.1.1"); !errors.Is(err, ErrInvalidCredentials) {
		t.Fatalf("old password still works: %v", err)
	}
	if _, err := svc.Login(ctx, "alice@example.com", "a-brand-new-password", "1.1.1.1"); err != nil {
		t.Fatal(err)
	}
	if err := users.SetPassword(ctx, "ghost@example.com", "a-brand-new-password"); !errors.Is(err, ErrUserNotFound) {
		t.Fatalf("unknown user: %v", err)
	}
}

func TestLastAdminIsProtected(t *testing.T) {
	db := testDB(t)
	users := NewUserService(db, 10)
	ctx := context.Background()
	if _, err := users.Create(ctx, "admin@example.com", testPassword, models.RoleAdmin); err != nil {
		t.Fatal(err)
	}
	// The fixture's trigger user is an admin too; remove it so this is the only one
	db.Where("email = ?", "test-owner@example.com").Delete(&models.User{})

	if err := users.SetDisabled(ctx, "admin@example.com", true); !errors.Is(err, ErrLastAdmin) {
		t.Errorf("disable: %v", err)
	}
	if _, err := users.Delete(ctx, "admin@example.com", true); !errors.Is(err, ErrLastAdmin) {
		t.Errorf("delete: %v", err)
	}
	if _, err := users.Create(ctx, "admin2@example.com", testPassword, models.RoleAdmin); err != nil {
		t.Fatal(err)
	}
	if err := users.SetDisabled(ctx, "admin@example.com", true); err != nil {
		t.Errorf("disable with a second admin: %v", err)
	}
}

func TestDeleteRefusesUserWithAccountsUnlessForced(t *testing.T) {
	db := testDB(t)
	users := NewUserService(db, 10)
	ctx := context.Background()
	u, _ := users.Create(ctx, "a@example.com", testPassword, models.RoleUser)
	acc := seedAccount(t, db, u.ID, "one@inbox.example.com")
	db.Create(&models.Message{AccountID: acc, Subject: "hi"})

	if _, err := users.Delete(ctx, "a@example.com", false); !errors.Is(err, ErrUserHasAccount) {
		t.Fatalf("err = %v, want ErrUserHasAccount", err)
	}
	n, err := users.Delete(ctx, "a@example.com", true)
	if err != nil || n != 1 {
		t.Fatalf("accounts=%d err=%v", n, err)
	}
	var accounts, messages, found int64
	db.Model(&models.MailAccount{}).Where("id = ?", acc).Count(&accounts)
	db.Model(&models.Message{}).Where("account_id = ?", acc).Count(&messages)
	db.Model(&models.User{}).Where("email = ?", "a@example.com").Count(&found)
	if accounts+messages+found != 0 {
		t.Fatalf("leftovers: accounts=%d messages=%d user=%d", accounts, messages, found)
	}
}

func TestDeleteWithoutAccounts(t *testing.T) {
	db := testDB(t)
	users := NewUserService(db, 10)
	ctx := context.Background()
	users.Create(ctx, "a@example.com", testPassword, models.RoleUser)
	if _, err := users.Delete(ctx, "a@example.com", false); err != nil {
		t.Fatal(err)
	}
	if _, err := users.Delete(ctx, "a@example.com", false); !errors.Is(err, ErrUserNotFound) {
		t.Fatalf("err = %v, want ErrUserNotFound", err)
	}
}

func seedAccount(t *testing.T, db *gorm.DB, userID uuid.UUID, email string) uuid.UUID {
	t.Helper()
	account := models.MailAccount{UserID: userID, Provider: "local", AccountID: email, Email: email, Password: "x", Domain: "inbox.example.com"}
	if err := db.Create(&account).Error; err != nil {
		t.Fatal(err)
	}
	return account.ID
}

func TestUserByIDActions(t *testing.T) {
	db := testDB(t)
	users := NewUserService(db, 10)
	ctx := context.Background()
	admin, _ := users.Create(ctx, "root@example.com", testPassword, models.RoleAdmin)
	other, _ := users.Create(ctx, "boss@example.com", testPassword, models.RoleAdmin)
	user, _ := users.Create(ctx, "a@example.com", testPassword, models.RoleUser)
	missing := uuid.New()
	// The fixture's trigger user is an admin too; remove it so only root and boss are admins
	db.Where("email = ?", "test-owner@example.com").Delete(&models.User{})

	if err := users.SetPasswordByID(ctx, missing, testPassword); !errors.Is(err, ErrUserNotFound) {
		t.Errorf("password of unknown id: %v", err)
	}
	if err := users.SetDisabledByID(ctx, admin.ID, missing, true); !errors.Is(err, ErrUserNotFound) {
		t.Errorf("disable unknown id: %v", err)
	}
	if _, err := users.DeleteByID(ctx, admin.ID, missing, false); !errors.Is(err, ErrUserNotFound) {
		t.Errorf("delete unknown id: %v", err)
	}

	// Nobody disables or deletes themselves, even with another admin around
	if err := users.SetDisabledByID(ctx, admin.ID, admin.ID, true); !errors.Is(err, ErrSelfAction) {
		t.Errorf("disable self: %v", err)
	}
	if _, err := users.DeleteByID(ctx, admin.ID, admin.ID, true); !errors.Is(err, ErrSelfAction) {
		t.Errorf("delete self: %v", err)
	}
	// Enabling oneself is harmless and allowed
	if err := users.SetDisabledByID(ctx, admin.ID, admin.ID, false); err != nil {
		t.Errorf("enable self: %v", err)
	}

	// The last enabled admin is protected when the actor is someone else (a non-admin
	// or a deleted account acting through a stale call)
	if err := users.SetDisabledByID(ctx, admin.ID, other.ID, true); err != nil {
		t.Fatal(err)
	}
	if err := users.SetDisabledByID(ctx, user.ID, admin.ID, true); !errors.Is(err, ErrLastAdmin) {
		t.Errorf("disable last admin: %v", err)
	}
	if _, err := users.DeleteByID(ctx, user.ID, admin.ID, true); !errors.Is(err, ErrLastAdmin) {
		t.Errorf("delete last admin: %v", err)
	}

	// Delete reports how many accounts block it
	seedAccount(t, db, user.ID, "one@inbox.example.com")
	n, err := users.DeleteByID(ctx, admin.ID, user.ID, false)
	if !errors.Is(err, ErrUserHasAccount) || n != 1 {
		t.Errorf("delete without force: n=%d err=%v", n, err)
	}
	if n, err = users.DeleteByID(ctx, admin.ID, user.ID, true); err != nil || n != 1 {
		t.Errorf("delete with force: n=%d err=%v", n, err)
	}
	if err := users.SetPasswordByID(ctx, other.ID, "a-new-long-password"); err != nil {
		t.Errorf("password by id: %v", err)
	}
}
