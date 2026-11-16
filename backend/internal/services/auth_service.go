package services

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"sync"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/reihanrere/qamail-manager/internal/auth"
	"github.com/reihanrere/qamail-manager/internal/models"
)

// Errors returned by AuthService. Handlers map them to 401 and 429.
var (
	// ErrInvalidCredentials covers an unknown email, a wrong password and a disabled user
	// alike, so a caller cannot tell which accounts exist.
	ErrInvalidCredentials = errors.New("invalid email or password")
	// ErrUnauthenticated means the session cookie is missing, unknown, expired or revoked.
	ErrUnauthenticated = errors.New("authentication required")
	// ErrLoginBusy means every password-check slot stayed taken for the whole wait. Nothing
	// was checked and the try does not count against the user; the client may retry.
	ErrLoginBusy = errors.New("login is busy, try again shortly")
)

// TooManyAttemptsError is returned while a client is locked out after repeated failed logins.
type TooManyAttemptsError struct{ RetryAfter time.Duration }

func (e *TooManyAttemptsError) Error() string {
	return fmt.Sprintf("too many failed login attempts, retry in %s", e.RetryAfter.Round(time.Second))
}

// Session tokens are 32 random bytes; only their SHA-256 is stored.
const sessionTokenBytes = 32

// lastSeenGranularity limits how often a busy session writes last_seen_at.
const lastSeenGranularity = time.Minute

// maxLoginEmailLength is the longest valid address (RFC 5321); longer input cannot match a user.
const maxLoginEmailLength = 254

// Defaults for AuthSettings fields left at zero (the environment always sets them).
const (
	defaultMaxConcurrentVerifies = 4
	defaultVerifyWait            = 2 * time.Second
)

// AuthSettings are the login tunables, all read from the environment.
type AuthSettings struct {
	SessionTTL       time.Duration
	LoginMaxAttempts int
	LoginWindow      time.Duration
	// MaxConcurrentVerifies bounds password checks running at once: each allocates 64 MiB,
	// and they run before the caller is authenticated. VerifyWait is how long a login waits
	// for a free slot before it is refused with ErrLoginBusy.
	MaxConcurrentVerifies int
	VerifyWait            time.Duration
}

// AuthService logs users in and validates their sessions.
type AuthService struct {
	db       *gorm.DB
	settings AuthSettings
	limiter  *auth.LoginLimiter
	now      func() time.Time
	// verifySlots is a counting semaphore for password checks
	verifySlots chan struct{}

	dummyOnce sync.Once
	dummyHash string
}

// NewAuthService builds the service.
func NewAuthService(db *gorm.DB, settings AuthSettings) *AuthService {
	if settings.MaxConcurrentVerifies <= 0 {
		settings.MaxConcurrentVerifies = defaultMaxConcurrentVerifies
	}
	if settings.VerifyWait <= 0 {
		settings.VerifyWait = defaultVerifyWait
	}
	return &AuthService{
		db:          db,
		settings:    settings,
		limiter:     auth.NewLoginLimiter(settings.LoginMaxAttempts, settings.LoginWindow),
		now:         time.Now,
		verifySlots: make(chan struct{}, settings.MaxConcurrentVerifies),
	}
}

// verify checks a password while holding one of the limited slots. It returns ErrLoginBusy
// when no slot frees up in time (or ctx ends), without having checked anything.
func (s *AuthService) verify(ctx context.Context, password, hash string) (bool, error) {
	timer := time.NewTimer(s.settings.VerifyWait)
	defer timer.Stop()
	select {
	case s.verifySlots <- struct{}{}:
		defer func() { <-s.verifySlots }()
	case <-timer.C:
		return false, ErrLoginBusy
	case <-ctx.Done():
		return false, ErrLoginBusy
	}
	return auth.VerifyPassword(password, hash)
}

// LoginResult is a new session: the cookie value and when it stops working.
type LoginResult struct {
	Token     string
	ExpiresAt time.Time
	User      models.User
}

