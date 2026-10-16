// Package db opens the Postgres pool and applies each service's migrations.
//
// Every service owns one Postgres schema. Migrations are embedded SQL files
// named NNN_description.sql, applied in order inside a transaction and
// recorded in <schema>.schema_migrations, so services can share one database
// without touching each other's tables.
package db

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"sort"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Open connects to Postgres, retrying while the database starts up. Every
// connection's search_path starts with schema, so queries name tables bare.
func Open(ctx context.Context, url, schema string, maxConns int32) (*pgxpool.Pool, error) {
	cfg, err := pgxpool.ParseConfig(url)
	if err != nil {
		return nil, fmt.Errorf("parse database url: %w", err)
	}
	cfg.ConnConfig.RuntimeParams["search_path"] = schema + ", public"
	if maxConns > 0 {
		cfg.MaxConns = maxConns
	}
	var pool *pgxpool.Pool
	for attempt := 1; ; attempt++ {
		pool, err = pgxpool.NewWithConfig(ctx, cfg)
		if err == nil {
			if err = pool.Ping(ctx); err == nil {
				return pool, nil
			}
			pool.Close()
		}
		if attempt == 30 {
			return nil, fmt.Errorf("connect to database: %w", err)
		}
		slog.Info("waiting for database", "attempt", attempt, "err", err)
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(time.Second):
		}
	}
}

// Migrate applies the not-yet-applied *.sql files in migrations to schema.
func Migrate(ctx context.Context, pool *pgxpool.Pool, schema string, migrations fs.FS) error {
	if _, err := pool.Exec(ctx, fmt.Sprintf(`CREATE SCHEMA IF NOT EXISTS %s;
		CREATE TABLE IF NOT EXISTS %s.schema_migrations (
			version TEXT PRIMARY KEY,
			applied_at TIMESTAMPTZ NOT NULL DEFAULT now())`, schema, schema)); err != nil {
		return fmt.Errorf("create migrations table: %w", err)
	}

	names, err := fs.Glob(migrations, "*.sql")
	if err != nil {
		return err
	}
	sort.Strings(names)

	for _, name := range names {
		version := strings.TrimSuffix(name, ".sql")
		body, err := fs.ReadFile(migrations, name)
		if err != nil {
			return err
		}
		if err := applyOne(ctx, pool, schema, version, string(body)); err != nil {
			return fmt.Errorf("migration %s: %w", name, err)
		}
	}
	return nil
}

func applyOne(ctx context.Context, pool *pgxpool.Pool, schema, version, body string) error {
	tx, err := pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx) //nolint:errcheck

	// Serialise concurrent starts of the same service.
	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtext($1))`, schema); err != nil {
		return err
	}
	var exists bool
	if err := tx.QueryRow(ctx, fmt.Sprintf(`SELECT EXISTS (SELECT 1 FROM %s.schema_migrations WHERE version = $1)`, schema), version).Scan(&exists); err != nil {
		return err
	}
	if exists {
		return nil
	}
	if _, err := tx.Exec(ctx, fmt.Sprintf("SET LOCAL search_path TO %s, public", schema)); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, body); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, fmt.Sprintf(`INSERT INTO %s.schema_migrations (version) VALUES ($1)`, schema), version); err != nil {
		return err
	}
	slog.Info("applied migration", "schema", schema, "version", version)
	return tx.Commit(ctx)
}

// IsUniqueViolation reports whether err is a unique-constraint failure,
// optionally on the named constraint.
func IsUniqueViolation(err error, constraint string) bool {
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) || pgErr.Code != "23505" {
		return false
	}
	return constraint == "" || pgErr.ConstraintName == constraint
}

// IsExclusionViolation reports whether err is an exclusion-constraint failure.
func IsExclusionViolation(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "23P01"
}

// IsNoRows reports whether err means the query matched nothing.
func IsNoRows(err error) bool { return errors.Is(err, pgx.ErrNoRows) }

// Page normalises 1-based page numbers and page sizes to LIMIT/OFFSET.
func Page(page, size, def, max int32) (limit, offset int32) {
	if size <= 0 {
		size = def
	}
	if size > max {
		size = max
	}
	if page < 1 {
		page = 1
	}
	return size, (page - 1) * size
}

// Unix converts a nullable timestamp to unix seconds (0 when NULL).
func Unix(t *time.Time) int64 {
	if t == nil {
		return 0
	}
	return t.Unix()
}
