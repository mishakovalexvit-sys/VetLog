package handler

import (
	"context"
	"errors"
	"html/template"
	"io"
	"io/fs"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"vetlog/internal/i18n"
	"vetlog/internal/lead"
	"vetlog/internal/mail"
	"vetlog/web"
)

type fakeStore struct {
	calls int
	err   error
}

func (f *fakeStore) Create(_ context.Context, _ lead.Lead, _ lead.Consent) (int64, error) {
	f.calls++
	return 1, f.err
}

type fakeNotifier struct{ sent []mail.Message }

func (f *fakeNotifier) Enqueue(m mail.Message) bool {
	f.sent = append(f.sent, m)
	return true
}

type testEnv struct {
	mux   http.Handler
	store *fakeStore
	notes *fakeNotifier
}

func newTestEnv(t *testing.T, storeErr error) *testEnv {
	t.Helper()
	bundle, err := i18n.Load()
	if err != nil {
		t.Fatal(err)
	}
	tpl := template.Must(template.ParseFS(web.FS, "templates/*.html"))
	static, err := fs.Sub(web.FS, "static")
	if err != nil {
		t.Fatal(err)
	}
	env := &testEnv{store: &fakeStore{err: storeErr}, notes: &fakeNotifier{}}
	h := New(Deps{
		Bundle:    bundle,
		Templates: tpl,
		Store:     env.store,
		Notifier:  env.notes,
		Log:       slog.New(slog.NewTextHandler(io.Discard, nil)),
		BaseURL:   "https://example.test",
	})
	mux := http.NewServeMux()
	h.Register(mux, static, func(context.Context) error { return nil })
	env.mux = mux
	return env
}

func do(t *testing.T, h http.Handler, method, path string, form url.Values, token string) *httptest.ResponseRecorder {
	t.Helper()
	var body io.Reader
	if form != nil {
		body = strings.NewReader(form.Encode())
	}
	req := httptest.NewRequest(method, path, body)
	if form != nil {
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	}
	if token != "" {
		req.AddCookie(&http.Cookie{Name: csrfCookie, Value: token})//nolint:gosec // тестовая cookie
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func tokenFor(t *testing.T, h http.Handler) string {
	t.Helper()
	for _, c := range do(t, h, http.MethodGet, "/", nil, "").Result().Cookies() {
		if c.Name == csrfCookie {
			return c.Value
		}
	}
	t.Fatal("csrf cookie not set")
	return ""
}

func validForm(token string) url.Values {
	return url.Values{
		"csrf_token": {token},
		"name":       {"Анна Смирнова"},
		"phone":      {"+7 (918) 000-00-00"},
		"clinic":     {"Ветклиника Лапа"},
		"consent":    {"on"},
	}
}

func TestIndex(t *testing.T) {
	tests := []struct {
		path, lang, action string
	}{
		{"/", "ru", `action="/lead"`},
		{"/en/", "en", `action="/en/lead"`},
	}
	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			env := newTestEnv(t, nil)
			rec := do(t, env.mux, http.MethodGet, tt.path, nil, "")
			if rec.Code != http.StatusOK {
				t.Fatalf("status = %d", rec.Code)
			}
			body := rec.Body.String()
			for _, want := range []string{
				`<html lang="` + tt.lang + `">`,
				tt.action,
				`hreflang="ru" href="https://example.test/"`,
				`hreflang="en" href="https://example.test/en/"`,
			} {
				if !strings.Contains(body, want) {
					t.Errorf("body misses %q", want)
				}
			}
		})
	}
}

func TestSubmitValidationMessagesByLanguage(t *testing.T) {
	tests := []struct {
		name     string
		path     string
		mutate   func(url.Values)
		wantText string
	}{
		{"ru no consent", "/lead", func(v url.Values) { delete(v, "consent") }, "Подтвердите согласие на обработку данных."},
		{"en bad phone", "/en/lead", func(v url.Values) { v.Set("phone", "abc") }, "Please check the phone number."},
		{"en bad email", "/en/lead", func(v url.Values) { v.Set("email", "bad") }, "Please check the email address."},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			env := newTestEnv(t, nil)
			token := tokenFor(t, env.mux)
			form := validForm(token)
			tt.mutate(form)

			rec := do(t, env.mux, http.MethodPost, tt.path, form, token)
			if rec.Code != http.StatusUnprocessableEntity {
				t.Fatalf("status = %d, want 422", rec.Code)
			}
			if !strings.Contains(rec.Body.String(), tt.wantText) {
				t.Errorf("body misses %q", tt.wantText)
			}
			if env.store.calls != 0 {
				t.Errorf("invalid form must not be stored")
			}
		})
	}
}

func TestSubmitRejectsBadCSRF(t *testing.T) {
	tests := []struct {
		name   string
		cookie string
	}{
		{"no cookie", ""},
		{"mismatched token", "other-token"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			env := newTestEnv(t, nil)
			form := validForm(tokenFor(t, env.mux))
			rec := do(t, env.mux, http.MethodPost, "/lead", form, tt.cookie)
			if rec.Code != http.StatusForbidden {
				t.Fatalf("status = %d, want 403", rec.Code)
			}
			if env.store.calls != 0 {
				t.Fatal("request without valid CSRF must not be stored")
			}
		})
	}
}

func TestHoneypotSilentlyDropsLead(t *testing.T) {
	env := newTestEnv(t, nil)
	token := tokenFor(t, env.mux)
	form := validForm(token)
	form.Set("website", "http://spam.example")

	rec := do(t, env.mux, http.MethodPost, "/lead", form, token)
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("status = %d, want 303", rec.Code)
	}
	if env.store.calls != 0 || len(env.notes.sent) != 0 {
		t.Fatal("honeypot submission must not be stored or mailed")
	}
}

func TestSubmitSuccessRedirectsAndNotifies(t *testing.T) {
	env := newTestEnv(t, nil)
	token := tokenFor(t, env.mux)

	rec := do(t, env.mux, http.MethodPost, "/en/lead", validForm(token), token)
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("status = %d, want 303", rec.Code)
	}
	if loc := rec.Header().Get("Location"); loc != "/en/?sent=1" {
		t.Fatalf("Location = %q", loc)
	}
	if env.store.calls != 1 {
		t.Fatalf("store calls = %d, want 1", env.store.calls)
	}
	if len(env.notes.sent) != 1 {
		t.Fatalf("mail messages = %d, want 1", len(env.notes.sent))
	}
}

func TestSubmitStoreFailureReturns500(t *testing.T) {
	env := newTestEnv(t, errors.New("db down"))
	token := tokenFor(t, env.mux)

	rec := do(t, env.mux, http.MethodPost, "/lead", validForm(token), token)
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", rec.Code)
	}
	if len(env.notes.sent) != 0 {
		t.Fatal("failed save must not send mail")
	}
}

func TestRateLimitFiveAttemptsPerMinute(t *testing.T) {
	env := newTestEnv(t, nil)
	token := tokenFor(t, env.mux)
	bad := validForm(token)
	bad.Set("name", "") // валидация не пройдёт, но попытка засчитывается

	for i := 1; i <= 5; i++ {
		rec := do(t, env.mux, http.MethodPost, "/lead", bad, token)
		if rec.Code != http.StatusUnprocessableEntity {
			t.Fatalf("attempt %d: status = %d, want 422", i, rec.Code)
		}
	}
	rec := do(t, env.mux, http.MethodPost, "/lead", bad, token)
	if rec.Code != http.StatusTooManyRequests {
		t.Fatalf("6th attempt: status = %d, want 429", rec.Code)
	}
}
