package axon

import (
	"context"
	"testing"

	"github.com/codewandler/axon/graph"
)

// buildPathGraph creates an Axon instance with a small graph for path-finding
// tests. Topology (edges are directed from left to right unless noted):
//
//	A --calls--> B --calls--> C --calls--> D
//	             |
//	             +--uses--> E
//
// Node IDs are derived from URIs so tests can reference them directly.
func buildPathGraph(t *testing.T) (*Axon, map[string]*graph.Node) {
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

	mkEdge("calls", a.ID, b.ID)
	mkEdge("calls", b.ID, c.ID)
	mkEdge("calls", c.ID, d.ID)
	mkEdge("uses", b.ID, e.ID)

	if err := ax.Flush(ctx); err != nil {
		t.Fatalf("Flush: %v", err)
	}

	return ax, map[string]*graph.Node{
		"A": a, "B": b, "C": c, "D": d, "E": e,
	}
}

func TestFindPath_DirectEdge(t *testing.T) {
	ax, nodes := buildPathGraph(t)
	ctx := context.Background()

	paths, err := ax.FindPath(ctx, nodes["A"].ID, nodes["B"].ID, PathOptions{})
	if err != nil {
		t.Fatalf("FindPath: %v", err)
	}
	if len(paths) == 0 {
		t.Fatal("expected at least one path, got none")
	}
	if paths[0].Length() != 1 {
		t.Errorf("expected path length 1, got %d", paths[0].Length())
	}
	if paths[0].Steps[0].Node.ID != nodes["A"].ID {
		t.Errorf("first step should be A, got %s", paths[0].Steps[0].Node.Name)
	}
	if paths[0].Steps[1].Node.ID != nodes["B"].ID {
		t.Errorf("last step should be B, got %s", paths[0].Steps[1].Node.Name)
	}
	if paths[0].Steps[1].EdgeType != "calls" {
		t.Errorf("edge type should be 'calls', got %q", paths[0].Steps[1].EdgeType)
	}
}

func TestFindPath_MultiHop(t *testing.T) {
	ax, nodes := buildPathGraph(t)
	ctx := context.Background()

	// A -> B -> C -> D: 3 hops
	paths, err := ax.FindPath(ctx, nodes["A"].ID, nodes["D"].ID, PathOptions{})
	if err != nil {
		t.Fatalf("FindPath: %v", err)
	}
	if len(paths) == 0 {
		t.Fatal("expected a path A→D")
	}
	if paths[0].Length() != 3 {
		t.Errorf("expected path length 3, got %d", paths[0].Length())
	}
}

func TestFindPath_NoPath(t *testing.T) {
	ax, nodes := buildPathGraph(t)
	ctx := context.Background()

	// D has no outgoing edges and is not a predecessor of A — no path D→A.
	paths, err := ax.FindPath(ctx, nodes["D"].ID, nodes["A"].ID, PathOptions{MaxDepth: 2})
	if err != nil {
		t.Fatalf("FindPath: %v", err)
	}
	// With MaxDepth=2 we can go 2 hops. D has no outgoing edges but has
	// incoming from C. Following backwards: D←C←B←A is 3 hops, so no path.
	if len(paths) != 0 {
		t.Errorf("expected no path, got %d", len(paths))
	}
}

func TestFindPath_ReverseTraversal(t *testing.T) {
	ax, nodes := buildPathGraph(t)
	ctx := context.Background()

	// D has no outgoing edges. The only way to reach B from D is via reverse
	// traversal of the calls-edges: D ←(calls)← C ←(calls)← B.
	// Both steps are Incoming=true.
	paths, err := ax.FindPath(ctx, nodes["D"].ID, nodes["B"].ID, PathOptions{})
	if err != nil {
		t.Fatalf("FindPath: %v", err)
	}
	if len(paths) == 0 {
		t.Fatal("expected reverse path D←C←B")
	}
	if paths[0].Length() != 2 {
		t.Errorf("expected 2-hop reverse path, got length %d", paths[0].Length())
	}
	for _, step := range paths[0].Steps[1:] {
		if !step.Incoming {
			t.Errorf("expected Incoming=true for step %s, got false", step.Node.Name)
		}
	}
}

func TestFindPath_MaxDepthRespected(t *testing.T) {
	ax, nodes := buildPathGraph(t)
	ctx := context.Background()

	// A→B→C→D is 3 hops; with MaxDepth=2 it should not be found.
	paths, err := ax.FindPath(ctx, nodes["A"].ID, nodes["D"].ID, PathOptions{MaxDepth: 2})
	if err != nil {
		t.Fatalf("FindPath: %v", err)
	}
	if len(paths) != 0 {
		t.Errorf("expected no paths within depth 2, got %d", len(paths))
	}
}

