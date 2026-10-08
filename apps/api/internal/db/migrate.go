package db

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Migration is one versioned schema change with up and down SQL.
type Migration struct {
	Version  int
	Name     string
	Up       string
	Down     string
	Checksum string
}

// MigrationStatus describes the state of a migration.
type MigrationStatus struct {
	Version   int    `json:"version"`
	Name      string `json:"name"`
	Applied   bool   `json:"applied"`
	Checksum  string `json:"checksum"`
	Drift     bool   `json:"drift"` // applied file content changed since it ran
	AppliedAt string `json:"applied_at,omitempty"`
}

var migrationFile = regexp.MustCompile(`^(\d{4})_([a-z0-9_]+)\.(up|down)\.sql$`)

// migrationLockID is an arbitrary constant for pg_advisory_lock so concurrent
// API instances never run migrations at the same time.
const migrationLockID = 727274001

// FindMigrationsDir locates db/migrations: explicit dir, then walking up from the
// working directory and the executable's directory.
func FindMigrationsDir(explicit string) (string, error) {
	if explicit != "" {
		if st, err := os.Stat(explicit); err == nil && st.IsDir() {
			return explicit, nil
		}
		return "", fmt.Errorf("MIGRATIONS_DIR %q not found", explicit)
	}
	var starts []string
	if wd, err := os.Getwd(); err == nil {
		starts = append(starts, wd)
	}
	if exe, err := os.Executable(); err == nil {
		starts = append(starts, filepath.Dir(exe))
	}
	for _, start := range starts {
		dir := start
		for i := 0; i < 8; i++ {
			cand := filepath.Join(dir, "db", "migrations")
			if st, err := os.Stat(cand); err == nil && st.IsDir() {
				return cand, nil
			}
			parent := filepath.Dir(dir)
			if parent == dir {
				break
			}
			dir = parent
		}
	}
	return "", errors.New("db/migrations directory not found (set MIGRATIONS_DIR)")
}

// LoadMigrations reads and validates migration files from dir.
func LoadMigrations(dir string) ([]Migration, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	byVersion := map[int]*Migration{}
	for _, e := range entries {
		m := migrationFile.FindStringSubmatch(e.Name())
		if m == nil {
			continue
		}
		v, _ := strconv.Atoi(m[1])
		body, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil {
			return nil, err
		}
		mig := byVersion[v]
		if mig == nil {
			mig = &Migration{Version: v, Name: m[2]}
			byVersion[v] = mig
		} else if mig.Name != m[2] {
			return nil, fmt.Errorf("migration %04d has conflicting names %q and %q", v, mig.Name, m[2])
		}
		if m[3] == "up" {
			mig.Up = string(body)
			sum := sha256.Sum256(body)
			mig.Checksum = hex.EncodeToString(sum[:])
		} else {
			mig.Down = string(body)
		}
	}
	var out []Migration
	for _, m := range byVersion {
		if m.Up == "" {
			return nil, fmt.Errorf("migration %04d_%s is missing its .up.sql", m.Version, m.Name)
		}
		if m.Down == "" {
			return nil, fmt.Errorf("migration %04d_%s is missing its .down.sql", m.Version, m.Name)
		}
		out = append(out, *m)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Version < out[j].Version })
	for i, m := range out {
		if m.Version != i+1 {
			return nil, fmt.Errorf("migration versions must be contiguous from 0001; found gap at %04d", m.Version)
		}
	}
	return out, nil
}

func ensureMigrationsTable(ctx context.Context, conn DBTX) error {
	_, err := conn.Exec(ctx, `CREATE TABLE IF NOT EXISTS schema_migrations (
		version    integer PRIMARY KEY,
		name       text NOT NULL,
		checksum   text NOT NULL,
		applied_at timestamptz NOT NULL DEFAULT now()
	)`)
	return err
}

