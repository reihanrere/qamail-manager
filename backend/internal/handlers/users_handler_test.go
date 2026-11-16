package handlers_test

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/google/uuid"
)

type userRow struct {
	ID        string `json:"id"`
	Email     string `json:"email"`
	Role      string `json:"role"`
	Disabled  bool   `json:"disabled"`
	Accounts  int64  `json:"accounts"`
	CreatedAt string `json:"createdAt"`
	Quota     struct {
		Used  int64  `json:"used"`
		Limit *int   `json:"limit"`
		Left  *int64 `json:"remaining"`
	} `json:"quota"`
}

func listUsers(t *testing.T, e *isolationEnv) []userRow {
	t.Helper()
	resp, body := do(t, e.app, http.MethodGet, "/api/users", "", e.admin)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET /api/users: %d %s", resp.StatusCode, body)
	}
	var out struct {
		Data []userRow `json:"data"`
	}
	if err := json.Unmarshal([]byte(body), &out); err != nil {
		t.Fatal(err)
	}
	return out.Data
}

func userIDOf(t *testing.T, e *isolationEnv, email string) string {
	t.Helper()
	for _, u := range listUsers(t, e) {
		if u.Email == email {
			return u.ID
		}
	}
	t.Fatalf("user %s not listed", email)
	return ""
}

// Every user-management route is for admins only: 403 for a user, 401 without a session.
func TestUsersRoutesAdminOnly(t *testing.T) {
	e := newIsolationEnv(t)
	someID := uuid.NewString()
	routes := []struct{ method, path, body string }{
		{http.MethodGet, "/api/users", ""},
		{http.MethodPost, "/api/users", `{"email":"x@example.com","password":"a-long-password-1"}`},
		{http.MethodPatch, "/api/users/" + someID, `{"disabled":true}`},
		{http.MethodDelete, "/api/users/" + someID, ""},
	}
	for _, r := range routes {
		resp, body := do(t, e.app, r.method, r.path, r.body, e.alice)
		if resp.StatusCode != http.StatusForbidden || !strings.Contains(body, `"admin_required"`) {
			t.Errorf("user %s %s: %d %s", r.method, r.path, resp.StatusCode, body)
		}
		resp, body = do(t, e.app, r.method, r.path, r.body, nil)
		if resp.StatusCode != http.StatusUnauthorized {
			t.Errorf("anonymous %s %s: %d %s", r.method, r.path, resp.StatusCode, body)
		}
		resp, body = do(t, e.app, r.method, r.path, r.body, e.admin)
		if resp.StatusCode == http.StatusForbidden || resp.StatusCode == http.StatusUnauthorized {
			t.Errorf("admin %s %s was refused: %d %s", r.method, r.path, resp.StatusCode, body)
		}
	}
}

func TestListUsers(t *testing.T) {
	e := newIsolationEnv(t)
	e.generate(t, e.alice, "a1")
	e.generate(t, e.alice, "a2")

	resp, body := do(t, e.app, http.MethodGet, "/api/users", "", e.admin)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("%d %s", resp.StatusCode, body)
	}
	for _, secret := range []string{"password", "hash", "argon2"} {
		if strings.Contains(strings.ToLower(body), secret) {
			t.Fatalf("response mentions %q: %s", secret, body)
		}
	}
	byEmail := map[string]userRow{}
	for _, u := range listUsers(t, e) {
		byEmail[u.Email] = u
	}
	if len(byEmail) != 3 {
		t.Fatalf("want 3 users, got %v", byEmail)
	}
	alice := byEmail["alice@example.com"]
	if alice.Role != "user" || alice.Disabled || alice.Accounts != 2 || alice.Quota.Used != 2 ||
		alice.Quota.Limit == nil || *alice.Quota.Limit != 10 || alice.CreatedAt == "" || alice.ID == "" {
		t.Errorf("alice row: %+v", alice)
	}
	if root := byEmail["root@example.com"]; root.Role != "admin" || root.Quota.Limit != nil {
		t.Errorf("admin row: %+v", root)
	}
}

