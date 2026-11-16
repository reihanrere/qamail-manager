package services

import (
	"context"
	"testing"

	"github.com/google/uuid"

	"github.com/reihanrere/qamail-manager/internal/models"
	"github.com/reihanrere/qamail-manager/internal/providers"
)

// A deleted local address must never be issued again, to anyone: whoever holds it next
// would receive the mail meant for its former owner.
func TestDeletedLocalAddressIsNotIssuedAgain(t *testing.T) {
	db := testDB(t)
	s := newIntegrationService(t, db)
	ctx := context.Background()

	var alice, bob models.User
	for _, u := range []*models.User{&alice, &bob} {
		*u = models.User{Email: uuid.NewString() + "@example.com", PasswordHash: "x", Role: models.RoleUser}
		if err := db.Create(u).Error; err != nil {
			t.Fatal(err)
		}
	}

	// A pool of one name leaves 360 possible addresses, so reuse would show up fast (every address bob frees is
	// retired too, so the loop stays short)
	settings := s.current()
	settings.FirstNames, settings.LastNames, settings.UsernameMaxAttempts = []string{"x"}, []string{"y"}, 200
	s.setEffective(settings)
	former := "x.y@inbox.example.com"
	account := models.MailAccount{UserID: alice.ID, Provider: providers.NameLocal, AccountID: uuid.NewString(),
		Email: former, Password: "p", Domain: "inbox.example.com", Status: models.StatusAvailable}
	if err := db.Create(&account).Error; err != nil {
		t.Fatal(err)
	}
	if err := s.DeleteAccount(ctx, alice.ID, account.ID); err != nil {
		t.Fatal(err)
	}

	for i := 0; i < 150; i++ {
		got, err := s.GenerateAccount(ctx, bob.ID, GenerateAccountInput{})
		if err != nil {
			t.Fatal(err)
		}
		if got.Email == former {
			t.Fatalf("iteration %d: %s was issued to bob after alice deleted it", i, former)
		}
		if err := s.DeleteAccount(ctx, bob.ID, got.ID); err != nil {
			t.Fatal(err)
		}
	}
}

// Every path that removes an account retires its address: the owner's delete (above), the
// cleanup job and 'user delete --force' all reach the database trigger.
func TestAnyAccountDeletionRetiresTheAddress(t *testing.T) {
	db := testDB(t)
	owner := models.User{Email: uuid.NewString() + "@example.com", PasswordHash: "x", Role: models.RoleUser}
	if err := db.Create(&owner).Error; err != nil {
		t.Fatal(err)
	}
	account := models.MailAccount{UserID: owner.ID, Provider: providers.NameLocal, AccountID: uuid.NewString(),
		Email: "gone@inbox.example.com", Password: "p", Domain: "inbox.example.com", Status: models.StatusAvailable}
	if err := db.Create(&account).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Where("user_id = ?", owner.ID).Delete(&models.MailAccount{}).Error; err != nil {
		t.Fatal(err)
	}
	var count int64
	db.Model(&models.RetiredAddress{}).Where("email = ?", "gone@inbox.example.com").Count(&count)
	if count != 1 {
		t.Fatalf("address not retired after a bulk delete, rows=%d", count)
	}
}
