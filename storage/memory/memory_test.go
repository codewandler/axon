package memory

import (
	"context"
	"errors"
	"testing"

	"github.com/codewandler/axon/graph"
	"github.com/codewandler/axon/storage"
)

func TestPutAndGetNode(t *testing.T) {
	ctx := context.Background()
	s := New()

	node := graph.NewNode("fs:file").
		WithURI("file:///test.txt").
		WithKey("/test.txt")

	if err := s.PutNode(ctx, node); err != nil {
		t.Fatalf("PutNode failed: %v", err)
	}

	// Get by ID
	got, err := s.GetNode(ctx, node.ID)
	if err != nil {
		t.Fatalf("GetNode failed: %v", err)
	}
	if got.ID != node.ID {
		t.Errorf("expected ID %s, got %s", node.ID, got.ID)
	}

	// Get by URI
	got, err = s.GetNodeByURI(ctx, "file:///test.txt")
	if err != nil {
		t.Fatalf("GetNodeByURI failed: %v", err)
	}
	if got.ID != node.ID {
		t.Errorf("expected ID %s, got %s", node.ID, got.ID)
	}

	// Get by Key
	got, err = s.GetNodeByKey(ctx, "fs:file", "/test.txt")
	if err != nil {
		t.Fatalf("GetNodeByKey failed: %v", err)
	}
	if got.ID != node.ID {
		t.Errorf("expected ID %s, got %s", node.ID, got.ID)
	}
}

func TestGetNodeNotFound(t *testing.T) {
	ctx := context.Background()
	s := New()

	_, err := s.GetNode(ctx, "nonexistent")
	if !errors.Is(err, storage.ErrNodeNotFound) {
		t.Errorf("expected ErrNodeNotFound, got %v", err)
	}

	_, err = s.GetNodeByURI(ctx, "file:///nonexistent")
	if !errors.Is(err, storage.ErrNodeNotFound) {
		t.Errorf("expected ErrNodeNotFound, got %v", err)
	}

	_, err = s.GetNodeByKey(ctx, "fs:file", "nonexistent")
	if !errors.Is(err, storage.ErrNodeNotFound) {
		t.Errorf("expected ErrNodeNotFound, got %v", err)
	}
}

func TestDeleteNode(t *testing.T) {
	ctx := context.Background()
	s := New()

	node := graph.NewNode("fs:file").
		WithURI("file:///test.txt").
		WithKey("/test.txt")

	_ = s.PutNode(ctx, node)

	if err := s.DeleteNode(ctx, node.ID); err != nil {
		t.Fatalf("DeleteNode failed: %v", err)
	}

	// Should not be found by any lookup
	_, err := s.GetNode(ctx, node.ID)
	if !errors.Is(err, storage.ErrNodeNotFound) {
		t.Error("node should not be found after delete")
	}

	_, err = s.GetNodeByURI(ctx, "file:///test.txt")
	if !errors.Is(err, storage.ErrNodeNotFound) {
		t.Error("node should not be found by URI after delete")
	}

	_, err = s.GetNodeByKey(ctx, "fs:file", "/test.txt")
	if !errors.Is(err, storage.ErrNodeNotFound) {
		t.Error("node should not be found by key after delete")
	}
}

func TestPutNodeUpdate(t *testing.T) {
	ctx := context.Background()
	s := New()

	node := graph.NewNode("fs:file").
		WithURI("file:///test.txt")

	_ = s.PutNode(ctx, node)

	// Update the node with new URI
	node.URI = "file:///updated.txt"
	_ = s.PutNode(ctx, node)

	// Old URI should not work
	_, err := s.GetNodeByURI(ctx, "file:///test.txt")
	if !errors.Is(err, storage.ErrNodeNotFound) {
		t.Error("old URI should not be found")
	}

	// New URI should work
	got, err := s.GetNodeByURI(ctx, "file:///updated.txt")
	if err != nil {
		t.Fatalf("GetNodeByURI failed: %v", err)
	}
	if got.ID != node.ID {
		t.Errorf("expected ID %s, got %s", node.ID, got.ID)
	}
}

