package main

import (
	"fmt"
	"os"

	"github.com/codewandler/axon/indexer/embeddings"
)

// resolveEmbeddingProvider constructs an embedding provider from CLI flags and
// environment variables. This is the single place in the CLI that knows how to
// build providers; it is not part of the embeddings library itself.
//
// Resolution order (highest priority first):
//
//  1. providerFlag  (from --embed-provider CLI flag)
//  2. AXON_EMBED_PROVIDER environment variable
//  3. "ollama" default
//
// Valid provider names: "ollama", "hugot"
func resolveEmbeddingProvider(providerFlag, modelPathFlag string) (embeddings.Provider, error) {
	name := providerFlag
	if name == "" {
		name = os.Getenv("AXON_EMBED_PROVIDER")
	}
	if name == "" {
		name = "ollama"
	}

	switch name {
	case "ollama":
		baseURL := os.Getenv("AXON_OLLAMA_URL")
		model := os.Getenv("AXON_OLLAMA_MODEL")
		return embeddings.NewOllama(baseURL, model, 0), nil

	case "hugot":
		model := os.Getenv("AXON_HUGOT_MODEL")
		path := modelPathFlag
		if path == "" {
			path = os.Getenv("AXON_HUGOT_MODEL_PATH")
		}
		return embeddings.NewHugot(path, model), nil

	default:
		return nil, fmt.Errorf("unknown embedding provider %q — valid values: ollama, hugot", name)
	}
}