// MigrateUp applies all pending migrations, each in its own transaction, under an advisory lock.
// It refuses to run if an already-applied migration's file content has changed (drift).
func MigrateUp(ctx context.Context, pool *pgxpool.Pool, migs []Migration, log *slog.Logger) (int, error) {
	conn, err := pool.Acquire(ctx)
	if err != nil {
		return 0, err
	}
	defer conn.Release()
	if _, err := conn.Exec(ctx, `SELECT pg_advisory_lock($1)`, migrationLockID); err != nil {
		return 0, err
	}
	defer conn.Exec(context.Background(), `SELECT pg_advisory_unlock($1)`, migrationLockID) //nolint:errcheck

	if err := ensureMigrationsTable(ctx, conn); err != nil {
		return 0, err
	}
	applied, err := appliedMigrations(ctx, conn)
	if err != nil {
		return 0, err
	}
	n := 0
	for _, m := range migs {
		if sum, ok := applied[m.Version]; ok {
			if sum != m.Checksum {
				return n, fmt.Errorf("migration %04d_%s was modified after being applied (checksum drift); add a new migration instead", m.Version, m.Name)
			}
			continue
		}
		err := pgx.BeginFunc(ctx, conn, func(tx pgx.Tx) error {
			if _, err := tx.Exec(ctx, m.Up); err != nil {
				return fmt.Errorf("apply %04d_%s: %w", m.Version, m.Name, err)
			}
			_, err := tx.Exec(ctx, `INSERT INTO schema_migrations (version, name, checksum) VALUES ($1, $2, $3)`, m.Version, m.Name, m.Checksum)
			return err
		})
		if err != nil {
			return n, err
		}
		n++
		if log != nil {
			log.Info("migration applied", "version", m.Version, "name", m.Name)
		}
	}
	return n, nil
}

// MigrateDown rolls back the most recent `steps` migrations.
func MigrateDown(ctx context.Context, pool *pgxpool.Pool, migs []Migration, steps int, log *slog.Logger) (int, error) {
	conn, err := pool.Acquire(ctx)
	if err != nil {
		return 0, err
	}
	defer conn.Release()
	if _, err := conn.Exec(ctx, `SELECT pg_advisory_lock($1)`, migrationLockID); err != nil {
		return 0, err
	}
	defer conn.Exec(context.Background(), `SELECT pg_advisory_unlock($1)`, migrationLockID) //nolint:errcheck
	if err := ensureMigrationsTable(ctx, conn); err != nil {
		return 0, err
	}
	applied, err := appliedMigrations(ctx, conn)
	if err != nil {
		return 0, err
	}
	n := 0
	for i := len(migs) - 1; i >= 0 && n < steps; i-- {
		m := migs[i]
		if _, ok := applied[m.Version]; !ok {
			continue
		}
		err := pgx.BeginFunc(ctx, conn, func(tx pgx.Tx) error {
			if _, err := tx.Exec(ctx, m.Down); err != nil {
				return fmt.Errorf("revert %04d_%s: %w", m.Version, m.Name, err)
			}
			_, err := tx.Exec(ctx, `DELETE FROM schema_migrations WHERE version = $1`, m.Version)
			return err
		})
		if err != nil {
			return n, err
		}
		n++
		if log != nil {
			log.Info("migration reverted", "version", m.Version, "name", m.Name)
		}
	}
	return n, nil
}

// Status reports applied/pending state and drift for each migration.
func Status(ctx context.Context, pool *pgxpool.Pool, migs []Migration) ([]MigrationStatus, error) {
	if err := ensureMigrationsTable(ctx, pool); err != nil {
		return nil, err
	}
	rows, err := pool.Query(ctx, `SELECT version, checksum, to_char(applied_at, 'YYYY-MM-DD"T"HH24:MI:SSOF') FROM schema_migrations`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	type ap struct{ sum, at string }
	applied := map[int]ap{}
	for rows.Next() {
		var v int
		var a ap
		if err := rows.Scan(&v, &a.sum, &a.at); err != nil {
			return nil, err
		}
		applied[v] = a
	}
	var out []MigrationStatus
	for _, m := range migs {
		st := MigrationStatus{Version: m.Version, Name: m.Name, Checksum: m.Checksum}
		if a, ok := applied[m.Version]; ok {
			st.Applied = true
			st.AppliedAt = a.at
			st.Drift = a.sum != m.Checksum
		}
		out = append(out, st)
	}
	return out, rows.Err()
}

func appliedMigrations(ctx context.Context, conn DBTX) (map[int]string, error) {
	rows, err := conn.Query(ctx, `SELECT version, checksum FROM schema_migrations`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[int]string{}
	for rows.Next() {
		var v int
		var sum string
		if err := rows.Scan(&v, &sum); err != nil {
			return nil, err
		}
		out[v] = sum
	}
	return out, rows.Err()
}
