package services

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"github.com/reihanrere/qamail-manager/internal/mailer"
	"github.com/reihanrere/qamail-manager/internal/models"
	"github.com/reihanrere/qamail-manager/internal/providers"
)

// isolationFixture: alice and bob each own a local account; bob's has one stored message
// with an attachment, and bob also owns a Mail.tm account.
type isolationFixture struct {
	s                  *AccountService
	db                 *gorm.DB
	alice, bob         uuid.UUID
	aliceAcc, bobAcc   *models.MailAccount
	bobRemote          *models.MailAccount
	bobMessage, bobAtt string
}

func newIsolationFixture(t *testing.T) *isolationFixture {
	t.Helper()
	db := testDB(t)
	s := newIntegrationService(t, db)
	// A configured SMTP server (never contacted: every send here is refused first)
	s.SetMailer(mailer.New(mailer.Config{Host: "127.0.0.1", Port: 1, Security: mailer.SecurityNone}))
	ctx := context.Background()

	f := &isolationFixture{s: s, db: db}
	for _, u := range []*uuid.UUID{&f.alice, &f.bob} {
		user := models.User{Email: uuid.NewString() + "@example.com", PasswordHash: "x", Role: models.RoleUser}
		if err := db.Create(&user).Error; err != nil {
			t.Fatal(err)
		}
		*u = user.ID
	}
	var err error
	if f.aliceAcc, err = s.GenerateAccount(ctx, f.alice, GenerateAccountInput{Tag: "alice-tag"}); err != nil {
		t.Fatal(err)
	}
	if f.bobAcc, err = s.GenerateAccount(ctx, f.bob, GenerateAccountInput{Tag: "bob-tag", Note: "bob-note"}); err != nil {
		t.Fatal(err)
	}
	if f.bobRemote, err = s.GenerateAccount(ctx, f.bob, GenerateAccountInput{Provider: providers.NameMailTM}); err != nil {
		t.Fatal(err)
	}
	raw := crlfBytes(strings.ReplaceAll(integrationEmail, "{{TO}}", f.bobAcc.Email))
	result, err := s.IngestMessage(ctx, raw, f.bobAcc.Email)
	if err != nil {
		t.Fatal(err)
	}
	f.bobMessage = result.MessageID
	detail, err := s.GetMessageDetail(ctx, f.bob, f.bobAcc.ID, f.bobMessage)
	if err != nil || len(detail.Attachments) == 0 {
		t.Fatalf("bob cannot read his own message: %v", err)
	}
	f.bobAtt = detail.Attachments[0].ID
	return f
}

// Every per-account operation by alice on bob's account must answer ErrAccountNotFound
// (404), exactly like an id that does not exist, and change nothing.
func TestIsolationEveryAccountOperation(t *testing.T) {
	f := newIsolationFixture(t)
	ctx := context.Background()
	tag := "hijacked"
	send := SendMessageInput{To: []string{"x@example.com"}, Subject: "s", Text: "t"}

	for _, target := range []*models.MailAccount{f.bobAcc, f.bobRemote} {
		id := target.ID
		ops := map[string]func() error{
			"get":        func() error { _, err := f.s.GetAccount(ctx, f.alice, id); return err },
			"update":     func() error { _, err := f.s.UpdateAccount(ctx, f.alice, id, UpdateAccountInput{Tag: &tag}); return err },
			"status":     func() error { return f.s.UpdateStatus(ctx, f.alice, id, models.StatusBlocked) },
			"replace":    func() error { _, err := f.s.ReplaceAccount(ctx, f.alice, id); return err },
			"inbox":      func() error { _, err := f.s.GetInbox(ctx, f.alice, id, 1, ""); return err },
			"search":     func() error { _, err := f.s.GetInbox(ctx, f.alice, id, 1, "invoice"); return err },
			"detail":     func() error { _, err := f.s.GetMessageDetail(ctx, f.alice, id, f.bobMessage); return err },
			"source":     func() error { _, err := f.s.GetMessageSource(ctx, f.alice, id, f.bobMessage); return err },
			"attachment": func() error { _, err := f.s.GetAttachment(ctx, f.alice, id, f.bobMessage, f.bobAtt); return err },
			"mark read":  func() error { return f.s.MarkMessageRead(ctx, f.alice, id, f.bobMessage) },
			"send":       func() error { _, err := f.s.SendMessage(ctx, f.alice, id, send); return err },
			"reply": func() error {
				_, err := f.s.SendMessage(ctx, f.alice, id, SendMessageInput{To: send.To, Text: "t", ReplyTo: f.bobMessage})
				return err
			},
			"delete msg": func() error { return f.s.DeleteMessage(ctx, f.alice, id, f.bobMessage) },
			"delete":     func() error { return f.s.DeleteAccount(ctx, f.alice, id) },
		}
		for name, op := range ops {
			if err := op(); !errors.Is(err, ErrAccountNotFound) {
				t.Errorf("%s on bob's %s account: err = %v, want ErrAccountNotFound", name, target.Provider, err)
			}
		}
	}

	// Nothing changed for bob
	acc, err := f.s.GetAccount(ctx, f.bob, f.bobAcc.ID)
	if err != nil || acc.Tag != "bob-tag" || acc.Status != models.StatusAvailable {
		t.Fatalf("bob's account changed: %+v %v", acc, err)
	}
	if _, err := f.s.GetMessageDetail(ctx, f.bob, f.bobAcc.ID, f.bobMessage); err != nil {
		t.Fatalf("bob's message is gone: %v", err)
	}
	var aliceAccounts int64
	f.db.Model(&models.MailAccount{}).Where("user_id = ?", f.alice).Count(&aliceAccounts)
	if aliceAccounts != 1 {
		t.Fatalf("alice now owns %d accounts; a refused replace must not create one", aliceAccounts)
	}
}

