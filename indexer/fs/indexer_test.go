package fs

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/codewandler/axon/graph"
	"github.com/codewandler/axon/indexer"
	"github.com/codewandler/axon/storage/memory"
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
	types.RegisterFSTypes(r)
	s := memory.New()
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

	// Should have: root dir, file1.txt, subdir, file2.txt = 4 nodes (no .hidden or secret.txt)
	if len(nodes) != 4 {
		t.Errorf("expected 4 nodes (with .hidden ignored), got %d", len(nodes))
		for _, n := range nodes {
			t.Logf("  %s: %s", n.Type, n.URI)
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

	// Each non-root node should have a contains edge
	if len(emitter.Edges) != 5 {
		t.Errorf("expected 5 edges, got %d", len(emitter.Edges))
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
