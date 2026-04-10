package embeddings

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
)

// Provider defines the interface for embedding providers.
type Provider interface {
	Embed(ctx context.Context, text string) ([]float32, error)
	Dimensions() int
	Name() string
}

// NullProvider returns zero vectors. Used as default/testing.
type NullProvider struct {
	dims int
}

// NewNull creates a NullProvider with the given dimensions (default 384).
func NewNull(dims int) *NullProvider {
	if dims <= 0 {
		dims = 384
	}
	return &NullProvider{dims: dims}
}

func (p *NullProvider) Embed(ctx context.Context, text string) ([]float32, error) {
	return make([]float32, p.dims), nil
}

func (p *NullProvider) Dimensions() int { return p.dims }
func (p *NullProvider) Name() string    { return "null" }

// OllamaProvider calls the Ollama API for embeddings.
type OllamaProvider struct {
	baseURL string
	model   string
}

// NewOllama creates an OllamaProvider.
func NewOllama(baseURL, model string) *OllamaProvider {
	if baseURL == "" {
		baseURL = "http://localhost:11434"
	}
	if model == "" {
		model = "nomic-embed-text"
	}
	return &OllamaProvider{baseURL: baseURL, model: model}
}

func (p *OllamaProvider) Dimensions() int { return 768 } // nomic-embed-text default
func (p *OllamaProvider) Name() string    { return "ollama/" + p.model }

func (p *OllamaProvider) Embed(ctx context.Context, text string) ([]float32, error) {
	payload := map[string]string{
		"model":  p.model,
		"prompt": text,
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequestWithContext(ctx, "POST", p.baseURL+"/api/embeddings", bytes.NewReader(body))
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
		return nil, fmt.Errorf("ollama API returned %d", resp.StatusCode)
	}

	var result struct {
		Embedding []float32 `json:"embedding"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, err
	}

	return result.Embedding, nil
}
