package embeddings

import "context"

// Provider generates vector embeddings from text.
// Implementations must be safe for concurrent use from multiple goroutines.
//
// Callers must call [Provider.Close] when done to release any held resources
// (sessions, HTTP connections, etc.).
type Provider interface {
	// Embed returns a vector embedding for the given text.
	// It is equivalent to calling EmbedBatch with a single-element slice.
	Embed(ctx context.Context, text string) ([]float32, error)

	// EmbedBatch returns embeddings for a slice of texts in a single call.
	// The returned slice has the same length and order as texts.
	// Implementations should send all inputs to the underlying model in one
	// request/inference pass to maximise hardware utilisation.
	EmbedBatch(ctx context.Context, texts []string) ([][]float32, error)

	// Dimensions returns the length of vectors produced by this provider.
	// All calls to Embed/EmbedBatch on the same provider return vectors of this length.
	Dimensions() int

	// Name returns a human-readable identifier, e.g. "ollama/nomic-embed-text".
	Name() string

	// Close releases any resources held by the provider.
	// It is safe to call Close on an uninitialized or already-closed provider.
	Close() error
}
