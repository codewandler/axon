package fs

import (
	"context"
	"os"
	"path/filepath"
	"strings"
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

	idx := New(Config{Exclude: []string{".*"}})
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
	nodes, err := g.FindNodes(ctx, graph.NodeFilter{}, graph.QueryOptions{})
	if err != nil {
		t.Fatalf("FindNodes failed: %v", err)
	}

	// Should have: root dir, file1.txt, subdir, file2.txt, .hidden dir = 5 nodes
	// .hidden matches the '.*' Exclude pattern so it gets a minimal node
	// for deletion detection. secret.txt inside .hidden is not indexed.
	if len(nodes) != 5 {
		t.Errorf("expected 5 nodes, got %d", len(nodes))
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
	// (.hidden is auto-ignored but still gets a minimal node with containment edges
	// for deletion detection)
	if len(children) != 3 {
		t.Errorf("expected 3 children, got %d", len(children))
		for _, n := range children {
			t.Logf("  %s: %s", n.Type, n.URI)
		}
	}
}

func TestIndexerIgnore(t *testing.T) {
	ctx := context.Background()
	dir := setupTestDir(t)
	g := setupGraph(t)

	// Ignore is a deprecated alias for Exclude; both are merged in New().
	// Use '.*' to exclude hidden dirs and 'subdir' to exclude subdir.
	idx := New(Config{
		Ignore: []string{".*", "subdir"},
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

	nodes, err := g.FindNodes(ctx, graph.NodeFilter{}, graph.QueryOptions{})
	if err != nil {
		t.Fatalf("FindNodes failed: %v", err)
	}

	// Should have: root dir, file1.txt, .hidden dir, subdir dir = 4 nodes
	// .hidden: matches '.*' Exclude pattern (minimal node, contents skipped)
	// subdir: matches 'subdir' Exclude pattern (minimal node, contents skipped)
	// file2.txt inside subdir: not indexed
	// secret.txt inside .hidden: not indexed
	if len(nodes) != 4 {
		t.Errorf("expected 4 nodes (root, file1.txt, .hidden dir, subdir dir), got %d", len(nodes))
		for _, n := range nodes {
			t.Logf("  %s: %s", n.Type, n.URI)
		}
	}

	// Verify ignored directory contents are not indexed
	for _, n := range nodes {
		name := filepath.Base(types.URIToPath(n.URI))
		if name == "secret.txt" {
			t.Error("secret.txt inside .hidden should not be indexed")
		}
		if name == "file2.txt" {
			t.Error("file2.txt inside subdir should not be indexed")
		}
	}
}

func TestIndexerCollecting(t *testing.T) {
	ctx := context.Background()
	dir := setupTestDir(t)
	g := setupGraph(t)

	idx := New(Config{Exclude: []string{".*"}})
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

	if len(emitter.Nodes) != 5 {
		t.Errorf("expected 5 nodes, got %d", len(emitter.Nodes))
		for _, n := range emitter.Nodes {
			t.Logf("  %s: %s", n.Type, n.URI)
		}
	}

	// Each non-root node should have a contains + contained_by edge (bidirectional)
	// 4 non-root nodes × 2 edges each = 8 edges
	if len(emitter.Edges) != 8 {
		t.Errorf("expected 8 edges (4 pairs), got %d", len(emitter.Edges))
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

func TestIndexSymlink(t *testing.T) {
	ctx := context.Background()
	testDir := t.TempDir()
	g := setupGraph(t)

	// Create a file and a symlink to it
	targetFile := filepath.Join(testDir, "target.txt")
	if err := os.WriteFile(targetFile, []byte("content"), 0644); err != nil {
		t.Fatalf("WriteFile failed: %v", err)
	}

	symlinkPath := filepath.Join(testDir, "link.txt")
	if err := os.Symlink(targetFile, symlinkPath); err != nil {
		if os.IsPermission(err) {
			t.Skip("symlink creation not permitted")
		}
		t.Fatalf("Symlink failed: %v", err)
	}

	// Index the directory
	idx := New(Config{})
	emitter := indexer.NewGraphEmitter(g, "gen-1")

	ictx := &indexer.Context{
		Root:       types.PathToURI(testDir),
		Generation: "gen-1",
		Graph:      g,
		Emitter:    emitter,
	}

	if err := idx.Index(ctx, ictx); err != nil {
		t.Fatalf("Index failed: %v", err)
	}

	// Flush to ensure writes are visible
	if err := g.Storage().Flush(ctx); err != nil {
		t.Fatalf("Flush failed: %v", err)
	}

	// Verify both file and symlink were indexed
	nodes, err := g.FindNodes(ctx, graph.NodeFilter{}, graph.QueryOptions{})
	if err != nil {
		t.Fatalf("FindNodes failed: %v", err)
	}

	var foundTarget, foundSymlink bool
	for _, node := range nodes {
		if node.Type == types.TypeFile && strings.HasSuffix(node.URI, "target.txt") {
			foundTarget = true
		}
		if node.Type == types.TypeLink && strings.HasSuffix(node.URI, "link.txt") {
			foundSymlink = true
		}
	}

	if !foundTarget {
		t.Error("target file not indexed")
	}
	if !foundSymlink {
		t.Error("symlink not indexed")
	}
}

func TestIndexerInclude(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	g := setupGraph(t)

	// Create two files with different extensions inside a subdirectory.
	if err := os.WriteFile(filepath.Join(dir, "keep.txt"), []byte("text"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "skip.md"), []byte("# markdown"), 0644); err != nil {
		t.Fatal(err)
	}
	sub := filepath.Join(dir, "sub")
	if err := os.Mkdir(sub, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(sub, "nested.txt"), []byte("nested"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(sub, "skip.go"), []byte("package sub"), 0644); err != nil {
		t.Fatal(err)
	}

	// Include only .txt files; directories are always traversed.
	idx := New(Config{Include: []string{"*.txt"}})
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

	nodes, err := g.FindNodes(ctx, graph.NodeFilter{}, graph.QueryOptions{})
	if err != nil {
		t.Fatalf("FindNodes failed: %v", err)
	}

	// Expected: root dir + keep.txt + sub dir + nested.txt = 4 nodes.
	// skip.md and skip.go don't match Include pattern.
	if len(nodes) != 4 {
		t.Errorf("expected 4 nodes (root, keep.txt, sub/, nested.txt), got %d", len(nodes))
		for _, n := range nodes {
			t.Logf("  %s: %s", n.Type, n.URI)
		}
	}

	// keep.txt must be present
	if _, err := g.GetNodeByURI(ctx, types.PathToURI(filepath.Join(dir, "keep.txt"))); err != nil {
		t.Error("keep.txt should be indexed")
	}

	// skip.md must NOT be present
	if _, err := g.GetNodeByURI(ctx, types.PathToURI(filepath.Join(dir, "skip.md"))); err == nil {
		t.Error("skip.md should not be indexed (doesn't match Include)")
	}

	// nested.txt must be present (subdirectory is traversed even without a match)
	if _, err := g.GetNodeByURI(ctx, types.PathToURI(filepath.Join(sub, "nested.txt"))); err != nil {
		t.Error("nested.txt should be indexed")
	}

	// skip.go must NOT be present
	if _, err := g.GetNodeByURI(ctx, types.PathToURI(filepath.Join(sub, "skip.go"))); err == nil {
		t.Error("skip.go should not be indexed (doesn't match Include)")
	}
}
