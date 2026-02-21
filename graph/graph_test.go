package graph_test

import (
	"context"
	"errors"
	"testing"

	"github.com/codewandler/axon/adapters/sqlite"
	"github.com/codewandler/axon/graph"
	"github.com/codewandler/axon/storage"
)

func setupGraph(t *testing.T) *graph.Graph {
	t.Helper()
	r := graph.NewRegistry()
	graph.RegisterNodeType[map[string]any](r, graph.NodeSpec{Type: "fs:dir"})
	graph.RegisterNodeType[map[string]any](r, graph.NodeSpec{Type: "fs:file"})
	r.RegisterEdgeType(graph.EdgeSpec{
		Type:      "contains",
		FromTypes: []string{"fs:dir"},
		ToTypes:   []string{"fs:file", "fs:dir"},
	})
	r.RegisterEdgeType(graph.EdgeSpec{
		Type:      "contained_by",
		FromTypes: []string{"fs:file", "fs:dir"},
		ToTypes:   []string{"fs:dir"},
	})
	r.RegisterEdgeType(graph.EdgeSpec{Type: "references"})

	s, err := sqlite.New(":memory:")
	if err != nil {
		t.Fatalf("sqlite.New failed: %v", err)
	}
	t.Cleanup(func() { s.Close() })
	return graph.New(s, r)
}

func TestGraphAddNode(t *testing.T) {
	ctx := context.Background()
	g := setupGraph(t)

	node := graph.NewNode("fs:file").WithURI("file:///test.txt")
	if err := g.AddNode(ctx, node); err != nil {
		t.Fatalf("AddNode failed: %v", err)
	}

	got, err := g.GetNode(ctx, node.ID)
	if err != nil {
		t.Fatalf("GetNode failed: %v", err)
	}
	if got.ID != node.ID {
		t.Errorf("expected ID %s, got %s", node.ID, got.ID)
	}
}

func TestGraphAddNodeUnknownType(t *testing.T) {
	ctx := context.Background()
	g := setupGraph(t)

	node := graph.NewNode("unknown:type")
	err := g.AddNode(ctx, node)
	if err == nil {
		t.Error("expected error for unknown type")
	}
	if !errors.Is(err, graph.ErrUnknownNodeType) {
		t.Errorf("expected ErrUnknownNodeType, got %v", err)
	}
}

func TestGraphAddEdge(t *testing.T) {
	ctx := context.Background()
	g := setupGraph(t)

	dir := graph.NewNode("fs:dir")
	file := graph.NewNode("fs:file")
	_ = g.AddNode(ctx, dir)
	_ = g.AddNode(ctx, file)

	edge := graph.NewEdge("contains", dir.ID, file.ID)
	if err := g.AddEdge(ctx, edge); err != nil {
		t.Fatalf("AddEdge failed: %v", err)
	}

	got, err := g.GetEdge(ctx, edge.ID)
	if err != nil {
		t.Fatalf("GetEdge failed: %v", err)
	}
	if got.ID != edge.ID {
		t.Errorf("expected ID %s, got %s", edge.ID, got.ID)
	}
}

func TestGraphAddEdgeInvalidType(t *testing.T) {
	ctx := context.Background()
	g := setupGraph(t)

	file1 := graph.NewNode("fs:file")
	file2 := graph.NewNode("fs:file")
	_ = g.AddNode(ctx, file1)
	_ = g.AddNode(ctx, file2)

	// contains edge requires fs:dir as source
	edge := graph.NewEdge("contains", file1.ID, file2.ID)
	err := g.AddEdge(ctx, edge)
	if err == nil {
		t.Error("expected error for invalid edge type")
	}
}

func TestGraphAddEdgeMissingNode(t *testing.T) {
	ctx := context.Background()
	g := setupGraph(t)

	dir := graph.NewNode("fs:dir")
	_ = g.AddNode(ctx, dir)

	// file doesn't exist
	edge := graph.NewEdge("contains", dir.ID, "nonexistent")
	err := g.AddEdge(ctx, edge)
	if !errors.Is(err, storage.ErrNodeNotFound) {
		t.Errorf("expected ErrNodeNotFound, got %v", err)
	}
}

