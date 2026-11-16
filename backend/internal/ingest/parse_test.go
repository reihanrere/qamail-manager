package ingest

import (
	"errors"
	"strconv"
	"strings"
	"testing"
	"unicode/utf8"
)

func crlf(s string) []byte { return []byte(strings.ReplaceAll(s, "\n", "\r\n")) }

func TestParseMultipartAlternative(t *testing.T) {
	raw := crlf(`From: "Shop Bot" <No-Reply@Shop.example>
To: Rinda <Rinda.Saputra91@inbox.example.com>
Cc: other@example.com
Subject: =?UTF-8?B?S29kZSBPVFAg8J+UkQ==?=
Message-ID: <abc123@shop.example>
MIME-Version: 1.0
Content-Type: multipart/mixed; boundary="outer"

--outer
Content-Type: multipart/alternative; boundary="inner"

--inner
Content-Type: text/plain; charset=utf-8

Use 482913 to sign in.
--inner
Content-Type: text/html; charset=utf-8
Content-Transfer-Encoding: quoted-printable

<p>Use <b>482913</b> to sign in.</p>
--inner--
--outer
Content-Type: application/pdf
Content-Disposition: attachment; filename="invoice.pdf"

JVBERi0=
--outer--
`)
	email, err := Parse(raw)
	if err != nil {
		t.Fatal(err)
	}
	if email.FromName != "Shop Bot" || email.FromAddress != "no-reply@shop.example" {
		t.Fatalf("unexpected from %q <%s>", email.FromName, email.FromAddress)
	}
	if email.Subject != "Kode OTP 🔑" {
		t.Fatalf("subject not decoded: %q", email.Subject)
	}
	if email.MessageID != "abc123@shop.example" {
		t.Fatalf("unexpected message id %q", email.MessageID)
	}
	if email.Text != "Use 482913 to sign in." || email.HTML != "<p>Use <b>482913</b> to sign in.</p>" {
		t.Fatalf("unexpected bodies %q / %q", email.Text, email.HTML)
	}
	if strings.Join(email.Recipients, ",") != "rinda.saputra91@inbox.example.com,other@example.com" {
		t.Fatalf("unexpected recipients %v", email.Recipients)
	}
}

func TestParseSinglePartLatin1AndBareDeliveredTo(t *testing.T) {
	raw := append(crlf(`Delivered-To: bagas.pratama@inbox.example.com
From: shop@example.com
Subject: Caf=?ISO-8859-1?Q?=E9?=
Content-Type: text/plain; charset=iso-8859-1

Caf`), 0xE9) // "é" in Latin-1
	email, err := Parse(raw)
	if err != nil {
		t.Fatal(err)
	}
	if email.Text != "Café" {
		t.Fatalf("charset not decoded: %q", email.Text)
	}
	if len(email.Recipients) != 1 || email.Recipients[0] != "bagas.pratama@inbox.example.com" {
		t.Fatalf("unexpected recipients %v", email.Recipients)
	}
}

func TestParseRejectsGarbage(t *testing.T) {
	if _, err := Parse([]byte("\x00\x01 not an email")); !errors.Is(err, ErrMalformed) {
		t.Fatalf("expected ErrMalformed, got %v", err)
	}
}

const withAttachments = `From: shop@example.com
To: ayu.putra@inbox.example.com
Subject: Invoice
Content-Type: multipart/mixed; boundary="mix"

--mix
Content-Type: multipart/related; boundary="rel"

--rel
Content-Type: text/html; charset=utf-8

<p>Logo: <img src="cid:logo"></p>
--rel
Content-Type: image/png
Content-ID: <logo>
Content-Disposition: inline
Content-Transfer-Encoding: base64

iVBORw0KGgo=
--rel--
--mix
Content-Type: application/pdf; name="invoice.pdf"
Content-Disposition: attachment; filename="invoice.pdf"
Content-Transfer-Encoding: base64

JVBERi0xLjQK
--mix
Content-Type: text/plain; charset=utf-8
Content-Disposition: attachment; filename="notes.txt"

hello attachment
--mix--
`

func TestParseAttachments(t *testing.T) {
	email, err := Parse(crlf(withAttachments))
	if err != nil {
		t.Fatal(err)
	}
	if email.HTML == "" || email.Text != "" {
		t.Fatalf("expected only the HTML body, got text=%q html=%q", email.Text, email.HTML)
	}
	want := []Attachment{
		{Index: 1, Filename: "attachment-1.png", ContentType: "image/png", Size: 8, ContentID: "logo"},
		{Index: 2, Filename: "invoice.pdf", ContentType: "application/pdf", Size: 9},
		{Index: 3, Filename: "notes.txt", ContentType: "text/plain", Size: 16},
	}
	if len(email.Attachments) != len(want) {
		t.Fatalf("got %+v", email.Attachments)
	}
	for i := range want {
		if email.Attachments[i] != want[i] {
			t.Errorf("attachment %d: got %+v, want %+v", i, email.Attachments[i], want[i])
		}
	}
}

