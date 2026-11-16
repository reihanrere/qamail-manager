package config

import (
	"strings"
	"testing"
	"time"
)

func validConfig() Config {
	return Config{
		AppPort:                    "8080",
		InboxSearchMaxPages:        10,
		IngestPort:                 "8081",
		IngestMaxBytes:             1 << 20,
		IngestMaxMIMEParts:         200,
		LoginVerifyWait:            time.Second,
		LoginMaxConcurrentVerifies: 4,
		MessageRetention:           time.Hour,
		MessagePruneInterval:       time.Minute,
		UsernameMaxAttempts:        5,
		BulkGenerateMax:            50,
		SMTPSecurity:               "starttls",
		TagMaxLength:               50,
		NoteMaxLength:              500,
		EventsHeartbeatInterval:    20 * time.Second,
		LegacyUsernamePattern:      "^old_prefix_",
		PasswordMinLength:          10,
		SessionTTL:                 time.Hour,
		LoginMaxAttempts:           5,
		LoginWindow:                time.Minute,
		UserAccountQuota:           10,
		ClientIPHeader:             "X-Real-IP",
	}
}

func TestValidate(t *testing.T) {
	tests := []struct {
		name    string
		mutate  func(*Config)
		wantErr string
	}{
		{name: "valid", mutate: func(*Config) {}},
		{name: "zero limit", mutate: func(c *Config) { c.TagMaxLength = 0 }, wantErr: "TAG_MAX_LENGTH"},
		{name: "short secret", mutate: func(c *Config) { c.IngestSecret = "short" }, wantErr: "INGEST_SECRET"},
		{name: "catch-all domain without secret", mutate: func(c *Config) { c.CatchallDomains = []string{"inbox.example.com"} }, wantErr: "INGEST_SECRET is required"},
		{name: "catch-all domain with secret", mutate: func(c *Config) {
			c.CatchallDomains = []string{"inbox.example.com"}
			c.IngestSecret = strings.Repeat("s", 32)
		}},
		{name: "same ports", mutate: func(c *Config) {
			c.IngestSecret = strings.Repeat("s", 32)
			c.IngestPort = "8080"
		}, wantErr: "INGEST_PORT"},
		{name: "zero prune interval", mutate: func(c *Config) { c.MessagePruneInterval = 0 }, wantErr: "MESSAGE_PRUNE_INTERVAL"},
		{name: "bad legacy pattern", mutate: func(c *Config) { c.LegacyUsernamePattern = "(" }, wantErr: "LEGACY_USERNAME_PATTERN"},
		{name: "zero quota", mutate: func(c *Config) { c.UserAccountQuota = 0 }, wantErr: "USER_ACCOUNT_QUOTA"},
		{name: "zero login attempts", mutate: func(c *Config) { c.LoginMaxAttempts = 0 }, wantErr: "LOGIN_MAX_ATTEMPTS"},
		{name: "zero session ttl", mutate: func(c *Config) { c.SessionTTL = 0 }, wantErr: "SESSION_TTL"},
		{name: "zero login window", mutate: func(c *Config) { c.LoginWindow = 0 }, wantErr: "LOGIN_WINDOW"},
		{name: "weak min length", mutate: func(c *Config) { c.PasswordMinLength = 4 }, wantErr: "PASSWORD_MIN_LENGTH"},
		{name: "admin email without password", mutate: func(c *Config) { c.AdminEmail = "a@example.com" }, wantErr: "set together"},
		{name: "admin password without email", mutate: func(c *Config) { c.AdminPassword = "long-enough-password" }, wantErr: "set together"},
		{name: "bad admin email", mutate: func(c *Config) {
			c.AdminEmail, c.AdminPassword = "not-an-email", "long-enough-password"
		}, wantErr: "ADMIN_EMAIL"},
		{name: "short admin password", mutate: func(c *Config) {
			c.AdminEmail, c.AdminPassword = "a@example.com", "short"
		}, wantErr: "ADMIN_PASSWORD"},
		{name: "valid admin", mutate: func(c *Config) {
			c.AdminEmail, c.AdminPassword = "a@example.com", "long-enough-password"
		}},
		{name: "wildcard cors origin", mutate: func(c *Config) { c.CORSAllowedOrigins = []string{"*"} }, wantErr: "CORS_ALLOWED_ORIGINS"},
		{name: "cors origin without scheme", mutate: func(c *Config) { c.CORSAllowedOrigins = []string{"localhost:5173"} }, wantErr: "CORS_ALLOWED_ORIGINS"},
		{name: "valid cors origin", mutate: func(c *Config) { c.CORSAllowedOrigins = []string{"http://localhost:5173"} }},
		{name: "bad trusted proxy", mutate: func(c *Config) { c.TrustedProxies = []string{"nginx"} }, wantErr: "TRUSTED_PROXIES"},
		{name: "valid trusted proxies", mutate: func(c *Config) { c.TrustedProxies = []string{"10.0.0.0/8", "127.0.0.1"} }},
		{name: "empty client ip header", mutate: func(c *Config) { c.ClientIPHeader = "" }, wantErr: "CLIENT_IP_HEADER"},
		{name: "zero heartbeat", mutate: func(c *Config) { c.EventsHeartbeatInterval = 0 }, wantErr: "EVENTS_HEARTBEAT_INTERVAL"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := validConfig()
			tt.mutate(&c)
			err := c.Validate()
			if tt.wantErr == "" {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("expected error mentioning %s, got %v", tt.wantErr, err)
			}
		})
	}
}

func TestSplitList(t *testing.T) {
	got := SplitList(" Budi, ,SARI ,dewi,")
	if strings.Join(got, "|") != "budi|sari|dewi" {
		t.Fatalf("got %v", got)
	}
	if SplitList("") != nil {
		t.Fatal("expected nil for empty input")
	}
}

func TestSplitOrigins(t *testing.T) {
	got := splitOrigins(" http://localhost:5173/ , ,https://App.example.com")
	if strings.Join(got, "|") != "http://localhost:5173|https://App.example.com" {
		t.Fatalf("got %v", got)
	}
}