func TestCreateUser(t *testing.T) {
	e := newIsolationEnv(t)
	post := func(body string) (int, string) {
		resp, out := do(t, e.app, http.MethodPost, "/api/users", body, e.admin)
		return resp.StatusCode, out
	}

	status, body := post(`{"email":"  New@Example.com ","password":"a-long-password-1"}`)
	if status != http.StatusCreated || !strings.Contains(body, `"new@example.com"`) || !strings.Contains(body, `"role":"user"`) {
		t.Fatalf("create: %d %s", status, body)
	}
	if resp, out := login(t, e.app, "new@example.com", "a-long-password-1", "10.1.1.1"); resp.StatusCode != http.StatusOK {
		t.Fatalf("new user cannot log in: %s", out)
	}
	if status, body = post(`{"email":"boss@example.com","password":"a-long-password-1","role":"admin"}`); status != http.StatusCreated || !strings.Contains(body, `"role":"admin"`) {
		t.Fatalf("create admin: %d %s", status, body)
	}

	for name, c := range map[string]struct {
		body   string
		status int
		code   string
	}{
		"duplicate":      {`{"email":"NEW@example.com","password":"a-long-password-1"}`, http.StatusConflict, "user_exists"},
		"short password": {`{"email":"s@example.com","password":"short"}`, http.StatusBadRequest, "password_too_short"},
		"long password":  {`{"email":"l@example.com","password":"` + strings.Repeat("x", 300) + `"}`, http.StatusBadRequest, "password_too_long"},
		"bad email":      {`{"email":"nope","password":"a-long-password-1"}`, http.StatusBadRequest, "invalid_email"},
		"bad role":       {`{"email":"r@example.com","password":"a-long-password-1","role":"root"}`, http.StatusBadRequest, "invalid_role"},
		"bad body":       {`{`, http.StatusBadRequest, "invalid_request_body"},
	} {
		status, body := post(c.body)
		if status != c.status || errCode(t, body) != c.code {
			t.Errorf("%s: %d %s", name, status, body)
		}
	}
}

// Disabling or resetting a password signs the user out at once.
func TestPatchUserRevokesSessions(t *testing.T) {
	e := newIsolationEnv(t)
	id := userIDOf(t, e, "alice@example.com")
	alive := func() bool {
		resp, _ := do(t, e.app, http.MethodGet, "/api/auth/me", "", e.alice)
		return resp.StatusCode == http.StatusOK
	}
	if !alive() {
		t.Fatal("alice should start logged in")
	}

	resp, body := do(t, e.app, http.MethodPatch, "/api/users/"+id, `{"disabled":true}`, e.admin)
	if resp.StatusCode != http.StatusOK || alive() {
		t.Fatalf("disable: %d %s (session alive: %v)", resp.StatusCode, body, alive())
	}
	if resp, _ := login(t, e.app, "alice@example.com", password, "10.2.2.2"); resp.StatusCode == http.StatusOK {
		t.Fatal("disabled user logged in")
	}
	var disabled bool
	for _, u := range listUsers(t, e) {
		if u.Email == "alice@example.com" {
			disabled = u.Disabled
		}
	}
	if !disabled {
		t.Error("list does not show alice as disabled")
	}

	if resp, body = do(t, e.app, http.MethodPatch, "/api/users/"+id, `{"disabled":false,"password":"brand-new-password-1"}`, e.admin); resp.StatusCode != http.StatusOK {
		t.Fatalf("enable + reset: %d %s", resp.StatusCode, body)
	}
	if strings.Contains(body, "brand-new") {
		t.Fatalf("response echoes the password: %s", body)
	}
	if resp, _ = login(t, e.app, "alice@example.com", password, "10.2.2.3"); resp.StatusCode == http.StatusOK {
		t.Fatal("old password still works")
	}
	resp, out := login(t, e.app, "alice@example.com", "brand-new-password-1", "10.2.2.4")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("new password: %s", out)
	}
	// A later reset signs out the session created by that login
	cookie := map[string]string{"Cookie": sessionCookie(resp).String()}
	if resp, body = do(t, e.app, http.MethodPatch, "/api/users/"+id, `{"password":"another-long-password-2"}`, e.admin); resp.StatusCode != http.StatusOK {
		t.Fatalf("reset: %d %s", resp.StatusCode, body)
	}
	if resp, _ = do(t, e.app, http.MethodGet, "/api/auth/me", "", cookie); resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("session survived a password reset: %d", resp.StatusCode)
	}
}

func TestPatchUserErrors(t *testing.T) {
	e := newIsolationEnv(t)
	id := userIDOf(t, e, "alice@example.com")
	rootID := userIDOf(t, e, "root@example.com")
	for name, c := range map[string]struct {
		path, body string
		status     int
		code       string
	}{
		"unknown id":     {"/api/users/" + uuid.NewString(), `{"disabled":true}`, http.StatusNotFound, "user_not_found"},
		"malformed id":   {"/api/users/nope", `{"disabled":true}`, http.StatusBadRequest, "invalid_user_id"},
		"empty patch":    {"/api/users/" + id, `{}`, http.StatusBadRequest, "invalid_input"},
		"short password": {"/api/users/" + id, `{"password":"short"}`, http.StatusBadRequest, "password_too_short"},
		"disable self":   {"/api/users/" + rootID, `{"disabled":true}`, http.StatusConflict, "cannot_modify_self"},
	} {
		resp, body := do(t, e.app, http.MethodPatch, c.path, c.body, e.admin)
		if resp.StatusCode != c.status || errCode(t, body) != c.code {
			t.Errorf("%s: %d %s", name, resp.StatusCode, body)
		}
	}
	// A refused request changes nothing
	if resp, body := do(t, e.app, http.MethodGet, "/api/auth/me", "", e.admin); resp.StatusCode != http.StatusOK {
		t.Fatalf("admin lost the session: %d %s", resp.StatusCode, body)
	}
}