func TestExtractAttachment(t *testing.T) {
	raw := crlf(withAttachments)
	meta, data, err := ExtractAttachment(raw, 2)
	if err != nil {
		t.Fatal(err)
	}
	if meta.Filename != "invoice.pdf" || string(data) != "%PDF-1.4\n" {
		t.Fatalf("got %+v %q", meta, data)
	}
	_, data, err = ExtractAttachment(raw, 3)
	if err != nil || string(data) != "hello attachment" {
		t.Fatalf("got %q, %v", data, err)
	}
	if _, _, err := ExtractAttachment(raw, 4); !errors.Is(err, ErrAttachmentNotFound) {
		t.Fatalf("expected ErrAttachmentNotFound, got %v", err)
	}
}

// nestedMultipart builds levels of multipart/mixed inside each other, ending in a text leaf.
func nestedMultipart(levels int) []byte {
	var b strings.Builder
	b.WriteString("From: a@example.com\r\nSubject: nested\r\nMIME-Version: 1.0\r\n")
	for i := 0; i < levels; i++ {
		if i == 0 {
			b.WriteString("Content-Type: multipart/mixed; boundary=\"b0\"\r\n\r\n")
		} else {
			b.WriteString("Content-Type: multipart/mixed; boundary=\"b" + strconv.Itoa(i) + "\"\r\n\r\n")
		}
		b.WriteString("--b" + strconv.Itoa(i) + "\r\n")
	}
	b.WriteString("Content-Type: text/plain\r\n\r\nhello\r\n")
	return []byte(b.String())
}

// A mail with tens of thousands of nested multiparts makes the MIME reader keep one buffered
// reader per level and re-scan the body through all of them, so memory and CPU explode.
// Parse must refuse it before the reader runs.
func TestParseRefusesDeeplyNestedMultipart(t *testing.T) {
	for _, levels := range []int{DefaultMaxParts + 1, 20000} {
		_, err := Parse(nestedMultipart(levels))
		if !errors.Is(err, ErrMalformed) {
			t.Fatalf("%d levels: want ErrMalformed, got %v", levels, err)
		}
	}
	if _, err := Parse(nestedMultipart(10)); err != nil {
		t.Fatalf("10 levels are ordinary: %v", err)
	}
}

func TestParseRefusesTooManyParts(t *testing.T) {
	var b strings.Builder
	b.WriteString("From: a@example.com\r\nSubject: flat\r\nContent-Type: multipart/mixed; boundary=\"x\"\r\n\r\n")
	for i := 0; i < 50; i++ {
		b.WriteString("--x\r\nContent-Type: text/plain\r\n\r\npart\r\n")
	}
	b.WriteString("--x--\r\n")
	raw := []byte(b.String())

	if _, err := ParseWithLimit(raw, 20); !errors.Is(err, ErrMalformed) {
		t.Fatalf("50 parts under a limit of 20: want ErrMalformed, got %v", err)
	}
	if _, err := ParseWithLimit(raw, 100); err != nil {
		t.Fatalf("50 parts under a limit of 100: %v", err)
	}
}

func TestParseLimitCountsMultipartCaseInsensitively(t *testing.T) {
	raw := []byte("From: a@example.com\r\nSubject: s\r\nContent-Type: MULTIPART/Mixed; boundary=\"x\"\r\n\r\n--x\r\nContent-Type: text/plain\r\n\r\nhi\r\n--x--\r\n")
	if _, err := ParseWithLimit(raw, 1); err != nil {
		t.Fatalf("one multipart under a limit of 1: %v", err)
	}
	two := append([]byte("X-Note: multipart/related\r\n"), raw...)
	if _, err := ParseWithLimit(two, 1); !errors.Is(err, ErrMalformed) {
		t.Fatalf("two mentions under a limit of 1: want ErrMalformed, got %v", err)
	}
}

// Postgres text columns reject NUL and invalid UTF-8. A sender must not be able to make the
// insert fail (HTTP 500, which the Worker retries), so Parse cleans every stored string.
func TestParseStripsBytesPostgresRejects(t *testing.T) {
	raw := crlf(`From: =?utf-8?b?AGJvYg==?= <bob@example.com>
To: rinda@inbox.example.com
Subject: =?utf-8?b?AA==?=hi
Message-ID: <p1@example.com>
Content-Type: multipart/mixed; boundary="b"

--b
Content-Type: text/plain

plain
--b
Content-Type: text/html
Content-Transfer-Encoding: base64

/w==
--b
Content-Type: application/pdf; name="a.pdf"
Content-Disposition: attachment; filename="=?utf-8?b?AGEucGRm?="

JVBERi0=
--b--
`)
	email, err := Parse(raw)
	if err != nil {
		t.Fatal(err)
	}
	fields := map[string]string{
		"subject": email.Subject, "from name": email.FromName, "text": email.Text, "html": email.HTML,
		"filename": email.Attachments[0].Filename,
	}
	for name, value := range fields {
		if strings.ContainsRune(value, 0) || !utf8.ValidString(value) {
			t.Errorf("%s still holds NUL or invalid UTF-8: %q", name, value)
		}
	}
	if email.Subject != "hi" {
		t.Errorf("subject = %q, want the readable part kept", email.Subject)
	}
}
