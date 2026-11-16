package database

import (
	"fmt"
	"log"

	"gorm.io/gorm"

	"github.com/reihanrere/qamail-manager/internal/auth"
	"github.com/reihanrere/qamail-manager/internal/models"
)

// AdminSeed carries ADMIN_EMAIL / ADMIN_PASSWORD. It is used only to create the first
// admin; once an admin exists it is ignored.
type AdminSeed struct {
	Email    string
	Password string
}

func (s AdminSeed) configured() bool { return s.Email != "" && s.Password != "" }

// createAdmin inserts the seed as an admin user.
func createAdmin(db *gorm.DB, seed AdminSeed) (*models.User, error) {
	hash, err := auth.HashPassword(seed.Password)
	if err != nil {
		return nil, fmt.Errorf("failed to hash admin password: %w", err)
	}
	admin := &models.User{Email: auth.NormalizeEmail(seed.Email), PasswordHash: hash, Role: models.RoleAdmin}
	if err := db.Create(admin).Error; err != nil {
		return nil, fmt.Errorf("failed to create admin %s: %w", admin.Email, err)
	}
	log.Printf("auth: created admin %s", admin.Email)
	return admin, nil
}

// claimOrphanAccounts gives every mail account that predates logins to the admin and then
// makes user_id mandatory. It must run before the final AutoMigrate of mail_accounts, which
// would otherwise fail (or leave rows ownerless) on a table that already holds data.
// Safe to repeat: once nothing is ownerless it only re-asserts NOT NULL.
func claimOrphanAccounts(db *gorm.DB, seed AdminSeed) error {
	if !db.Migrator().HasTable(&models.MailAccount{}) {
		return nil // fresh database: AutoMigrate creates the table with the column
	}
	return db.Transaction(func(tx *gorm.DB) error {
		if !tx.Migrator().HasColumn(&models.MailAccount{}, "user_id") {
			if err := tx.Exec(`ALTER TABLE mail_accounts ADD COLUMN user_id uuid`).Error; err != nil {
				return fmt.Errorf("failed to add mail_accounts.user_id: %w", err)
			}
		}

		var orphans int64
		if err := tx.Model(&models.MailAccount{}).Where("user_id IS NULL").Count(&orphans).Error; err != nil {
			return err
		}
		if orphans > 0 {
			var admins []models.User
			if err := tx.Where("role = ?", models.RoleAdmin).Order("created_at, id").Limit(1).Find(&admins).Error; err != nil {
				return err
			}
			var admin models.User
			switch {
			case len(admins) > 0:
				admin = admins[0]
			case !seed.configured():
				return fmt.Errorf("%d mail accounts have no owner and there is no admin to take them: "+
					"set ADMIN_EMAIL and ADMIN_PASSWORD, then restart", orphans)
			default:
				created, err := createAdmin(tx, seed)
				if err != nil {
					return err
				}
				admin = *created
			}
			// Plain SQL: leaves updated_at alone, since this is bookkeeping, not an edit.
			if err := tx.Exec(`UPDATE mail_accounts SET user_id = ? WHERE user_id IS NULL`, admin.ID).Error; err != nil {
				return fmt.Errorf("failed to assign accounts to %s: %w", admin.Email, err)
			}
			log.Printf("auth: assigned %d existing accounts to admin %s", orphans, admin.Email)
		}

		// GORM never tightens an existing column to NOT NULL, so do it explicitly.
		if err := tx.Exec(`ALTER TABLE mail_accounts ALTER COLUMN user_id SET NOT NULL`).Error; err != nil {
			return fmt.Errorf("failed to require mail_accounts.user_id: %w", err)
		}
		return nil
	})
}

// ensureAdmin creates the first admin from the seed when no user exists yet.
func ensureAdmin(db *gorm.DB, seed AdminSeed) error {
	var users int64
	if err := db.Model(&models.User{}).Count(&users).Error; err != nil {
		return err
	}
	if users > 0 {
		return nil
	}
	if !seed.configured() {
		log.Println("auth: no users exist; run `./api user add <email> --admin` (docker compose exec backend), or set ADMIN_EMAIL and ADMIN_PASSWORD, to create the first admin")
		return nil
	}
	_, err := createAdmin(db, seed)
	return err
}
