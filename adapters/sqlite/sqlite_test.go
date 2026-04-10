package sqlite

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/codewandler/axon/graph"
	"github.com/codewandler/axon/storage"
)

func setupTestDB(t *testing.T) *Storage {
	t.Helper()

	dir := t.TempDir()
	dbPath := filepath.Join(dir, "test.db")

	s, err := New(dbPath)
	if err != nil {
		t.Fatalf("New failed: %v", err)
	}

	t.Cleanup(func() {
		s.Close()
	})

	return s
}

func TestPutAndGetNode(t *testing.T) {
	ctx := context.Background()
	s := setupTestDB(t)

	node := graph.NewNode("fs:file").
		WithURI("file:///test.txt").
		WithKey("/test.txt").
		WithData(map[string]any{"name": "test.txt", "size": 1024})

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
	if got.Type != node.Type {
		t.Errorf("expected Type %s, got %s", node.Type, got.Type)
	}
	if got.URI != node.URI {
		t.Errorf("expected URI %s, got %s", node.URI, got.URI)
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
	s := setupTestDB(t)

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
	s := setupTestDB(t)

	node := graph.NewNode("fs:file").WithURI("file:///test.txt")
	_ = s.PutNode(ctx, node)

	if err := s.DeleteNode(ctx, node.ID); err != nil {
		t.Fatalf("DeleteNode failed: %v", err)
	}

	_, err := s.GetNode(ctx, node.ID)
	if !errors.Is(err, storage.ErrNodeNotFound) {
		t.Error("node should not be found after delete")
	}
}

func TestPutNodeUpdate(t *testing.T) {
	ctx := context.Background()
	s := setupTestDB(t)

	node := graph.NewNode("fs:file").WithURI("file:///test.txt").WithKey("test.txt")
	node.Data = map[string]any{"size": 100}
	_ = s.PutNode(ctx, node)

	// Update the node's data (not URI - URI is the identity)
	node.Data = map[string]any{"size": 200}
	node.Generation = "gen2"
	_ = s.PutNode(ctx, node)

	got, err := s.GetNode(ctx, node.ID)
	if err != nil {
		t.Fatalf("GetNode failed: %v", err)
	}
	data := got.Data.(map[string]any)
	if int(data["size"].(float64)) != 200 {
		t.Errorf("expected updated size 200, got %v", data["size"])
	}
	if got.Generation != "gen2" {
		t.Errorf("expected generation gen2, got %s", got.Generation)
	}
}

func TestPutAndGetEdge(t *testing.T) {
	ctx := context.Background()
	s := setupTestDB(t)

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
	if got.Type != edge.Type {
		t.Errorf("expected Type %s, got %s", edge.Type, got.Type)
	}
}

func TestGetEdgesFrom(t *testing.T) {
	ctx := context.Background()
	s := setupTestDB(t)

	dir := graph.NewNode("fs:dir")
	file1 := graph.NewNode("fs:file")
	file2 := graph.NewNode("fs:file")
	_ = s.PutNode(ctx, dir)
	_ = s.PutNode(ctx, file1)
	_ = s.PutNode(ctx, file2)

	_ = s.PutEdge(ctx, graph.NewEdge("contains", dir.ID, file1.ID))
	_ = s.PutEdge(ctx, graph.NewEdge("contains", dir.ID, file2.ID))

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
	s := setupTestDB(t)

	dir1 := graph.NewNode("fs:dir")
	dir2 := graph.NewNode("fs:dir")
	file := graph.NewNode("fs:file")
	_ = s.PutNode(ctx, dir1)
	_ = s.PutNode(ctx, dir2)
	_ = s.PutNode(ctx, file)

	_ = s.PutEdge(ctx, graph.NewEdge("references", dir1.ID, file.ID))
	_ = s.PutEdge(ctx, graph.NewEdge("references", dir2.ID, file.ID))

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
	s := setupTestDB(t)

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
}

func TestFindNodes(t *testing.T) {
	ctx := context.Background()
	s := setupTestDB(t)

	file1 := graph.NewNode("fs:file").WithURI("file:///home/user/a.txt")
	file2 := graph.NewNode("fs:file").WithURI("file:///home/user/b.txt")
	dir := graph.NewNode("fs:dir").WithURI("file:///home/user")
	other := graph.NewNode("fs:file").WithURI("file:///other/c.txt")

	_ = s.PutNode(ctx, file1)
	_ = s.PutNode(ctx, file2)
	_ = s.PutNode(ctx, dir)
	_ = s.PutNode(ctx, other)

	// Find by type
	nodes, err := s.FindNodes(ctx, graph.NodeFilter{Type: "fs:file"}, graph.QueryOptions{})
	if err != nil {
		t.Fatalf("FindNodes failed: %v", err)
	}
	if len(nodes) != 3 {
		t.Errorf("expected 3 files, got %d", len(nodes))
	}

	// Find by URI prefix
	nodes, err = s.FindNodes(ctx, graph.NodeFilter{URIPrefix: "file:///home/user"}, graph.QueryOptions{})
	if err != nil {
		t.Fatalf("FindNodes failed: %v", err)
	}
	if len(nodes) != 3 {
		t.Errorf("expected 3 nodes under /home/user, got %d", len(nodes))
	}

	// Find by type AND URI prefix
	nodes, err = s.FindNodes(ctx, graph.NodeFilter{Type: "fs:file", URIPrefix: "file:///home/user"}, graph.QueryOptions{})
	if err != nil {
		t.Fatalf("FindNodes failed: %v", err)
	}
	if len(nodes) != 2 {
		t.Errorf("expected 2 files under /home/user, got %d", len(nodes))
	}
}

func TestDeleteStaleByURIPrefix(t *testing.T) {
	ctx := context.Background()
	s := setupTestDB(t)

	node1 := graph.NewNode("fs:file").WithURI("file:///test/a.txt").WithGeneration("gen-1")
	node2 := graph.NewNode("fs:file").WithURI("file:///test/b.txt").WithGeneration("gen-2")
	node3 := graph.NewNode("fs:file").WithURI("file:///other/c.txt").WithGeneration("gen-1")

	_ = s.PutNode(ctx, node1)
	_ = s.PutNode(ctx, node2)
	_ = s.PutNode(ctx, node3)

	deleted, err := s.DeleteStaleByURIPrefix(ctx, "file:///test", "gen-2")
	if err != nil {
		t.Fatalf("DeleteStaleByURIPrefix failed: %v", err)
	}
	if deleted != 1 {
		t.Errorf("expected 1 deleted, got %d", deleted)
	}

	_, err = s.GetNode(ctx, node1.ID)
	if !errors.Is(err, storage.ErrNodeNotFound) {
		t.Error("stale node should be deleted")
	}

	_, err = s.GetNode(ctx, node2.ID)
	if err != nil {
		t.Error("current gen node should exist")
	}

	_, err = s.GetNode(ctx, node3.ID)
	if err != nil {
		t.Error("node outside URI prefix should exist")
	}
}

func TestDeleteByURIPrefix(t *testing.T) {
	ctx := context.Background()
	s := setupTestDB(t)

	node1 := graph.NewNode("fs:file").WithURI("file:///test/a.txt").WithGeneration("gen-1")
	node2 := graph.NewNode("fs:file").WithURI("file:///test/b.txt").WithGeneration("gen-2")
	node3 := graph.NewNode("fs:file").WithURI("file:///other/c.txt").WithGeneration("gen-1")

	_ = s.PutNode(ctx, node1)
	_ = s.PutNode(ctx, node2)
	_ = s.PutNode(ctx, node3)

	deleted, err := s.DeleteByURIPrefix(ctx, "file:///test")
	if err != nil {
		t.Fatalf("DeleteByURIPrefix failed: %v", err)
	}
	if deleted != 2 {
		t.Errorf("expected 2 deleted, got %d", deleted)
	}

	_, err = s.GetNode(ctx, node1.ID)
	if !errors.Is(err, storage.ErrNodeNotFound) {
		t.Error("node1 should be deleted")
	}

	_, err = s.GetNode(ctx, node2.ID)
	if !errors.Is(err, storage.ErrNodeNotFound) {
		t.Error("node2 should be deleted")
	}

	_, err = s.GetNode(ctx, node3.ID)
	if err != nil {
		t.Error("node outside URI prefix should exist")
	}
}

func TestFindStaleByURIPrefix(t *testing.T) {
	ctx := context.Background()
	s := setupTestDB(t)

	node1 := graph.NewNode("fs:file").WithURI("file:///test/a.txt").WithGeneration("gen-1")
	node2 := graph.NewNode("fs:file").WithURI("file:///test/b.txt").WithGeneration("gen-2")
	node3 := graph.NewNode("fs:file").WithURI("file:///other/c.txt").WithGeneration("gen-1")

	_ = s.PutNode(ctx, node1)
	_ = s.PutNode(ctx, node2)
	_ = s.PutNode(ctx, node3)

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
	s := setupTestDB(t)

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
	s := setupTestDB(t)

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

func TestPersistence(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "test.db")

	// Create and populate
	s1, err := New(dbPath)
	if err != nil {
		t.Fatalf("New failed: %v", err)
	}

	node := graph.NewNode("fs:file").WithURI("file:///test.txt")
	_ = s1.PutNode(ctx, node)
	s1.Close()

	// Reopen and verify
	s2, err := New(dbPath)
	if err != nil {
		t.Fatalf("New (reopen) failed: %v", err)
	}
	defer s2.Close()

	got, err := s2.GetNode(ctx, node.ID)
	if err != nil {
		t.Fatalf("GetNode after reopen failed: %v", err)
	}
	if got.URI != node.URI {
		t.Errorf("expected URI %s, got %s", node.URI, got.URI)
	}
}

func TestNewCreatesFile(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "new.db")

	// File shouldn't exist yet
	if _, err := os.Stat(dbPath); !os.IsNotExist(err) {
		t.Fatal("database file should not exist yet")
	}

	s, err := New(dbPath)
	if err != nil {
		t.Fatalf("New failed: %v", err)
	}
	s.Close()

	// File should exist now
	if _, err := os.Stat(dbPath); err != nil {
		t.Errorf("database file should exist: %v", err)
	}
}

