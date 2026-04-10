package main

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	sqliteadapter "github.com/codewandler/axon/adapters/sqlite"
	"github.com/codewandler/axon/graph"
)

// setupGCTestDB creates a real SQLite DB in a temp dir, populated with nodes
// and an orphaned edge, and returns an openedCmdCtx-like struct via the package-
// level openDB path.  Because openDB reads global state (flagDBDir, flagGlobal)
// we set up a .axon/graph.db in a temp dir, seed it manually, and invoke
// runGC by pointing the global DB-dir flag at that dir.
func setupGCTestDB(t *testing.T) (dbPath string, cleanup func()) {
	t.Helper()
	dir := t.TempDir()
	axonDir := filepath.Join(dir, ".axon")
	if err := os.MkdirAll(axonDir, 0755); err != nil {
		t.Fatalf("mkdir .axon: %v", err)
	}
	dbPath = filepath.Join(axonDir, "graph.db")

	s, err := sqliteadapter.New(dbPath)
	if err != nil {
		t.Fatalf("New sqlite: %v", err)
	}
	defer s.Close()

	ctx := context.Background()

	node1 := graph.NewNode("vcs:repo").WithName(".")
	node2 := graph.NewNode("fs:file").WithURI("file:///src/old.go").WithName("old.go")
	if err := s.PutNode(ctx, node1); err != nil {
		t.Fatalf("PutNode node1: %v", err)
	}
	if err := s.PutNode(ctx, node2); err != nil {
		t.Fatalf("PutNode node2: %v", err)
	}

	edge := graph.NewEdge("contains", node1.ID, node2.ID)
	if err := s.PutEdge(ctx, edge); err != nil {
		t.Fatalf("PutEdge: %v", err)
	}

	// Remove node2 so the edge becomes orphaned.
	if err := s.DeleteNode(ctx, node2.ID); err != nil {
		t.Fatalf("DeleteNode node2: %v", err)
	}
	if err := s.Flush(ctx); err != nil {
		t.Fatalf("Flush: %v", err)
	}

	return dbPath, func() {}
}

// captureGCOutput runs runGC with the supplied flags and captures stdout.
func captureGCOutput(t *testing.T, dbPath string, dryRun, quiet bool) string {
	t.Helper()

	// Override global flags used by openDB.
	old := flagDBDir
	flagDBDir = filepath.Dir(dbPath) // points directly to the .axon/ dir
	defer func() { flagDBDir = old }()

	oldDry := flagGCDryRun
	flagGCDryRun = dryRun
	defer func() { flagGCDryRun = oldDry }()

	oldQuiet := flagGCQuiet
	flagGCQuiet = quiet
	defer func() { flagGCQuiet = oldQuiet }()

	// Redirect stdout.
	origStdout := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("os.Pipe: %v", err)
	}
	os.Stdout = w

	runErr := runGC(gcCmd, nil)

	w.Close()
	os.Stdout = origStdout

	var buf bytes.Buffer
	buf.ReadFrom(r)
	output := buf.String()

	if runErr != nil {
		t.Fatalf("runGC error: %v", runErr)
	}
	return output
}

// edgeCountInDB counts edges remaining in the SQLite file directly.
func edgeCountInDB(t *testing.T, dbPath string) int {
	t.Helper()
	s, err := sqliteadapter.New(dbPath)
	if err != nil {
		t.Fatalf("reopen db: %v", err)
	}
	defer s.Close()
	ctx := context.Background()
	edges, err := s.FindOrphanedEdges(ctx)
	if err != nil {
		t.Fatalf("FindOrphanedEdges: %v", err)
	}
	return len(edges)
}

// TestGC_DryRun_NoWrites verifies that --dry-run produces no deletes.
func TestGC_DryRun_NoWrites(t *testing.T) {
	dbPath, _ := setupGCTestDB(t)

	out := captureGCOutput(t, dbPath, true /* dryRun */, false /* quiet */)

	// Output must mention "Would delete" (dry-run language).
	if !strings.Contains(out, "Would delete") {
		t.Errorf("expected 'Would delete' in dry-run output, got:\n%s", out)
	}
	// Output must contain the dry-run notice.
	if !strings.Contains(out, "dry run") {
		t.Errorf("expected 'dry run' in output, got:\n%s", out)
	}
	// Edge must still be present (no writes).
	if count := edgeCountInDB(t, dbPath); count != 1 {
		t.Errorf("dry-run should not delete edges; orphaned count = %d, want 1", count)
	}
}

// TestGC_NormalRun_DeletesOrphans verifies that a normal run removes orphaned edges.
func TestGC_NormalRun_DeletesOrphans(t *testing.T) {
	dbPath, _ := setupGCTestDB(t)

	out := captureGCOutput(t, dbPath, false /* dryRun */, false /* quiet */)

	// Output must mention "Deleted".
	if !strings.Contains(out, "Deleted") {
		t.Errorf("expected 'Deleted' in normal-run output, got:\n%s", out)
	}
	// Edge must be gone.
	if count := edgeCountInDB(t, dbPath); count != 0 {
		t.Errorf("normal run should delete orphaned edges; orphaned count = %d, want 0", count)
	}
}

// TestGC_Verbose_ListsEdges verifies that verbose (non-quiet) output contains
// per-edge lines with type and node description.
func TestGC_Verbose_ListsEdges(t *testing.T) {
	dbPath, _ := setupGCTestDB(t)

	out := captureGCOutput(t, dbPath, true /* dryRun */, false /* quiet */)

	// Must contain the edge type in brackets.
	if !strings.Contains(out, "[contains]") {
		t.Errorf("verbose output missing '[contains]' edge type line, got:\n%s", out)
	}
	// Must show the "Orphaned edges" header.
	if !strings.Contains(out, "Orphaned edges") {
		t.Errorf("verbose output missing 'Orphaned edges' header, got:\n%s", out)
	}
}

// TestGC_Quiet_SuppressesDetail verifies that --quiet shows only the summary.
func TestGC_Quiet_SuppressesDetail(t *testing.T) {
	dbPath, _ := setupGCTestDB(t)

	out := captureGCOutput(t, dbPath, true /* dryRun */, true /* quiet */)

	// Must NOT contain per-edge lines (no brackets).
	if strings.Contains(out, "[contains]") {
		t.Errorf("quiet mode should suppress per-edge lines, got:\n%s", out)
	}
	// Must still show a summary count.
	if !strings.Contains(out, "1") {
		t.Errorf("quiet mode should show count, got:\n%s", out)
	}
}
