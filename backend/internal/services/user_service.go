package services

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"github.com/reihanrere/qamail-manager/internal/auth"
	"github.com/reihanrere/qamail-manager/internal/models"
)

// Errors returned by UserService (used by the command-line tool).
var (
	ErrUserNotFound   = errors.New("user not found")
	ErrUserExists     = errors.New("a user with this email already exists")
	ErrUserHasAccount = errors.New("user still owns mail accounts")
	ErrLastAdmin      = errors.New("this would leave no enabled admin")
	ErrInvalidRole    = errors.New("unknown role")
	// ErrSelfAction: an admin may not disable or delete their own user through the API.
	ErrSelfAction = errors.New("you cannot disable or delete your own user")
)

// UserService manages users. There is no public signup: only an admin creates them, through
// the command-line tool or the admin API (which share these methods).
type UserService struct {
	db                *gorm.DB
	passwordMinLength int
}

// NewUserService builds the service; passwordMinLength is PASSWORD_MIN_LENGTH.
func NewUserService(db *gorm.DB, passwordMinLength int) *UserService {
	return &UserService{db: db, passwordMinLength: passwordMinLength}
}

// UserSummary is a user with the number of accounts they own.
type UserSummary struct {
	models.User
	Accounts int64
}

// Create adds a user with the given role.
func (s *UserService) Create(ctx context.Context, email, password, role string) (*models.User, error) {
	email = auth.NormalizeEmail(email)
	if err := auth.ValidateEmail(email); err != nil {
		return nil, err
	}
	if err := auth.ValidatePassword(password, s.passwordMinLength); err != nil {
		return nil, err
	}
	if role != models.RoleAdmin && role != models.RoleUser {
		return nil, fmt.Errorf("%w %q", ErrInvalidRole, role)
	}
	var existing int64
	if err := s.db.WithContext(ctx).Model(&models.User{}).Where("email = ?", email).Count(&existing).Error; err != nil {
		return nil, err
	}
	if existing > 0 {
		return nil, ErrUserExists
	}
	hash, err := auth.HashPassword(password)
	if err != nil {
		return nil, err
	}
	user := &models.User{Email: email, PasswordHash: hash, Role: role}
	if err := s.db.WithContext(ctx).Create(user).Error; err != nil {
		return nil, fmt.Errorf("failed to create user: %w", err)
	}
	return user, nil
}

// List returns every user with their account count, oldest first.
func (s *UserService) List(ctx context.Context) ([]UserSummary, error) {
	var users []models.User
	if err := s.db.WithContext(ctx).Order("created_at, email").Find(&users).Error; err != nil {
		return nil, err
	}
	type count struct {
		UserID uuidString
		N      int64
	}
	var counts []count
	if err := s.db.WithContext(ctx).Model(&models.MailAccount{}).
		Select("user_id::text AS user_id, count(*) AS n").Group("user_id").Scan(&counts).Error; err != nil {
		return nil, err
	}
	byUser := make(map[string]int64, len(counts))
	for _, c := range counts {
		byUser[string(c.UserID)] = c.N
	}
	out := make([]UserSummary, len(users))
	for i, u := range users {
		out[i] = UserSummary{User: u, Accounts: byUser[u.ID.String()]}
	}
	return out, nil
}

// uuidString scans a uuid column cast to text.
type uuidString string

// SetPassword replaces a user's password and signs them out everywhere.
func (s *UserService) SetPassword(ctx context.Context, email, password string) error {
	return s.setPassword(ctx, s.byEmail(email), password)
}

// SetPasswordByID is SetPassword for the admin API, which addresses users by id.
func (s *UserService) SetPasswordByID(ctx context.Context, id uuid.UUID, password string) error {
	return s.setPassword(ctx, s.byID(id), password)
}

func (s *UserService) setPassword(ctx context.Context, lookup userLookup, password string) error {
	if err := auth.ValidatePassword(password, s.passwordMinLength); err != nil {
		return err
	}
	hash, err := auth.HashPassword(password)
	if err != nil {
		return err
	}
	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		user, err := lookup(tx)
		if err != nil {
			return err
		}
		return s.applyPassword(tx, user, hash)
	})
}

func (s *UserService) applyPassword(tx *gorm.DB, user *models.User, hash string) error {
	if err := tx.Model(user).Update("password_hash", hash).Error; err != nil {
		return err
	}
	return tx.Where("user_id = ?", user.ID).Delete(&models.Session{}).Error
}

// SetDisabled blocks or unblocks logins. Disabling signs the user out at once; their data stays.
func (s *UserService) SetDisabled(ctx context.Context, email string, disabled bool) error {
	return s.setDisabled(ctx, s.byEmail(email), uuid.Nil, disabled)
}

// SetDisabledByID is SetDisabled for the admin API. actor is the admin making the request,
// who may not disable themselves.
func (s *UserService) SetDisabledByID(ctx context.Context, actor, id uuid.UUID, disabled bool) error {
	return s.setDisabled(ctx, s.byID(id), actor, disabled)
}