func TestPutAndGetEdge(t *testing.T) {
	ctx := context.Background()
	s := New()

	node1 := graph.NewNode("fs:dir")
	node2 := graph.NewNode("fs:file")
	_ = s.PutNode(ctx, node1)
	_ = s.PutNode(ctx, node2)

	edge := graph.NewEdge("contains", node1.ID, node2.ID)
	if err := s.PutEdge(ctx, edge); err != nil {
		t.Fatalf("PutEdge failed: %v", err)
	}

	got, err := s.GetEdge(ctx, edge.ID)
	if err != nil {
		t.Fatalf("GetEdge failed: %v", err)
	}
	if got.ID != edge.ID {
		t.Errorf("expected ID %s, got %s", edge.ID, got.ID)
	}
}

func TestGetEdgesFrom(t *testing.T) {
	ctx := context.Background()
	s := New()

	dir := graph.NewNode("fs:dir")
	file1 := graph.NewNode("fs:file")
	file2 := graph.NewNode("fs:file")
	_ = s.PutNode(ctx, dir)
	_ = s.PutNode(ctx, file1)
	_ = s.PutNode(ctx, file2)

	edge1 := graph.NewEdge("contains", dir.ID, file1.ID)
	edge2 := graph.NewEdge("contains", dir.ID, file2.ID)
	_ = s.PutEdge(ctx, edge1)
	_ = s.PutEdge(ctx, edge2)

	edges, err := s.GetEdgesFrom(ctx, dir.ID)
	if err != nil {
		t.Fatalf("GetEdgesFrom failed: %v", err)
	}
	if len(edges) != 2 {
		t.Errorf("expected 2 edges, got %d", len(edges))
	}
}

func TestGetEdgesTo(t *testing.T) {
	ctx := context.Background()
	s := New()

	dir1 := graph.NewNode("fs:dir")
	dir2 := graph.NewNode("fs:dir")
	file := graph.NewNode("fs:file")
	_ = s.PutNode(ctx, dir1)
	_ = s.PutNode(ctx, dir2)
	_ = s.PutNode(ctx, file)

	// File linked from two directories (unusual but valid for testing)
	edge1 := graph.NewEdge("references", dir1.ID, file.ID)
	edge2 := graph.NewEdge("references", dir2.ID, file.ID)
	_ = s.PutEdge(ctx, edge1)
	_ = s.PutEdge(ctx, edge2)

	edges, err := s.GetEdgesTo(ctx, file.ID)
	if err != nil {
		t.Fatalf("GetEdgesTo failed: %v", err)
	}
	if len(edges) != 2 {
		t.Errorf("expected 2 edges, got %d", len(edges))
	}
}

func TestDeleteEdge(t *testing.T) {
	ctx := context.Background()
	s := New()

	node1 := graph.NewNode("fs:dir")
	node2 := graph.NewNode("fs:file")
	_ = s.PutNode(ctx, node1)
	_ = s.PutNode(ctx, node2)

	edge := graph.NewEdge("contains", node1.ID, node2.ID)
	_ = s.PutEdge(ctx, edge)

	if err := s.DeleteEdge(ctx, edge.ID); err != nil {
		t.Fatalf("DeleteEdge failed: %v", err)
	}

	_, err := s.GetEdge(ctx, edge.ID)
	if !errors.Is(err, storage.ErrEdgeNotFound) {
		t.Error("edge should not be found after delete")
	}

	// Edge indexes should be cleaned up
	edges, _ := s.GetEdgesFrom(ctx, node1.ID)
	if len(edges) != 0 {
		t.Error("edge should be removed from edgesFrom index")
	}

	edges, _ = s.GetEdgesTo(ctx, node2.ID)
	if len(edges) != 0 {
		t.Error("edge should be removed from edgesTo index")
	}
}

