package integration

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/Prathameshppawar/ideavault/apps/api/internal/domain"
	"github.com/Prathameshppawar/ideavault/apps/api/internal/service"
	"github.com/Prathameshppawar/ideavault/apps/api/internal/testutil"
)

// env is the shared, migrated database + App + test server (nil without TEST_DATABASE_URL).
var env *testutil.Env

func TestMain(m *testing.M) { os.Exit(run(m)) }

func run(m *testing.M) int {
	url := testutil.DatabaseURL()
	if url == "" {
		return m.Run() // every test skips via requireEnv
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	e, err := testutil.Setup(ctx, url)
	cancel()
	if err != nil {
		fmt.Fprintln(os.Stderr, "integration setup failed:", err)
		return 1
	}
	env = e
	defer env.Close()
	return m.Run()
}

// requireEnv returns the shared environment or skips the test.
func requireEnv(t *testing.T) *testutil.Env {
	t.Helper()
	if env == nil {
		t.Skip(testutil.EnvDatabaseURL + " is not set; skipping DB-backed test")
	}
	return env
}

func ctx() context.Context { return context.Background() }

// ---------- small helpers shared by the integration tests ----------

// result carries a (value, error) pair so calls can be asserted inline: try(f()).must(t).
type result[T any] struct {
	v   T
	err error
}

func try[T any](v T, err error) result[T] { return result[T]{v, err} }

func (r result[T]) must(t *testing.T) T {
	t.Helper()
	if r.err != nil {
		t.Fatalf("unexpected error: %v", r.err)
	}
	return r.v
}

func wantKind(t *testing.T, err error, kind domain.ErrorKind) {
	t.Helper()
	if err == nil {
		t.Fatalf("expected a %s error, got nil", kind)
	}
	if got := domain.KindOf(err); got != kind {
		t.Fatalf("expected a %s error, got %q: %v", kind, got, err)
	}
}

func decision(ideaID uuid.UUID, stmt string) service.RecordKnowledgeInput {
	return service.RecordKnowledgeInput{IdeaID: ideaID, Kind: domain.KindDecision, Statement: stmt}
}

func knowledge(ideaID uuid.UUID, kind domain.KnowledgeKind, stmt string) service.RecordKnowledgeInput {
	return service.RecordKnowledgeInput{IdeaID: ideaID, Kind: kind, Statement: stmt}
}

func labelsOf(items []domain.KnowledgeItem) map[string]domain.KnowledgeItem {
	out := map[string]domain.KnowledgeItem{}
	for _, it := range items {
		out[it.Label] = it
	}
	return out
}

func countRows(t *testing.T, query string, args ...any) int {
	t.Helper()
	var n int
	if err := env.App.Pool.QueryRow(ctx(), query, args...).Scan(&n); err != nil {
		t.Fatalf("count %q: %v", query, err)
	}
	return n
}
