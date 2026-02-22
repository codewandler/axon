package context

import (
	"context"
	"testing"

	"github.com/codewandler/axon/adapters/sqlite"
	"github.com/codewandler/axon/graph"
	"github.com/codewandler/axon/types"
)

func setupTestGraph(t *testing.T) graph.Storage {
	t.Helper()

	r := graph.NewRegistry()
	types.RegisterCommonEdges(r)
	types.RegisterGoTypes(r)

	s, err := sqlite.New(":memory:")
	if err != nil {
		t.Fatalf("sqlite.New failed: %v", err)
	}
	t.Cleanup(func() { s.Close() })

	ctx := context.Background()

	// Create a mock graph with Go symbols
	// Package
	pkgNode := graph.NewNode("go:package").
		WithURI("go+file:///test/pkg/example").
		WithName("example").
		WithData(map[string]any{
			"name":        "example",
			"import_path": "example.com/test/example",
		})
	s.PutNode(ctx, pkgNode)

	// Interface
	ifaceNode := graph.NewNode("go:interface").
		WithURI("go+file:///test/pkg/example/interface/Storage").
		WithName("Storage").
		WithData(map[string]any{
			"name":     "Storage",
			"doc":      "Storage is the main interface",
			"exported": true,
			"position": map[string]any{
				"file":     "/test/pkg/storage.go",
				"line":     10.0,
				"end_line": 20.0,
			},
		})
	s.PutNode(ctx, ifaceNode)

	// Interface method
	methodNode := graph.NewNode("go:method").
		WithURI("go+file:///test/pkg/example/interface/Storage/method/Get").
		WithName("Get").
		WithData(map[string]any{
			"name":      "Get",
			"signature": "Get(id string) (*Item, error)",
			"exported":  true,
			"position": map[string]any{
				"file":     "/test/pkg/storage.go",
				"line":     12.0,
				"end_line": 12.0,
			},
		})
	s.PutNode(ctx, methodNode)

	// Struct implementing the interface
	structNode := graph.NewNode("go:struct").
		WithURI("go+file:///test/pkg/example/struct/MemoryStorage").
		WithName("MemoryStorage").
		WithData(map[string]any{
			"name":     "MemoryStorage",
			"doc":      "MemoryStorage implements Storage",
			"exported": true,
			"position": map[string]any{
				"file":     "/test/pkg/memory.go",
				"line":     5.0,
				"end_line": 15.0,
			},
		})
	s.PutNode(ctx, structNode)

	// Struct method (implementing Get)
	structMethodNode := graph.NewNode("go:method").
		WithURI("go+file:///test/pkg/example/method/MemoryStorage.Get").
		WithName("Get").
		WithData(map[string]any{
			"name":      "Get",
			"receiver":  "MemoryStorage",
			"signature": "Get(id string) (*Item, error)",
			"exported":  true,
			"position": map[string]any{
				"file":     "/test/pkg/memory.go",
				"line":     20.0,
				"end_line": 30.0,
			},
		})
	s.PutNode(ctx, structMethodNode)

	// Function
	funcNode := graph.NewNode("go:func").
		WithURI("go+file:///test/pkg/example/func/NewStorage").
		WithName("NewStorage").
		WithData(map[string]any{
			"name":      "NewStorage",
			"signature": "NewStorage() Storage",
			"exported":  true,
			"position": map[string]any{
				"file":     "/test/pkg/storage.go",
				"line":     25.0,
				"end_line": 35.0,
			},
		})
	s.PutNode(ctx, funcNode)

	// Reference to Storage
	refNode := graph.NewNode("go:ref").
		WithURI("go+file:///test/pkg/example/ref/main.go:10:5").
		WithName("Storage").
		WithData(map[string]any{
			"name":        "Storage",
			"kind":        "type",
			"target_type": "go:interface",
			"position": map[string]any{
				"file":     "/test/pkg/main.go",
				"line":     10.0,
				"end_line": 10.0,
			},
		})
	s.PutNode(ctx, refNode)

	// Another reference to Storage (call)
	refNode2 := graph.NewNode("go:ref").
		WithURI("go+file:///test/pkg/example/ref/main.go:15:3").
		WithName("Storage").
		WithData(map[string]any{
			"name":        "Storage",
			"kind":        "call",
			"target_type": "go:interface",
			"position": map[string]any{
				"file":     "/test/pkg/main.go",
				"line":     15.0,
				"end_line": 15.0,
			},
		})
	s.PutNode(ctx, refNode2)

	// Edges
	// Package defines interface
	s.PutEdge(ctx, graph.NewEdge("defines", pkgNode.ID, ifaceNode.ID))
	// Interface has method
	s.PutEdge(ctx, graph.NewEdge("has", ifaceNode.ID, methodNode.ID))
	// Package defines struct
	s.PutEdge(ctx, graph.NewEdge("defines", pkgNode.ID, structNode.ID))
	// Struct has method
	s.PutEdge(ctx, graph.NewEdge("has", structNode.ID, structMethodNode.ID))
	// Package defines function
	s.PutEdge(ctx, graph.NewEdge("defines", pkgNode.ID, funcNode.ID))
	// Interface belongs_to package
	s.PutEdge(ctx, graph.NewEdge("belongs_to", ifaceNode.ID, pkgNode.ID))
	// Struct belongs_to package
	s.PutEdge(ctx, graph.NewEdge("belongs_to", structNode.ID, pkgNode.ID))
	// References edge
	s.PutEdge(ctx, graph.NewEdge("references", refNode.ID, ifaceNode.ID))
	s.PutEdge(ctx, graph.NewEdge("references", refNode2.ID, ifaceNode.ID))

	s.Flush(ctx)

	return s
}

