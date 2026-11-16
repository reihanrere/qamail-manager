package services

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"gorm.io/gorm"

	"github.com/reihanrere/qamail-manager/internal/models"
)

const testPassword = "correct-horse-battery"

func newAuthFixture(t *testing.T) (*AuthService, *UserService, *gorm.DB) {
	t.Helper()
	db := testDB(t)
	svc := NewAuthService(db, AuthSettings{SessionTTL: time.Hour, LoginMaxAttempts: 3, LoginWindow: time.Minute})
	users := NewUserService(db, 10)
	if _, err := users.Create(context.Background(), "alice@example.com", testPassword, models.RoleUser); err != nil {
		t.Fatal(err)
	}
	return svc, users, db
}

func TestLogin(t *testing.T) {
	svc, _, _ := newAuthFixture(t)
	ctx := context.Background()

	res, err := svc.Login(ctx, "  Alice@Example.COM ", testPassword, "1.1.1.1")
	if err != nil {
		t.Fatal(err)
	}
	if res.Token == "" || res.User.Email != "alice@example.com" || !res.ExpiresAt.After(time.Now()) {
		t.Fatalf("unexpected result: %+v", res)
	}

	for name, tc := range map[string]struct{ email, password string }{
		"wrong password": {"alice@example.com", "wrong-password-here"},
		"unknown email":  {"nobody@example.com", testPassword},
	} {
		if _, err := svc.Login(ctx, tc.email, tc.password, "2.2.2.2"); !errors.Is(err, ErrInvalidCredentials) {
			t.Errorf("%s: err = %v, want ErrInvalidCredentials", name, err)
		}
	}
}

func TestLoginStoresOnlyTheTokenHash(t *testing.T) {
	svc, _, db := newAuthFixture(t)
	res, err := svc.Login(context.Background(), "alice@example.com", testPassword, "1.1.1.1")
	if err != nil {
		t.Fatal(err)
	}
	var stored models.Session
	if err := db.First(&stored).Error; err != nil {
		t.Fatal(err)
	}
	if stored.TokenHash == res.Token || stored.TokenHash != hashToken(res.Token) {
		t.Fatal("session must store the hash of the token, not the token")
	}
}

func TestLoginDisabledUserLooksLikeWrongPassword(t *testing.T) {
	svc, users, _ := newAuthFixture(t)
	ctx := context.Background()
	if _, err := users.Create(ctx, "admin@example.com", testPassword, models.RoleAdmin); err != nil {
		t.Fatal(err)
	}
	if err := users.SetDisabled(ctx, "alice@example.com", true); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Login(ctx, "alice@example.com", testPassword, "1.1.1.1"); !errors.Is(err, ErrInvalidCredentials) {
		t.Fatalf("err = %v, want ErrInvalidCredentials", err)
	}
}

func TestLoginRateLimit(t *testing.T) {
	svc, _, _ := newAuthFixture(t)
	ctx := context.Background()

	for i := 0; i < 3; i++ {
		if _, err := svc.Login(ctx, "alice@example.com", "wrong-password-here", "9.9.9.9"); !errors.Is(err, ErrInvalidCredentials) {
			t.Fatalf("attempt %d: %v", i, err)
		}
	}
	// Locked: even the right password is refused
	_, err := svc.Login(ctx, "alice@example.com", testPassword, "9.9.9.9")
	var tooMany *TooManyAttemptsError
	if !errors.As(err, &tooMany) || tooMany.RetryAfter <= 0 {
		t.Fatalf("err = %v, want TooManyAttemptsError", err)
	}
	// Another client IP has its own budget
	if _, err := svc.Login(ctx, "alice@example.com", testPassword, "8.8.8.8"); err != nil {
		t.Fatalf("other IP: %v", err)
	}
}

func TestLoginSuccessClearsFailures(t *testing.T) {
	svc, _, _ := newAuthFixture(t)
	ctx := context.Background()
	for i := 0; i < 2; i++ {
		_, _ = svc.Login(ctx, "alice@example.com", "wrong-password-here", "7.7.7.7")
	}
	if _, err := svc.Login(ctx, "alice@example.com", testPassword, "7.7.7.7"); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		if _, err := svc.Login(ctx, "alice@example.com", "wrong-password-here", "7.7.7.7"); !errors.Is(err, ErrInvalidCredentials) {
			t.Fatalf("failure counter was not reset: %v", err)
		}
	}
}

func TestAuthenticate(t *testing.T) {
	svc, _, _ := newAuthFixture(t)
	ctx := context.Background()
	res, err := svc.Login(ctx, "alice@example.com", testPassword, "1.1.1.1")
	if err != nil {
		t.Fatal(err)
	}

	user, err := svc.Authenticate(ctx, res.Token)
	if err != nil || user.Email != "alice@example.com" {
		t.Fatalf("user=%v err=%v", user, err)
	}
	for _, token := range []string{"", "nonsense", res.Token + "x"} {
		if _, err := svc.Authenticate(ctx, token); !errors.Is(err, ErrUnauthenticated) {
			t.Errorf("token %q: err = %v, want ErrUnauthenticated", token, err)
		}
	}
}

