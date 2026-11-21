package lead

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"
)

type Repo struct {
	pool *pgxpool.Pool
}

func NewRepo(pool *pgxpool.Pool) *Repo {
	return &Repo{pool: pool}
}

// Create сохраняет заявку и запись о согласии в одной транзакции.
func (r *Repo) Create(ctx context.Context, l Lead, c Consent) (int64, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return 0, fmt.Errorf("begin: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	var email any // пустой email сохраняем как NULL
	if l.Email != "" {
		email = l.Email
	}

	var id int64
	err = tx.QueryRow(ctx,
		`INSERT INTO leads (name, phone, clinic, email, lang) VALUES ($1, $2, $3, $4, $5) RETURNING id`,
		l.Name, l.Phone, l.Clinic, email, l.Lang,
	).Scan(&id)
	if err != nil {
		return 0, fmt.Errorf("insert lead: %w", err)
	}

	_, err = tx.Exec(ctx,
		`INSERT INTO lead_consents (lead_id, accepted_at, ip, user_agent) VALUES ($1, $2, $3, $4)`,
		id, c.AcceptedAt, c.IP, c.UserAgent,
	)
	if err != nil {
		return 0, fmt.Errorf("insert consent: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return 0, fmt.Errorf("commit: %w", err)
	}
	return id, nil
}
