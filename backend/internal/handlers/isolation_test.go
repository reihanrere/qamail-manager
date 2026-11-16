package handlers_test

import (
	"bufio"
	"encoding/json"
	"net"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/gofiber/fiber/v3"

	"github.com/reihanrere/qamail-manager/internal/handlers"
	"github.com/reihanrere/qamail-manager/internal/models"
	"github.com/reihanrere/qamail-manager/internal/providers"
	"github.com/reihanrere/qamail-manager/internal/routes"
	"github.com/reihanrere/qamail-manager/internal/services"
)

const isolationDomain = "inbox.example.com"

const rawEmail = "From: Shop <shop@example.com>\r\nTo: {{TO}}\r\nSubject: Code 482913\r\nMessage-ID: <{{ID}}@example.com>\r\n" +
	"Content-Type: multipart/mixed; boundary=\"b\"\r\n\r\n--b\r\nContent-Type: text/plain\r\n\r\nYour code is 482913\r\n" +
	"--b\r\nContent-Type: application/pdf; name=\"a.pdf\"\r\nContent-Disposition: attachment; filename=\"a.pdf\"\r\n" +
	"Content-Transfer-Encoding: base64\r\n\r\nJVBERi0xLjQK\r\n--b--\r\n"

type isolationEnv struct {
	app      *fiber.App
	accounts *services.AccountService
	users    *services.UserService
	alice    map[string]string // request headers carrying alice's session cookie
	bob      map[string]string
	admin    map[string]string
}

func newIsolationEnv(t *testing.T) *isolationEnv {
	t.Helper()
	db := openTestDB(t)
	users := services.NewUserService(db, 10)
	for email, role := range map[string]string{"alice@example.com": models.RoleUser, "bob@example.com": models.RoleUser, "root@example.com": models.RoleAdmin} {
		if _, err := users.Create(t.Context(), email, password, role); err != nil {
			t.Fatal(err)
		}
	}
	accounts, err := services.NewAccountService(db, services.Settings{
		DefaultProvider:       providers.NameLocal,
		Limits:                services.Limits{TagMaxLength: 50, NoteMaxLength: 500},
		UsernameMaxAttempts:   5,
		BulkGenerateMax:       5,
		UserAccountQuota:      10,
		AutoMarkUsed:          services.AutoMarkOff,
		AccountCleanupAction:  services.CleanupBlock,
		InboxSearchMaxPages:   10,
		MessagePruneInterval:  time.Hour,
		LegacyUsernamePattern: "^old_prefix_",
		IngestEnabled:         true,
	}, providers.NewLocal(db, isolationDomain))
	if err != nil {
		t.Fatal(err)
	}

	app := newTestFiber()
	routes.RegisterRoutes(app, handlers.NewAuthHandler(newAuthService(db), false, nil),
		handlers.NewAccountHandler(accounts), handlers.NewInboxHandler(accounts),
		handlers.NewEventsHandler(accounts.Events(), newAuthService(db), time.Second),
		handlers.NewUserHandler(users, accounts))

	env := &isolationEnv{app: app, accounts: accounts, users: users}
	for _, who := range []struct {
		email  string
		target *map[string]string
	}{{"alice@example.com", &env.alice}, {"bob@example.com", &env.bob}, {"root@example.com", &env.admin}} {
		resp, body := login(t, app, who.email, password, "10.9.9.9")
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("login %s: %s", who.email, body)
		}
		*who.target = map[string]string{"Cookie": sessionCookie(resp).String()}
	}
	return env
}

// generate creates an account through the API as the given user.
func (e *isolationEnv) generate(t *testing.T, as map[string]string, tag string) (id, email string) {
	t.Helper()
	resp, body := do(t, e.app, http.MethodPost, "/api/accounts/generate", `{"tag":"`+tag+`"}`, as)
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("generate: %d %s", resp.StatusCode, body)
	}
	var env struct {
		Data struct{ ID, Email string } `json:"data"`
	}
	_ = json.Unmarshal([]byte(body), &env)
	return env.Data.ID, env.Data.Email
}

