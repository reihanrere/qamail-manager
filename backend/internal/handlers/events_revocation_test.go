package handlers_test

import (
	"bufio"
	"net"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/gofiber/fiber/v3"
)

// An open event stream must stop delivering once its session ends, however it ends:
// logout, password reset or disabling the user (the CLI runs in another process, so the
// stream can only notice through the database).
func TestEventStreamEndsWhenSessionIsRevoked(t *testing.T) {
	revokers := map[string]func(t *testing.T, e *isolationEnv){
		"logout": func(t *testing.T, e *isolationEnv) {
			if resp, body := do(t, e.app, http.MethodPost, "/api/auth/logout", "", e.alice); resp.StatusCode != http.StatusOK {
				t.Fatalf("logout: %s", body)
			}
		},
		"passwd": func(t *testing.T, e *isolationEnv) {
			if err := e.users.SetPassword(t.Context(), "alice@example.com", "another-long-password"); err != nil {
				t.Fatal(err)
			}
		},
		"api-disable": func(t *testing.T, e *isolationEnv) {
			id := userIDOf(t, e, "alice@example.com")
			if resp, body := do(t, e.app, http.MethodPatch, "/api/users/"+id, `{"disabled":true}`, e.admin); resp.StatusCode != http.StatusOK {
				t.Fatalf("PATCH /api/users: %s", body)
			}
		},
		"api-reset": func(t *testing.T, e *isolationEnv) {
			id := userIDOf(t, e, "alice@example.com")
			if resp, body := do(t, e.app, http.MethodPatch, "/api/users/"+id, `{"password":"another-long-password"}`, e.admin); resp.StatusCode != http.StatusOK {
				t.Fatalf("PATCH /api/users: %s", body)
			}
		},
		"api-delete": func(t *testing.T, e *isolationEnv) {
			id := userIDOf(t, e, "alice@example.com")
			if resp, body := do(t, e.app, http.MethodDelete, "/api/users/"+id+"?force=true", "", e.admin); resp.StatusCode != http.StatusOK {
				t.Fatalf("DELETE /api/users: %s", body)
			}
		},
		"disable": func(t *testing.T, e *isolationEnv) {
			if err := e.users.SetDisabled(t.Context(), "alice@example.com", true); err != nil {
				t.Fatal(err)
			}
		},
	}
	for name, revoke := range revokers {
		t.Run(name, func(t *testing.T) {
			e := newIsolationEnv(t)
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

			lines := make(chan string, 64)
			go func() {
				scanner := bufio.NewScanner(resp.Body)
				for scanner.Scan() {
					lines <- scanner.Text()
				}
				close(lines)
			}()
			select {
			case line := <-lines:
				if !strings.Contains(line, "connected") {
					t.Fatalf("first line: %q", line)
				}
			case <-time.After(5 * time.Second):
				t.Fatal("stream never connected")
			}

			revoke(t, e)
			// Deleting the user also removes the address, so a refused ingest is fine here
			raw := strings.NewReplacer("{{TO}}", aliceEmail, "{{ID}}", "after-revoke").Replace(rawEmail)
			_, _ = e.accounts.IngestMessage(t.Context(), []byte(raw), aliceEmail)

			// The stream must close without ever carrying the new mail
			deadline := time.After(5 * time.Second)
			for {
				select {
				case line, ok := <-lines:
					if !ok {
						return
					}
					if strings.Contains(line, aliceEmail) || strings.Contains(line, "message.created") {
						t.Fatalf("revoked stream still delivered: %q", line)
					}
				case <-deadline:
					t.Fatal("revoked stream stayed open")
				}
			}
		})
	}
}
