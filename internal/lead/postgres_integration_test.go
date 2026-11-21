//go:build integration

package lead_test

import (
	"context"
	"log/slog"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/testcontainers/testcontainers-go/modules/postgres"

	"vetlog/internal/db"
	"vetlog/internal/lead"
	"vetlog/migrations"
)

// Запуск: go test -tags=integration ./internal/lead/... (нужен Docker)
func TestRepoCreateIntegration(t *testing.T) {
	ctx := context.Background()

	ctr, err := postgres.Run(ctx, "postgres:16-alpine",
		postgres.WithDatabase("vetlog"),
		postgres.WithUsername("vetlog"),
		postgres.WithPassword("vetlog"),
		postgres.BasicWaitStrategies(),
	)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ctr.Terminate(ctx) })

	dsn, err := ctr.ConnectionString(ctx, "sslmode=disable")
	if err != nil {
		t.Fatal(err)
	}
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)

	// Повторный прогон миграций не должен падать и менять схему.
	for i := 0; i < 2; i++ {
		if err := db.Migrate(ctx, pool, migrations.FS, slog.Default()); err != nil {
			t.Fatalf("migrate run %d: %v", i+1, err)
		}
	}

	repo := lead.NewRepo(pool)
	id, err := repo.Create(ctx,
		lead.Lead{Name: "Анна", Phone: "+79180000000", Clinic: "Лапа", Lang: "ru"},
		lead.Consent{AcceptedAt: time.Now(), IP: "192.0.2.1", UserAgent: "test"},
	)
	if err != nil {
		t.Fatal(err)
	}

	var consents int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM lead_consents WHERE lead_id = $1`, id).Scan(&consents); err != nil {
		t.Fatal(err)
	}
	if consents != 1 {
		t.Fatalf("consents = %d, want 1", consents)
	}

	var email *string
	if err := pool.QueryRow(ctx, `SELECT email FROM leads WHERE id = $1`, id).Scan(&email); err != nil {
		t.Fatal(err)
	}
	if email != nil {
		t.Fatalf("empty email must be NULL, got %q", *email)
	}
}
