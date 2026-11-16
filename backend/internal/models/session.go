package models

import (
	"time"

	"github.com/google/uuid"
)

// Session is one login. Only a hash of the random cookie token is stored, so a leaked
// database cannot be replayed as cookies. Deleting the row revokes the login.
type Session struct {
	ID uuid.UUID `gorm:"type:uuid;default:gen_random_uuid();primaryKey"`

	UserID uuid.UUID `gorm:"type:uuid;not null;index"`
	User   *User     `gorm:"foreignKey:UserID;constraint:OnDelete:CASCADE"`

	TokenHash string `gorm:"uniqueIndex;not null"`

	ExpiresAt  time.Time `gorm:"not null;index"`
	CreatedAt  time.Time
	LastSeenAt time.Time
}