func TestFindPath_EdgeTypeFilter(t *testing.T) {
	ax, nodes := buildPathGraph(t)
	ctx := context.Background()

	// A→B via "calls"; B→E via "uses". With EdgeTypes=["uses"] only the
	// uses-edge is traversable. A can't reach E in one "uses" hop (A→B is
	// "calls"), so no path from A to E via only "uses" edges.
	paths, err := ax.FindPath(ctx, nodes["A"].ID, nodes["E"].ID, PathOptions{
		EdgeTypes: []string{"uses"},
	})
	if err != nil {
		t.Fatalf("FindPath: %v", err)
	}
	if len(paths) != 0 {
		t.Errorf("expected no path with only 'uses' edges from A to E, got %d", len(paths))
	}

	// B→E via "uses": should work with EdgeTypes=["uses"].
	paths, err = ax.FindPath(ctx, nodes["B"].ID, nodes["E"].ID, PathOptions{
		EdgeTypes: []string{"uses"},
	})
	if err != nil {
		t.Fatalf("FindPath: %v", err)
	}
	if len(paths) == 0 {
		t.Fatal("expected path B→E via 'uses'")
	}
	if paths[0].Steps[1].EdgeType != "uses" {
		t.Errorf("expected edge type 'uses', got %q", paths[0].Steps[1].EdgeType)
	}
}

func TestFindPath_Defaults(t *testing.T) {
	ax, nodes := buildPathGraph(t)
	ctx := context.Background()

	// Zero-value PathOptions should use sensible defaults (MaxDepth=6, MaxPaths=3).
	paths, err := ax.FindPath(ctx, nodes["A"].ID, nodes["D"].ID, PathOptions{})
	if err != nil {
		t.Fatalf("FindPath: %v", err)
	}
	if len(paths) == 0 {
		t.Fatal("expected paths with default options")
	}
}

func TestFindPath_MaxPaths(t *testing.T) {
	// Build a graph with two distinct paths from S to T:
	//   S --a--> M1 --b--> T
	//   S --c--> M2 --d--> T
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
	s := mkNode("S", "test:node:S")
	m1 := mkNode("M1", "test:node:M1")
	m2 := mkNode("M2", "test:node:M2")
	tNode := mkNode("T", "test:node:T")

	for _, e := range [][3]string{
		{"a", s.ID, m1.ID},
		{"b", m1.ID, tNode.ID},
		{"c", s.ID, m2.ID},
		{"d", m2.ID, tNode.ID},
	} {
		if err := ax.storage.PutEdge(ctx, graph.NewEdge(e[0], e[1], e[2])); err != nil {
			t.Fatalf("PutEdge: %v", err)
		}
	}
	if err := ax.Flush(ctx); err != nil {
		t.Fatalf("Flush: %v", err)
	}

	// MaxPaths=1 should return exactly one path.
	paths, err := ax.FindPath(ctx, s.ID, tNode.ID, PathOptions{MaxPaths: 1})
	if err != nil {
		t.Fatalf("FindPath: %v", err)
	}
	if len(paths) != 1 {
		t.Errorf("MaxPaths=1: expected 1 path, got %d", len(paths))
	}

	// MaxPaths=2 should return both paths.
	paths, err = ax.FindPath(ctx, s.ID, tNode.ID, PathOptions{MaxPaths: 2})
	if err != nil {
		t.Fatalf("FindPath: %v", err)
	}
	if len(paths) != 2 {
		t.Errorf("MaxPaths=2: expected 2 paths, got %d", len(paths))
	}
}

func TestFindPath_SameNode(t *testing.T) {
	ax, nodes := buildPathGraph(t)
	ctx := context.Background()

	// From and to are the same node — no meaningful path.
	paths, err := ax.FindPath(ctx, nodes["A"].ID, nodes["A"].ID, PathOptions{})
	if err != nil {
		t.Fatalf("FindPath: %v", err)
	}
	if len(paths) != 0 {
		t.Errorf("expected no path from A to A, got %d", len(paths))
	}
}

func TestPath_Length(t *testing.T) {
	p := &Path{Steps: []PathStep{{}, {}, {}}}
	if p.Length() != 2 {
		t.Errorf("Length(): expected 2, got %d", p.Length())
	}

	empty := &Path{}
	if empty.Length() != 0 {
		t.Errorf("Length() of empty path: expected 0, got %d", empty.Length())
	}
}
