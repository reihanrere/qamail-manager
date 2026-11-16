package handlers_test

import (
	"net/http"
	"net/url"
	"testing"
)

const loginJSON = `{"email":"alice@example.com","password":"` + password + `"}`

// A cross-site page can make the victim's browser POST a form to /api/auth/login with the
// attacker's own credentials, which would swap the victim's session for the attacker's
// (login CSRF). Forms cannot send JSON, and the Origin check closes the rest.
func TestLoginRefusesCrossSiteRequests(t *testing.T) {
	cases := []struct {
		name       string
		body       string
		header     map[string]string
		wantStatus int
		wantCode   string
	}{
		{"urlencoded form", "email=alice%40example.com&password=" + url.QueryEscape(password),
			map[string]string{"Content-Type": "application/x-www-form-urlencoded", "Origin": "http://evil.example"}, http.StatusBadRequest, "invalid_request_body"},
		{"urlencoded form without origin", "email=alice%40example.com&password=" + url.QueryEscape(password),
			map[string]string{"Content-Type": "application/x-www-form-urlencoded"}, http.StatusBadRequest, "invalid_request_body"},
		{"multipart form", "--b\r\nContent-Disposition: form-data; name=\"email\"\r\n\r\nalice@example.com\r\n--b--\r\n",
			map[string]string{"Content-Type": "multipart/form-data; boundary=b"}, http.StatusBadRequest, "invalid_request_body"},
		// <form enctype="text/plain"> can be made to produce a JSON-looking body
		{"text/plain json body", loginJSON,
			map[string]string{"Content-Type": "text/plain", "Origin": "http://evil.example"}, http.StatusBadRequest, "invalid_request_body"},
		{"json from another origin", loginJSON,
			map[string]string{"Origin": "http://evil.example"}, http.StatusForbidden, "cross_origin_request"},
		{"json from opaque origin", loginJSON,
			map[string]string{"Origin": "null"}, http.StatusForbidden, "cross_origin_request"},
		{"json from same host other port", loginJSON,
			map[string]string{"Origin": "http://example.com:9999"}, http.StatusForbidden, "cross_origin_request"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			app, _ := newApp(t, false)
			header := map[string]string{"X-Real-IP": "10.0.0.9"}
			for k, v := range tc.header {
				header[k] = v
			}
			resp, body := do(t, app, http.MethodPost, "/api/auth/login", tc.body, header)
			if resp.StatusCode != tc.wantStatus || errCode(t, body) != tc.wantCode {
				t.Fatalf("status %d: %s", resp.StatusCode, body)
			}
			if sessionCookie(resp) != nil {
				t.Fatal("a refused login must not set a session cookie")
			}
		})
	}
}

func TestLoginAcceptsSameOriginAndListedOrigins(t *testing.T) {
	app, _ := newAppWithOrigins(t, false, []string{"http://localhost:5173"})
	for name, origin := range map[string]string{
		"no origin (curl, tests)": "",
		"same origin":             "http://example.com", // httptest's default Host
		"listed CORS origin":      "http://localhost:5173",
	} {
		t.Run(name, func(t *testing.T) {
			header := map[string]string{"X-Real-IP": "10.0.0.8"}
			if origin != "" {
				header["Origin"] = origin
			}
			resp, body := do(t, app, http.MethodPost, "/api/auth/login", loginJSON, header)
			if resp.StatusCode != http.StatusOK || sessionCookie(resp) == nil {
				t.Fatalf("status %d: %s", resp.StatusCode, body)
			}
		})
	}
	// Content-Type may carry parameters
	resp, body := do(t, app, http.MethodPost, "/api/auth/login", loginJSON,
		map[string]string{"Content-Type": "application/json; charset=utf-8", "X-Real-IP": "10.0.0.7"})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("charset parameter: %d %s", resp.StatusCode, body)
	}
}
