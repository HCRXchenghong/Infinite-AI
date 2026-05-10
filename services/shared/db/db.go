package db

import (
	"context"
	"errors"
	"os"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

const schemaLockKey int64 = 4_901_001

func Connect(ctx context.Context, databaseURL string) (*pgxpool.Pool, error) {
	config, err := pgxpool.ParseConfig(databaseURL)
	if err != nil {
		return nil, err
	}
	config.MaxConns = 20
	config.MinConns = 2
	config.MaxConnLifetime = 30 * time.Minute
	pool, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		return nil, err
	}
	if err := pool.Ping(ctx); err != nil {
		return nil, err
	}
	return pool, nil
}

func EnsureSchema(ctx context.Context, pool *pgxpool.Pool) error {
	conn, err := pool.Acquire(ctx)
	if err != nil {
		return err
	}
	defer conn.Release()

	if _, err := conn.Exec(ctx, "SELECT pg_advisory_lock($1)", schemaLockKey); err != nil {
		return err
	}
	defer func() {
		_, _ = conn.Exec(context.Background(), "SELECT pg_advisory_unlock($1)", schemaLockKey)
	}()

	sqlBytes, err := os.ReadFile("db/migrations/001_init.sql")
	if err != nil {
		return err
	}
	_, err = conn.Exec(ctx, string(sqlBytes))
	return err
}

func CleanDSN(dsn string) string {
	if dsn == "" {
		return ""
	}
	if idx := strings.Index(dsn, "@"); idx > 0 {
		start := strings.Index(dsn, "://")
		if start > 0 && start+3 < idx {
			return dsn[:start+3] + "***:***" + dsn[idx:]
		}
	}
	return dsn
}

var ErrNotFound = errors.New("not found")
