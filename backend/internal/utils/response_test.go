package utils

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gofiber/fiber/v3"
)

func errorBody(t *testing.T, acceptLanguage, code, fallback string) (map[string]any, *http.Response) {
	t.Helper()
	app := fiber.New()
	app.Get("/", func(c fiber.Ctx) error {
		return Error(c, http.StatusBadRequest, code, fallback, map[string]any{"max": 50})
	})
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	if acceptLanguage != "" {
		req.Header.Set("Accept-Language", acceptLanguage)
	}
	resp, err := app.Test(req)
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := io.ReadAll(resp.Body)
	var body map[string]any
	if err := json.Unmarshal(raw, &body); err != nil {
		t.Fatalf("bad json %q: %v", raw, err)
	}
	return body, resp
}

func TestErrorIsLocalized(t *testing.T) {
	body, resp := errorBody(t, "id-ID,id;q=0.9,en;q=0.8", "bulk_count", "raw")
	if body["message"] != "Jumlah harus antara 1 dan 50" || body["code"] != "bulk_count" {
		t.Errorf("indonesian: %v", body)
	}
	if resp.Header.Get("Content-Language") != "id" {
		t.Errorf("Content-Language = %q", resp.Header.Get("Content-Language"))
	}

	body, resp = errorBody(t, "", "bulk_count", "raw")
	if body["message"] != "Count must be between 1 and 50" {
		t.Errorf("default english: %v", body)
	}
	if resp.Header.Get("Content-Language") != "en" {
		t.Errorf("Content-Language = %q", resp.Header.Get("Content-Language"))
	}
}

func TestErrorKeepsFallbackForUnknownCode(t *testing.T) {
	body, _ := errorBody(t, "id", "not_translated", "plain fallback")
	if body["message"] != "plain fallback" {
		t.Errorf("got %v", body)
	}
}