func TestConcurrentWrites(t *testing.T) {
	ctx := context.Background()
	s := setupTestDB(t)

	const numGoroutines = 10
	const nodesPerGoroutine = 20

	errCh := make(chan error, numGoroutines)
	doneCh := make(chan bool, numGoroutines)

	// Spawn multiple goroutines writing nodes concurrently
	for i := 0; i < numGoroutines; i++ {
		go func(id int) {
			for j := 0; j < nodesPerGoroutine; j++ {
				uri := filepath.Join("file:///", "test", fmt.Sprintf("%d-%d.txt", id, j))
				key := filepath.Join("/test", fmt.Sprintf("%d-%d.txt", id, j))
				node := graph.NewNode("fs:file").
					WithURI(uri).
					WithKey(key).
					WithGeneration("gen-1")

				if err := s.PutNode(ctx, node); err != nil {
					errCh <- err
					return
				}
			}
			doneCh <- true
		}(i)
	}

	// Wait for all goroutines
	for i := 0; i < numGoroutines; i++ {
		select {
		case err := <-errCh:
			t.Fatalf("concurrent write failed: %v", err)
		case <-doneCh:
			// Success
		}
	}

	// Flush to ensure all writes are persisted
	if err := s.Flush(ctx); err != nil {
		t.Fatalf("Flush failed: %v", err)
	}

	// Verify all nodes were written
	nodes, err := s.FindNodes(ctx, graph.NodeFilter{}, graph.QueryOptions{})
	if err != nil {
		t.Fatalf("FindNodes failed: %v", err)
	}

	expectedCount := numGoroutines * nodesPerGoroutine
	if len(nodes) != expectedCount {
		t.Errorf("expected %d nodes, got %d", expectedCount, len(nodes))
	}
}

