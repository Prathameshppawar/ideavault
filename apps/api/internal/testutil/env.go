package testutil

import (
	"context"
	"fmt"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"time"

	"github.com/google/uuid"

	"github.com/Prathameshppawar/ideavault/apps/api/internal/app"
	"github.com/Prathameshppawar/ideavault/apps/api/internal/domain"
	"github.com/Prathameshppawar/ideavault/apps/api/internal/repository/postgres"
	"github.com/Prathameshppawar/ideavault/apps/api/internal/security"
	"github.com/Prathameshppawar/ideavault/apps/api/internal/service"
)

// DefaultPassword is the password of every user created by Env.NewUser.
const DefaultPassword = "correct horse battery staple"

var (
	hashOnce sync.Once
	hashVal  string
	hashErr  error
	userSeq  atomic.Int64
)

// passwordHash computes the Argon2id hash of DefaultPassword once per process.
func passwordHash() (string, error) {
	hashOnce.Do(func() { hashVal, hashErr = security.HashPassword(DefaultPassword) })
	return hashVal, hashErr
}

// Env is a migrated database, a wired App and an httptest server in front of it.
type Env struct {
	App        *app.App
	Server     *httptest.Server
	URL        string
	Migrations *MigrationReport
}

// Setup verifies migrations (up → down → up) on a fresh schema, builds the App
// and starts an httptest server. Call Close when done.
func Setup(ctx context.Context, url string) (*Env, error) {
	rep, err := VerifyMigrations(ctx, url)
	if err != nil {
		return nil, fmt.Errorf("prepare schema: %w", err)
	}
	a, err := NewApp(ctx, Config(url))
	if err != nil {
		return nil, fmt.Errorf("build app: %w", err)
	}
	return &Env{App: a, Server: httptest.NewServer(a.Handler()), URL: url, Migrations: rep}, nil
}

// Close stops the server and releases the App.
func (e *Env) Close() {
	if e.Server != nil {
		e.Server.Close()
	}
	if e.App != nil {
		e.App.Close()
	}
}

// Svc returns the business-logic service.
func (e *Env) Svc() *service.Service { return e.App.Service }

// Store returns the repository.
func (e *Env) Store() *postgres.Store { return e.App.Store }

// User is a test user.
type User struct {
	ID       uuid.UUID
	Email    string
	Password string
}

// Actor returns a service actor acting as the user.
func (u *User) Actor() service.Actor { return service.UserActor(u.ID) }

// NewUser inserts a fresh user directly (fast; the password hash is shared).
func (e *Env) NewUser(t TB) *User {
	t.Helper()
	h, err := passwordHash()
	if err != nil {
		t.Fatalf("hash password: %v", err)
	}
	email := fmt.Sprintf("user%d-%s@ideavault.test", userSeq.Add(1), uuid.NewString()[:8])
	u, err := e.Store().CreateUser(context.Background(), email, "Test User", h)
	if err != nil {
		t.Fatalf("create user: %v", err)
	}
	return &User{ID: u.ID, Email: email, Password: DefaultPassword}
}

// TruncateAll deletes every user and everything owned by users (the model
// registry is kept). Use it only in tests that need a pristine vault (first-run
// setup); tests in the integration package run sequentially.
func (e *Env) TruncateAll(t TB) {
	t.Helper()
	if _, err := e.App.Pool.Exec(context.Background(), `TRUNCATE users, jobs CASCADE`); err != nil {
		t.Fatalf("truncate: %v", err)
	}
}

// DrainJobs runs every queued job until the queue is empty. Delayed jobs (e.g.
// embeddings scheduled half a second ahead) are pulled forward so results are
// deterministic and no test sleeps.
func (e *Env) DrainJobs(t TB) int {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	total := 0
	for round := 0; round < 5; round++ {
		if _, err := e.App.Pool.Exec(ctx, `UPDATE jobs SET run_after = now() WHERE status = 'QUEUED' AND run_after > now()`); err != nil {
			t.Fatalf("pull jobs forward: %v", err)
		}
		n, err := e.App.Worker.Drain(ctx, 1000)
		if err != nil {
			t.Fatalf("drain jobs: %v", err)
		}
		total += n
		var pending int
		if err := e.App.Pool.QueryRow(ctx, `SELECT count(*) FROM jobs WHERE status = 'QUEUED'`).Scan(&pending); err != nil {
			t.Fatalf("count jobs: %v", err)
		}
		if pending == 0 {
			return total
		}
	}
	return total
}

// FixturePath returns the absolute path of a shared fixture under tests/fixtures.
func FixturePath(t TB, parts ...string) string {
	t.Helper()
	root, err := RepoRoot()
	if err != nil {
		t.Fatalf("repo root: %v", err)
	}
	return filepath.Join(append([]string{root, "tests", "fixtures"}, parts...)...)
}

// Fixture reads a shared fixture file under tests/fixtures.
func Fixture(t TB, parts ...string) []byte {
	t.Helper()
	b, err := os.ReadFile(FixturePath(t, parts...))
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	return b
}

// MustIdea creates an idea for u and fails the test on error.
func (e *Env) MustIdea(t TB, u *User, title, origin string) *service.IdeaCreated {
	t.Helper()
	res, err := e.Svc().CreateIdea(context.Background(), u.Actor(), service.CreateIdeaInput{Title: title, OriginText: origin})
	if err != nil {
		t.Fatalf("create idea %q: %v", title, err)
	}
	return res
}

// MustRecord records a knowledge item and fails the test on error.
func (e *Env) MustRecord(t TB, u *User, in service.RecordKnowledgeInput) *domain.KnowledgeItem {
	t.Helper()
	it, err := e.Svc().RecordKnowledge(context.Background(), u.Actor(), in)
	if err != nil {
		t.Fatalf("record %s %q: %v", in.Kind, in.Statement, err)
	}
	return it
}

// MustCheckpoint creates a checkpoint and fails the test on error.
func (e *Env) MustCheckpoint(t TB, u *User, ideaID uuid.UUID, branchID *uuid.UUID, title string) *domain.Checkpoint {
	t.Helper()
	cp, err := e.Svc().CreateCheckpoint(context.Background(), u.Actor(), service.CreateCheckpointInput{IdeaID: ideaID, BranchID: branchID, Title: title})
	if err != nil {
		t.Fatalf("create checkpoint: %v", err)
	}
	return cp
}
