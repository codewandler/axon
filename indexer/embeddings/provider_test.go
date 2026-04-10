package embeddings

import (
	"context"
	"testing"
)

func TestNullProvider(t *testing.T) {
	p := NewNull(384)
	if p.Dimensions() != 384 {
		t.Errorf("expected 384 dims, got %d", p.Dimensions())
	}
	if p.Name() != "null" {
		t.Errorf("expected name 'null', got %q", p.Name())
	}

	ctx := context.Background()
	embedding, err := p.Embed(ctx, "test text")
	if err != nil {
		t.Fatalf("Embed failed: %v", err)
	}
	if len(embedding) != 384 {
		t.Errorf("expected 384 dims, got %d", len(embedding))
	}
	for i, v := range embedding {
		if v != 0 {
			t.Errorf("expected zero at dim %d, got %f", i, v)
		}
	}
}
