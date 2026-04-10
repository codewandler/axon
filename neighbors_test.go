package axon

import (
	"context"
	"testing"

	"github.com/codewandler/axon/graph"
)

// buildNeighborsGraph creates an Axon instance with a small graph for
// neighbors tests. Topology:
//
//	A --calls--> B --calls--> C
//	A --uses-->  D
//	             E  (isolated, no edges)
func buildNeighborsGraph(t *testing.T) (*Axon, map[string]*graph.Node) {
	t.Helper()

	ax, err := New(Config{Dir: t.TempDir()})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	ctx := context.Background()

	mkNode := func(name, uri string) *graph.Node {
		n := graph.NewNode("test:node").WithName(name).WithURI(uri)
		if err := ax.WriteNode(ctx, n); err != nil {
			t.Fatalf("WriteNode %s: %v", name, err)
		}
		return n
	}

	a := mkNode("A", "test:node:A")
	b := mkNode("B", "test:node:B")
	c := mkNode("C", "test:node:C")
	d := mkNode("D", "test:node:D")
	e := mkNode("E", "test:node:E")

	mkEdge := func(edgeType, fromID, toID string) {
		edge := graph.NewEdge(edgeType, fromID, toID)
		if err := ax.storage.PutEdge(ctx, edge); err != nil {
			t.Fatalf("PutEdge %s: %v", edgeType, err)
		}
	}

	mkEdge("calls", a.ID, b.ID) // A -> B
	mkEdge("calls", b.ID, c.ID) // B -> C
	mkEdge("uses", a.ID, d.ID)  // A -> D

	if err := ax.Flush(ctx); err != nil {
		t.Fatalf("Flush: %v", err)
	}

	return ax, map[string]*graph.Node{
		"A": a, "B": b, "C": c, "D": d, "E": e,
	}
}

// nodeNames extracts the Name field from a slice of NeighborResults.
func neighborNames(results []*NeighborResult) []string {
	names := make([]string, len(results))
	for i, r := range results {
		names[i] = r.Node.Name
	}
	return names
}

// containsName reports whether name appears in results.
func containsNeighborName(results []*NeighborResult, name string) bool {
	for _, r := range results {
		if r.Node.Name == name {
			return true
		}
	}
	return false
}

// ── Neighbors — outgoing direction ──────────────────────────────────────────

func TestNeighbors_Outgoing(t *testing.T) {
	ax, nodes := buildNeighborsGraph(t)
	ctx := context.Background()

	results, err := ax.Neighbors(ctx, nodes["A"].URI, NeighborsOptions{Direction: "out"})
	if err != nil {
		t.Fatalf("Neighbors: %v", err)
	}

	// A has two outgoing edges: calls→B and uses→D
	if len(results) != 2 {
		t.Fatalf("expected 2 outgoing neighbors of A, got %d: %v", len(results), neighborNames(results))
	}
	if !containsNeighborName(results, "B") {
		t.Error("expected B in outgoing neighbors")
	}
	if !containsNeighborName(results, "D") {
		t.Error("expected D in outgoing neighbors")
	}
	for _, r := range results {
		if r.Direction != "out" {
			t.Errorf("expected direction 'out', got %q", r.Direction)
		}
	}
}

// ── Neighbors — incoming direction ──────────────────────────────────────────

func TestNeighbors_Incoming(t *testing.T) {
	ax, nodes := buildNeighborsGraph(t)
	ctx := context.Background()

	results, err := ax.Neighbors(ctx, nodes["B"].URI, NeighborsOptions{Direction: "in"})
	if err != nil {
		t.Fatalf("Neighbors: %v", err)
	}

	// B has one incoming edge: A --calls--> B
	if len(results) != 1 {
		t.Fatalf("expected 1 incoming neighbor of B, got %d: %v", len(results), neighborNames(results))
	}
	if results[0].Node.Name != "A" {
		t.Errorf("expected A, got %s", results[0].Node.Name)
	}
	if results[0].Direction != "in" {
		t.Errorf("expected direction 'in', got %q", results[0].Direction)
	}
	if results[0].EdgeType != "calls" {
		t.Errorf("expected edge type 'calls', got %q", results[0].EdgeType)
	}
}

// ── Neighbors — both directions ──────────────────────────────────────────────

