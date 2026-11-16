package services

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"github.com/reihanrere/qamail-manager/internal/models"
)

// ErrForbidden is returned when the user's role does not allow the action; handlers answer 403.
var ErrForbidden = errors.New("admin access required")

// Quota is how many accounts a user holds against the limit. BLOCKED accounts are not
// counted, so replacing an account never needs spare room. Limit is nil for admins, who
// have no limit.
type Quota struct {
	Used  int64  `json:"used"`
	Limit *int   `json:"limit"`
	Left  *int64 `json:"remaining"`
}

// isAdmin reports whether userID is an enabled admin. An unknown user is not an admin.
func (s *AccountService) isAdmin(ctx context.Context, userID uuid.UUID) (bool, error) {
	var user models.User
	err := s.db.WithContext(ctx).Select("role").Where("id = ?", userID).First(&user).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("failed to load user: %w", err)
	}
	return user.IsAdmin(), nil
}

// countedAccounts counts userID's accounts that use up quota.
func (s *AccountService) countedAccounts(ctx context.Context, userID uuid.UUID) (int64, error) {
	var used int64
	err := s.owned(ctx, userID).Where("status <> ?", models.StatusBlocked).Count(&used).Error
	if err != nil {
		return 0, fmt.Errorf("failed to count accounts: %w", err)
	}
	return used, nil
}

// Quota reports userID's quota usage.
func (s *AccountService) Quota(ctx context.Context, userID uuid.UUID) (Quota, error) {
	used, err := s.countedAccounts(ctx, userID)
	if err != nil {
		return Quota{}, err
	}
	admin, err := s.isAdmin(ctx, userID)
	if err != nil || admin {
		return Quota{Used: used}, err
	}
	limit := s.current().UserAccountQuota
	left := max(int64(limit)-used, 0)
	return Quota{Used: used, Limit: &limit, Left: &left}, nil
}

// checkQuota rejects the request when userID cannot add want more accounts. The whole
// request is refused, never a part of it, and the message names what is left.
func (s *AccountService) checkQuota(ctx context.Context, userID uuid.UUID, want int) error {
	quota, err := s.Quota(ctx, userID)
	if err != nil || quota.Limit == nil {
		return err
	}
	if int64(want) > *quota.Left {
		return invalid("quota_exceeded", map[string]any{"limit": *quota.Limit, "used": quota.Used, "remaining": *quota.Left, "requested": want},
			"account quota exceeded: %d of %d used, %d left, %d requested", quota.Used, *quota.Limit, *quota.Left, want)
	}
	return nil
}
