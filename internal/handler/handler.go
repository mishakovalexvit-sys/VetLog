package handler

import (
	"bytes"
	"context"
	"fmt"
	"html/template"
	"io/fs"
	"log/slog"
	"net/http"
	"time"

	"vetlog/internal/i18n"
	"vetlog/internal/lead"
	"vetlog/internal/mail"
)

const maxFormBytes = 16 << 10

// LeadStore сохраняет заявку и согласие в одной транзакции.
type LeadStore interface {
	Create(ctx context.Context, l lead.Lead, c lead.Consent) (int64, error)
}

// Notifier ставит письмо в очередь без блокировки.
type Notifier interface {
	Enqueue(m mail.Message) bool
}

type Deps struct {
	Bundle       *i18n.Bundle
	Templates    *template.Template
	Store        LeadStore
	Notifier     Notifier
	Log          *slog.Logger
	BaseURL      string
	CookieSecure bool
	TrustProxy   bool
}

type Handler struct {
	deps    Deps
	limiter *IPLimiter
}

func New(d Deps) *Handler {
	return &Handler{deps: d, limiter: NewIPLimiter(5, 5)}
}

// locale задаёт пути одной языковой версии.
type locale struct {
	Lang, Home, Action, Privacy string
}

var locales = map[string]locale{
	"ru": {Lang: "ru", Home: "/", Action: "/lead", Privacy: "/privacy"},
	"en": {Lang: "en", Home: "/en/", Action: "/en/lead", Privacy: "/en/privacy"},
}

// page описывает страницу: шаблон и её пути в обеих языковых версиях (canonical, hreflang).
type page struct {
	tmpl, ru, en string
}

var (
	homePage    = page{tmpl: "index.html", ru: "/", en: "/en/"}
	privacyPage = page{tmpl: "privacy.html", ru: "/privacy", en: "/en/privacy"}
)

func (p page) path(lang string) string {
	if lang == "en" {
		return p.en
	}
	return p.ru
}

type alternate struct {
	Lang, Href string
}

// pageData — контракт данных шаблона:
// Lang, Home, Action, Canonical, CSRF, PrivacyPath, Alternates, Sent, Values, Errors, FormError, T.
// PolicyReady используется только privacy.html.
type pageData struct {
	Lang        string
	Home        string
	Action      string
	Canonical   string
	CSRF        string
	PrivacyPath string
	Alternates  []alternate
	Sent        bool
	PolicyReady bool
	Values      map[string]string
	Errors      map[string]string
	FormError   string

	tr func(string) string
}

// T вызывается из шаблона: {{.T "key"}}.
func (d pageData) T(key string) string { return d.tr(key) }

func (h *Handler) Index(lang string) http.HandlerFunc {
	loc := locales[lang]
	return func(w http.ResponseWriter, r *http.Request) {
		token := csrfToken(w, r, h.deps.CookieSecure)
		h.renderPage(w, homePage, loc, http.StatusOK, pageData{
			CSRF: token,
			Sent: r.URL.Query().Get("sent") == "1",
		})
	}
}

// Privacy отдаёт политику. Если ключей privacy.title и privacy.body нет в словаре,
// шаблон показывает заглушку «текст в подготовке».
func (h *Handler) Privacy(lang string) http.HandlerFunc {
	loc := locales[lang]
	return func(w http.ResponseWriter, r *http.Request) {
		ready := h.deps.Bundle.Has(lang, "privacy.title") && h.deps.Bundle.Has(lang, "privacy.body")
		h.renderPage(w, privacyPage, loc, http.StatusOK, pageData{PolicyReady: ready})
	}
}