func TestAuthenticateExpiredSession(t *testing.T) {
	svc, _, _ := newAuthFixture(t)
	ctx := context.Background()
	res, err := svc.Login(ctx, "alice@example.com", testPassword, "1.1.1.1")
	if err != nil {
		t.Fatal(err)
	}
	svc.now = func() time.Time { return time.Now().Add(2 * time.Hour) }
	if _, err := svc.Authenticate(ctx, res.Token); !errors.Is(err, ErrUnauthenticated) {
		t.Fatalf("err = %v, want ErrUnauthenticated", err)
	}
}

func TestLogoutRevokesSession(t *testing.T) {
	svc, _, _ := newAuthFixture(t)
	ctx := context.Background()
	res, _ := svc.Login(ctx, "alice@example.com", testPassword, "1.1.1.1")
	if err := svc.Logout(ctx, res.Token); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Authenticate(ctx, res.Token); !errors.Is(err, ErrUnauthenticated) {
		t.Fatalf("err = %v, want ErrUnauthenticated", err)
	}
	if err := svc.Logout(ctx, "never-existed"); err != nil {
		t.Fatalf("logout with unknown token: %v", err)
	}
}

func TestAuthenticateTracksLastSeen(t *testing.T) {
	svc, _, db := newAuthFixture(t)
	ctx := context.Background()
	res, _ := svc.Login(ctx, "alice@example.com", testPassword, "1.1.1.1")

	later := time.Now().Add(10 * time.Minute)
	svc.now = func() time.Time { return later }
	if _, err := svc.Authenticate(ctx, res.Token); err != nil {
		t.Fatal(err)
	}
	var s models.Session
	if err := db.First(&s).Error; err != nil {
		t.Fatal(err)
	}
	if s.LastSeenAt.Before(later.Add(-time.Second)) {
		t.Fatalf("last_seen_at = %v, want about %v", s.LastSeenAt, later)
	}
}

func TestDisablingUserEndsActiveSessions(t *testing.T) {
	svc, users, db := newAuthFixture(t)
	ctx := context.Background()
	if _, err := users.Create(ctx, "admin@example.com", testPassword, models.RoleAdmin); err != nil {
		t.Fatal(err)
	}
	res, _ := svc.Login(ctx, "alice@example.com", testPassword, "1.1.1.1")
	if err := users.SetDisabled(ctx, "alice@example.com", true); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Authenticate(ctx, res.Token); !errors.Is(err, ErrUnauthenticated) {
		t.Fatalf("err = %v, want ErrUnauthenticated", err)
	}
	var n int64
	db.Model(&models.Session{}).Count(&n)
	if n != 0 {
		t.Fatalf("%d sessions left", n)
	}
	// Enabling again allows a fresh login; data was kept
	if err := users.SetDisabled(ctx, "alice@example.com", false); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Login(ctx, "alice@example.com", testPassword, "1.1.1.1"); err != nil {
		t.Fatal(err)
	}
}

func TestSessionOfDisabledUserRejectedEvenIfRowRemains(t *testing.T) {
	svc, _, db := newAuthFixture(t)
	ctx := context.Background()
	res, _ := svc.Login(ctx, "alice@example.com", testPassword, "1.1.1.1")
	// Disabled flag flipped directly (not through UserService), the session row still exists
	db.Model(&models.User{}).Where("email = ?", "alice@example.com").Update("disabled", true)
	if _, err := svc.Authenticate(ctx, res.Token); !errors.Is(err, ErrUnauthenticated) {
		t.Fatalf("err = %v, want ErrUnauthenticated", err)
	}
}

func TestLoginPrunesExpiredSessions(t *testing.T) {
	svc, _, db := newAuthFixture(t)
	ctx := context.Background()
	if _, err := svc.Login(ctx, "alice@example.com", testPassword, "1.1.1.1"); err != nil {
		t.Fatal(err)
	}
	svc.now = func() time.Time { return time.Now().Add(2 * time.Hour) }
	if _, err := svc.Login(ctx, "alice@example.com", testPassword, "1.1.1.1"); err != nil {
		t.Fatal(err)
	}
	var n int64
	db.Model(&models.Session{}).Count(&n)
	if n != 1 {
		t.Fatalf("%d sessions, want 1 (the expired one pruned)", n)
	}
}

