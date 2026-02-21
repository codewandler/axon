package render

import (
	"context"
	"strings"
	"testing"

	"github.com/codewandler/axon/graph"
	"github.com/codewandler/axon/storage/memory"
	"github.com/codewandler/axon/types"
)

func setupTestGraph(t *testing.T) (*graph.Graph, string) {
	t.Helper()

	r := graph.NewRegistry()
	types.RegisterFSTypes(r)
	s := memory.New()
	g := graph.New(s, r)
	ctx := context.Background()

	// Create structure:
	// root/
	//   file1.txt
	//   subdir/
	//     file2.txt
	//     nested/
	//       deep.txt

	root := graph.NewNode(types.TypeDir).
		WithURI("file:///root").
		WithKey("/root").
		WithData(types.DirData{Name: "root"})
	_ = g.Storage().PutNode(ctx, root)

	file1 := graph.NewNode(types.TypeFile).
		WithURI("file:///root/file1.txt").
		WithKey("/root/file1.txt").
		WithData(types.FileData{Name: "file1.txt"})
	_ = g.Storage().PutNode(ctx, file1)
	_ = g.Storage().PutEdge(ctx, graph.NewEdge(types.EdgeContains, root.ID, file1.ID))

	subdir := graph.NewNode(types.TypeDir).
		WithURI("file:///root/subdir").
		WithKey("/root/subdir").
		WithData(types.DirData{Name: "subdir"})
	_ = g.Storage().PutNode(ctx, subdir)
	_ = g.Storage().PutEdge(ctx, graph.NewEdge(types.EdgeContains, root.ID, subdir.ID))

	file2 := graph.NewNode(types.TypeFile).
		WithURI("file:///root/subdir/file2.txt").
		WithKey("/root/subdir/file2.txt").
		WithData(types.FileData{Name: "file2.txt"})
	_ = g.Storage().PutNode(ctx, file2)
	_ = g.Storage().PutEdge(ctx, graph.NewEdge(types.EdgeContains, subdir.ID, file2.ID))

	nested := graph.NewNode(types.TypeDir).
		WithURI("file:///root/subdir/nested").
		WithKey("/root/subdir/nested").
		WithData(types.DirData{Name: "nested"})
	_ = g.Storage().PutNode(ctx, nested)
	_ = g.Storage().PutEdge(ctx, graph.NewEdge(types.EdgeContains, subdir.ID, nested.ID))

	deep := graph.NewNode(types.TypeFile).
		WithURI("file:///root/subdir/nested/deep.txt").
		WithKey("/root/subdir/nested/deep.txt").
		WithData(types.FileData{Name: "deep.txt"})
	_ = g.Storage().PutNode(ctx, deep)
	_ = g.Storage().PutEdge(ctx, graph.NewEdge(types.EdgeContains, nested.ID, deep.ID))

	return g, root.ID
}

func TestTreeBasic(t *testing.T) {
	ctx := context.Background()
	g, rootID := setupTestGraph(t)

	opts := Options{
		MaxDepth:  0, // unlimited
		ShowIDs:   true,
		ShowTypes: true,
	}

	output, err := Tree(ctx, g, rootID, opts)
	if err != nil {
		t.Fatalf("Tree failed: %v", err)
	}

	// Check for expected content
	if !strings.Contains(output, "root/") {
		t.Error("output should contain root/")
	}
	if !strings.Contains(output, "file1.txt") {
		t.Error("output should contain file1.txt")
	}
	if !strings.Contains(output, "subdir/") {
		t.Error("output should contain subdir/")
	}
	if !strings.Contains(output, "file2.txt") {
		t.Error("output should contain file2.txt")
	}
	if !strings.Contains(output, "nested/") {
		t.Error("output should contain nested/")
	}
	if !strings.Contains(output, "deep.txt") {
		t.Error("output should contain deep.txt")
	}
	if !strings.Contains(output, "(fs:dir)") {
		t.Error("output should contain type annotations")
	}
	if !strings.Contains(output, "[") {
		t.Error("output should contain node IDs")
	}
}

func TestTreeDepthLimit(t *testing.T) {
	ctx := context.Background()
	g, rootID := setupTestGraph(t)

	opts := Options{
		MaxDepth:  2,
		ShowIDs:   false,
		ShowTypes: false,
	}

	output, err := Tree(ctx, g, rootID, opts)
	if err != nil {
		t.Fatalf("Tree failed: %v", err)
	}

	// Should show depth indication for nested/
	if !strings.Contains(output, "... +1 items") {
		t.Error("output should indicate nested has children at depth limit")
	}

	// Should not contain deep.txt (beyond depth)
	if strings.Contains(output, "deep.txt") {
		t.Error("output should not contain deep.txt (beyond depth limit)")
	}
}

func TestTreeNoIDs(t *testing.T) {
	ctx := context.Background()
	g, rootID := setupTestGraph(t)

	opts := Options{
		MaxDepth:  0,
		ShowIDs:   false,
		ShowTypes: false,
	}

	output, err := Tree(ctx, g, rootID, opts)
	if err != nil {
		t.Fatalf("Tree failed: %v", err)
	}

	if strings.Contains(output, "[") {
		t.Error("output should not contain IDs when ShowIDs is false")
	}
	if strings.Contains(output, "(fs:") {
		t.Error("output should not contain types when ShowTypes is false")
	}
}

func TestTreeFromURI(t *testing.T) {
	ctx := context.Background()
	g, _ := setupTestGraph(t)

	output, err := TreeFromURI(ctx, g, "file:///root/subdir", DefaultOptions())
	if err != nil {
		t.Fatalf("TreeFromURI failed: %v", err)
	}

	// Should contain subdir and an ID
	if !strings.Contains(output, "subdir/") || !strings.Contains(output, "[") {
		t.Error("output should contain subdir and node ID")
	}
}

func TestTreeStructure(t *testing.T) {
	ctx := context.Background()
	g, rootID := setupTestGraph(t)

	opts := Options{
		MaxDepth:  0,
		ShowIDs:   false,
		ShowTypes: false,
	}

	output, err := Tree(ctx, g, rootID, opts)
	if err != nil {
		t.Fatalf("Tree failed: %v", err)
	}

	// Check tree structure characters exist
	if !strings.Contains(output, "├──") && !strings.Contains(output, "└──") {
		t.Error("output should contain tree branch characters")
	}
}