func TestNeighbors_Both(t *testing.T) {
	ax, nodes := buildNeighborsGraph(t)
	ctx := context.Background()

	// B has: A->B (incoming calls) and B->C (outgoing calls)
	results, err := ax.Neighbors(ctx, nodes["B"].URI, NeighborsOptions{Direction: "both"})
	if err != nil {
		t.Fatalf("Neighbors: %v", err)
	}

	if len(results) != 2 {
		t.Fatalf("expected 2 neighbors of B (both), got %d: %v", len(results), neighborNames(results))
	}
	if !containsNeighborName(results, "A") {
		t.Error("expected A in both-direction neighbors of B")
	}
	if !containsNeighborName(results, "C") {
		t.Error("expected C in both-direction neighbors of B")
	}
}

// ── Neighbors — default direction is "both" ───────────────────────────────

func TestNeighbors_DefaultDirection(t *testing.T) {
	ax, nodes := buildNeighborsGraph(t)
	ctx := context.Background()

	// With zero-value options, direction should default to "both"
	results, err := ax.Neighbors(ctx, nodes["B"].URI, NeighborsOptions{})
	if err != nil {
		t.Fatalf("Neighbors: %v", err)
	}

	if len(results) != 2 {
		t.Fatalf("expected 2 neighbors of B with default options, got %d", len(results))
	}
}

// ── Neighbors — edge type filter ─────────────────────────────────────────

func TestNeighbors_EdgeTypeFilter(t *testing.T) {
	ax, nodes := buildNeighborsGraph(t)
	ctx := context.Background()

	// A has calls→B and uses→D; filter to "uses" only
	results, err := ax.Neighbors(ctx, nodes["A"].URI, NeighborsOptions{
		Direction: "out",
		EdgeTypes: []string{"uses"},
	})
	if err != nil {
		t.Fatalf("Neighbors: %v", err)
	}

	if len(results) != 1 {
		t.Fatalf("expected 1 neighbor after edge type filter, got %d", len(results))
	}
	if results[0].Node.Name != "D" {
		t.Errorf("expected D, got %s", results[0].Node.Name)
	}
	if results[0].EdgeType != "uses" {
		t.Errorf("expected edge type 'uses', got %q", results[0].EdgeType)
	}
}

// ── Neighbors — max limit ─────────────────────────────────────────────────

func TestNeighbors_MaxLimit(t *testing.T) {
	ax, nodes := buildNeighborsGraph(t)
	ctx := context.Background()

	// A has 2 outgoing neighbors; limit to 1
	results, err := ax.Neighbors(ctx, nodes["A"].URI, NeighborsOptions{
		Direction: "out",
		Max:       1,
	})
	if err != nil {
		t.Fatalf("Neighbors: %v", err)
	}

	if len(results) != 1 {
		t.Fatalf("expected exactly 1 result with Max=1, got %d", len(results))
	}
}

// ── Neighbors — isolated node ─────────────────────────────────────────────

func TestNeighbors_NoEdges(t *testing.T) {
	ax, nodes := buildNeighborsGraph(t)
	ctx := context.Background()

	results, err := ax.Neighbors(ctx, nodes["E"].URI, NeighborsOptions{})
	if err != nil {
		t.Fatalf("Neighbors: %v", err)
	}

	if len(results) != 0 {
		t.Fatalf("expected no neighbors for isolated node E, got %d", len(results))
	}
}

// ── Neighbors — node not found ────────────────────────────────────────────

func TestNeighbors_NodeNotFound(t *testing.T) {
	ax, _ := buildNeighborsGraph(t)
	ctx := context.Background()

	_, err := ax.Neighbors(ctx, "test:node:NONEXISTENT", NeighborsOptions{})
	if err == nil {
		t.Fatal("expected error for non-existent URI, got nil")
	}
}

// ── Neighbors — result contains node metadata ─────────────────────────────

func TestNeighbors_ResultMetadata(t *testing.T) {
	ax, nodes := buildNeighborsGraph(t)
	ctx := context.Background()

	results, err := ax.Neighbors(ctx, nodes["A"].URI, NeighborsOptions{
		Direction: "out",
		EdgeTypes: []string{"calls"},
	})
	if err != nil {
		t.Fatalf("Neighbors: %v", err)
	}

	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}

	r := results[0]
	if r.Node == nil {
		t.Fatal("Node must not be nil")
	}
	if r.Node.ID == "" {
		t.Error("Node ID must not be empty")
	}
	if r.Node.Type == "" {
		t.Error("Node type must not be empty")
	}
	if r.EdgeType == "" {
		t.Error("EdgeType must not be empty")
	}
	if r.Direction == "" {
		t.Error("Direction must not be empty")
	}
}
