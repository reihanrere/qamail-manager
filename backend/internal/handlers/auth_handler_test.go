package handlers_test

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/gofiber/fiber/v3"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	"github.com/reihanrere/qamail-manager/internal/database"
	"github.com/reihanrere/qamail-manager/internal/handlers"
	"github.com/reihanrere/qamail-manager/internal/models"
	"github.com/reihanrere/qamail-manager/internal/routes"
	"github.com/reihanrere/qamail-manager/internal/services"
)

const password = "correct-horse-battery"

// newApp builds the real route table on a disposable database (see TEST_DATABASE_DSN in
// internal/services/integration_test.go) with one user alice@example.com.
func newApp(t *testing.T, cookieSecure bool) (*fiber.App, *services.UserService) {
	t.Helper()
	return newAppWithOrigins(t, cookieSecure, nil)
}

// newAppWithOrigins is newApp with CORS_ALLOWED_ORIGINS set, which also lets those
// origins call login.
func newAppWithOrigins(t *testing.T, cookieSecure bool, origins []string) (*fiber.App, *services.UserService) {
	t.Helper()
	db := openTestDB(t)
	users := services.NewUserService(db, 10)
	if _, err := users.Create(t.Context(), "alice@example.com", password, models.RoleUser); err != nil {
		t.Fatal(err)
	}
	app := newTestFiber()
	routes.RegisterRoutes(app, handlers.NewAuthHandler(newAuthService(db), cookieSecure, origins),
		handlers.NewAccountHandler(nil), handlers.NewInboxHandler(nil), handlers.NewEventsHandler(nil, nil, time.Second), handlers.NewUserHandler(users, nil))
	return app, users
}

func newAuthService(db *gorm.DB) *services.AuthService {
	return services.NewAuthService(db, services.AuthSettings{SessionTTL: time.Hour, LoginMaxAttempts: 3, LoginWindow: time.Minute})
}

// newTestFiber trusts every peer so tests can set the client address with X-Real-IP.
func newTestFiber() *fiber.App {
	return fiber.New(fiber.Config{
		ProxyHeader:      "X-Real-IP",
		TrustProxy:       true,
		TrustProxyConfig: fiber.TrustProxyConfig{Proxies: []string{"0.0.0.0/0", "::/0"}},
	})
}

// openTestDB opens the disposable database and empties it.
func openTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	dsn := os.Getenv("TEST_DATABASE_DSN")
	if dsn == "" {
		t.Skip("TEST_DATABASE_DSN not set")
	}
	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatal(err)
	}
	if err := database.Migrate(db, database.AdminSeed{}); err != nil {
		t.Fatal(err)
	}
	if err := db.Exec("TRUNCATE messages, mail_accounts, retired_addresses, sessions, users, setting_overrides CASCADE").Error; err != nil {
		t.Fatal(err)
	}
	return db
}

func do(t *testing.T, app *fiber.App, method, path, body string, header map[string]string) (*http.Response, string) {
	t.Helper()
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	for k, v := range header {
		req.Header.Set(k, v)
	}
	resp, err := app.Test(req, fiber.TestConfig{Timeout: 15 * time.Second})
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	return resp, string(raw)
}

func login(t *testing.T, app *fiber.App, email, pw, ip string) (*http.Response, string) {
	body, _ := json.Marshal(map[string]string{"email": email, "password": pw})
	return do(t, app, http.MethodPost, "/api/auth/login", string(body), map[string]string{"X-Real-IP": ip})
}

func sessionCookie(resp *http.Response) *http.Cookie {
	for _, c := range resp.Cookies() {
		if c.Name == handlers.SessionCookieName {
			return c
		}
	}
	return nil
}

func errCode(t *testing.T, body string) string {
	t.Helper()
	var env struct {
		Code string `json:"code"`
	}
	if err := json.Unmarshal([]byte(body), &env); err != nil {
		t.Fatalf("not JSON: %q", body)
	}
	return env.Code
}

// Every route under /api except login must answer 401 without a session. The test reads
// the route table, so a route added later is covered without editing this test.
func TestEveryAPIRouteRequiresLogin(t *testing.T) {
	app, _ := newApp(t, false)
	checked := 0
	for _, route := range app.GetRoutes(true) {
		if !strings.HasPrefix(route.Path, "/api") || (route.Method == http.MethodPost && route.Path == "/api/auth/login") {
			continue
		}
		if route.Method == http.MethodHead {
			continue
		}
		path := route.Path
		for _, p := range route.Params {
			path = strings.Replace(path, ":"+p, "11111111-1111-1111-1111-111111111111", 1)
		}
		resp, body := do(t, app, route.Method, path, "", nil)
		if resp.StatusCode != http.StatusUnauthorized || errCode(t, body) != "unauthenticated" {
			t.Errorf("%s %s: status %d body %s, want 401 unauthenticated", route.Method, route.Path, resp.StatusCode, body)
		}
		checked++
	}
	if checked < 20 {
		t.Fatalf("only %d routes checked; the route table looks wrong", checked)
	}
	// Unknown paths under /api must not leak a 404 to anonymous callers either
	if resp, _ := do(t, app, http.MethodGet, "/api/does-not-exist", "", nil); resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("unknown /api path: status %d, want 401", resp.StatusCode)
	}
}