func (h *Handler) Submit(lang string) http.HandlerFunc {
	loc := locales[lang]
	return func(w http.ResponseWriter, r *http.Request) {
		r.Body = http.MaxBytesReader(w, r.Body, maxFormBytes)
		token := csrfToken(w, r, h.deps.CookieSecure)
		ip := clientIP(r, h.deps.TrustProxy)

		// Лимит считает все POST, включая невалидные.
		if !h.limiter.Allow(ip) {
			h.renderPage(w, homePage, loc, http.StatusTooManyRequests,
				pageData{CSRF: token, FormError: "form.error.rate_limit"})
			return
		}
		if err := r.ParseForm(); err != nil {
			h.deps.Log.Warn("parse form failed", slog.Any("err", err))
			h.renderPage(w, homePage, loc, http.StatusBadRequest,
				pageData{CSRF: token, FormError: "form.error.generic"})
			return
		}
		if !validCSRF(r) {
			h.renderPage(w, homePage, loc, http.StatusForbidden,
				pageData{CSRF: token, FormError: "form.error.csrf"})
			return
		}

		values := map[string]string{
			"name":   r.PostFormValue("name"),
			"phone":  r.PostFormValue("phone"),
			"clinic": r.PostFormValue("clinic"),
			"email":  r.PostFormValue("email"),
		}

		// Honeypot: бот заполнил скрытое поле. Отвечаем как при успехе, но ничего не сохраняем.
		if r.PostFormValue("website") != "" {
			h.deps.Log.Info("honeypot triggered", slog.String("ip", ip))
			http.Redirect(w, r, loc.Home+"?sent=1", http.StatusSeeOther)
			return
		}

		norm, errs := lead.Validate(lead.Input{
			Name:    values["name"],
			Phone:   values["phone"],
			Clinic:  values["clinic"],
			Email:   values["email"],
			Consent: r.PostFormValue("consent") == "on",
		})
		if len(errs) > 0 {
			t := h.deps.Bundle.Func(lang)
			msgs := make(map[string]string, len(errs))
			for field, code := range errs {
				msgs[field] = t("err." + code)
			}
			h.renderPage(w, homePage, loc, http.StatusUnprocessableEntity,
				pageData{CSRF: token, Values: values, Errors: msgs})
			return
		}

		l := lead.Lead{
			Name: norm.Name, Phone: norm.Phone, Clinic: norm.Clinic, Email: norm.Email, Lang: lang,
		}
		id, err := h.deps.Store.Create(r.Context(), l, lead.Consent{
			AcceptedAt: time.Now(), IP: ip, UserAgent: r.UserAgent(),
		})
		if err != nil {
			h.deps.Log.Error("save lead failed", slog.Any("err", err))
			h.renderPage(w, homePage, loc, http.StatusInternalServerError,
				pageData{CSRF: token, Values: values, FormError: "err.server"})
			return
		}

		h.deps.Log.Info("lead saved", slog.Int64("lead_id", id), slog.String("lang", lang))
		if !h.deps.Notifier.Enqueue(mail.LeadMessage(l)) {
			h.deps.Log.Warn("mail queue unavailable", slog.Int64("lead_id", id))
		}
		// PRG: после POST — редирект, чтобы F5 не отправлял заявку повторно.
		http.Redirect(w, r, loc.Home+"?sent=1", http.StatusSeeOther)
	}
}

func (h *Handler) renderPage(w http.ResponseWriter, p page, loc locale, status int, d pageData) {
	d.Lang = loc.Lang
	d.Home = loc.Home
	d.Action = loc.Action
	d.PrivacyPath = loc.Privacy
	d.tr = h.deps.Bundle.Func(loc.Lang)
	d.Canonical = h.deps.BaseURL + p.path(loc.Lang)
	d.Alternates = []alternate{
		{Lang: "ru", Href: h.deps.BaseURL + p.ru},
		{Lang: "en", Href: h.deps.BaseURL + p.en},
		{Lang: "x-default", Href: h.deps.BaseURL + p.ru},
	}

	var buf bytes.Buffer
	if err := h.deps.Templates.ExecuteTemplate(&buf, p.tmpl, d); err != nil {
		h.deps.Log.Error("render failed", slog.String("template", p.tmpl), slog.Any("err", err))
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(status)
	_, _ = buf.WriteTo(w)
}

// Healthz проверяет, что приложение живо и БД отвечает.
func Healthz(ping func(context.Context) error) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
		defer cancel()
		if err := ping(ctx); err != nil {
			http.Error(w, "db unavailable", http.StatusServiceUnavailable)
			return
		}
		fmt.Fprint(w, "ok")
	}
}

func (h *Handler) Register(mux *http.ServeMux, static fs.FS, ping func(context.Context) error) {
	mux.HandleFunc("GET /{$}", h.Index("ru"))
	mux.HandleFunc("GET /en/{$}", h.Index("en"))
	mux.Handle("GET /en", http.RedirectHandler("/en/", http.StatusMovedPermanently))
	mux.HandleFunc("GET /privacy", h.Privacy("ru"))
	mux.HandleFunc("GET /en/privacy", h.Privacy("en"))
	mux.HandleFunc("POST /lead", h.Submit("ru"))
	mux.HandleFunc("POST /en/lead", h.Submit("en"))
	mux.HandleFunc("GET /healthz", Healthz(ping))
	mux.Handle("GET /static/", http.StripPrefix("/static/", http.FileServerFS(static)))
}