func (s *UserService) setDisabled(ctx context.Context, lookup userLookup, actor uuid.UUID, disabled bool) error {
	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		user, err := lookup(tx)
		if err != nil {
			return err
		}
		return s.applyDisabled(tx, user, actor, disabled)
	})
}

func (s *UserService) applyDisabled(tx *gorm.DB, user *models.User, actor uuid.UUID, disabled bool) error {
	if disabled {
		if user.ID == actor {
			return ErrSelfAction
		}
		if err := s.guardLastAdmin(tx, user); err != nil {
			return err
		}
	}
	if err := tx.Model(user).Update("disabled", disabled).Error; err != nil {
		return err
	}
	if disabled {
		return tx.Where("user_id = ?", user.ID).Delete(&models.Session{}).Error
	}
	return nil
}

// UserChanges are the edits the admin API applies to one user; nil fields stay as they are.
type UserChanges struct {
	Disabled *bool
	Password *string
}

// UpdateByID applies the changes in one transaction, so a refused change (a weak password,
// disabling oneself) leaves the user as it was. Disabling or a new password signs the user out.
func (s *UserService) UpdateByID(ctx context.Context, actor, id uuid.UUID, changes UserChanges) error {
	var hash string
	if changes.Password != nil {
		if err := auth.ValidatePassword(*changes.Password, s.passwordMinLength); err != nil {
			return err
		}
		var err error
		if hash, err = auth.HashPassword(*changes.Password); err != nil {
			return err
		}
	}
	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		user, err := s.byID(id)(tx)
		if err != nil {
			return err
		}
		if changes.Disabled != nil {
			if err := s.applyDisabled(tx, user, actor, *changes.Disabled); err != nil {
				return err
			}
		}
		if changes.Password != nil {
			return s.applyPassword(tx, user, hash)
		}
		return nil
	})
}

// Delete removes a user. While they still own accounts it is refused unless force is set,
// which also deletes those accounts and their stored messages from this database (the
// mailboxes at the provider are not touched). It reports how many accounts the user owns,
// also when it refuses because of them.
func (s *UserService) Delete(ctx context.Context, email string, force bool) (accounts int64, err error) {
	return s.delete(ctx, s.byEmail(email), uuid.Nil, force)
}

// DeleteByID is Delete for the admin API. actor is the admin making the request, who may
// not delete themselves.
func (s *UserService) DeleteByID(ctx context.Context, actor, id uuid.UUID, force bool) (accounts int64, err error) {
	return s.delete(ctx, s.byID(id), actor, force)
}

func (s *UserService) delete(ctx context.Context, lookup userLookup, actor uuid.UUID, force bool) (accounts int64, err error) {
	err = s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		user, err := lookup(tx)
		if err != nil {
			return err
		}
		if user.ID == actor {
			return ErrSelfAction
		}
		if err := s.guardLastAdmin(tx, user); err != nil {
			return err
		}
		if err := tx.Model(&models.MailAccount{}).Where("user_id = ?", user.ID).Count(&accounts).Error; err != nil {
			return err
		}
		if accounts > 0 {
			if !force {
				return ErrUserHasAccount
			}
			if err := tx.Exec(`DELETE FROM messages WHERE account_id IN (SELECT id FROM mail_accounts WHERE user_id = ?)`, user.ID).Error; err != nil {
				return err
			}
			if err := tx.Where("user_id = ?", user.ID).Delete(&models.MailAccount{}).Error; err != nil {
				return err
			}
		}
		// Sessions go with the user (ON DELETE CASCADE)
		return tx.Delete(user).Error
	})
	if errors.Is(err, ErrUserHasAccount) {
		return accounts, err
	}
	if err != nil {
		return 0, err
	}
	return accounts, nil
}

// userLookup finds the user a write applies to, inside the write's transaction.
type userLookup func(tx *gorm.DB) (*models.User, error)

func (s *UserService) byEmail(email string) userLookup {
	return func(tx *gorm.DB) (*models.User, error) { return s.find(tx, email) }
}

func (s *UserService) byID(id uuid.UUID) userLookup {
	return func(tx *gorm.DB) (*models.User, error) {
		var user models.User
		err := tx.Where("id = ?", id).First(&user).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrUserNotFound
		}
		if err != nil {
			return nil, err
		}
		return &user, nil
	}
}

func (s *UserService) find(tx *gorm.DB, email string) (*models.User, error) {
	var user models.User
	err := tx.Where("email = ?", auth.NormalizeEmail(email)).First(&user).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrUserNotFound
	}
	if err != nil {
		return nil, err
	}
	return &user, nil
}

// guardLastAdmin refuses to disable or delete the only enabled admin, which would lock
// everyone out of settings with no way back except the database.
func (s *UserService) guardLastAdmin(tx *gorm.DB, user *models.User) error {
	if !user.IsAdmin() || user.Disabled {
		return nil
	}
	var others int64
	if err := tx.Model(&models.User{}).
		Where("role = ? AND disabled = false AND id <> ?", models.RoleAdmin, user.ID).Count(&others).Error; err != nil {
		return err
	}
	if others == 0 {
		return ErrLastAdmin
	}
	return nil
}
