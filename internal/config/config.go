package config

import (
	"errors"
	"fmt"
	"log/slog"
	"os"
	"strconv"
	"strings"

	"vetlog/internal/mail"
)

type Config struct {
	HTTPAddr       string
	DatabaseURL    string
	BaseURL        string
	MigrateOnStart bool
	CookieSecure   bool
	TrustProxy     bool
	LogLevel       slog.Level
	SMTP           mail.SMTPConfig
}

func Load() (Config, error) {
	var c Config
	c.HTTPAddr = getEnv("HTTP_ADDR", ":8080")
	c.DatabaseURL = os.Getenv("DATABASE_URL")
	c.BaseURL = strings.TrimRight(getEnv("BASE_URL", "http://localhost:8080"), "/")
	if c.DatabaseURL == "" {
		return c, errors.New("DATABASE_URL is required")
	}

	var err error
	if c.MigrateOnStart, err = boolEnv("MIGRATE_ON_START", true); err != nil {
		return c, err
	}
	if c.CookieSecure, err = boolEnv("COOKIE_SECURE", true); err != nil {
		return c, err
	}
	if c.TrustProxy, err = boolEnv("TRUST_PROXY", false); err != nil {
		return c, err
	}
	if err := c.LogLevel.UnmarshalText([]byte(getEnv("LOG_LEVEL", "info"))); err != nil {
		return c, fmt.Errorf("LOG_LEVEL: %w", err)
	}

	port, err := smtpPort(getEnv("SMTP_PORT", "587"))
	if err != nil {
		return c, err
	}
	c.SMTP = mail.SMTPConfig{
		Host:     os.Getenv("SMTP_HOST"),
		Port:     port,
		User:     os.Getenv("SMTP_USER"),
		Password: os.Getenv("SMTP_PASSWORD"),
		From:     os.Getenv("SMTP_FROM"),
		To:       splitList(os.Getenv("SMTP_TO")),
	}
	return c, nil
}

// smtpPort принимает только submission-порт 587 (STARTTLS). Порт 465 (неявный TLS) не поддерживается.
func smtpPort(v string) (int, error) {
	port, err := strconv.Atoi(v)
	if err != nil {
		return 0, fmt.Errorf("SMTP_PORT: %w", err)
	}
	if port == 465 {
		return 0, errors.New("SMTP_PORT=465 (неявный TLS) не поддерживается: используйте 587 со STARTTLS")
	}
	if port < 1 || port > 65535 {
		return 0, fmt.Errorf("SMTP_PORT: %d вне диапазона 1-65535", port)
	}
	return port, nil
}

func getEnv(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func boolEnv(key string, def bool) (bool, error) {
	v := os.Getenv(key)
	if v == "" {
		return def, nil
	}
	b, err := strconv.ParseBool(v)
	if err != nil {
		return false, fmt.Errorf("%s: %w", key, err)
	}
	return b, nil
}

func splitList(s string) []string {
	var out []string
	for _, p := range strings.Split(s, ",") {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}