func TestGraphNeighbors(t *testing.T) {
	ctx := context.Background()
	g := setupGraph(t)

	// Create structure: dir -> file1, dir -> file2
	dir := graph.NewNode("fs:dir")
	file1 := graph.NewNode("fs:file")
	file2 := graph.NewNode("fs:file")
	_ = g.AddNode(ctx, dir)
	_ = g.AddNode(ctx, file1)
	_ = g.AddNode(ctx, file2)
	_ = g.AddEdge(ctx, graph.NewEdge("contains", dir.ID, file1.ID))
	_ = g.AddEdge(ctx, graph.NewEdge("contains", dir.ID, file2.ID))

	// Outgoing neighbors of dir
	neighbors, err := g.Neighbors(ctx, dir.ID, graph.Outgoing)
	if err != nil {
		t.Fatalf("Neighbors failed: %v", err)
	}
	if len(neighbors) != 2 {
		t.Errorf("expected 2 neighbors, got %d", len(neighbors))
	}

	// Incoming neighbors of file1 (should be dir)
	parents, err := g.Neighbors(ctx, file1.ID, graph.Incoming)
	if err != nil {
		t.Fatalf("Neighbors failed: %v", err)
	}
	if len(parents) != 1 {
		t.Errorf("expected 1 parent, got %d", len(parents))
	}
	if parents[0].ID != dir.ID {
		t.Errorf("expected parent %s, got %s", dir.ID, parents[0].ID)
	}
}

func TestGraphChildrenAndParents(t *testing.T) {
	ctx := context.Background()
	g := setupGraph(t)

	dir := graph.NewNode("fs:dir")
	file := graph.NewNode("fs:file")
	_ = g.AddNode(ctx, dir)
	_ = g.AddNode(ctx, file)
	// Create both directions (as EmitContainment does)
	_ = g.AddEdge(ctx, graph.NewEdge("contains", dir.ID, file.ID))
	_ = g.AddEdge(ctx, graph.NewEdge("contained_by", file.ID, dir.ID))

	children, _ := g.Children(ctx, dir.ID)
	if len(children) != 1 || children[0].ID != file.ID {
		t.Error("Children() did not return correct results")
	}

	parents, _ := g.Parents(ctx, file.ID)
	if len(parents) != 1 || parents[0].ID != dir.ID {
		t.Error("Parents() did not return correct results")
	}
}

func TestGraphFindNodes(t *testing.T) {
	ctx := context.Background()
	g := setupGraph(t)

	dir := graph.NewNode("fs:dir").WithURI("file:///home")
	file := graph.NewNode("fs:file").WithURI("file:///home/test.txt")
	_ = g.AddNode(ctx, dir)
	_ = g.AddNode(ctx, file)

	nodes, err := g.FindNodes(ctx, graph.NodeFilter{Type: "fs:file"})
	if err != nil {
		t.Fatalf("FindNodes failed: %v", err)
	}
	if len(nodes) != 1 {
		t.Errorf("expected 1 file, got %d", len(nodes))
	}
}

func TestGraphGetNodeByURI(t *testing.T) {
	ctx := context.Background()
	g := setupGraph(t)

	node := graph.NewNode("fs:file").WithURI("file:///test.txt")
	_ = g.AddNode(ctx, node)

	got, err := g.GetNodeByURI(ctx, "file:///test.txt")
	if err != nil {
		t.Fatalf("GetNodeByURI failed: %v", err)
	}
	if got.ID != node.ID {
		t.Errorf("expected ID %s, got %s", node.ID, got.ID)
	}
}

func TestGraphDeleteNode(t *testing.T) {
	ctx := context.Background()
	g := setupGraph(t)

	node := graph.NewNode("fs:file")
	_ = g.AddNode(ctx, node)

	if err := g.DeleteNode(ctx, node.ID); err != nil {
		t.Fatalf("DeleteNode failed: %v", err)
	}

	_, err := g.GetNode(ctx, node.ID)
	if !errors.Is(err, storage.ErrNodeNotFound) {
		t.Error("node should be deleted")
	}
}

func TestGraphDeleteEdge(t *testing.T) {
	ctx := context.Background()
	g := setupGraph(t)

	dir := graph.NewNode("fs:dir")
	file := graph.NewNode("fs:file")
	_ = g.AddNode(ctx, dir)
	_ = g.AddNode(ctx, file)
	edge := graph.NewEdge("contains", dir.ID, file.ID)
	_ = g.AddEdge(ctx, edge)

	if err := g.DeleteEdge(ctx, edge.ID); err != nil {
		t.Fatalf("DeleteEdge failed: %v", err)
	}

	_, err := g.GetEdge(ctx, edge.ID)
	if !errors.Is(err, storage.ErrEdgeNotFound) {
		t.Error("edge should be deleted")
	}
}