func TestWalk_FindsDefinitions(t *testing.T) {
	storage := setupTestGraph(t)
	ctx := context.Background()

	task := ParseTask("implement caching for Storage")
	items, err := Walk(ctx, storage, task, DefaultWalkOptions())
	if err != nil {
		t.Fatalf("Walk failed: %v", err)
	}

	// Should find the Storage interface
	var foundStorage bool
	for _, item := range items {
		if item.Node.Name == "Storage" && item.Ring == RingDefinition {
			foundStorage = true
			if item.Score != 100.0 {
				t.Errorf("Expected definition score 100, got %v", item.Score)
			}
			break
		}
	}
	if !foundStorage {
		t.Error("Expected to find Storage definition")
	}
}

func TestWalk_ExpandsRings(t *testing.T) {
	storage := setupTestGraph(t)
	ctx := context.Background()

	task := ParseTask("modify Storage interface")
	items, err := Walk(ctx, storage, task, DefaultWalkOptions())
	if err != nil {
		t.Fatalf("Walk failed: %v", err)
	}

	// Check we have items from different rings
	ringCounts := make(map[Ring]int)
	for _, item := range items {
		ringCounts[item.Ring]++
	}

	if ringCounts[RingDefinition] == 0 {
		t.Error("Expected items in Ring 0 (definition)")
	}
	// Ring 1 should have the Get method (child of Storage)
	// Ring 2 should have references
	// Ring 3 should have siblings from same package

	t.Logf("Ring distribution: %v", ringCounts)
	t.Logf("Total items: %d", len(items))
}

func TestWalk_ScoresCorrectly(t *testing.T) {
	storage := setupTestGraph(t)
	ctx := context.Background()

	task := ParseTask("fix Storage")
	task.Intent = IntentFix // Explicitly set for test

	items, err := Walk(ctx, storage, task, DefaultWalkOptions())
	if err != nil {
		t.Fatalf("Walk failed: %v", err)
	}

	// Items should be sorted by score descending
	for i := 1; i < len(items); i++ {
		if items[i].Score > items[i-1].Score {
			t.Errorf("Items not sorted by score: %v > %v at position %d",
				items[i].Score, items[i-1].Score, i)
		}
	}

	// Ring 0 items should have highest scores
	for _, item := range items {
		if item.Ring == RingDefinition && item.Score < 100 {
			t.Errorf("Definition item %s has score %v, expected >= 100",
				item.Node.Name, item.Score)
		}
	}
}

func TestWalk_RespectsMaxRing(t *testing.T) {
	storage := setupTestGraph(t)
	ctx := context.Background()

	task := ParseTask("check Storage")

	// Test with MaxRing = 0 (definitions only)
	opts := WalkOptions{MaxRing: RingDefinition}
	items, err := Walk(ctx, storage, task, opts)
	if err != nil {
		t.Fatalf("Walk failed: %v", err)
	}

	for _, item := range items {
		if item.Ring > RingDefinition {
			t.Errorf("Found item with ring %v when max was %v", item.Ring, RingDefinition)
		}
	}
}

func TestWalk_EmptySymbols(t *testing.T) {
	storage := setupTestGraph(t)
	ctx := context.Background()

	task := ParseTask("do something general")
	task.Symbols = nil // No symbols

	items, err := Walk(ctx, storage, task, DefaultWalkOptions())
	if err != nil {
		t.Fatalf("Walk failed: %v", err)
	}

	if len(items) != 0 {
		t.Errorf("Expected no items for empty symbols, got %d", len(items))
	}
}

func TestWalk_ExtractsPosition(t *testing.T) {
	storage := setupTestGraph(t)
	ctx := context.Background()

	task := ParseTask("Storage interface")
	items, err := Walk(ctx, storage, task, DefaultWalkOptions())
	if err != nil {
		t.Fatalf("Walk failed: %v", err)
	}

	// Find the Storage definition and check position
	for _, item := range items {
		if item.Node.Name == "Storage" && item.Ring == RingDefinition {
			if item.File != "/test/pkg/storage.go" {
				t.Errorf("Expected file /test/pkg/storage.go, got %s", item.File)
			}
			if item.StartLine != 10 {
				t.Errorf("Expected StartLine 10, got %d", item.StartLine)
			}
			if item.EndLine != 20 {
				t.Errorf("Expected EndLine 20, got %d", item.EndLine)
			}
			return
		}
	}
	t.Error("Did not find Storage definition with position info")
}
