package main

import (
	"context"
	"errors"
	"fmt"
	"html/template"
	"io/fs"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"vetlog/internal/config"
	"vetlog/internal/db"
	"vetlog/internal/handler"
	"vetlog/internal/i18n"
	"vetlog/internal/lead"
	"vetlog/internal/mail"
	"vetlog/migrations"
	"vetlog/web"
)

func main() { os.Exit(realMain()) }

func realMain() int {
	// Distroless-образ без shell: healthcheck запускается самим бинарником.
	if len(os.Args) > 1 && os.Args[1] == "healthcheck" {
		return healthcheck()
	}

	cfg, err := config.Load()
	if err != nil {
		fmt.Fprintln(os.Stderr, "config:", err)
		return 1
	}

	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: cfg.LogLevel})).
		With("service", "vetlog")
	slog.SetDefault(logger)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if err := run(ctx, cfg, logger); err != nil {
		logger.Error("fatal", slog.Any("err", err))
		return 1
	}
	return 0
}

// healthURL: HEALTH_URL имеет приоритет. Иначе адрес строится из HTTP_ADDR:
// пустой хост и 0.0.0.0/[::] заменяются на 127.0.0.1.
func healthURL() string {
	if u := os.Getenv("HEALTH_URL"); u != "" {
		return u
	}
	addr := os.Getenv("HTTP_ADDR")
	if addr == "" {
		addr = ":8080"
	}
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		return "http://127.0.0.1:8080/healthz"
	}
	if host == "" || host == "0.0.0.0" || host == "::" {
		host = "127.0.0.1"
	}
	return "http://" + net.JoinHostPort(host, port) + "/healthz"
}

func healthcheck() int {
	client := http.Client{Timeout: 3 * time.Second}
	resp, err := client.Get(healthURL())
	if err != nil {
		return 1
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return 1
	}
	return 0
}

func run(ctx context.Context, cfg config.Config, log *slog.Logger) error {
	bundle, err := i18n.Load()
	if err != nil {
		return fmt.Errorf("i18n: %w", err)
	}
	tpl, err := template.ParseFS(web.FS, "templates/*.html")
	if err != nil {
		return fmt.Errorf("templates: %w", err)
	}
	static, err := fs.Sub(web.FS, "static")
	if err != nil {
		return err
	}

	pool, err := pgxpool.New(ctx, cfg.DatabaseURL)
	if err != nil {
		return fmt.Errorf("db pool: %w", err)
	}
	defer pool.Close()
	if err := pool.Ping(ctx); err != nil {
		return fmt.Errorf("db ping: %w", err)
	}
	if cfg.MigrateOnStart {
		if err := db.Migrate(ctx, pool, migrations.FS, log); err != nil {
			return fmt.Errorf("migrate: %w", err)
		}
	} else {
		log.Info("migrations disabled by MIGRATE_ON_START=false")
	}

	var sender mail.Sender = mail.LogOnly{Log: log}
	if cfg.SMTP.Host != "" {
		sender = mail.SMTP{Config: cfg.SMTP}
	} else {
		log.Warn("SMTP_HOST is empty: lead notifications are logged, not sent")
	}
	queue := mail.NewQueue(sender, log, 100, 3, 2*time.Second)
	queue.Start(1)

	h := handler.New(handler.Deps{
		Bundle:       bundle,
		Templates:    tpl,
		Store:        lead.NewRepo(pool),
		Notifier:     queue,
		Log:          log,
		BaseURL:      cfg.BaseURL,
		CookieSecure: cfg.CookieSecure,
		TrustProxy:   cfg.TrustProxy,
	})
	mux := http.NewServeMux()
	h.Register(mux, static, pool.Ping)

	srv := &http.Server{
		Addr:              cfg.HTTPAddr,
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       10 * time.Second,
		WriteTimeout:      15 * time.Second,
		IdleTimeout:       60 * time.Second,
	}

	errCh := make(chan error, 1)
	go func() {
		log.Info("http server listening", slog.String("addr", cfg.HTTPAddr))
		errCh <- srv.ListenAndServe()
	}()

	select {
	case <-ctx.Done():
	case err := <-errCh:
		if !errors.Is(err, http.ErrServerClosed) {
			queue.Close()
			return err
		}
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		log.Warn("http shutdown", slog.Any("err", err))
	}
	queue.Close() // дописать письма из очереди
	log.Info("stopped")
	return nil
}