// A burst of simultaneous wrong guesses may not slip past LOGIN_MAX_ATTEMPTS just because
// none of them has been counted yet when the others check the limit.
func TestLoginBurstCannotExceedAttemptLimit(t *testing.T) {
	svc, _, _ := newAuthFixture(t) // limit: 3 attempts
	svc = NewAuthService(svc.db, AuthSettings{SessionTTL: time.Hour, LoginMaxAttempts: 3, LoginWindow: time.Minute, MaxConcurrentVerifies: 20, VerifyWait: 30 * time.Second})
	ctx := context.Background()

	var wg sync.WaitGroup
	results := make(chan error, 12)
	start := make(chan struct{})
	for i := 0; i < 12; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			_, err := svc.Login(ctx, "alice@example.com", "wrong-password-here", "5.5.5.5")
			results <- err
		}()
	}
	close(start)
	wg.Wait()
	close(results)

	guesses := 0
	for err := range results {
		var tooMany *TooManyAttemptsError
		switch {
		case errors.Is(err, ErrInvalidCredentials):
			guesses++
		case errors.As(err, &tooMany):
		default:
			t.Fatalf("unexpected error: %v", err)
		}
	}
	if guesses != 3 {
		t.Fatalf("%d passwords were checked, want exactly the limit of 3", guesses)
	}
}

// Password hashing takes 64 MiB per call, so only a few may run at once; the others wait a
// little and then get ErrLoginBusy instead of piling up memory.
func TestLoginRefusesWhenAllVerifySlotsAreBusy(t *testing.T) {
	_, _, db := newAuthFixture(t)
	svc := NewAuthService(db, AuthSettings{SessionTTL: time.Hour, LoginMaxAttempts: 100, LoginWindow: time.Minute, MaxConcurrentVerifies: 1, VerifyWait: time.Millisecond})
	ctx := context.Background()

	var wg sync.WaitGroup
	var busy, checked atomic.Int32
	start := make(chan struct{})
	for i := 0; i < 6; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			<-start
			// A different email each time: every request has a fresh rate-limit budget
			_, err := svc.Login(ctx, fmt.Sprintf("user%d@example.com", i), "wrong-password-here", "6.6.6.6")
			switch {
			case errors.Is(err, ErrLoginBusy):
				busy.Add(1)
			case errors.Is(err, ErrInvalidCredentials):
				checked.Add(1)
			default:
				t.Errorf("unexpected error: %v", err)
			}
		}(i)
	}
	close(start)
	wg.Wait()
	if busy.Load() == 0 || checked.Load() == 0 {
		t.Fatalf("busy=%d checked=%d, want some of each with one slot", busy.Load(), checked.Load())
	}

	// A refused request must not count against the user's attempts
	one := NewAuthService(db, AuthSettings{SessionTTL: time.Hour, LoginMaxAttempts: 1, LoginWindow: time.Minute, MaxConcurrentVerifies: 1, VerifyWait: time.Millisecond})
	one.verifySlots <- struct{}{} // hold the only slot
	if _, err := one.Login(ctx, "alice@example.com", testPassword, "4.4.4.4"); !errors.Is(err, ErrLoginBusy) {
		t.Fatalf("err = %v, want ErrLoginBusy", err)
	}
	<-one.verifySlots
	if _, err := one.Login(ctx, "alice@example.com", testPassword, "4.4.4.4"); err != nil {
		t.Fatalf("the busy answer used up the attempt: %v", err)
	}
}

// SetPassword signs the user out everywhere. A login that checked the old password just
// before the change must not end up with a session after it.
func TestPasswordChangeLeavesNoSessionFromAnInFlightLogin(t *testing.T) {
	svc, users, db := newAuthFixture(t)
	svc = NewAuthService(db, AuthSettings{SessionTTL: time.Hour, LoginMaxAttempts: 10, LoginWindow: time.Minute, MaxConcurrentVerifies: 4, VerifyWait: 30 * time.Second})
	ctx := context.Background()

	// Hold the login right before its session is stored
	reached := make(chan struct{})
	release := make(chan struct{})
	const hook = "test:hold-session-insert"
	if err := db.Callback().Create().Before("gorm:create").Register(hook, func(tx *gorm.DB) {
		if tx.Statement.Table == "sessions" {
			close(reached)
			<-release
		}
	}); err != nil {
		t.Fatal(err)
	}
	defer db.Callback().Create().Remove(hook)

	loginDone := make(chan error, 1)
	go func() {
		_, err := svc.Login(ctx, "alice@example.com", testPassword, "3.3.3.3")
		loginDone <- err
	}()
	select {
	case <-reached:
	case <-time.After(10 * time.Second):
		t.Fatal("login never reached the session insert")
	}

	passwdDone := make(chan error, 1)
	go func() { passwdDone <- users.SetPassword(ctx, "alice@example.com", "another-long-password") }()
	// Give the password change time to finish, or to start waiting for the login
	time.Sleep(300 * time.Millisecond)
	close(release)

	if err := <-passwdDone; err != nil {
		t.Fatal(err)
	}
	<-loginDone // either outcome is fine: success is revoked, failure never stored a session
	var sessions int64
	db.Model(&models.Session{}).Count(&sessions)
	if sessions != 0 {
		t.Fatalf("%d session(s) survived the password change", sessions)
	}
}
