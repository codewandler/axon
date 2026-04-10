package embeddings

import (
	"context"
	"testing"
)

// Compile-time interface checks — these fail at build time if any provider
// stops satisfying the Provider interface.
var _ Provider = (*NullProvider)(nil)
var _ Provider = (*OllamaProvider)(nil)

func TestNullProvider(t *testing.T) {
	p := NewNull(384)

	if p.Dimensions() != 384 {
		t.Errorf("Dimensions: want 384, got %d", p.Dimensions())
	}
	if p.Name() != "null" {
		t.Errorf("Name: want %q, got %q", "null", p.Name())
	}

	vec, err := p.Embed(context.Background(), "test")
	if err != nil {
		t.Fatalf("Embed: %v", err)
	}
	if len(vec) != 384 {
		t.Errorf("Embed length: want 384, got %d", len(vec))
	}
	for i, v := range vec {
		if v != 0 {
			t.Errorf("Embed[%d]: want 0, got %f", i, v)
		}
	}

	if err := p.Close(); err != nil {
		t.Errorf("Close: %v", err)
	}
}

func TestNullProviderDefaultDims(t *testing.T) {
	p := NewNull(0)
	if p.Dimensions() != 384 {
		t.Errorf("expected default 384 dims, got %d", p.Dimensions())
	}
}

func TestNullProviderEmbedBatch(t *testing.T) {
	p := NewNull(384)
	texts := []string{"foo", "bar", "baz"}
	out, err := p.EmbedBatch(context.Background(), texts)
	if err != nil {
		t.Fatalf("EmbedBatch: %v", err)
	}
	if len(out) != len(texts) {
		t.Fatalf("EmbedBatch len: want %d, got %d", len(texts), len(out))
	}
	for i, vec := range out {
		if len(vec) != 384 {
			t.Errorf("out[%d] length: want 384, got %d", i, len(vec))
		}
	}
}

func TestOllamaProviderDefaults(t *testing.T) {
	p := NewOllama("", "", 0)
	if p.Name() != "ollama/nomic-embed-text" {
		t.Errorf("Name: got %q", p.Name())
	}
	if p.Dimensions() != 768 {
		t.Errorf("Dimensions: got %d", p.Dimensions())
	}
	if err := p.Close(); err != nil {
		t.Errorf("Close: %v", err)
	}
}

func TestOllamaProviderCustomDims(t *testing.T) {
	p := NewOllama("http://localhost:11434", "mxbai-embed-large", 1024)
	if p.Dimensions() != 1024 {
		t.Errorf("Dimensions: want 1024, got %d", p.Dimensions())
	}
	if p.Name() != "ollama/mxbai-embed-large" {
		t.Errorf("Name: got %q", p.Name())
	}
}
