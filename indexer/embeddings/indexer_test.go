package embeddings

import (
	"context"
	"testing"

	"github.com/codewandler/axon/adapters/sqlite"
	"github.com/codewandler/axon/graph"
	"github.com/codewandler/axon/indexer"
	"github.com/codewandler/axon/types"
)

func TestPostIndex(t *testing.T) {
	ctx := context.Background()

	r := graph.NewRegistry()
	types.RegisterCommonEdges(r)
	types.RegisterGoTypes(r)

	s, err := sqlite.New(":memory:")
	if err != nil {
		t.Fatalf("sqlite.New: %v", err)
	}
	t.Cleanup(func() { s.Close() })

	g := graph.New(s, r)

	// Add a go:func node
	emitter := indexer.NewGraphEmitter(g, "gen-1")
	node := graph.NewNode(types.TypeGoFunc).
		WithURI("go+file:///test/mod/pkg/mypkg/func/DoSomething").
		WithKey("DoSomething").
		WithName("DoSomething")
	if err := emitter.EmitNode(ctx, node); err != nil {
		t.Fatalf("EmitNode: %v", err)
	}
	if err := s.Flush(ctx); err != nil {
		t.Fatalf("Flush: %v", err)
	}

	// Run post-indexer with NullProvider
	idx := New(NewNull(384))
	ictx := &indexer.Context{
		Root:       "go+file:///test/mod",
		Generation: "gen-1",
		Graph:      g,
		Emitter:    emitter,
	}

	if err := idx.PostIndex(ctx, ictx); err != nil {
		t.Fatalf("PostIndex: %v", err)
	}

	// Verify embedding was stored
	embedding, err := s.GetEmbedding(ctx, node.ID)
	if err != nil {
		t.Fatalf("GetEmbedding: %v", err)
	}
	if len(embedding) != 384 {
		t.Errorf("expected 384 dims, got %d", len(embedding))
	}
}
