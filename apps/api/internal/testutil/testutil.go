// Package testutil provides helpers for IdeaVault's DB-backed tests: schema reset
// and migrations, a fully wired App in offline (MOCK_AI) mode, an httptest server,
// authenticated HTTP clients, SSE parsing and deterministic job draining.
//
// DB-backed tests read TEST_DATABASE_URL and skip when it is unset, so
// `go test ./...` works without Docker. Never point TEST_DATABASE_URL at a
// database whose data you want to keep: the public schema is dropped.
package testutil

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/Prathameshppawar/ideavault/apps/api/internal/app"
	"github.com/Prathameshppawar/ideavault/apps/api/internal/config"
	"github.com/Prathameshppawar/ideavault/apps/api/internal/db"
	"github.com/Prathameshppawar/ideavault/apps/api/internal/observability"
)

// EnvDatabaseURL names the environment variable holding the test database URL.
const EnvDatabaseURL = "TEST_DATABASE_URL"

// DatabaseURL returns TEST_DATABASE_URL (empty when DB tests should be skipped).
func DatabaseURL() string { return strings.TrimSpace(os.Getenv(EnvDatabaseURL)) }

// TB is the subset of testing.TB the helpers need (lets TestMain-style code reuse them).
type TB interface {
	Helper()
	Fatalf(format string, args ...any)
	Skip(args ...any)
	Logf(format string, args ...any)
	Cleanup(func())
}

// RequireDB skips t when no test database is configured and returns its URL.
func RequireDB(t TB) string {
	t.Helper()
	u := DatabaseURL()
	if u == "" {
		t.Skip(EnvDatabaseURL + " is not set; skipping DB-backed test")
	}
	return u
}

// Logger returns a quiet logger: warnings and errors only, and only when
// IV_TEST_LOG=1 (otherwise discarded) so test output stays readable.
func Logger() *slog.Logger {
	var w io.Writer = io.Discard
	if os.Getenv("IV_TEST_LOG") == "1" {
		w = os.Stderr
	}
	return observability.NewLogger(w, "warn", "text")
}

// MigrationsDir locates the repository's db/migrations directory.
func MigrationsDir() (string, error) { return db.FindMigrationsDir("") }

// RepoRoot returns the repository root (the directory containing db/migrations and tests/).
func RepoRoot() (string, error) {
	dir, err := MigrationsDir()
	if err != nil {
		return "", err
	}
	return filepath.Dir(filepath.Dir(dir)), nil
}

// ResetSchema drops and recreates the public schema, then applies every migration.
func ResetSchema(ctx context.Context, url string) error {
	pool, err := db.Connect(ctx, url)
	if err != nil {
		return err
	}
	defer pool.Close()
	return resetAndMigrate(ctx, pool)
}

func resetAndMigrate(ctx context.Context, pool *pgxpool.Pool) error {
	if _, err := pool.Exec(ctx, `DROP SCHEMA IF EXISTS public CASCADE; CREATE SCHEMA public;`); err != nil {
		return fmt.Errorf("reset schema: %w", err)
	}
	migs, err := loadMigrations()
	if err != nil {
		return err
	}
	if _, err := db.MigrateUp(ctx, pool, migs, nil); err != nil {
		return fmt.Errorf("migrate up: %w", err)
	}
	return nil
}

func loadMigrations() ([]db.Migration, error) {
	dir, err := MigrationsDir()
	if err != nil {
		return nil, err
	}
	return db.LoadMigrations(dir)
}

// MigrationReport records the outcome of the up → down → up verification.
type MigrationReport struct {
	Count      int
	Up         error
	Down       error
	ReUp       error
	DownTables []string // user tables remaining after rolling everything back
	Status     []db.MigrationStatus
}

// Err returns the first failure of the verification, if any.
func (r *MigrationReport) Err() error {
	switch {
	case r == nil:
		return errors.New("migrations were not verified")
	case r.Up != nil:
		return fmt.Errorf("up: %w", r.Up)
	case r.Down != nil:
		return fmt.Errorf("down: %w", r.Down)
	case r.ReUp != nil:
		return fmt.Errorf("re-up: %w", r.ReUp)
	}
	return nil
}

// VerifyMigrations resets the schema and runs up → down(all) → up, recording each
// step. The schema is always left fully migrated (it is reset again on failure).
func VerifyMigrations(ctx context.Context, url string) (*MigrationReport, error) {
	pool, err := db.Connect(ctx, url)
	if err != nil {
		return nil, err
	}
	defer pool.Close()
	migs, err := loadMigrations()
	if err != nil {
		return nil, err
	}
	rep := &MigrationReport{Count: len(migs)}
	if _, err := pool.Exec(ctx, `DROP SCHEMA IF EXISTS public CASCADE; CREATE SCHEMA public;`); err != nil {
		return nil, err
	}
	_, rep.Up = db.MigrateUp(ctx, pool, migs, nil)
	if rep.Up == nil {
		_, rep.Down = db.MigrateDown(ctx, pool, migs, len(migs), nil)
		if rep.Down == nil {
			rows, err := pool.Query(ctx, `SELECT tablename FROM pg_tables WHERE schemaname = 'public' AND tablename <> 'schema_migrations' ORDER BY 1`)
			if err == nil {
				for rows.Next() {
					var n string
					if rows.Scan(&n) == nil {
						rep.DownTables = append(rep.DownTables, n)
					}
				}
				rows.Close()
			}
		}
		_, rep.ReUp = db.MigrateUp(ctx, pool, migs, nil)
	}
	if rep.Err() != nil {
		// Leave a usable schema for the remaining tests.
		if err := resetAndMigrate(ctx, pool); err != nil {
			return rep, err
		}
	}
	rep.Status, _ = db.Status(ctx, pool, migs)
	return rep, nil
}

// Config returns a deterministic test configuration: offline planner only, local
// hash embeddings, generous rate limits, no Redis, no bootstrap user.
func Config(url string) *config.Config {
	return &config.Config{
		Env:                   "test",
		Port:                  0,
		WebOrigins:            []string{"http://localhost:3000"},
		DatabaseURL:           url,
		AutoMigrate:           false,
		MasterKey:             []byte("ideavault-test-master-key-32byte"),
		SessionTTL:            24 * time.Hour,
		EmbeddingProvider:     "local",
		MockAI:                true,
		RateLimitAuthPerMin:   100000,
		RateLimitAgentPerMin:  100000,
		RateLimitImportPerMin: 100000,
		RateLimitAPIPerMin:    100000,
		WorkerConcurrency:     1,
		JobPollInterval:       time.Second,
		MaxUploadBytes:        50 << 20,
		LogLevel:              "warn",
		LogFormat:             "text",
	}
}

// NewApp builds a wired App for cfg. The background worker is not started; use
// Env.DrainJobs to process jobs deterministically.
func NewApp(ctx context.Context, cfg *config.Config) (*app.App, error) {
	if len(cfg.MasterKey) != 32 {
		return nil, errors.New("test config needs a 32-byte master key")
	}
	return app.Build(ctx, cfg, Logger())
}
