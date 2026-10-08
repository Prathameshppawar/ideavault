package db

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgconn"
)

func TestRepositoryMigrationsAreWellFormed(t *testing.T) {
	dir, err := FindMigrationsDir("")
	if err != nil {
		t.Fatalf("FindMigrationsDir: %v", err)
	}
	migs, err := LoadMigrations(dir)
	if err != nil {
		t.Fatalf("LoadMigrations: %v", err)
	}
	if len(migs) < 4 {
		t.Fatalf("expected at least 4 migrations, got %d", len(migs))
	}
	for i, m := range migs {
		if m.Version != i+1 || m.Name == "" || len(m.Checksum) != 64 || strings.TrimSpace(m.Up) == "" || strings.TrimSpace(m.Down) == "" {
			t.Errorf("migration %d malformed: version=%d name=%q", i, m.Version, m.Name)
		}
	}
	// Down migrations of tables protected by immutability triggers must enable the escape hatch.
	for _, m := range migs {
		if strings.Contains(m.Up, "iv_forbid_history_mutation()") && strings.Contains(m.Up, "CREATE TRIGGER") && !strings.Contains(m.Down, "allow_history_delete") && m.Version > 1 {
			t.Errorf("%04d_%s creates immutable tables but its down migration does not allow history deletion", m.Version, m.Name)
		}
	}
}

func writeFiles(t *testing.T, files map[string]string) string {
	t.Helper()
	dir := t.TempDir()
	for name, body := range files {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

func TestLoadMigrationsValidation(t *testing.T) {
	tests := []struct {
		name    string
		files   map[string]string
		wantErr string
	}{
		{"ok", map[string]string{"0001_a.up.sql": "x", "0001_a.down.sql": "y", "0002_b.up.sql": "x", "0002_b.down.sql": "y", "README.md": "ignored"}, ""},
		{"missing down", map[string]string{"0001_a.up.sql": "x"}, "missing its .down.sql"},
		{"missing up", map[string]string{"0001_a.down.sql": "x"}, "missing its .up.sql"},
		{"gap", map[string]string{"0001_a.up.sql": "x", "0001_a.down.sql": "y", "0003_c.up.sql": "x", "0003_c.down.sql": "y"}, "contiguous"},
		{"conflicting names", map[string]string{"0001_a.up.sql": "x", "0001_b.down.sql": "y"}, "conflicting names"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			migs, err := LoadMigrations(writeFiles(t, tt.files))
			if tt.wantErr == "" {
				if err != nil || len(migs) != 2 || migs[0].Name != "a" || migs[1].Version != 2 {
					t.Fatalf("got %+v, %v", migs, err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Errorf("want error containing %q, got %v", tt.wantErr, err)
			}
		})
	}
	if _, err := LoadMigrations(filepath.Join(t.TempDir(), "missing")); err == nil {
		t.Error("missing directory must fail")
	}
}

func TestFindMigrationsDirExplicit(t *testing.T) {
	dir := t.TempDir()
	got, err := FindMigrationsDir(dir)
	if err != nil || got != dir {
		t.Errorf("explicit dir: %q %v", got, err)
	}
	if _, err := FindMigrationsDir(filepath.Join(dir, "nope")); err == nil {
		t.Error("explicit missing dir must fail")
	}
}

func TestErrorClassification(t *testing.T) {
	tests := []struct {
		code              string
		unique, fk, check bool
	}{
		{"23505", true, false, false},
		{"23503", false, true, false},
		{"23514", false, false, true},
		{"42P01", false, false, false},
	}
	for _, tt := range tests {
		err := error(&pgconn.PgError{Code: tt.code, ConstraintName: "c_" + tt.code})
		wrapped := errors.Join(errors.New("ctx"), err)
		if IsUniqueViolation(wrapped) != tt.unique || IsForeignKeyViolation(wrapped) != tt.fk || IsCheckViolation(wrapped) != tt.check {
			t.Errorf("classification of %s wrong", tt.code)
		}
		if ConstraintName(wrapped) != "c_"+tt.code {
			t.Errorf("ConstraintName = %q", ConstraintName(wrapped))
		}
	}
	if IsUniqueViolation(errors.New("x")) || ConstraintName(errors.New("x")) != "" {
		t.Error("non-pg errors are unclassified")
	}
}
