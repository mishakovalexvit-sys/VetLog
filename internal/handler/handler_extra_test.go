package handler

import (
	"io/fs"
	"net/http"
	"regexp"
	"strings"
	"testing"

	"vetlog/internal/i18n"
	"vetlog/internal/lead"
	"vetlog/web"
)

func TestPrivacyRendersPolicy(t *testing.T) {
	tests := []struct {
		path, wantText, wantLang string
	}{
		{"/privacy", "1. Какие данные мы получаем", "ru"},
		{"/en/privacy", "1. What data we collect", "en"},
	}
	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			env := newTestEnv(t, nil)
			rec := do(t, env.mux, http.MethodGet, tt.path, nil, "")
			if rec.Code != http.StatusOK {
				t.Fatalf("status = %d, want 200", rec.Code)
			}
			body := rec.Body.String()
			if !strings.Contains(body, tt.wantText) {
				t.Errorf("policy text %q not rendered", tt.wantText)
			}
			if strings.Contains(body, "в подготовке") || strings.Contains(body, "being prepared") {
				t.Error("stub shown although policy exists")
			}
			if !strings.Contains(body, `<html lang="`+tt.wantLang+`">`) {
				t.Errorf("html lang %q not set", tt.wantLang)
			}
		})
	}
}

func TestIndexLinksToPrivacy(t *testing.T) {
	tests := []struct {
		path, want string
	}{
		{"/", `href="/privacy"`},
		{"/en/", `href="/en/privacy"`},
	}
	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			env := newTestEnv(t, nil)
			body := do(t, env.mux, http.MethodGet, tt.path, nil, "").Body.String()
			if !strings.Contains(body, tt.want) {
				t.Errorf("page misses %q", tt.want)
			}
		})
	}
}

func TestIndexContainsCSRFAndNoBookingPromises(t *testing.T) {
	env := newTestEnv(t, nil)
	body := do(t, env.mux, http.MethodGet, "/", nil, "").Body.String()
	if !strings.Contains(body, `name="csrf_token"`) {
		t.Error("form has no csrf_token field")
	}
	if !strings.Contains(body, `name="website"`) {
		t.Error("honeypot field missing")
	}
	for _, bad := range []string{"напоминан", "reminder"} {
		if strings.Contains(strings.ToLower(body), bad) {
			t.Errorf("page promises reminders: %q", bad)
		}
	}
}

var tplKeyRe = regexp.MustCompile(`\.T "([^"]+)"`)

// TestTemplateKeysExist: каждый ключ, который используют шаблоны и обработчик,
// должен быть в обоих словарях. Иначе на странице появится сырой ключ.
func TestTemplateKeysExist(t *testing.T) {
	bundle, err := i18n.Load()
	if err != nil {
		t.Fatal(err)
	}
	keys := map[string]bool{}
	for _, name := range []string{"index.html", "privacy.html"} {
		raw, err := fs.ReadFile(web.FS, "templates/"+name)
		if err != nil {
			t.Fatal(err)
		}
		for _, m := range tplKeyRe.FindAllStringSubmatch(string(raw), -1) {
			keys[m[1]] = true
		}
	}
	// Ключи, которые передаёт обработчик (не видны регэкспу, потому что значения динамические).
	for _, code := range lead.ErrorCodes {
		keys["err."+code] = true
	}
	for _, k := range []string{"form.error.csrf", "form.error.rate_limit", "form.error.generic", "err.server"} {
		keys[k] = true
	}
	for _, lang := range []string{"ru", "en"} {
		for k := range keys {
			if !bundle.Has(lang, k) {
				t.Errorf("%s: key %q used by templates/handler is missing in YAML", lang, k)
			}
		}
	}
}
