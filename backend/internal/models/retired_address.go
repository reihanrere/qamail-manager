package models

import "time"

// RetiredAddress is an address that once belonged to a mail account that no longer exists.
// A database trigger fills it whenever a mail_accounts row is deleted, by any path, and
// address generation refuses everything listed here. Without it a deleted address could be
// issued to another user, who would then receive the mail meant for its former owner.
type RetiredAddress struct {
	Email     string    `gorm:"primaryKey" json:"-"`
	RetiredAt time.Time `gorm:"not null;default:now()" json:"-"`
}