func TestFindNodes(t *testing.T) {
	ctx := context.Background()
	s := New()

	file1 := graph.NewNode("fs:file").WithURI("file:///home/user/a.txt")
	file2 := graph.NewNode("fs:file").WithURI("file:///home/user/b.txt")
	dir := graph.NewNode("fs:dir").WithURI("file:///home/user")
	other := graph.NewNode("fs:file").WithURI("file:///other/c.txt")

	_ = s.PutNode(ctx, file1)
	_ = s.PutNode(ctx, file2)
	_ = s.PutNode(ctx, dir)
	_ = s.PutNode(ctx, other)

	// Find by type
	nodes, err := s.FindNodes(ctx, graph.NodeFilter{Type: "fs:file"})
	if err != nil {
		t.Fatalf("FindNodes failed: %v", err)
	}
	if len(nodes) != 3 {
		t.Errorf("expected 3 files, got %d", len(nodes))
	}

	// Find by URI prefix
	nodes, err = s.FindNodes(ctx, graph.NodeFilter{URIPrefix: "file:///home/user"})
	if err != nil {
		t.Fatalf("FindNodes failed: %v", err)
	}
	if len(nodes) != 3 {
		t.Errorf("expected 3 nodes under /home/user, got %d", len(nodes))
	}

	// Find by type AND URI prefix
	nodes, err = s.FindNodes(ctx, graph.NodeFilter{Type: "fs:file", URIPrefix: "file:///home/user"})
	if err != nil {
		t.Fatalf("FindNodes failed: %v", err)
	}
	if len(nodes) != 2 {
		t.Errorf("expected 2 files under /home/user, got %d", len(nodes))
	}
}

func TestDeleteStaleByURIPrefix(t *testing.T) {
	ctx := context.Background()
	s := New()

	// Create nodes with different generations and URI prefixes
	node1 := graph.NewNode("fs:file").WithURI("file:///test/a.txt").WithGeneration("gen-1")
	node2 := graph.NewNode("fs:file").WithURI("file:///test/b.txt").WithGeneration("gen-2")
	node3 := graph.NewNode("fs:file").WithURI("file:///other/c.txt").WithGeneration("gen-1")

	_ = s.PutNode(ctx, node1)
	_ = s.PutNode(ctx, node2)
	_ = s.PutNode(ctx, node3)

	// Delete stale nodes under file:///test with current gen-2
	deleted, err := s.DeleteStaleByURIPrefix(ctx, "file:///test", "gen-2")
	if err != nil {
		t.Fatalf("DeleteStaleByURIPrefix failed: %v", err)
	}
	if deleted != 1 {
		t.Errorf("expected 1 deleted, got %d", deleted)
	}

	// node1 should be gone
	_, err = s.GetNode(ctx, node1.ID)
	if !errors.Is(err, storage.ErrNodeNotFound) {
		t.Error("stale node should be deleted")
	}

	// node2 should still exist (current gen)
	_, err = s.GetNode(ctx, node2.ID)
	if err != nil {
		t.Error("current gen node should exist")
	}

	// node3 should still exist (different URI prefix)
	_, err = s.GetNode(ctx, node3.ID)
	if err != nil {
		t.Error("node outside URI prefix should exist")
	}
}

func TestDeleteByURIPrefix(t *testing.T) {
	ctx := context.Background()
	s := New()

	// Create nodes with different URI prefixes
	node1 := graph.NewNode("fs:file").WithURI("file:///test/a.txt").WithGeneration("gen-1")
	node2 := graph.NewNode("fs:file").WithURI("file:///test/b.txt").WithGeneration("gen-2")
	node3 := graph.NewNode("fs:file").WithURI("file:///other/c.txt").WithGeneration("gen-1")

	_ = s.PutNode(ctx, node1)
	_ = s.PutNode(ctx, node2)
	_ = s.PutNode(ctx, node3)

	// Delete all nodes under file:///test (regardless of generation)
	deleted, err := s.DeleteByURIPrefix(ctx, "file:///test")
	if err != nil {
		t.Fatalf("DeleteByURIPrefix failed: %v", err)
	}
	if deleted != 2 {
		t.Errorf("expected 2 deleted, got %d", deleted)
	}

	// node1 and node2 should be gone
	_, err = s.GetNode(ctx, node1.ID)
	if !errors.Is(err, storage.ErrNodeNotFound) {
		t.Error("node1 should be deleted")
	}
	_, err = s.GetNode(ctx, node2.ID)
	if !errors.Is(err, storage.ErrNodeNotFound) {
		t.Error("node2 should be deleted")
	}

	// node3 should still exist (different URI prefix)
	_, err = s.GetNode(ctx, node3.ID)
	if err != nil {
		t.Error("node outside URI prefix should exist")
	}
}

