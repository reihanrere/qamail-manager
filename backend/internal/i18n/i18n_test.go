package i18n

import (
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
)

func keys(m map[string]string) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func TestEveryLanguageCoversTheSameCodes(t *testing.T) {
	want := keys(catalog[English])
	for _, lang := range Languages() {
		got := keys(catalog[lang])
		if strings.Join(got, ",") != strings.Join(want, ",") {
			t.Errorf("%s catalog differs from %s:\n got  %v\n want %v", lang, English, got, want)
		}
		for code, text := range catalog[lang] {
			if strings.TrimSpace(text) == "" {
				t.Errorf("%s/%s is empty", lang, code)
			}
		}
	}
}

func TestPlaceholdersMatchAcrossLanguages(t *testing.T) {
	names := func(s string) string {
		var out []string
		for _, m := range placeholder.FindAllStringSubmatch(s, -1) {
			out = append(out, m[1])
		}
		sort.Strings(out)
		return strings.Join(out, ",")
	}
	for code, en := range catalog[English] {
		if id := catalog[Indonesian][code]; names(en) != names(id) {
			t.Errorf("%s: placeholders differ: en=%q id=%q", code, en, id)
		}
	}
}

// Every error code the backend can emit must have a translation. Codes are string literals
// in the handlers, the services and the validation helper, so read them from the source.
func TestEveryEmittedCodeIsTranslated(t *testing.T) {
	literal := regexp.MustCompile(`(?:invalid\(|utils\.Error\([^"\n]*?,\s*|\bcode[A-Za-z]* += )"([a-z]+(?:_[a-z]+)*)"`)
	var found = map[string]bool{}
	for _, dir := range []string{"../services", "../handlers"} {
		files, err := filepath.Glob(filepath.Join(dir, "*.go"))
		if err != nil {
			t.Fatal(err)
		}
		for _, f := range files {
			if strings.HasSuffix(f, "_test.go") {
				continue
			}
			src, err := os.ReadFile(f)
			if err != nil {
				t.Fatal(err)
			}
			for _, m := range literal.FindAllStringSubmatch(string(src), -1) {
				found[m[1]] = true
			}
		}
	}
	if len(found) < 20 {
		t.Fatalf("scan found only %d codes; the pattern is probably stale", len(found))
	}
	for code := range found {
		if _, ok := catalog[English][code]; !ok {
			t.Errorf("code %q is emitted but has no translation", code)
		}
	}
}

func TestMessage(t *testing.T) {
	got, ok := Message(Indonesian, "quota_exceeded", map[string]any{"used": 3, "limit": 3, "remaining": 0, "requested": 1}, "")
	if !ok || got != "Kuota akun tercapai: 3 dari 3 terpakai, sisa 0, diminta 1" {
		t.Errorf("got %q ok=%v", got, ok)
	}
	if got, _ := Message(English, "upstream_error", nil, "mail.tm said 500"); got != "The mail service failed to answer: mail.tm said 500" {
		t.Errorf("detail not kept: %q", got)
	}
	if got, _ := Message(English, "internal_error", nil, "pq: secret detail"); strings.Contains(got, "secret") {
		t.Errorf("internal detail leaked: %q", got)
	}
	if _, ok := Message(English, "no_such_code", nil, ""); ok {
		t.Error("unknown code must report ok=false")
	}
	if got, _ := Message("fr", "invalid_email", nil, ""); got != "That is not a valid email address" {
		t.Errorf("unknown language should fall back to English, got %q", got)
	}
}

func TestNegotiate(t *testing.T) {
	cases := map[string]string{
		"":                        English,
		"id":                      Indonesian,
		"id-ID,id;q=0.9,en;q=0.8": Indonesian,
		"en-US,en;q=0.9,id;q=0.8": English,
		"fr-FR,fr;q=0.9":          English,
		"fr, id;q=0.5":            Indonesian,
		"id;q=0, en":              English,
		"en;q=0.5, id;q=0.9":      Indonesian,
		"*":                       English,
		"in-ID":                   Indonesian,
		"id;q=abc":                English,
	}
	for header, want := range cases {
		if got := Negotiate(header); got != want {
			t.Errorf("Negotiate(%q) = %s, want %s", header, got, want)
		}
	}
}