func (e *isolationEnv) ingest(t *testing.T, to, id string) {
	t.Helper()
	raw := strings.NewReplacer("{{TO}}", to, "{{ID}}", id).Replace(rawEmail)
	if _, err := e.accounts.IngestMessage(t.Context(), []byte(raw), to); err != nil {
		t.Fatal(err)
	}
}

// Alice uses every per-account route of the API on bob's account: each must answer 404
// account_not_found and change nothing. The routes come from the route table, so a new
// per-account route is covered without editing this test.
func TestHTTPIsolationEveryAccountRoute(t *testing.T) {
	e := newIsolationEnv(t)
	bobID, bobEmail := e.generate(t, e.bob, "bob-tag")
	e.generate(t, e.alice, "alice-tag")
	e.ingest(t, bobEmail, "m1")

	// Bob's own message and attachment ids, read as bob
	resp, body := do(t, e.app, http.MethodGet, "/api/accounts/"+bobID+"/messages", "", e.bob)
	var inbox struct {
		Data []struct{ ID string } `json:"data"`
	}
	_ = json.Unmarshal([]byte(body), &inbox)
	if resp.StatusCode != http.StatusOK || len(inbox.Data) != 1 {
		t.Fatalf("bob inbox: %d %s", resp.StatusCode, body)
	}
	messageID := inbox.Data[0].ID
	resp, body = do(t, e.app, http.MethodGet, "/api/accounts/"+bobID+"/messages/"+messageID, "", e.bob)
	var detail struct {
		Data struct {
			Attachments []struct{ ID string } `json:"attachments"`
		} `json:"data"`
	}
	_ = json.Unmarshal([]byte(body), &detail)
	if resp.StatusCode != http.StatusOK || len(detail.Data.Attachments) != 1 {
		t.Fatalf("bob detail: %d %s", resp.StatusCode, body)
	}
	attachmentID := detail.Data.Attachments[0].ID

	bodies := map[string]string{
		"PATCH /api/accounts/:id":                          `{"tag":"hijacked"}`,
		"PATCH /api/accounts/:id/status":                   `{"status":"BLOCKED"}`,
		"POST /api/accounts/:id/messages/send":             `{"to":["x@example.com"],"subject":"s","text":"t"}`,
		"PATCH /api/accounts/:id/messages/:messageId/read": "",
	}
	// Alice (a plain user) and root (an admin, who is not bob either) both get 404 on every
	// route. The one exception is sending, which a plain user is refused earlier with 403.
	checked := 0
	for name, intruder := range map[string]map[string]string{"alice": e.alice, "admin": e.admin} {
		for _, route := range e.app.GetRoutes(true) {
			if !strings.HasPrefix(route.Path, "/api/accounts/") || !strings.Contains(route.Path, ":id") || route.Method == http.MethodHead {
				continue
			}
			path := strings.NewReplacer(":id", bobID, ":messageId", messageID, ":attachmentId", attachmentID).Replace(route.Path)
			resp, body := do(t, e.app, route.Method, path, bodies[route.Method+" "+route.Path], intruder)
			if name == "alice" && route.Path == "/api/accounts/:id/messages/send" {
				if resp.StatusCode != http.StatusForbidden || errCode(t, body) != "admin_required" {
					t.Errorf("alice %s %s: %d %s, want 403 admin_required", route.Method, route.Path, resp.StatusCode, body)
				}
				continue
			}
			if resp.StatusCode != http.StatusNotFound || errCode(t, body) != "account_not_found" {
				t.Errorf("%s %s %s: %d %s, want 404 account_not_found", name, route.Method, route.Path, resp.StatusCode, body)
			}
			checked++
		}
	}
	if checked < 23 {
		t.Fatalf("only %d per-account routes checked", checked)
	}

	// Bob's account and message are untouched
	resp, body = do(t, e.app, http.MethodGet, "/api/accounts/"+bobID, "", e.bob)
	if resp.StatusCode != http.StatusOK || !strings.Contains(body, `"tag":"bob-tag"`) || !strings.Contains(body, `"status":"AVAILABLE"`) {
		t.Fatalf("bob's account changed: %d %s", resp.StatusCode, body)
	}
	for _, path := range []string{
		"/api/accounts/" + bobID + "/messages/" + messageID,
		"/api/accounts/" + bobID + "/messages/" + messageID + "/source",
		"/api/accounts/" + bobID + "/messages/" + messageID + "/attachments/" + attachmentID,
	} {
		if resp, body := do(t, e.app, http.MethodGet, path, "", e.bob); resp.StatusCode != http.StatusOK {
			t.Errorf("bob GET %s: %d %s", path, resp.StatusCode, body)
		}
	}
}

