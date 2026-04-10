package main

import (
	"os"
	"path/filepath"
	"testing"
)

// mkAxonDB creates the .axon/graph.db structure inside dir.
func mkAxonDB(t *testing.T, dir string) {
	t.Helper()
	axonPath := filepath.Join(dir, axonDir)
	if err := os.MkdirAll(axonPath, 0755); err != nil {
		t.Fatalf("mkAxonDB: failed to create %s: %v", axonPath, err)
	}
	dbPath := filepath.Join(axonPath, axonDBFile)
	if err := os.WriteFile(dbPath, []byte{}, 0644); err != nil {
		t.Fatalf("mkAxonDB: failed to create %s: %v", dbPath, err)
	}
}

// TestResolveDB_Default_NoCWDDB_Fails verifies that the default (non-global)
// mode does NOT walk up to a parent directory — it must return an error when
// the CWD has no DB even if an ancestor does.
func TestResolveDB_Default_NoCWDDB_Fails(t *testing.T) {
	parent := t.TempDir()
	mkAxonDB(t, parent)

	child := filepath.Join(parent, "child")
	if err := os.Mkdir(child, 0755); err != nil {
		t.Fatalf("failed to create child dir: %v", err)
	}

	loc, err := resolveDB("", false, child, false)
	if err == nil {
		t.Fatalf("expected an error (no DB in CWD), but got loc=%+v", loc)
	}
}

// TestResolveDB_Default_CWDHasDB_Succeeds verifies that when the CWD contains
// a valid .axon/graph.db the default mode returns it.
func TestResolveDB_Default_CWDHasDB_Succeeds(t *testing.T) {
	dir := t.TempDir()
	mkAxonDB(t, dir)

	loc, err := resolveDB("", false, dir, false)
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}

	want := filepath.Join(dir, axonDir, axonDBFile)
	if loc.Path != want {
		t.Errorf("loc.Path = %q, want %q", loc.Path, want)
	}
}

// TestResolveDB_Global_WalksUp verifies that --global mode walks up the
// directory tree and finds a DB in a parent directory.
func TestResolveDB_Global_WalksUp(t *testing.T) {
	parent := t.TempDir()
	mkAxonDB(t, parent)

	child := filepath.Join(parent, "child")
	if err := os.Mkdir(child, 0755); err != nil {
		t.Fatalf("failed to create child dir: %v", err)
	}

	loc, err := resolveDB("", true, child, false)
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}

	want := filepath.Join(parent, axonDir, axonDBFile)
	if loc.Path != want {
		t.Errorf("loc.Path = %q, want %q", loc.Path, want)
	}
}

// TestResolveDB_Default_ForWrite_CreatesCWDDB verifies that with forWrite=true
// the default mode creates .axon/graph.db in the CWD and reports Created=true.
func TestResolveDB_Default_ForWrite_CreatesCWDDB(t *testing.T) {
	dir := t.TempDir()
	// No DB pre-created — resolveDB should make it.

	loc, err := resolveDB("", false, dir, true)
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}

	want := filepath.Join(dir, axonDir, axonDBFile)
	if loc.Path != want {
		t.Errorf("loc.Path = %q, want %q", loc.Path, want)
	}
	if !loc.Created {
		t.Errorf("loc.Created = false, want true")
	}
	if loc.IsGlobal {
		t.Errorf("loc.IsGlobal = true, want false")
	}
}

// TestResolveDB_ExplicitDBDir_Wins verifies that an explicit --db-dir value
// takes precedence over everything else. The dir points directly to the folder
// that contains graph.db (NOT a .axon/ sub-directory).
func TestResolveDB_ExplicitDBDir_Wins(t *testing.T) {
	startDir := t.TempDir()
	explicitDir := t.TempDir()

	// Place graph.db directly in explicitDir (no .axon/ sub-dir).
	dbPath := filepath.Join(explicitDir, axonDBFile)
	if err := os.WriteFile(dbPath, []byte{}, 0644); err != nil {
		t.Fatalf("failed to create %s: %v", dbPath, err)
	}

	loc, err := resolveDB(explicitDir, false, startDir, false)
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}

	want := filepath.Join(explicitDir, axonDBFile)
	if loc.Path != want {
		t.Errorf("loc.Path = %q, want %q", loc.Path, want)
	}
}
