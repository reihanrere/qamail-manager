// Package i18n localizes the `message` of API error responses.
//
// Every error carries a stable `code` and `params`; this package turns them into English
// or Indonesian text, chosen from the request's Accept-Language header. English is the
// default. The web app translates by `code` itself, so this mostly serves API clients,
// scripts and the Email Worker.
package i18n

import (
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

// Supported languages.
const (
	English    = "en"
	Indonesian = "id"
)

// Default is used when Accept-Language names no supported language.
const Default = English

var placeholder = regexp.MustCompile(`\{\{\s*(\w+)\s*\}\}`)

// detailCodes keep the original (English) detail after the localized text, because the
// detail says what the other side answered and is what an operator needs to read.
var detailCodes = map[string]bool{
	"upstream_error":  true,
	"malformed_email": true,
}

// Message returns the text for code in lang with {{params}} filled in. ok is false when the
// code has no entry, so the caller can keep its own message. detail (the original message)
// is appended for codes that need it.
func Message(lang, code string, params map[string]any, detail string) (text string, ok bool) {
	table, found := catalog[lang]
	if !found {
		table = catalog[Default]
	}
	pattern, found := table[code]
	if !found {
		return "", false
	}
	text = placeholder.ReplaceAllStringFunc(pattern, func(m string) string {
		name := placeholder.FindStringSubmatch(m)[1]
		if v, present := params[name]; present {
			return fmt.Sprint(v)
		}
		return ""
	})
	if detailCodes[code] && detail != "" {
		text += ": " + detail
	}
	return text, true
}

// Codes lists every code that has a translation, sorted. The catalogs of all languages must
// cover exactly these codes (checked by the tests).
func Codes() []string {
	codes := make([]string, 0, len(catalog[Default]))
	for code := range catalog[Default] {
		codes = append(codes, code)
	}
	sort.Strings(codes)
	return codes
}

// Languages lists the supported language codes.
func Languages() []string { return []string{English, Indonesian} }

// Negotiate picks the supported language with the highest quality value in an
// Accept-Language header ("id-ID,id;q=0.9,en;q=0.8"). Ties go to the one listed first;
// no match gives Default.
func Negotiate(header string) string {
	best, bestQ := Default, -1.0
	for _, part := range strings.Split(header, ",") {
		tag, q := parseTag(part)
		if q <= 0 {
			continue
		}
		primary, _, _ := strings.Cut(tag, "-")
		var lang string
		switch primary {
		case English:
			lang = English
		case Indonesian, "in": // "in" is the legacy code for Indonesian
			lang = Indonesian
		default:
			continue
		}
		if q > bestQ {
			best, bestQ = lang, q
		}
	}
	return best
}

func parseTag(part string) (tag string, q float64) {
	fields := strings.Split(strings.TrimSpace(part), ";")
	tag = strings.ToLower(strings.TrimSpace(fields[0]))
	q = 1
	for _, f := range fields[1:] {
		f = strings.TrimSpace(f)
		if v, ok := strings.CutPrefix(f, "q="); ok {
			parsed, err := strconv.ParseFloat(v, 64)
			if err != nil {
				return tag, 0
			}
			q = parsed
		}
	}
	return tag, q
}
