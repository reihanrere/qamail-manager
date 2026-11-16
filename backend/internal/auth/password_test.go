package auth

import (
	"errors"
	"strings"
	"testing"
)

func TestHashAndVerify(t *testing.T) {
	hash, err := HashPassword("correct horse battery")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(hash, "$argon2id$v=19$") {
		t.Fatalf("unexpected format: %s", hash)
	}
	if ok, err := VerifyPassword("correct horse battery", hash); err != nil || !ok {
		t.Fatalf("right password rejected: ok=%v err=%v", ok, err)
	}
	if ok, err := VerifyPassword("wrong", hash); err != nil || ok {
		t.Fatalf("wrong password accepted: ok=%v err=%v", ok, err)
	}
}

func TestHashUsesRandomSalt(t *testing.T) {
	a, _ := HashPassword("same password")
	b, _ := HashPassword("same password")
	if a == b {
		t.Fatal("two hashes of one password must differ")
	}
}

func TestVerifyRejectsMalformedHash(t *testing.T) {
	for _, bad := range []string{
		"", "plain", "$argon2i$v=19$m=1,t=1,p=1$c2FsdA$aGFzaA",
		"$argon2id$v=18$m=1,t=1,p=1$c2FsdA$aGFzaA", "$argon2id$v=19$m=0,t=1,p=1$c2FsdA$aGFzaA",
		"$argon2id$v=19$m=1,t=1,p=1$!!$aGFzaA", "$argon2id$v=19$m=1,t=1,p=1$c2FsdA$",
	} {
		if ok, err := VerifyPassword("x", bad); ok || !errors.Is(err, ErrInvalidHash) {
			t.Errorf("%q: ok=%v err=%v, want ErrInvalidHash", bad, ok, err)
		}
	}
}

func TestPasswordLengthLimit(t *testing.T) {
	long := strings.Repeat("a", MaxPasswordBytes+1)
	if _, err := HashPassword(long); !errors.Is(err, ErrPasswordTooLong) {
		t.Fatalf("got %v", err)
	}
	hash, _ := HashPassword("short enough")
	if ok, err := VerifyPassword(long, hash); ok || err != nil {
		t.Fatalf("oversized input: ok=%v err=%v", ok, err)
	}
}

func TestValidatePassword(t *testing.T) {
	if err := ValidatePassword("123456789", 10); !errors.Is(err, ErrPasswordTooShort) {
		t.Fatalf("got %v", err)
	}
	if err := ValidatePassword(strings.Repeat("a", 10), 10); err != nil {
		t.Fatal(err)
	}
	if err := ValidatePassword(strings.Repeat("a", MaxPasswordBytes+1), 10); !errors.Is(err, ErrPasswordTooLong) {
		t.Fatalf("got %v", err)
	}
}

func TestEmail(t *testing.T) {
	if got := NormalizeEmail("  Admin@Example.COM "); got != "admin@example.com" {
		t.Fatalf("got %q", got)
	}
	for _, ok := range []string{"a@example.com", "  A@Example.com "} {
		if err := ValidateEmail(ok); err != nil {
			t.Errorf("%q rejected: %v", ok, err)
		}
	}
	for _, bad := range []string{"", "nope", "a@b", "Bob <a@example.com>", "a@@example.com", "a b@example.com"} {
		if err := ValidateEmail(bad); !errors.Is(err, ErrInvalidEmail) {
			t.Errorf("%q accepted", bad)
		}
	}
}
