package axon

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/codewandler/axon/graph"
	"github.com/codewandler/axon/types"
)

func setupTestDir(t *testing.T) string {
	t.Helper()

	dir := t.TempDir()

	// Create structure:
	// dir/
	//   file1.txt
	//   subdir/
	//     file2.txt
	//   .git/        (should be ignored)
	//     config

	if err := os.WriteFile(filepath.Join(dir, "file1.txt"), []byte("hello"), 0644); err != nil {
		t.Fatal(err)
	}

	subdir := filepath.Join(dir, "subdir")
	if err := os.Mkdir(subdir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(subdir, "file2.txt"), []byte("world"), 0644); err != nil {
		t.Fatal(err)
	}

	gitDir := filepath.Join(dir, ".git")
	if err := os.Mkdir(gitDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(gitDir, "config"), []byte("[core]"), 0644); err != nil {
		t.Fatal(err)
	}

	return dir
}

func TestAxonNew(t *testing.T) {
	dir := setupTestDir(t)

	ax, err := New(Config{Dir: dir})
	if err != nil {
		t.Fatalf("New failed: %v", err)
	}

	if ax.Graph() == nil {
		t.Error("Graph() returned nil")
	}
}

func TestAxonIndex(t *testing.T) {
	ctx := context.Background()
	dir := setupTestDir(t)

	ax, err := New(Config{Dir: dir})
	if err != nil {
		t.Fatalf("New failed: %v", err)
	}

	result, err := ax.Index(ctx, "")
	if err != nil {
		t.Fatalf("Index failed: %v", err)
	}

	// Should have file1.txt, file2.txt = 2 files
	if result.Files != 2 {
		t.Errorf("expected 2 files, got %d", result.Files)
	}

	// Should have dir, subdir, .git = 3 directories
	// (ignored directories are indexed as nodes so we can detect their deletion,
	// but their contents are skipped)
	if result.Directories != 3 {
		t.Errorf("expected 3 directories, got %d", result.Directories)
	}

	// .git directory should exist as a node (for deletion detection)
	nodes, err := ax.Graph().FindNodes(ctx, graph.NodeFilter{URIPrefix: types.PathToURI(filepath.Join(dir, ".git"))}, graph.QueryOptions{})
	if err != nil {
		t.Fatalf("FindNodes failed: %v", err)
	}
	if len(nodes) != 1 {
		t.Errorf("expected .git directory node, found %d nodes", len(nodes))
	}

	// But .git contents should not be indexed (check for a file that would be inside)
	allNodes, err := ax.Graph().FindNodes(ctx, graph.NodeFilter{}, graph.QueryOptions{})
	if err != nil {
		t.Fatalf("FindNodes failed: %v", err)
	}
	for _, n := range allNodes {
		path := types.URIToPath(n.URI)
		if strings.Contains(path, ".git/") {
			t.Errorf("expected .git contents to be skipped, found: %s", path)
		}
	}
}

func TestAxonReindex(t *testing.T) {
	ctx := context.Background()
	dir := setupTestDir(t)

	ax, err := New(Config{Dir: dir})
	if err != nil {
		t.Fatalf("New failed: %v", err)
	}

	// First index
	_, err = ax.Index(ctx, "")
	if err != nil {
		t.Fatalf("First Index failed: %v", err)
	}

	// Add a new file
	if err := os.WriteFile(filepath.Join(dir, "newfile.txt"), []byte("new"), 0644); err != nil {
		t.Fatal(err)
	}

	// Remove an existing file
	if err := os.Remove(filepath.Join(dir, "file1.txt")); err != nil {
		t.Fatal(err)
	}

	// Reindex
	result, err := ax.Index(ctx, "")
	if err != nil {
		t.Fatalf("Reindex failed: %v", err)
	}

	// Should have newfile.txt, file2.txt = 2 files
	if result.Files != 2 {
		t.Errorf("expected 2 files after reindex, got %d", result.Files)
	}

	// Should have removed 1 stale node (file1.txt)
	if result.StaleRemoved < 1 {
		t.Errorf("expected at least 1 stale entry removed, got %d", result.StaleRemoved)
	}

	// file1.txt should no longer exist
	_, err = ax.Graph().GetNodeByURI(ctx, types.PathToURI(filepath.Join(dir, "file1.txt")))
	if err == nil {
		t.Error("file1.txt should have been removed from graph")
	}

	// newfile.txt should exist
	_, err = ax.Graph().GetNodeByURI(ctx, types.PathToURI(filepath.Join(dir, "newfile.txt")))
	if err != nil {
		t.Error("newfile.txt should exist in graph")
	}
}

func TestAxonCustomIgnore(t *testing.T) {
	ctx := context.Background()
	dir := setupTestDir(t)

	// Don't ignore .git, but ignore subdir
	ax, err := New(Config{
		Dir:      dir,
		FSIgnore: []string{"subdir"},
	})
	if err != nil {
		t.Fatalf("New failed: %v", err)
	}

	result, err := ax.Index(ctx, "")
	if err != nil {
		t.Fatalf("Index failed: %v", err)
	}

	// Should have file1.txt, config = 2 files (.git not ignored, but subdir is)
	if result.Files != 2 {
		t.Errorf("expected 2 files, got %d", result.Files)
	}

	// subdir should exist as a node (for deletion detection) but contents skipped
	nodes, err := ax.Graph().FindNodes(ctx, graph.NodeFilter{URIPrefix: types.PathToURI(filepath.Join(dir, "subdir"))}, graph.QueryOptions{})
	if err != nil {
		t.Fatalf("FindNodes failed: %v", err)
	}
	if len(nodes) != 1 {
		t.Errorf("expected subdir directory node only, found %d nodes", len(nodes))
	}

	// Check that file2.txt inside subdir is NOT indexed
	for _, n := range nodes {
		if filepath.Base(types.URIToPath(n.URI)) == "file2.txt" {
			t.Errorf("expected subdir contents to be skipped")
		}
	}
}