// Login checks the credentials and opens a session. clientIP only feeds the rate limit
// (one budget per IP and email). The cost of an unknown email matches a wrong password,
// and a disabled user is rejected only after the password check, so timing and message
// reveal nothing about which emails exist.
func (s *AuthService) Login(ctx context.Context, email, password, clientIP string) (*LoginResult, error) {
	email = auth.NormalizeEmail(email)
	if len(email) > maxLoginEmailLength {
		email = email[:maxLoginEmailLength]
	}
	key := clientIP + "|" + email

	// Counting the try and checking the limit are one step, so a burst of parallel requests
	// cannot all pass before the first failure is recorded
	if ok, retryAfter := s.limiter.Attempt(key); !ok {
		return nil, &TooManyAttemptsError{RetryAfter: retryAfter}
	}

	var user models.User
	err := s.db.WithContext(ctx).Where("email = ?", email).First(&user).Error
	switch {
	case errors.Is(err, gorm.ErrRecordNotFound):
		if _, verr := s.verify(ctx, password, s.dummy()); errors.Is(verr, ErrLoginBusy) {
			s.limiter.Undo(key)
			return nil, ErrLoginBusy
		}
		return nil, ErrInvalidCredentials
	case err != nil:
		s.limiter.Undo(key)
		return nil, err
	}

	ok, verr := s.verify(ctx, password, user.PasswordHash)
	if errors.Is(verr, ErrLoginBusy) {
		s.limiter.Undo(key)
		return nil, ErrLoginBusy
	}
	if verr != nil || !ok || user.Disabled {
		return nil, ErrInvalidCredentials
	}

	token, err := newSessionToken()
	if err != nil {
		return nil, err
	}
	now := s.now()
	session := models.Session{
		UserID:     user.ID,
		TokenHash:  hashToken(token),
		ExpiresAt:  now.Add(s.settings.SessionTTL),
		LastSeenAt: now,
	}
	// The password was checked against a copy of the user read earlier. Lock the row and
	// compare again before storing the session: SetPassword and SetDisabled change the row
	// and then delete the user's sessions, so a login racing them either sees the change
	// here or is deleted by them, and never keeps a session they meant to end.
	err = s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var current models.User
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&current, "id = ?", user.ID).Error; err != nil {
			return err
		}
		if current.PasswordHash != user.PasswordHash || current.Disabled {
			return ErrInvalidCredentials
		}
		return tx.Create(&session).Error
	})
	if errors.Is(err, ErrInvalidCredentials) {
		return nil, ErrInvalidCredentials
	}
	if err != nil {
		return nil, fmt.Errorf("failed to create session: %w", err)
	}
	s.limiter.Reset(key)
	// Housekeeping: expired sessions are useless, so drop them whenever someone logs in
	s.db.WithContext(ctx).Where("expires_at <= ?", now).Delete(&models.Session{})

	return &LoginResult{Token: token, ExpiresAt: session.ExpiresAt, User: user}, nil
}

// Authenticate returns the user behind a session cookie value, or ErrUnauthenticated when
// the session is unknown, expired, revoked, or its user is disabled.
func (s *AuthService) Authenticate(ctx context.Context, token string) (*models.User, error) {
	if token == "" || len(token) > 128 {
		return nil, ErrUnauthenticated
	}
	now := s.now()
	var session models.Session
	err := s.db.WithContext(ctx).Preload("User").
		Where("token_hash = ? AND expires_at > ?", hashToken(token), now).First(&session).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrUnauthenticated
	}
	if err != nil {
		return nil, err
	}
	if session.User == nil || session.User.Disabled {
		return nil, ErrUnauthenticated
	}
	if now.Sub(session.LastSeenAt) >= lastSeenGranularity {
		// Best effort: failing to note activity must not fail the request
		s.db.WithContext(ctx).Model(&models.Session{}).Where("id = ?", session.ID).Update("last_seen_at", now)
	}
	return session.User, nil
}

// Logout revokes the session behind token. An unknown token is not an error.
func (s *AuthService) Logout(ctx context.Context, token string) error {
	return s.db.WithContext(ctx).Where("token_hash = ?", hashToken(token)).Delete(&models.Session{}).Error
}

// dummy returns a valid hash to verify against when the email is unknown.
func (s *AuthService) dummy() string {
	s.dummyOnce.Do(func() {
		s.dummyHash, _ = auth.HashPassword("dummy-password-for-timing")
	})
	return s.dummyHash
}

func newSessionToken() (string, error) {
	raw := make([]byte, sessionTokenBytes)
	if _, err := rand.Read(raw); err != nil {
		return "", fmt.Errorf("failed to generate session token: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(raw), nil
}

func hashToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}
