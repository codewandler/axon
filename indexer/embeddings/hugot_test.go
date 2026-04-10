package embeddings

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

// Compile-time interface check.
var _ Provider = (*HugotProvider)(nil)

func TestHugotProviderDefaults(t *testing.T) {
	p := NewHugot("", "")

	wantName := "hugot/" + DefaultHugotModel
	if p.Name() != wantName {
		t.Errorf("Name: want %q, got %q", wantName, p.Name())
	}
	if p.Dimensions() != DefaultHugotDims {
		t.Errorf("Dimensions: want %d, got %d", DefaultHugotDims, p.Dimensions())
	}

	// modelPath must default to ~/.axon/models/<org>_<model>
	home, _ := os.UserHomeDir()
	wantPath := filepath.Join(home, ".axon", "models", "KnightsAnalytics_all-MiniLM-L6-v2")
	if p.modelPath != wantPath {
		t.Errorf("modelPath: want %q, got %q", wantPath, p.modelPath)
	}

	// Close on uninitialized provider must be a no-op
	if err := p.Close(); err != nil {
		t.Errorf("Close on uninitialized: %v", err)
	}
}

func TestHugotProviderCustomPaths(t *testing.T) {
	p := NewHugot("/tmp/mymodels/minilm", "MyOrg/my-model")

	if p.modelPath != "/tmp/mymodels/minilm" {
		t.Errorf("modelPath: got %q", p.modelPath)
	}
	if p.modelName != "MyOrg/my-model" {
		t.Errorf("modelName: got %q", p.modelName)
	}
	if p.Name() != "hugot/MyOrg/my-model" {
		t.Errorf("Name: got %q", p.Name())
	}
}

// TestHugotProviderEmbed runs a real embedding against the downloaded model.
// Skipped unless AXON_TEST_HUGOT=1 is set. Downloads ~90 MB on first run,
// then cached at ~/.axon/models/all-MiniLM-L6-v2.
func TestHugotProviderEmbed(t *testing.T) {
	if os.Getenv("AXON_TEST_HUGOT") == "" {
		t.Skip("set AXON_TEST_HUGOT=1 to run (downloads model on first run ~90 MB)")
	}

	p := NewHugot("", "")
	t.Cleanup(func() { _ = p.Close() })

	vec, err := p.Embed(context.Background(), "the graph storage interface")
	if err != nil {
		t.Fatalf("Embed: %v", err)
	}
	if len(vec) != DefaultHugotDims {
		t.Errorf("embedding length: want %d, got %d", DefaultHugotDims, len(vec))
	}

	nonZero := 0
	for _, v := range vec {
		if v != 0 {
			nonZero++
		}
	}
	if nonZero == 0 {
		t.Error("embedding is all zeros — model produced no output")
	}

	// Second call must reuse the session (no re-download, no re-init)
	vec2, err := p.Embed(context.Background(), "indexer interface")
	if err != nil {
		t.Fatalf("Embed (second call): %v", err)
	}
	if len(vec2) != DefaultHugotDims {
		t.Errorf("second embedding length: want %d, got %d", DefaultHugotDims, len(vec2))
	}

	// EmbedBatch with multiple inputs
	texts := []string{"storage interface", "indexer interface", "graph node"}
	out, err := p.EmbedBatch(context.Background(), texts)
	if err != nil {
		t.Fatalf("EmbedBatch: %v", err)
	}
	if len(out) != len(texts) {
		t.Errorf("EmbedBatch len: want %d, got %d", len(texts), len(out))
	}
	for i, v := range out {
		if len(v) != DefaultHugotDims {
			t.Errorf("out[%d] length: want %d, got %d", i, DefaultHugotDims, len(v))
		}
	}
}
