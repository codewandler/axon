package embeddings

import "context"

// NullProvider returns zero vectors of a fixed dimension.
// Use in tests or as a no-op placeholder where a real provider is not needed.
type NullProvider struct {
	dims int
}

// NewNull creates a NullProvider. dims defaults to 384 if <= 0.
func NewNull(dims int) *NullProvider {
	if dims <= 0 {
		dims = 384
	}
	return &NullProvider{dims: dims}
}

func (p *NullProvider) Embed(_ context.Context, _ string) ([]float32, error) {
	return make([]float32, p.dims), nil
}

func (p *NullProvider) EmbedBatch(_ context.Context, texts []string) ([][]float32, error) {
	out := make([][]float32, len(texts))
	for i := range out {
		out[i] = make([]float32, p.dims)
	}
	return out, nil
}

func (p *NullProvider) Dimensions() int { return p.dims }
func (p *NullProvider) Name() string    { return "null" }
func (p *NullProvider) Close() error    { return nil }
