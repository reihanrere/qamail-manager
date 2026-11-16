package cli

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/reihanrere/qamail-manager/internal/auth"
	"github.com/reihanrere/qamail-manager/internal/models"
	"github.com/reihanrere/qamail-manager/internal/services"
)

type fakeUsers struct {
	calls    []string
	err      error
	accounts int64
}

func (f *fakeUsers) Create(_ context.Context, email, password, role string) (*models.User, error) {
	f.calls = append(f.calls, "create "+email+" "+password+" "+role)
	return &models.User{Email: email, Role: role}, f.err
}
func (f *fakeUsers) List(context.Context) ([]services.UserSummary, error) {
	return []services.UserSummary{
		{User: models.User{ID: uuid.New(), Email: "a@example.com", Role: "admin", CreatedAt: time.Date(2026, 1, 2, 0, 0, 0, 0, time.UTC)}, Accounts: 3},
		{User: models.User{ID: uuid.New(), Email: "b@example.com", Role: "user", Disabled: true}, Accounts: 0},
	}, f.err
}
func (f *fakeUsers) SetPassword(_ context.Context, email, password string) error {
	f.calls = append(f.calls, "passwd "+email+" "+password)
	return f.err
}
func (f *fakeUsers) SetDisabled(_ context.Context, email string, disabled bool) error {
	f.calls = append(f.calls, "disabled "+email+" "+map[bool]string{true: "true", false: "false"}[disabled])
	return f.err
}
func (f *fakeUsers) Delete(_ context.Context, email string, force bool) (int64, error) {
	f.calls = append(f.calls, "delete "+email+" "+map[bool]string{true: "force", false: "plain"}[force])
	return f.accounts, f.err
}

func run(t *testing.T, users *fakeUsers, args ...string) (string, error) {
	t.Helper()
	var out bytes.Buffer
	pw := func(string, bool) (string, error) { return "typed-password-1", nil }
	err := RunUser(context.Background(), args, users, pw, &out)
	return out.String(), err
}

func TestAdd(t *testing.T) {
	u := &fakeUsers{}
	out, err := run(t, u, "add", "x@example.com")
	if err != nil || !strings.Contains(out, "created user x@example.com") || u.calls[0] != "create x@example.com typed-password-1 user" {
		t.Fatalf("out=%q err=%v calls=%v", out, err, u.calls)
	}
	u = &fakeUsers{}
	// The flag may come before or after the email
	for _, args := range [][]string{{"add", "--admin", "x@example.com"}, {"add", "x@example.com", "--admin"}} {
		if _, err := run(t, u, args...); err != nil {
			t.Fatal(err)
		}
	}
	for _, call := range u.calls {
		if !strings.HasSuffix(call, " admin") {
			t.Fatalf("call %q is not an admin", call)
		}
	}
}

func TestPasswordIsNeverAnArgument(t *testing.T) {
	u := &fakeUsers{}
	_, err := run(t, u, "add", "x@example.com", "secret-from-argv")
	if err == nil || len(u.calls) != 0 {
		t.Fatalf("extra argument accepted: err=%v calls=%v", err, u.calls)
	}
}

func TestOtherCommands(t *testing.T) {
	u := &fakeUsers{accounts: 2}
	for _, tc := range []struct {
		args []string
		call string
		out  string
	}{
		{[]string{"passwd", "A@Example.com"}, "passwd A@Example.com typed-password-1", "password changed for a@example.com"},
		{[]string{"disable", "x@example.com"}, "disabled x@example.com true", "disabled x@example.com"},
		{[]string{"enable", "x@example.com"}, "disabled x@example.com false", "enabled x@example.com"},
		{[]string{"delete", "x@example.com"}, "delete x@example.com plain", "deleted x@example.com"},
		{[]string{"delete", "x@example.com", "--force"}, "delete x@example.com force", "2 mail account(s)"},
	} {
		u.calls = nil
		out, err := run(t, u, tc.args...)
		if err != nil || len(u.calls) != 1 || u.calls[0] != tc.call || !strings.Contains(out, tc.out) {
			t.Errorf("%v: out=%q err=%v calls=%v", tc.args, out, err, u.calls)
		}
	}
}

func TestList(t *testing.T) {
	out, err := run(t, &fakeUsers{}, "list")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"EMAIL", "a@example.com", "admin", "enabled", "3", "2026-01-02", "b@example.com", "disabled"} {
		if !strings.Contains(out, want) {
			t.Errorf("list output missing %q:\n%s", want, out)
		}
	}
}

func TestFriendlyErrors(t *testing.T) {
	for err, want := range map[error]string{
		services.ErrUserExists:     "already exists",
		services.ErrUserNotFound:   "no user",
		services.ErrUserHasAccount: "--force",
		services.ErrLastAdmin:      "only enabled admin",
		auth.ErrPasswordTooShort:   "too short",
		auth.ErrInvalidEmail:       "valid email",
	} {
		_, got := run(t, &fakeUsers{err: err}, "disable", "x@example.com")
		if got == nil || !strings.Contains(got.Error(), want) {
			t.Errorf("%v -> %v, want mention of %q", err, got, want)
		}
	}
	if _, got := run(t, &fakeUsers{err: errors.New("boom")}, "enable", "x@example.com"); got == nil || got.Error() != "boom" {
		t.Errorf("unknown errors must pass through, got %v", got)
	}
}

func TestUsageErrors(t *testing.T) {
	for _, args := range [][]string{
		nil, {"nope"}, {"add"}, {"add", "--bogus", "x@example.com"}, {"list", "x"}, {"disable", "x@example.com", "--force"},
		{"delete", "a@example.com", "b@example.com"},
	} {
		u := &fakeUsers{}
		if _, err := run(t, u, args...); err == nil {
			t.Errorf("%v: expected an error", args)
		}
		if len(u.calls) != 0 {
			t.Errorf("%v: reached the service: %v", args, u.calls)
		}
	}
}
