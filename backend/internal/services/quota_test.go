package services

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/reihanrere/qamail-manager/internal/models"
)

func newQuotaFixture(t *testing.T, quota int) (s *AccountService, user uuid.UUID) {
	t.Helper()
	db := testDB(t)
	s = newIntegrationService(t, db)
	row := models.User{Email: uuid.NewString() + "@example.com", PasswordHash: "x", Role: models.RoleUser}
	if err := db.Create(&row).Error; err != nil {
		t.Fatal(err)
	}
	setQuota(t, s, quota)
	return s, row.ID
}

func setQuota(t *testing.T, s *AccountService, quota int) {
	t.Helper()
	raw, _ := json.Marshal(quota)
	patch := SettingsPatch{Values: map[string]json.RawMessage{"userAccountQuota": raw}}
	if err := s.UpdateSettings(context.Background(), patch); err != nil {
		t.Fatal(err)
	}
}

func isQuota(err error) bool {
	v, ok := AsValidationError(err)
	return ok && v.Code == "quota_exceeded"
}

func TestIntegrationQuotaStopsAtTheLimit(t *testing.T) {
	ctx := context.Background()
	s, user := newQuotaFixture(t, 3)
	for i := 0; i < 3; i++ {
		if _, err := s.GenerateAccount(ctx, user, GenerateAccountInput{}); err != nil {
			t.Fatalf("account %d within quota: %v", i+1, err)
		}
	}
	if _, err := s.GenerateAccount(ctx, user, GenerateAccountInput{}); !isQuota(err) {
		t.Fatalf("fourth account: want quota_exceeded, got %v", err)
	}
	quota, err := s.Quota(ctx, user)
	if err != nil || quota.Used != 3 || *quota.Limit != 3 || *quota.Left != 0 {
		t.Fatalf("quota = %+v, %v", quota, err)
	}
}

func TestIntegrationQuotaRejectsWholeBulkAndNamesWhatIsLeft(t *testing.T) {
	ctx := context.Background()
	s, user := newQuotaFixture(t, 3)
	if _, err := s.GenerateAccount(ctx, user, GenerateAccountInput{}); err != nil {
		t.Fatal(err)
	}
	_, err := s.GenerateAccounts(ctx, user, GenerateAccountInput{}, 3)
	if !isQuota(err) || !strings.Contains(err.Error(), "2 left") {
		t.Fatalf("bulk of 3 with 2 left: got %v", err)
	}
	if quota, _ := s.Quota(ctx, user); quota.Used != 1 {
		t.Fatalf("a rejected bulk created accounts: used = %d", quota.Used)
	}
	result, err := s.GenerateAccounts(ctx, user, GenerateAccountInput{}, 2)
	if err != nil || len(result.Accounts) != 2 {
		t.Fatalf("bulk that fits exactly: %v %+v", err, result)
	}
}

func TestIntegrationQuotaReplaceAndBlocked(t *testing.T) {
	ctx := context.Background()
	s, user := newQuotaFixture(t, 2)
	first, _ := s.GenerateAccount(ctx, user, GenerateAccountInput{})
	if _, err := s.GenerateAccount(ctx, user, GenerateAccountInput{}); err != nil {
		t.Fatal(err)
	}

	// Full, yet replace works: the old account is blocked and frees its slot
	replaced, err := s.ReplaceAccount(ctx, user, first.ID)
	if err != nil {
		t.Fatalf("replace at full quota: %v", err)
	}
	if quota, _ := s.Quota(ctx, user); quota.Used != 2 {
		t.Fatalf("after replace used = %d, want 2 (BLOCKED is not counted)", quota.Used)
	}
	// Replacing the already BLOCKED account would add a counted one
	if _, err := s.ReplaceAccount(ctx, user, first.ID); !isQuota(err) {
		t.Fatalf("replace of a BLOCKED account at full quota: got %v", err)
	}
	// Blocking an account frees a slot
	if err := s.UpdateStatus(ctx, user, replaced.Account.ID, models.StatusBlocked); err != nil {
		t.Fatal(err)
	}
	if _, err := s.GenerateAccount(ctx, user, GenerateAccountInput{}); err != nil {
		t.Fatalf("generate after blocking one: %v", err)
	}
}

func TestIntegrationQuotaAdminUnlimitedAndLiveSetting(t *testing.T) {
	ctx := context.Background()
	s, user := newQuotaFixture(t, 1)
	// testOwnerID is an admin: never limited
	for i := 0; i < 3; i++ {
		if _, err := s.GenerateAccount(ctx, testOwnerID, GenerateAccountInput{}); err != nil {
			t.Fatalf("admin account %d: %v", i+1, err)
		}
	}
	if quota, _ := s.Quota(ctx, testOwnerID); quota.Limit != nil || quota.Left != nil {
		t.Fatalf("admin quota has a limit: %+v", quota)
	}

	if _, err := s.GenerateAccount(ctx, user, GenerateAccountInput{}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.GenerateAccount(ctx, user, GenerateAccountInput{}); !isQuota(err) {
		t.Fatalf("want quota_exceeded, got %v", err)
	}
	// Raised from Settings: effective at once, no restart
	setQuota(t, s, 2)
	if _, err := s.GenerateAccount(ctx, user, GenerateAccountInput{}); err != nil {
		t.Fatalf("after raising the quota: %v", err)
	}
	// Reset returns to the environment default (10)
	if err := s.UpdateSettings(ctx, SettingsPatch{Reset: []string{"userAccountQuota"}}); err != nil {
		t.Fatal(err)
	}
	if quota, _ := s.Quota(ctx, user); *quota.Limit != 10 {
		t.Fatalf("after reset limit = %d, want 10", *quota.Limit)
	}
}

func TestQuotaSettingMustBePositive(t *testing.T) {
	s, _ := NewAccountService(nil, testSettings("mailtm"), stubProvider{"mailtm"})
	bad := testSettings("mailtm")
	bad.UserAccountQuota = 0
	if err := s.validateSettings(bad); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("quota 0: want ErrInvalidInput, got %v", err)
	}
}
