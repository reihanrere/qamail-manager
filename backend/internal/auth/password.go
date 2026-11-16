// Package auth holds the credential primitives: password hashing and input rules.
package auth

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"net/mail"
	"strings"

	"golang.org/x/crypto/argon2"
)

// Argon2id cost (OWASP guidance). The parameters are stored inside every hash, so they can
// be raised later without invalidating existing passwords.
const (
	argonTime    uint32 = 3
	argonMemory  uint32 = 64 * 1024 // KiB
	argonThreads uint8  = 2
	argonKeyLen  uint32 = 32
	saltLen             = 16

	// MaxPasswordBytes bounds the input handed to the hash function.
	MaxPasswordBytes = 256
)

// Errors returned by the credential helpers.
var (
	ErrInvalidHash      = errors.New("auth: invalid password hash")
	ErrPasswordTooLong  = errors.New("auth: password too long")
	ErrPasswordTooShort = errors.New("auth: password too short")
	ErrInvalidEmail     = errors.New("auth: invalid email address")
)

// HashPassword returns a PHC-format argon2id hash with a random salt.
func HashPassword(password string) (string, error) {
	if len(password) > MaxPasswordBytes {
		return "", ErrPasswordTooLong
	}
	salt := make([]byte, saltLen)
	if _, err := rand.Read(salt); err != nil {
		return "", fmt.Errorf("auth: read random salt: %w", err)
	}
	key := argon2.IDKey([]byte(password), salt, argonTime, argonMemory, argonThreads, argonKeyLen)
	return fmt.Sprintf("$argon2id$v=%d$m=%d,t=%d,p=%d$%s$%s",
		argon2.Version, argonMemory, argonTime, argonThreads,
		base64.RawStdEncoding.EncodeToString(salt), base64.RawStdEncoding.EncodeToString(key)), nil
}

// VerifyPassword compares a password with a hash from HashPassword in constant time.
// A malformed hash returns ErrInvalidHash.
func VerifyPassword(password, encoded string) (bool, error) {
	if len(password) > MaxPasswordBytes {
		return false, nil
	}
	parts := strings.Split(encoded, "$")
	if len(parts) != 6 || parts[0] != "" || parts[1] != "argon2id" {
		return false, ErrInvalidHash
	}
	var version int
	if _, err := fmt.Sscanf(parts[2], "v=%d", &version); err != nil || version != argon2.Version {
		return false, ErrInvalidHash
	}
	var memory, iterations uint32
	var threads uint8
	if _, err := fmt.Sscanf(parts[3], "m=%d,t=%d,p=%d", &memory, &iterations, &threads); err != nil ||
		memory == 0 || iterations == 0 || threads == 0 {
		return false, ErrInvalidHash
	}
	salt, err := base64.RawStdEncoding.DecodeString(parts[4])
	if err != nil || len(salt) == 0 {
		return false, ErrInvalidHash
	}
	want, err := base64.RawStdEncoding.DecodeString(parts[5])
	if err != nil || len(want) == 0 {
		return false, ErrInvalidHash
	}
	got := argon2.IDKey([]byte(password), salt, iterations, memory, threads, uint32(len(want)))
	return subtle.ConstantTimeCompare(got, want) == 1, nil
}

// NormalizeEmail trims and lower-cases an address; that form is what is stored and compared.
func NormalizeEmail(email string) string {
	return strings.ToLower(strings.TrimSpace(email))
}

// ValidateEmail accepts a plain address (no display name) after normalizing it.
func ValidateEmail(email string) error {
	email = NormalizeEmail(email)
	parsed, err := mail.ParseAddress(email)
	if err != nil || parsed.Address != email || !strings.Contains(email[strings.LastIndex(email, "@"):], ".") {
		return ErrInvalidEmail
	}
	return nil
}

// ValidatePassword enforces the minimum length (PASSWORD_MIN_LENGTH) and the hash input cap.
func ValidatePassword(password string, minLength int) error {
	if len(password) < minLength {
		return ErrPasswordTooShort
	}
	if len(password) > MaxPasswordBytes {
		return ErrPasswordTooLong
	}
	return nil
}
