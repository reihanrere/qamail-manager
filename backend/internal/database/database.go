package database

import (
	"fmt"

	"gorm.io/driver/postgres"
	"gorm.io/gorm"

	"github.com/reihanrere/qamail-manager/internal/config"
	"github.com/reihanrere/qamail-manager/internal/models"
)

// Connect opens a GORM/PostgreSQL connection and runs auto-migration.
func Connect(cfg *config.Config) (*gorm.DB, error) {
	dsn := fmt.Sprintf(
		"host=%s port=%s user=%s password=%s dbname=%s sslmode=disable",
		cfg.DBHost, cfg.DBPort, cfg.DBUser, cfg.DBPassword, cfg.DBName,
	)

	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{})
	if err != nil {
		return nil, fmt.Errorf("failed to connect to database: %w", err)
	}

	if err := Migrate(db, AdminSeed{Email: cfg.AdminEmail, Password: cfg.AdminPassword}); err != nil {
		return nil, err
	}

	return db, nil
}

// Migrate creates or updates the schema and, on the first start with logins, hands every
// existing account to the admin from seed. It is idempotent and runs on every start.
func Migrate(db *gorm.DB, seed AdminSeed) error {
	if err := db.AutoMigrate(&models.User{}, &models.Session{}); err != nil {
		return fmt.Errorf("failed to auto-migrate users: %w", err)
	}
	if err := claimOrphanAccounts(db, seed); err != nil {
		return err
	}
	if err := db.AutoMigrate(&models.MailAccount{}, &models.Message{}, &models.SettingOverride{}, &models.RetiredAddress{}); err != nil {
		return fmt.Errorf("failed to auto-migrate database: %w", err)
	}
	if err := ensureAdmin(db, seed); err != nil {
		return err
	}

	// One stored copy per Message-ID and account, enforced by the database so concurrent
	// retries from Cloudflare cannot both insert. Older duplicates are removed first.
	statements := []string{
		`DELETE FROM messages a USING messages b
		 WHERE a.message_id <> '' AND a.account_id = b.account_id AND a.message_id = b.message_id
		   AND (a.created_at, a.id) > (b.created_at, b.id)`,
		`CREATE UNIQUE INDEX IF NOT EXISTS idx_messages_account_message_id
		 ON messages (account_id, message_id) WHERE message_id <> ''`,
		// Deleted addresses stay reserved forever, whichever code path deleted the row
		`CREATE OR REPLACE FUNCTION retire_deleted_address() RETURNS trigger AS $$
		 BEGIN
		   INSERT INTO retired_addresses (email, retired_at) VALUES (OLD.email, now()) ON CONFLICT DO NOTHING;
		   RETURN OLD;
		 END $$ LANGUAGE plpgsql`,
		`DROP TRIGGER IF EXISTS mail_accounts_retire_address ON mail_accounts`,
		`CREATE TRIGGER mail_accounts_retire_address AFTER DELETE ON mail_accounts
		 FOR EACH ROW EXECUTE FUNCTION retire_deleted_address()`,
	}
	for _, statement := range statements {
		if err := db.Exec(statement).Error; err != nil {
			return fmt.Errorf("failed to migrate database objects: %w", err)
		}
	}
	return nil
}