// A message id of bob's, asked for through alice's own account, is not found either.
func TestIsolationMessageIDThroughOwnAccount(t *testing.T) {
	f := newIsolationFixture(t)
	ctx := context.Background()
	if _, err := f.s.GetMessageDetail(ctx, f.alice, f.aliceAcc.ID, f.bobMessage); !errors.Is(err, providers.ErrMessageNotFound) {
		t.Errorf("detail: %v", err)
	}
	if _, err := f.s.GetMessageSource(ctx, f.alice, f.aliceAcc.ID, f.bobMessage); !errors.Is(err, providers.ErrMessageNotFound) {
		t.Errorf("source: %v", err)
	}
	if _, err := f.s.GetAttachment(ctx, f.alice, f.aliceAcc.ID, f.bobMessage, f.bobAtt); !errors.Is(err, providers.ErrMessageNotFound) {
		t.Errorf("attachment: %v", err)
	}
	if err := f.s.DeleteMessage(ctx, f.alice, f.aliceAcc.ID, f.bobMessage); !errors.Is(err, providers.ErrMessageNotFound) {
		t.Errorf("delete: %v", err)
	}
	// Searching her own inbox for words in bob's mail finds nothing (the OR in the search
	// must not widen the account filter)
	for _, term := range []string{"invoice", "482913", "shop@example.com", "Shop Bot"} {
		found, err := f.s.GetInbox(ctx, f.alice, f.aliceAcc.ID, 1, term)
		if err != nil || len(found.Messages) != 0 {
			t.Errorf("search %q in alice's inbox: %+v %v", term, found, err)
		}
	}
	reply := SendMessageInput{To: []string{"x@example.com"}, Text: "t", ReplyTo: f.bobMessage}
	if _, err := f.s.SendMessage(ctx, f.alice, f.aliceAcc.ID, reply); !errors.Is(err, providers.ErrMessageNotFound) {
		t.Errorf("reply: %v", err)
	}
}

func TestIsolationListStatsAndExport(t *testing.T) {
	f := newIsolationFixture(t)
	ctx := context.Background()

	list, meta, err := f.s.ListAccounts(ctx, f.alice, ListAccountsParams{})
	if err != nil || meta.Total != 1 || len(list) != 1 || list[0].ID != f.aliceAcc.ID {
		t.Fatalf("alice list: %+v %+v %v", list, meta, err)
	}
	// Search terms that match bob's rows must not widen the owner filter (OR precedence)
	for _, term := range []string{f.bobAcc.Email, "bob-tag", "bob-note", f.bobAcc.Domain} {
		list, _, err := f.s.ListAccounts(ctx, f.alice, ListAccountsParams{Search: term})
		if err != nil {
			t.Fatal(err)
		}
		for _, a := range list {
			if a.UserID != f.alice {
				t.Fatalf("search %q returned bob's account %s", term, a.Email)
			}
		}
	}
	for _, params := range []ListAccountsParams{{Provider: providers.NameMailTM}, {Legacy: true}, {Status: models.StatusAvailable}} {
		list, _, _ := f.s.ListAccounts(ctx, f.alice, params)
		for _, a := range list {
			if a.UserID != f.alice {
				t.Fatalf("filter %+v returned bob's account", params)
			}
		}
	}

	stats, err := f.s.Stats(ctx, f.alice)
	if err != nil {
		t.Fatal(err)
	}
	if stats.Total != 1 || stats.ByStatus[models.StatusAvailable] != 1 {
		t.Errorf("alice stats count bob's accounts: %+v", stats)
	}
	for _, p := range stats.ByProvider {
		if p.Provider == providers.NameMailTM || p.Messages != 0 {
			t.Errorf("alice provider stats include bob: %+v", stats.ByProvider)
		}
	}
	bobStats, _ := f.s.Stats(ctx, f.bob)
	if bobStats.Total != 2 {
		t.Errorf("bob stats total = %d, want 2", bobStats.Total)
	}

	var buf bytes.Buffer
	if err := f.s.ExportAccountsCSV(ctx, f.alice, ListAccountsParams{}, &buf); err != nil {
		t.Fatal(err)
	}
	if out := buf.String(); strings.Contains(out, f.bobAcc.Email) || strings.Contains(out, f.bobRemote.Email) || !strings.Contains(out, f.aliceAcc.Email) {
		t.Fatalf("alice export:\n%s", out)
	}
}

