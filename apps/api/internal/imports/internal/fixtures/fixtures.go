// Package fixtures locates the repository's shared import fixtures
// (tests/fixtures/imports) for tests in the imports packages.
package fixtures

import (
	"os"
	"path/filepath"
	"testing"
)

// Root returns the absolute path of tests/fixtures/imports, found by walking
// up from the working directory. It fails the test when not found.
func Root(t testing.TB) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	for {
		candidate := filepath.Join(dir, "tests", "fixtures")
		if st, err := os.Stat(candidate); err == nil && st.IsDir() {
			return filepath.Join(candidate, "imports")
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatalf("tests/fixtures not found above working directory")
		}
		dir = parent
	}
}

// Path joins parts onto the fixture root.
func Path(t testing.TB, parts ...string) string {
	t.Helper()
	return filepath.Join(append([]string{Root(t)}, parts...)...)
}

// Read returns the contents of a fixture file.
func Read(t testing.TB, parts ...string) []byte {
	t.Helper()
	p := Path(t, parts...)
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatalf("read fixture %s: %v", p, err)
	}
	return b
}