func TestLoginSetsHardenedCookieAndMeWorks(t *testing.T) {
	app, _ := newApp(t, true)
	resp, body := login(t, app, "Alice@Example.com", password, "10.0.0.1")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status %d: %s", resp.StatusCode, body)
	}
	cookie := sessionCookie(resp)
	if cookie == nil || cookie.Value == "" {
		t.Fatal("no session cookie")
	}
	if !cookie.HttpOnly || !cookie.Secure || cookie.SameSite != http.SameSiteLaxMode || cookie.Path != "/" || cookie.Domain != "" {
		t.Errorf("cookie flags wrong: %+v", cookie)
	}
	if strings.Contains(body, "password") || strings.Contains(body, cookie.Value) {
		t.Errorf("login body leaks secrets: %s", body)
	}

	resp, body = do(t, app, http.MethodGet, "/api/auth/me", "", map[string]string{"Cookie": cookie.String()})
	if resp.StatusCode != http.StatusOK || !strings.Contains(body, `"email":"alice@example.com"`) || !strings.Contains(body, `"role":"user"`) {
		t.Fatalf("me: %d %s", resp.StatusCode, body)
	}
}

func TestCookieNotSecureWhenConfiguredOff(t *testing.T) {
	app, _ := newApp(t, false)
	resp, _ := login(t, app, "alice@example.com", password, "10.0.0.1")
	if c := sessionCookie(resp); c == nil || c.Secure {
		t.Fatalf("cookie = %+v, want present and not Secure", c)
	}
}

func TestWrongPasswordAndUnknownEmailGiveTheSameAnswer(t *testing.T) {
	app, _ := newApp(t, false)
	r1, b1 := login(t, app, "alice@example.com", "wrong-password-here", "10.0.0.2")
	r2, b2 := login(t, app, "nobody@example.com", "wrong-password-here", "10.0.0.3")
	if r1.StatusCode != http.StatusUnauthorized || r2.StatusCode != http.StatusUnauthorized {
		t.Fatalf("statuses %d, %d", r1.StatusCode, r2.StatusCode)
	}
	if b1 != b2 || errCode(t, b1) != "invalid_credentials" {
		t.Fatalf("bodies differ or wrong code: %s vs %s", b1, b2)
	}
	if sessionCookie(r1) != nil || sessionCookie(r2) != nil {
		t.Fatal("failed login set a cookie")
	}
}

func TestLoginRejectsBadBody(t *testing.T) {
	app, _ := newApp(t, false)
	resp, body := do(t, app, http.MethodPost, "/api/auth/login", "{not json", nil)
	if resp.StatusCode != http.StatusBadRequest || errCode(t, body) != "invalid_request_body" {
		t.Fatalf("%d %s", resp.StatusCode, body)
	}
	resp, _ = login(t, app, "", "", "10.0.0.4")
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("empty credentials: status %d", resp.StatusCode)
	}
}

func TestLoginRateLimitUsesClientIPFromProxyHeader(t *testing.T) {
	app, _ := newApp(t, false)
	for i := 0; i < 3; i++ {
		login(t, app, "alice@example.com", "wrong-password-here", "10.1.1.1")
	}
	resp, body := login(t, app, "alice@example.com", password, "10.1.1.1")
	if resp.StatusCode != http.StatusTooManyRequests || errCode(t, body) != "too_many_login_attempts" {
		t.Fatalf("%d %s", resp.StatusCode, body)
	}
	if secs, err := strconv.Atoi(resp.Header.Get("Retry-After")); err != nil || secs < 1 || secs > 60 {
		t.Fatalf("Retry-After = %q", resp.Header.Get("Retry-After"))
	}
	if !strings.Contains(body, "retryAfterSeconds") {
		t.Errorf("missing retryAfterSeconds param: %s", body)
	}
	// A different client address is unaffected
	if resp, _ := login(t, app, "alice@example.com", password, "10.2.2.2"); resp.StatusCode != http.StatusOK {
		t.Fatalf("other IP: status %d", resp.StatusCode)
	}
}

func TestLogoutRevokesSessionAndClearsCookie(t *testing.T) {
	app, _ := newApp(t, false)
	resp, _ := login(t, app, "alice@example.com", password, "10.0.0.5")
	cookie := sessionCookie(resp)
	header := map[string]string{"Cookie": cookie.String()}

	resp, _ = do(t, app, http.MethodPost, "/api/auth/logout", "", header)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("logout status %d", resp.StatusCode)
	}
	if c := sessionCookie(resp); c == nil || c.Value != "" || c.MaxAge >= 0 {
		t.Errorf("cookie not cleared: %+v", c)
	}
	// The old cookie value is dead on the server, not just removed from the browser
	if resp, _ := do(t, app, http.MethodGet, "/api/auth/me", "", header); resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("me after logout: status %d", resp.StatusCode)
	}
}

func TestDisablingUserEndsTheLiveSession(t *testing.T) {
	app, users := newApp(t, false)
	if _, err := users.Create(t.Context(), "admin@example.com", password, models.RoleAdmin); err != nil {
		t.Fatal(err)
	}
	resp, _ := login(t, app, "alice@example.com", password, "10.0.0.6")
	header := map[string]string{"Cookie": sessionCookie(resp).String()}
	if resp, _ := do(t, app, http.MethodGet, "/api/auth/me", "", header); resp.StatusCode != http.StatusOK {
		t.Fatalf("me before disable: %d", resp.StatusCode)
	}
	if err := users.SetDisabled(t.Context(), "alice@example.com", true); err != nil {
		t.Fatal(err)
	}
	if resp, _ := do(t, app, http.MethodGet, "/api/auth/me", "", header); resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("me after disable: %d", resp.StatusCode)
	}
}

func TestEventsStreamNeedsLogin(t *testing.T) {
	app, _ := newApp(t, false)
	resp, _ := do(t, app, http.MethodGet, "/api/events", "", nil)
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("status %d", resp.StatusCode)
	}
}