func TestEmbedding(t *testing.T) {
	ctx := context.Background()
	s := setupTestDB(t)

	// Create a node first
	node := graph.NewNode("go:func").
		WithURI("file:///test/func").
		WithKey("testfunc").
		WithName("TestFunc")
	if err := s.PutNode(ctx, node); err != nil {
		t.Fatalf("PutNode: %v", err)
	}
	if err := s.Flush(ctx); err != nil {
		t.Fatalf("Flush: %v", err)
	}

	// Store embedding
	embedding := []float32{0.1, 0.2, 0.3, 0.9}
	if err := s.PutEmbedding(ctx, node.ID, embedding); err != nil {
		t.Fatalf("PutEmbedding: %v", err)
	}

	// Retrieve embedding
	got, err := s.GetEmbedding(ctx, node.ID)
	if err != nil {
		t.Fatalf("GetEmbedding: %v", err)
	}
	if len(got) != len(embedding) {
		t.Fatalf("expected %d dims, got %d", len(embedding), len(got))
	}
	for i, v := range embedding {
		if got[i] != v {
			t.Errorf("dim %d: expected %f, got %f", i, v, got[i])
		}
	}

	// FindSimilar
	results, err := s.FindSimilar(ctx, embedding, 5, nil)
	if err != nil {
		t.Fatalf("FindSimilar: %v", err)
	}
	if len(results) == 0 {
		t.Fatal("expected at least one result")
	}
	if results[0].ID != node.ID {
		t.Errorf("expected top result to be %s, got %s", node.ID, results[0].ID)
	}
}