func TestHTTPIsolationListStatsExport(t *testing.T) {
	e := newIsolationEnv(t)
	_, bobEmail := e.generate(t, e.bob, "bob-tag")
	_, aliceEmail := e.generate(t, e.alice, "alice-tag")

	for _, path := range []string{"/api/accounts/", "/api/accounts/?search=" + bobEmail, "/api/accounts/?search=bob-tag", "/api/accounts/export"} {
		resp, body := do(t, e.app, http.MethodGet, path, "", e.alice)
		if resp.StatusCode != http.StatusOK || strings.Contains(body, bobEmail) {
			t.Errorf("alice %s: %d, leaks bob=%v", path, resp.StatusCode, strings.Contains(body, bobEmail))
		}
	}
	if _, body := do(t, e.app, http.MethodGet, "/api/accounts/", "", e.alice); !strings.Contains(body, aliceEmail) {
		t.Error("alice does not see her own account")
	}
	_, body := do(t, e.app, http.MethodGet, "/api/accounts/stats", "", e.alice)
	if !strings.Contains(body, `"total":1`) {
		t.Errorf("alice stats: %s", body)
	}
}

// The live stream of one user never carries another user's events. Runs on a real
// listener because app.Test does not stream.
func TestHTTPIsolationEventStream(t *testing.T) {
	e := newIsolationEnv(t)
	_, bobEmail := e.generate(t, e.bob, "b")
	_, aliceEmail := e.generate(t, e.alice, "a")

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	go func() { _ = e.app.Listener(ln, fiber.ListenConfig{DisableStartupMessage: true}) }()
	defer func() { _ = e.app.Shutdown() }()

	req, _ := http.NewRequest(http.MethodGet, "http://"+ln.Addr().String()+"/api/events", nil)
	req.Header.Set("Cookie", e.alice["Cookie"])
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("stream status %d", resp.StatusCode)
	}

	lines := make(chan string, 64)
	go func() {
		scanner := bufio.NewScanner(resp.Body)
		for scanner.Scan() {
			lines <- scanner.Text()
		}
		close(lines)
	}()
	waitFor := func(want string, d time.Duration) (string, bool) {
		deadline := time.After(d)
		for {
			select {
			case line, ok := <-lines:
				if !ok {
					return "", false
				}
				if strings.Contains(line, want) {
					return line, true
				}
				if strings.Contains(line, bobEmail) {
					return line, true
				}
			case <-deadline:
				return "", false
			}
		}
	}
	if _, ok := waitFor(": connected", 5*time.Second); !ok {
		t.Fatal("stream never connected")
	}

	e.ingest(t, bobEmail, "for-bob")
	e.ingest(t, aliceEmail, "for-alice")
	// Events are delivered in order, so bob's (published first) would show up before alice's
	line, ok := waitFor(aliceEmail, 5*time.Second)
	if !ok {
		t.Fatal("alice never received her own event")
	}
	if strings.Contains(line, bobEmail) {
		t.Fatalf("alice's stream carried bob's event: %s", line)
	}
}
