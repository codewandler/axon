package embeddings

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
)

const defaultOllamaDims = 768 // matches nomic-embed-text default

// OllamaProvider calls the local Ollama daemon's embedding API.
// Data never leaves localhost.
//
// Uses the /api/embed endpoint (Ollama ≥0.1.31) which accepts a batch of
// inputs in a single request, dramatically reducing HTTP overhead on GPU-
// accelerated deployments where inference is much faster than the round-trip.
type OllamaProvider struct {
	baseURL string
	model   string
	dims    int
}

// NewOllama creates an OllamaProvider.
//   - baseURL defaults to "http://localhost:11434" if empty.
//   - model   defaults to "nomic-embed-text" if empty.
//   - dims    defaults to 768 if <= 0 (correct for nomic-embed-text).
//     Pass the correct value when using a different model.
func NewOllama(baseURL, model string, dims int) *OllamaProvider {
	if baseURL == "" {
		baseURL = "http://localhost:11434"
	}
	if model == "" {
		model = "nomic-embed-text"
	}
	if dims <= 0 {
		dims = defaultOllamaDims
	}
	return &OllamaProvider{baseURL: baseURL, model: model, dims: dims}
}

func (p *OllamaProvider) Dimensions() int { return p.dims }
func (p *OllamaProvider) Name() string    { return "ollama/" + p.model }
func (p *OllamaProvider) Close() error    { return nil }

// Embed returns an embedding for a single text. Delegates to EmbedBatch.
func (p *OllamaProvider) Embed(ctx context.Context, text string) ([]float32, error) {
	out, err := p.EmbedBatch(ctx, []string{text})
	if err != nil {
		return nil, err
	}
	return out[0], nil
}

// EmbedBatch sends all texts to Ollama's /api/embed endpoint in a single
// HTTP request. This is far more efficient than one call per text, especially
// when Ollama is running on a GPU where the HTTP overhead dominates latency.
func (p *OllamaProvider) EmbedBatch(ctx context.Context, texts []string) ([][]float32, error) {
	payload := struct {
		Model string   `json:"model"`
		Input []string `json:"input"`
	}{
		Model: p.model,
		Input: texts,
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		p.baseURL+"/api/embed", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("ollama: unexpected status %d", resp.StatusCode)
	}

	var result struct {
		Embeddings [][]float32 `json:"embeddings"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, err
	}
	if len(result.Embeddings) != len(texts) {
		return nil, fmt.Errorf("ollama: expected %d embeddings, got %d",
			len(texts), len(result.Embeddings))
	}
	return result.Embeddings, nil
}
