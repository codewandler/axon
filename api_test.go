package axon

import (
	"context"
	"testing"
	"time"

	"github.com/codewandler/axon/graph"
	"github.com/codewandler/axon/indexer/embeddings"
)

// stubEmbedder returns a fixed all-ones vector so cosine similarity = 1.0.
type stubEmbedder struct{ dims int }

func (s *stubEmbedder) Embed(_ context.Context, _ string) ([]float32, error) {
	v := make([]float32, s.dims)
	for i := range v {
		v[i] = 1
	}
	return v, nil
}
func (s *stubEmbedder) EmbedBatch(_ context.Context, texts []string) ([][]float32, error) {
	out := make([][]float32, len(texts))
	for i := range texts {
		v := make([]float32, s.dims)
		for j := range v {
			v[j] = 1
		}
		out[i] = v
	}
	return out, nil
}
func (s *stubEmbedder) Dimensions() int { return s.dims }
func (s *stubEmbedder) Name() string    { return "stub" }
func (s *stubEmbedder) Close() error    { return nil }

var _ embeddings.Provider = (*stubEmbedder)(nil)

// ── Search (SearchOptions) ───────────────────────────────────────────────────

func TestAxon_Search_NoProvider(t *testing.T) {
	ax, err := New(Config{Dir: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	_, err = ax.Search(context.Background(), []string{"x"}, SearchOptions{})
	if err != ErrNoEmbeddingProvider {
		t.Fatalf("want ErrNoEmbeddingProvider, got %v", err)
	}
}

func TestAxon_Search_MinScore_FiltersResults(t *testing.T) {
	dir := t.TempDir()
	provider := &stubEmbedder{dims: 4}
	ax, err := New(Config{Dir: dir, EmbeddingProvider: provider})
	if err != nil {
		t.Fatal(err)
	}

	node := graph.NewNode("memory:note").WithName("hello").WithURI("memory:note:hello")
	if err := ax.WriteNode(context.Background(), node); err != nil {
		t.Fatal(err)
	}

	ctx := context.Background()

	// MinScore 0 — should return the node (score = 1.0 with identical unit vecs).
	got, err := ax.Search(ctx, []string{"hello"}, SearchOptions{Limit: 5, MinScore: 0})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) == 0 {
		t.Fatal("expected results with MinScore=0")
	}

	// MinScore above maximum possible score — should return nothing.
	got, err = ax.Search(ctx, []string{"hello"}, SearchOptions{Limit: 5, MinScore: 1.1})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 {
		t.Fatalf("expected no results with MinScore=1.1, got %d", len(got))
	}
}

// ── WriteNode (PutNode + Flush + auto-embed) ─────────────────────────────────

func TestAxon_WriteNode_FoundByFind(t *testing.T) {
	ax, err := New(Config{Dir: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}

	node := graph.NewNode("memory:fact").WithName("Honey never spoils").WithURI("memory:fact:honey")
	if err := ax.WriteNode(context.Background(), node); err != nil {
		t.Fatal(err)
	}

	nodes, err := ax.Find(context.Background(), graph.NodeFilter{Type: "memory:fact"}, graph.QueryOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if len(nodes) == 0 {
		t.Fatal("expected node to be findable after WriteNode")
	}
	if nodes[0].Name != "Honey never spoils" {
		t.Errorf("unexpected name %q", nodes[0].Name)
	}
}

func TestAxon_WriteNode_AutoEmbed(t *testing.T) {
	provider := &stubEmbedder{dims: 4}
	ax, err := New(Config{Dir: t.TempDir(), EmbeddingProvider: provider})
	if err != nil {
		t.Fatal(err)
	}

	node := graph.NewNode("memory:note").WithName("auto-embed test").WithURI("memory:note:embed-test")
	if err := ax.WriteNode(context.Background(), node); err != nil {
		t.Fatal(err)
	}

	// The node should be findable via SemanticSearch because WriteNode embedded it.
	results, err := ax.SemanticSearch(context.Background(), []string{"test"}, 5, nil)
	if err != nil {
		t.Fatal(err)
	}
	var found bool
	for _, r := range results {
		if r.Name == "auto-embed test" {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected WriteNode to embed the node so it appears in SemanticSearch")
	}
}

func TestAxon_WriteNode_NoEmbedWithoutProvider(t *testing.T) {
	// No EmbeddingProvider — WriteNode should succeed without embedding.
	ax, err := New(Config{Dir: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}

	node := graph.NewNode("memory:note").WithName("no embed").WithURI("memory:note:no-embed")
	if err := ax.WriteNode(context.Background(), node); err != nil {
		t.Fatalf("WriteNode without provider: %v", err)
	}
}

// ── GetNodeByURI / PutNode / Flush ───────────────────────────────────────────

func TestAxon_GetNodeByURI(t *testing.T) {
	ax, err := New(Config{Dir: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}

	node := graph.NewNode("memory:note").WithName("lookup test").WithURI("memory:note:lookup")
	if err := ax.WriteNode(context.Background(), node); err != nil {
		t.Fatal(err)
	}

	got, err := ax.GetNodeByURI(context.Background(), "memory:note:lookup")
	if err != nil {
		t.Fatalf("GetNodeByURI: %v", err)
	}
	if got.Name != "lookup test" {
		t.Errorf("unexpected name %q", got.Name)
	}
}

func TestAxon_PutNode_And_Flush(t *testing.T) {
	ax, err := New(Config{Dir: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}

	node := graph.NewNode("memory:note").WithName("raw put").WithURI("memory:note:raw")
	if err := ax.PutNode(context.Background(), node); err != nil {
		t.Fatalf("PutNode: %v", err)
	}
	if err := ax.Flush(context.Background()); err != nil {
		t.Fatalf("Flush: %v", err)
	}

	nodes, err := ax.Find(context.Background(), graph.NodeFilter{Type: "memory:note"}, graph.QueryOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if len(nodes) == 0 {
		t.Fatal("expected node after PutNode + Flush")
	}
}

// ── NodeFilter.Normalize (Extensions dot-stripping) ─────────────────────────

func TestAxon_Find_Extensions_DotNormalized(t *testing.T) {
	ax, err := New(Config{Dir: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()

	for _, name := range []string{"main.go", "main.py"} {
		n := graph.NewNode("fs:file").WithName(name)
		if err := ax.WriteNode(ctx, n); err != nil {
			t.Fatal(err)
		}
	}

	// With dot prefix — should behave identically to without.
	nodes, err := ax.Find(ctx, graph.NodeFilter{Extensions: []string{".go"}}, graph.QueryOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if len(nodes) != 1 || nodes[0].Name != "main.go" {
		t.Errorf("expected main.go only, got %v", nodes)
	}
}

// ── TTL / WriteNode with TTL ───────────────────────────────────────────────

func TestAxon_WriteNode_WithTTL_ExpiredInvisible(t *testing.T) {
	// Write a node with a TTL already elapsed, then verify it's not findable.
	ax, err := New(Config{Dir: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()

	past := time.Now().Add(-time.Second)
	node := graph.NewNode("memory:note").
		WithURI("memory://api-ttl/expired").
		WithName("stale note")
	node.ExpiresAt = &past

	if err := ax.WriteNode(ctx, node); err != nil {
		t.Fatalf("WriteNode: %v", err)
	}

	// Must not appear in Find.
	nodes, err := ax.Find(ctx, graph.NodeFilter{Type: "memory:note"}, graph.QueryOptions{})
	if err != nil {
		t.Fatalf("Find: %v", err)
	}
	for _, n := range nodes {
		if n.URI == node.URI {
			t.Error("expired node must not appear in Find results")
		}
	}

	// After DeleteExpired, the row is physically gone.
	delN, _, err := ax.storage.DeleteExpired(ctx)
	if err != nil {
		t.Fatalf("DeleteExpired: %v", err)
	}
	if delN != 1 {
		t.Errorf("expected 1 node deleted by DeleteExpired, got %d", delN)
	}
}
