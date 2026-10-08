package integration

import (
	"testing"
)

// TestMigrationsUpDownUp checks the verification TestMain ran on a fresh schema:
// every migration applies, rolls back completely and re-applies without drift.
func TestMigrationsUpDownUp(t *testing.T) {
	e := requireEnv(t)
	rep := e.Migrations
	if err := rep.Err(); err != nil {
		t.Fatalf("migrations are not reversible: %v", err)
	}
	if rep.Count < 4 {
		t.Fatalf("expected at least 4 migrations, got %d", rep.Count)
	}
	if len(rep.DownTables) != 0 {
		t.Errorf("rolling back every migration left tables behind: %v", rep.DownTables)
	}
	if len(rep.Status) != rep.Count {
		t.Fatalf("status rows = %d", len(rep.Status))
	}
	for _, s := range rep.Status {
		if !s.Applied || s.Drift {
			t.Errorf("migration %04d_%s applied=%v drift=%v", s.Version, s.Name, s.Applied, s.Drift)
		}
	}
	// The schema the tests run against has the core tables, extensions and triggers.
	for _, table := range []string{"users", "ideas", "branches", "checkpoints", "knowledge_items", "decisions", "relationships", "artifacts",
		"artifact_versions", "artifact_provenance", "context_packs", "deltas", "imports", "import_items", "jobs", "embeddings", "agent_runs", "tool_calls"} {
		if countRows(t, `SELECT count(*) FROM pg_tables WHERE schemaname = 'public' AND tablename = $1`, table) != 1 {
			t.Errorf("table %s missing", table)
		}
	}
	for _, ext := range []string{"vector", "pg_trgm", "citext", "pgcrypto"} {
		if countRows(t, `SELECT count(*) FROM pg_extension WHERE extname = $1`, ext) != 1 {
			t.Errorf("extension %s missing", ext)
		}
	}
	for _, trg := range []string{"checkpoints_immutable", "checkpoint_items_immutable", "idea_versions_immutable", "artifact_versions_immutable",
		"artifact_provenance_immutable", "knowledge_content_guard"} {
		if countRows(t, `SELECT count(*) FROM pg_trigger WHERE tgname = $1`, trg) != 1 {
			t.Errorf("trigger %s missing", trg)
		}
	}
}