// TestPragmasApplyToAllConnections verifies that per-connection PRAGMAs
// (especially busy_timeout) are set on every connection from the pool,
// not just the first one. This was the root cause of SQLITE_BUSY errors:
// database/sql pools connections and PRAGMAs set via ExecContext only
// apply to the connection that runs them.
func TestPragmasApplyToAllConnections(t *testing.T) {
	s := setupTestDB(t)
	ctx := context.Background()

	// Force multiple connections by running concurrent queries.
	// Each connection should have busy_timeout set via DSN _pragma.
	for i := 0; i < 10; i++ {
		var busyTimeout int
		err := s.db.QueryRowContext(ctx, "PRAGMA busy_timeout").Scan(&busyTimeout)
		if err != nil {
			t.Fatalf("iteration %d: PRAGMA busy_timeout query failed: %v", i, err)
		}
		if busyTimeout != 30000 {
			t.Errorf("iteration %d: expected busy_timeout=30000, got %d", i, busyTimeout)
		}

		var journalMode string
		err = s.db.QueryRowContext(ctx, "PRAGMA journal_mode").Scan(&journalMode)
		if err != nil {
			t.Fatalf("iteration %d: PRAGMA journal_mode query failed: %v", i, err)
		}
		if journalMode != "wal" {
			t.Errorf("iteration %d: expected journal_mode=wal, got %s", i, journalMode)
		}
	}
}

func TestBuildDSN(t *testing.T) {
	tests := []struct {
		name string
		path string
		want []string // substrings that must appear
	}{
		{
			name: "file path includes pragmas",
			path: "/tmp/test.db",
			want: []string{
				"file:/tmp/test.db?",
				"busy_timeout",
				"journal_mode",
				"synchronous",
			},
		},
		{
			name: "memory includes pragmas and shared cache",
			path: ":memory:",
			want: []string{
				"mode=memory",
				"cache=shared",
				"busy_timeout",
				"journal_mode",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dsn := buildDSN(tt.path)
			for _, sub := range tt.want {
				if !strings.Contains(dsn, sub) {
					t.Errorf("buildDSN(%q) = %q, missing %q", tt.path, dsn, sub)
				}
			}
		})
	}
}

