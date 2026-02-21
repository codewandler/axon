package fs

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/codewandler/axon/adapters/sqlite"
	"github.com/codewandler/axon/graph"
	"github.com/codewandler/axon/indexer"
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
	//   .hidden/
	//     secret.txt

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

	hidden := filepath.Join(dir, ".hidden")
	if err := os.Mkdir(hidden, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(hidden, "secret.txt"), []byte("secret"), 0644); err != nil {
		t.Fatal(err)
	}

	return dir
}

func setupGraph(t *testing.T) *graph.Graph {
	t.Helper()
	r := graph.NewRegistry()
	types.RegisterCommonEdges(r)
	types.RegisterFSTypes(r)
	s, err := sqlite.New(":memory:")
	if err != nil {
		t.Fatalf("sqlite.New failed: %v", err)
	}
	t.Cleanup(func() { s.Close() })
	return graph.New(s, r)
}

func TestIndexerBasic(t *testing.T) {
	ctx := context.Background()
	dir := setupTestDir(t)
	g := setupGraph(t)

	idx := New(Config{})
	emitter := indexer.NewGraphEmitter(g, "gen-1")

	ictx := &indexer.Context{
		Root:       types.PathToURI(dir),
		Generation: "gen-1",
		Graph:      g,
		Emitter:    emitter,
	}

	if err := idx.Index(ctx, ictx); err != nil {
		t.Fatalf("Index failed: %v", err)
	}

	// Find all nodes
	nodes, err := g.FindNodes(ctx, graph.NodeFilter{})
	if err != nil {
		t.Fatalf("FindNodes failed: %v", err)
	}

	// Should have: root dir, file1.txt, subdir, file2.txt, .hidden, secret.txt = 6 nodes
	if len(nodes) != 6 {
		t.Errorf("expected 6 nodes, got %d", len(nodes))
		for _, n := range nodes {
			t.Logf("  %s: %s", n.Type, n.URI)
		}
	}

	// Check we can find the root by URI
	rootNode, err := g.GetNodeByURI(ctx, types.PathToURI(dir))
	if err != nil {
		t.Fatalf("GetNodeByURI failed: %v", err)
	}
	if rootNode.Type != types.TypeDir {
		t.Errorf("expected root to be dir, got %s", rootNode.Type)
	}

	// Check root has children
	children, err := g.Children(ctx, rootNode.ID)
	if err != nil {
		t.Fatalf("Children failed: %v", err)
	}
	// file1.txt, subdir, .hidden = 3 children
	if len(children) != 3 {
		t.Errorf("expected 3 children, got %d", len(children))
	}
}

func TestIndexerIgnore(t *testing.T) {
	ctx := context.Background()
	dir := setupTestDir(t)
	g := setupGraph(t)

	idx := New(Config{
		Ignore: []string{".hidden"},
	})
	emitter := indexer.NewGraphEmitter(g, "gen-1")

	ictx := &indexer.Context{
		Root:       types.PathToURI(dir),
		Generation: "gen-1",
		Graph:      g,
		Emitter:    emitter,
	}

	if err := idx.Index(ctx, ictx); err != nil {
		t.Fatalf("Index failed: %v", err)
	}

	nodes, err := g.FindNodes(ctx, graph.NodeFilter{})
	if err != nil {
		t.Fatalf("FindNodes failed: %v", err)
	}

	// Should have: root dir, file1.txt, subdir, file2.txt, .hidden dir = 5 nodes
	// (no secret.txt inside .hidden - contents are skipped)
	// Note: ignored directories are still indexed as nodes (so we can detect deletion),
	// but their contents are skipped
	if len(nodes) != 5 {
		t.Errorf("expected 5 nodes (with .hidden dir but not contents), got %d", len(nodes))
		for _, n := range nodes {
			t.Logf("  %s: %s", n.Type, n.URI)
		}
	}

	// Verify .hidden contents are not indexed
	for _, n := range nodes {
		if filepath.Base(types.URIToPath(n.URI)) == "secret.txt" {
			t.Error("secret.txt inside .hidden should not be indexed")
		}
	}
}

func TestIndexerCollecting(t *testing.T) {
	ctx := context.Background()
	dir := setupTestDir(t)
	g := setupGraph(t)

	idx := New(Config{})
	emitter := &indexer.CollectingEmitter{}

	ictx := &indexer.Context{
		Root:       types.PathToURI(dir),
		Generation: "gen-1",
		Graph:      g,
		Emitter:    emitter,
	}

	if err := idx.Index(ctx, ictx); err != nil {
		t.Fatalf("Index failed: %v", err)
	}

	if len(emitter.Nodes) != 6 {
		t.Errorf("expected 6 nodes, got %d", len(emitter.Nodes))
	}

	// Each non-root node should have a contains + contained_by edge (bidirectional)
	if len(emitter.Edges) != 10 {
		t.Errorf("expected 10 edges (5 pairs), got %d", len(emitter.Edges))
	}
}

func TestIndexerMeta(t *testing.T) {
	idx := New(Config{})

	if idx.Name() != "fs" {
		t.Errorf("expected name 'fs', got %q", idx.Name())
	}

	schemes := idx.Schemes()
	if len(schemes) != 1 || schemes[0] != "file" {
		t.Errorf("expected schemes [file], got %v", schemes)
	}

	if !idx.Handles("file:///home/user") {
		t.Error("should handle file:// URIs")
	}

	if idx.Handles("https://example.com") {
		t.Error("should not handle https:// URIs")
	}
}
