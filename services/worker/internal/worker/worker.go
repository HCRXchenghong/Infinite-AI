package worker

import (
	"context"
	"log"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/seron-cheng/infinite-ai/services/shared/config"
	"github.com/seron-cheng/infinite-ai/services/shared/db"
)

type Runner struct {
	Config config.Config
	DB     *pgxpool.Pool
}

func New(ctx context.Context, cfg config.Config) (*Runner, error) {
	pool, err := db.Connect(ctx, cfg.DatabaseURL)
	if err != nil {
		return nil, err
	}
	if err := db.EnsureSchema(ctx, pool); err != nil {
		return nil, err
	}
	return &Runner{
		Config: cfg,
		DB:     pool,
	}, nil
}

func (r *Runner) Close() {
	if r.DB != nil {
		r.DB.Close()
	}
}

func (r *Runner) Run(ctx context.Context) error {
	ticker := time.NewTicker(r.Config.WorkerPollInterval)
	defer ticker.Stop()
	log.Printf("worker started with poll interval %s", r.Config.WorkerPollInterval)

	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
			if err := r.tick(ctx); err != nil {
				log.Printf("worker tick error: %v", err)
			}
		}
	}
}

func (r *Runner) tick(ctx context.Context) error {
	_, err := r.DB.Exec(ctx, `
		UPDATE subscriptions
		SET status = 'expired', updated_at = NOW()
		WHERE status = 'active' AND ends_at IS NOT NULL AND ends_at < NOW()
	`)
	if err != nil {
		return err
	}
	_, err = r.DB.Exec(ctx, `
		UPDATE redeem_codes
		SET status = 'expired'
		WHERE status = 'active' AND expires_at IS NOT NULL AND expires_at < NOW()
	`)
	return err
}
