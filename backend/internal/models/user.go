package models

import (
	"time"

	"github.com/google/uuid"
)

// User roles.
const (
	RoleAdmin = "admin"
	RoleUser  = "user"
)

// User is a person who can log in. Email is the identity (stored trimmed and lower-cased)
// so a one-time-code login can be added later without a schema change.
type User struct {
	ID uuid.UUID `gorm:"type:uuid;default:gen_random_uuid();primaryKey" json:"id"`

	Email        string `gorm:"uniqueIndex;not null" json:"email"`
	PasswordHash string `gorm:"not null" json:"-"`
	Role         string `gorm:"not null;default:user" json:"role"`
	// Disabled users cannot log in and their sessions stop working; their data is kept.
	Disabled bool `gorm:"not null;default:false" json:"disabled"`

	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`
}

// IsAdmin reports whether the user has the admin role.
func (u User) IsAdmin() bool { return u.Role == RoleAdmin }
