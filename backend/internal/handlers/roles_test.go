package handlers_test

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
)

// Settings may be read by every user but changed only by an admin.
func TestSettingsPatchAdminOnly(t *testing.T) {
	e := newIsolationEnv(t)
	const patch = `{"values":{"bulkGenerateMax":4}}`

	resp, body := do(t, e.app, http.MethodPatch, "/api/settings", patch, e.alice)
	if resp.StatusCode != http.StatusForbidden || !strings.Contains(body, `"admin_required"`) {
		t.Fatalf("user PATCH /api/settings: %d %s", resp.StatusCode, body)
	}
	resp, body = do(t, e.app, http.MethodPatch, "/api/settings", patch, e.admin)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("admin PATCH /api/settings: %d %s", resp.StatusCode, body)
	}
	resp, body = do(t, e.app, http.MethodPatch, "/api/settings", patch, nil)
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("anonymous PATCH /api/settings: %d %s", resp.StatusCode, body)
	}
}

// A non-admin sees only the settings the UI needs; the admin sees all of them.
func TestSettingsGetHidesAdminValues(t *testing.T) {
	e := newIsolationEnv(t)
	read := func(as map[string]string) (editable, defaults map[string]any, sending bool) {
		resp, body := do(t, e.app, http.MethodGet, "/api/settings", "", as)
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("GET /api/settings: %d %s", resp.StatusCode, body)
		}
		var out struct {
			Data struct {
				Editable map[string]any `json:"editable"`
				Defaults map[string]any `json:"defaults"`
				Inbox    struct {
					SendingEnabled bool `json:"sendingEnabled"`
				} `json:"inbox"`
			} `json:"data"`
		}
		if err := json.Unmarshal([]byte(body), &out); err != nil {
			t.Fatal(err)
		}
		return out.Data.Editable, out.Data.Defaults, out.Data.Inbox.SendingEnabled
	}

	userEditable, userDefaults, _ := read(e.alice)
	for _, key := range []string{"legacyUsernamePattern", "usernameFirstNames", "inboxSyncInterval", "messageRetention"} {
		if _, ok := userEditable[key]; ok {
			t.Errorf("non-admin sees %q", key)
		}
	}
	if userEditable["bulkGenerateMax"] == nil || userEditable["userAccountQuota"] == nil {
		t.Errorf("non-admin misses a setting the UI needs: %v", userEditable)
	}
	if len(userDefaults) != 0 {
		t.Errorf("non-admin sees defaults: %v", userDefaults)
	}
	adminEditable, adminDefaults, _ := read(e.admin)
	if adminEditable["legacyUsernamePattern"] == nil || len(adminDefaults) != len(adminEditable) {
		t.Errorf("admin view is incomplete: %v / %v", adminEditable, adminDefaults)
	}
}

// Sending email is for admins; the role check comes before the account lookup.
func TestSendMessageAdminOnly(t *testing.T) {
	e := newIsolationEnv(t)
	aliceID, _ := e.generate(t, e.alice, "mine")
	const send = `{"to":["x@example.com"],"subject":"s","text":"t"}`

	resp, body := do(t, e.app, http.MethodPost, "/api/accounts/"+aliceID+"/messages/send", send, e.alice)
	if resp.StatusCode != http.StatusForbidden || !strings.Contains(body, `"admin_required"`) {
		t.Fatalf("user send from own account: %d %s", resp.StatusCode, body)
	}
	// The admin passes the role check; there is no SMTP server in the test, so the send
	// is refused for that reason and not with 403.
	resp, body = do(t, e.app, http.MethodPost, "/api/accounts/"+aliceID+"/messages/send", send, e.admin)
	if resp.StatusCode == http.StatusForbidden {
		t.Fatalf("admin send was refused as forbidden: %s", body)
	}
}

// The quota is a 403 with the numbers, and stats report it.
func TestQuotaOverHTTP(t *testing.T) {
	e := newIsolationEnv(t)
	if resp, body := do(t, e.app, http.MethodPatch, "/api/settings", `{"values":{"userAccountQuota":2}}`, e.admin); resp.StatusCode != http.StatusOK {
		t.Fatalf("set quota: %d %s", resp.StatusCode, body)
	}
	e.generate(t, e.alice, "1")
	e.generate(t, e.alice, "2")

	resp, body := do(t, e.app, http.MethodPost, "/api/accounts/generate", `{"tag":"3"}`, e.alice)
	if resp.StatusCode != http.StatusForbidden || !strings.Contains(body, `"quota_exceeded"`) {
		t.Fatalf("generate over quota: %d %s", resp.StatusCode, body)
	}
	resp, body = do(t, e.app, http.MethodPost, "/api/accounts/generate/bulk", `{"count":2}`, e.alice)
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("bulk over quota: %d %s", resp.StatusCode, body)
	}
	// Bob has his own allowance, the admin has none
	e.generate(t, e.bob, "b")
	for i := 0; i < 3; i++ {
		e.generate(t, e.admin, "a")
	}

	_, body = do(t, e.app, http.MethodGet, "/api/accounts/stats", "", e.alice)
	var stats struct {
		Data struct {
			Quota struct {
				Used      int  `json:"used"`
				Limit     *int `json:"limit"`
				Remaining *int `json:"remaining"`
			} `json:"quota"`
		} `json:"data"`
	}
	if err := json.Unmarshal([]byte(body), &stats); err != nil {
		t.Fatal(err)
	}
	q := stats.Data.Quota
	if q.Used != 2 || q.Limit == nil || *q.Limit != 2 || q.Remaining == nil || *q.Remaining != 0 {
		t.Errorf("alice quota = %+v, want used 2 limit 2 remaining 0 (%s)", q, body)
	}
	_, body = do(t, e.app, http.MethodGet, "/api/accounts/stats", "", e.admin)
	if !strings.Contains(body, `"limit":null`) {
		t.Errorf("admin quota has a limit: %s", body)
	}
}
