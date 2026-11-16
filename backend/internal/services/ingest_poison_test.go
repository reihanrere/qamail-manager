package services

import (
	"context"
	"testing"
)

// Mail whose decoded subject or body holds NUL or invalid UTF-8 must be stored, not fail with
// a database error: that error is a 500, which the Worker queues and retries for days.
func TestIngestStoresMailWithBytesPostgresRejects(t *testing.T) {
	db := testDB(t)
	s := newIntegrationService(t, db)
	ctx := context.Background()
	account, err := s.GenerateAccount(ctx, testOwnerID, GenerateAccountInput{})
	if err != nil {
		t.Fatal(err)
	}
	raw := crlfBytes("From: a@example.com\nTo: " + account.Email + "\nSubject: =?utf-8?b?AA==?=poison\nMessage-ID: <poison@example.com>\n" +
		"Content-Type: text/plain\nContent-Transfer-Encoding: base64\n\n/w==\n")
	result, err := s.IngestMessage(ctx, raw, account.Email)
	if err != nil {
		t.Fatalf("poison mail was refused: %v", err)
	}
	if result.Duplicate {
		t.Fatal("unexpected duplicate")
	}
}