func TestFindStaleByURIPrefix(t *testing.T) {
	ctx := context.Background()
	s := New()

	// Create nodes with different generations
	node1 := graph.NewNode("fs:file").WithURI("file:///test/a.txt").WithGeneration("gen-1")
	node2 := graph.NewNode("fs:file").WithURI("file:///test/b.txt").WithGeneration("gen-2")
	node3 := graph.NewNode("fs:file").WithURI("file:///other/c.txt").WithGeneration("gen-1")

	_ = s.PutNode(ctx, node1)
	_ = s.PutNode(ctx, node2)
	_ = s.PutNode(ctx, node3)

	// Find stale nodes under file:///test with current gen-2
	stale, err := s.FindStaleByURIPrefix(ctx, "file:///test", "gen-2")
	if err != nil {
		t.Fatalf("FindStaleByURIPrefix failed: %v", err)
	}
	if len(stale) != 1 {
		t.Errorf("expected 1 stale node, got %d", len(stale))
	}
	if len(stale) > 0 && stale[0].ID != node1.ID {
		t.Errorf("expected stale node to be node1")
	}
}

func TestDeleteStaleEdges(t *testing.T) {
	ctx := context.Background()
	s := New()

	node1 := graph.NewNode("fs:dir")
	node2 := graph.NewNode("fs:file")
	_ = s.PutNode(ctx, node1)
	_ = s.PutNode(ctx, node2)

	edge1 := graph.NewEdge("contains", node1.ID, node2.ID).WithGeneration("gen-1")
	edge2 := graph.NewEdge("references", node1.ID, node2.ID).WithGeneration("gen-2")
	_ = s.PutEdge(ctx, edge1)
	_ = s.PutEdge(ctx, edge2)

	deleted, err := s.DeleteStaleEdges(ctx, "gen-2")
	if err != nil {
		t.Fatalf("DeleteStaleEdges failed: %v", err)
	}
	if deleted != 1 {
		t.Errorf("expected 1 deleted, got %d", deleted)
	}

	_, err = s.GetEdge(ctx, edge1.ID)
	if !errors.Is(err, storage.ErrEdgeNotFound) {
		t.Error("stale edge should be deleted")
	}

	_, err = s.GetEdge(ctx, edge2.ID)
	if err != nil {
		t.Error("current gen edge should exist")
	}
}

func TestDeleteOrphanedEdges(t *testing.T) {
	ctx := context.Background()
	s := New()

	node1 := graph.NewNode("fs:dir")
	node2 := graph.NewNode("fs:file")
	_ = s.PutNode(ctx, node1)
	_ = s.PutNode(ctx, node2)

	edge := graph.NewEdge("contains", node1.ID, node2.ID)
	_ = s.PutEdge(ctx, edge)

	// Delete one node
	_ = s.DeleteNode(ctx, node2.ID)

	// Edge should be orphaned
	deleted, err := s.DeleteOrphanedEdges(ctx)
	if err != nil {
		t.Fatalf("DeleteOrphanedEdges failed: %v", err)
	}
	if deleted != 1 {
		t.Errorf("expected 1 deleted, got %d", deleted)
	}

	_, err = s.GetEdge(ctx, edge.ID)
	if !errors.Is(err, storage.ErrEdgeNotFound) {
		t.Error("orphaned edge should be deleted")
	}
}