func TestDeleteUser(t *testing.T) {
	e := newIsolationEnv(t)
	aliceID := userIDOf(t, e, "alice@example.com")
	bobID := userIDOf(t, e, "bob@example.com")
	rootID := userIDOf(t, e, "root@example.com")
	_, aliceEmail := e.generate(t, e.alice, "a")
	e.generate(t, e.bob, "b")
	e.ingest(t, aliceEmail, "m1")

	if resp, body := do(t, e.app, http.MethodDelete, "/api/users/"+rootID, "", e.admin); resp.StatusCode != http.StatusConflict || errCode(t, body) != "cannot_modify_self" {
		t.Fatalf("delete self: %d %s", resp.StatusCode, body)
	}
	if resp, body := do(t, e.app, http.MethodDelete, "/api/users/"+uuid.NewString(), "", e.admin); resp.StatusCode != http.StatusNotFound || errCode(t, body) != "user_not_found" {
		t.Fatalf("unknown: %d %s", resp.StatusCode, body)
	}

	// Without force a user who owns accounts stays, and the answer says how many
	resp, body := do(t, e.app, http.MethodDelete, "/api/users/"+aliceID, "", e.admin)
	if resp.StatusCode != http.StatusConflict || errCode(t, body) != "user_has_accounts" || !strings.Contains(body, `"accounts":1`) {
		t.Fatalf("delete without force: %d %s", resp.StatusCode, body)
	}
	if resp, _ := do(t, e.app, http.MethodGet, "/api/auth/me", "", e.alice); resp.StatusCode != http.StatusOK {
		t.Fatal("a refused delete ended alice's session")
	}

	resp, body = do(t, e.app, http.MethodDelete, "/api/users/"+aliceID+"?force=true", "", e.admin)
	if resp.StatusCode != http.StatusOK || !strings.Contains(body, `"accounts":1`) {
		t.Fatalf("delete with force: %d %s", resp.StatusCode, body)
	}
	if resp, _ := do(t, e.app, http.MethodGet, "/api/auth/me", "", e.alice); resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("deleted user's session survived: %d", resp.StatusCode)
	}
	if resp, _ := login(t, e.app, "alice@example.com", password, "10.3.3.3"); resp.StatusCode == http.StatusOK {
		t.Fatal("deleted user logged in")
	}
	// Bob and his account are untouched
	if resp, body := do(t, e.app, http.MethodGet, "/api/accounts", "", e.bob); resp.StatusCode != http.StatusOK || !strings.Contains(body, `"b"`) {
		t.Fatalf("bob's data: %d %s", resp.StatusCode, body)
	}
	// A user with no accounts needs no force
	if resp, body := do(t, e.app, http.MethodPost, "/api/users", `{"email":"temp@example.com","password":"a-long-password-1"}`, e.admin); resp.StatusCode != http.StatusCreated {
		t.Fatalf("create temp: %d %s", resp.StatusCode, body)
	}
	if resp, body := do(t, e.app, http.MethodDelete, "/api/users/"+userIDOf(t, e, "temp@example.com"), "", e.admin); resp.StatusCode != http.StatusOK || !strings.Contains(body, `"accounts":0`) {
		t.Fatalf("delete temp: %d %s", resp.StatusCode, body)
	}
	if resp, body := do(t, e.app, http.MethodDelete, "/api/users/"+bobID+"?force=true", "", e.admin); resp.StatusCode != http.StatusOK {
		t.Fatalf("delete bob: %d %s", resp.StatusCode, body)
	}
	if n := len(listUsers(t, e)); n != 1 {
		t.Fatalf("want only the admin left, got %d users", n)
	}
}

// Routes that change users refuse a request that a browser sent from another site.
func TestUsersRoutesRefuseCrossOrigin(t *testing.T) {
	e := newIsolationEnv(t)
	id := userIDOf(t, e, "alice@example.com")
	evil := map[string]string{"Cookie": e.admin["Cookie"], "Origin": "https://evil.example"}
	for _, r := range []struct{ method, path, body string }{
		{http.MethodPost, "/api/users", `{"email":"x@example.com","password":"a-long-password-1"}`},
		{http.MethodPatch, "/api/users/" + id, `{"disabled":true}`},
		{http.MethodDelete, "/api/users/" + id, ""},
	} {
		resp, body := do(t, e.app, r.method, r.path, r.body, evil)
		if resp.StatusCode != http.StatusForbidden || errCode(t, body) != "cross_origin_request" {
			t.Errorf("%s %s: %d %s", r.method, r.path, resp.StatusCode, body)
		}
	}
	if resp, body := do(t, e.app, http.MethodPatch, "/api/users/"+id, `{"disabled":true}`, evil); resp.StatusCode == http.StatusOK {
		t.Fatalf("cross-origin change went through: %s", body)
	}
}