func TestIsolationEventsReachOnlyTheOwner(t *testing.T) {
	f := newIsolationFixture(t)
	ctx := context.Background()
	aliceEvents, stopA := f.s.Events().Subscribe(f.alice)
	defer stopA()
	bobEvents, stopB := f.s.Events().Subscribe(f.bob)
	defer stopB()

	raw := crlfBytes(strings.ReplaceAll(strings.ReplaceAll(integrationEmail, "{{TO}}", f.bobAcc.Email), "invoice-1@", "invoice-2@"))
	if _, err := f.s.IngestMessage(ctx, raw, f.bobAcc.Email); err != nil {
		t.Fatal(err)
	}
	if err := f.s.UpdateStatus(ctx, f.bob, f.bobAcc.ID, models.StatusUsed); err != nil {
		t.Fatal(err)
	}
	if _, err := f.s.GenerateAccount(ctx, f.bob, GenerateAccountInput{}); err != nil {
		t.Fatal(err)
	}
	if len(aliceEvents) != 0 {
		t.Fatalf("alice received bob's event: %+v", <-aliceEvents)
	}
	if len(bobEvents) < 3 {
		t.Fatalf("bob received %d events, want at least 3", len(bobEvents))
	}
	first := <-bobEvents
	if first.Type != EventMessageCreated || first.AccountEmail != f.bobAcc.Email {
		t.Fatalf("bob's first event: %+v", first)
	}
}

func TestIsolationIngestLandsWithTheOwnerOnly(t *testing.T) {
	f := newIsolationFixture(t)
	ctx := context.Background()

	bobInbox, err := f.s.GetInbox(ctx, f.bob, f.bobAcc.ID, 1, "")
	if err != nil || bobInbox.Meta.Total != 1 {
		t.Fatalf("bob inbox: %+v %v", bobInbox, err)
	}
	aliceInbox, err := f.s.GetInbox(ctx, f.alice, f.aliceAcc.ID, 1, "")
	if err != nil || aliceInbox.Meta.Total != 0 {
		t.Fatalf("alice inbox: %+v %v", aliceInbox, err)
	}
	// Ingest still works for alice's address too
	raw := crlfBytes(strings.ReplaceAll(integrationEmail, "{{TO}}", f.aliceAcc.Email))
	if _, err := f.s.IngestMessage(ctx, raw, f.aliceAcc.Email); err != nil {
		t.Fatal(err)
	}
	if inbox, _ := f.s.GetInbox(ctx, f.alice, f.aliceAcc.ID, 1, ""); inbox.Meta.Total != 1 {
		t.Fatalf("alice inbox after ingest: %+v", inbox.Meta)
	}
	if inbox, _ := f.s.GetInbox(ctx, f.bob, f.bobAcc.ID, 1, ""); inbox.Meta.Total != 1 {
		t.Fatalf("bob inbox grew from alice's mail: %+v", inbox.Meta)
	}
}

// Addresses stay unique across users: the duplicate check is global on purpose.
func TestIsolationEmailUniqueAcrossUsers(t *testing.T) {
	f := newIsolationFixture(t)
	dup := models.MailAccount{UserID: f.alice, Provider: providers.NameLocal, AccountID: "dup-" + uuid.NewString(),
		Email: f.bobAcc.Email, Password: "x", Domain: f.bobAcc.Domain}
	if err := f.db.Create(&dup).Error; err == nil || !isUniqueViolation(err) {
		t.Fatalf("second account with bob's address: err = %v, want unique violation", err)
	}
}

// A request that reached the service without a user is refused, not run unscoped.
func TestNoOwnerIsRefused(t *testing.T) {
	f := newIsolationFixture(t)
	ctx := context.Background()
	if _, _, err := f.s.ListAccounts(ctx, uuid.Nil, ListAccountsParams{}); !errors.Is(err, errNoOwner) {
		t.Errorf("list: %v", err)
	}
	if _, err := f.s.Stats(ctx, uuid.Nil); !errors.Is(err, errNoOwner) {
		t.Errorf("stats: %v", err)
	}
	if _, err := f.s.GetAccount(ctx, uuid.Nil, f.bobAcc.ID); !errors.Is(err, errNoOwner) {
		t.Errorf("get: %v", err)
	}
	if _, err := f.s.GenerateAccount(ctx, uuid.Nil, GenerateAccountInput{}); !errors.Is(err, errNoOwner) {
		t.Errorf("generate: %v", err)
	}
	if err := f.s.DeleteAccount(ctx, uuid.Nil, f.bobAcc.ID); !errors.Is(err, errNoOwner) {
		t.Errorf("delete: %v", err)
	}
	if _, err := f.s.GetAccount(ctx, f.bob, f.bobAcc.ID); err != nil {
		t.Fatalf("bob's account must survive: %v", err)
	}
}
