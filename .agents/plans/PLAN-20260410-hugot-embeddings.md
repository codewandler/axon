# PLAN: Hugot Embedding Provider + Benchmark

**Design ref**: `DESIGN-20260410-hugot-embeddings.md`  
**Date**: 2026-04-10  
**Estimated total**: ~55 minutes

---

## Architecture note

The `indexer/embeddings` package must be structured so it can be extracted as a standalone library
later with minimal friction. The rule is:

> **Provider files must never import axon-internal packages.**
> Only `indexer.go` is allowed to import `graph`, `indexer`, `types`.

Current problems to fix before adding Hugot:
- `provider.go` mixes the interface AND all implementations in one file — split it.
- `OllamaProvider.Dimensions()` is hardcoded to 768 — wrong for any non-nomic model.
- No `Close() error` on the interface — Hugot holds a session that needs cleanup.
- `resolveEmbeddingProvider()` is duplicated between `index.go` and `search.go`.

---

## Task 1 — Split `provider.go` into one file per concern

**Files modified**: `indexer/embeddings/provider.go`  
**Files created**: `indexer/embeddings/null.go`, `indexer/embeddings/ollama.go`, `indexer/embeddings/doc.go`  
**Estimated time**: 5 minutes

### `indexer/embeddings/doc.go`

```go
// Package embeddings provides the Provider interface and built-in implementations
// for generating vector embeddings from text.
//
// Library boundary
//
// This package is designed to be extractable as a standalone library. All files
// except indexer.go are free of axon-internal dependencies and import only the
// standard library and optional third-party embedding backends.
//
// Provider implementations:
//   - NullProvider  – zero vectors, for testing
//   - OllamaProvider – calls Ollama daemon's HTTP API (local, no data leaves host)
//   - HugotProvider  – runs ONNX models in-process via Hugot (pure Go, no daemon)
package embeddings
```

### `indexer/embeddings/provider.go` (interface only)

Replace entire file content:

```go
package embeddings

import "context"

// Provider generates vector embeddings from text. Implementations must be safe
// for concurrent use from multiple goroutines.
//
// Callers must call Close when done with a provider to release any held resources.
type Provider interface {
	// Embed returns a vector embedding for the given text.
	Embed(ctx context.Context, text string) ([]float32, error)

	// Dimensions returns the length of vectors produced by this provider.
	Dimensions() int

	// Name returns a human-readable identifier, e.g. "ollama/nomic-embed-text".
	Name() string

	// Close releases any resources held by the provider (sessions, connections).
	// It is safe to call Close on an uninitialized or already-closed provider.
	Close() error
}
```

### `indexer/embeddings/null.go`

```go
package embeddings

import "context"

// NullProvider returns zero vectors of a fixed dimension. Use in tests or as
// a no-op placeholder.
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

func (p *NullProvider) Dimensions() int { return p.dims }
func (p *NullProvider) Name() string    { return "null" }
func (p *NullProvider) Close() error    { return nil }
```

### `indexer/embeddings/ollama.go`

Move `OllamaProvider` from `provider.go` into its own file and fix dimensions:

```go
package embeddings

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
)

const defaultOllamaDims = 768 // nomic-embed-text default

// OllamaProvider calls the local Ollama daemon's embedding API.
// Data never leaves localhost.
type OllamaProvider struct {
	baseURL string
	model   string
	dims    int
}

// NewOllama creates an OllamaProvider.
//   - baseURL defaults to "http://localhost:11434" if empty.
//   - model   defaults to "nomic-embed-text" if empty.
//   - dims    defaults to 768 if <= 0 (matches nomic-embed-text).
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

func (p *OllamaProvider) Embed(ctx context.Context, text string) ([]float32, error) {
	payload := map[string]string{
		"model":  p.model,
		"prompt": text,
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		p.baseURL+"/api/embeddings", bytes.NewReader(body))
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
		Embedding []float32 `json:"embedding"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, err
	}
	return result.Embedding, nil
}
```

**Verification**:
```
go build ./indexer/embeddings/
go test ./indexer/embeddings/
```

> **Note**: `NewOllama` signature changes from `(baseURL, model string)` to `(baseURL, model string, dims int)`.
> The only callers are `cmd/axon/index.go` and `cmd/axon/search.go` — both will be updated in Task 6.

---

## Task 2 — Update `provider_test.go` for new interface

**Files modified**: `indexer/embeddings/provider_test.go`  
**Estimated time**: 3 minutes

Replace entire file:

```go
package embeddings

import (
	"context"
	"testing"
)

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
		t.Errorf("Embed len: want 384, got %d", len(vec))
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

// Ensure all providers satisfy the interface at compile time.
var _ Provider = (*NullProvider)(nil)
var _ Provider = (*OllamaProvider)(nil)
```

**Verification**:
```
go test ./indexer/embeddings/
```

---

## Task 3 — Add hugot dependency

**Files modified**: `go.mod`, `go.sum`  
**Estimated time**: 3 minutes

```bash
go get github.com/knights-analytics/hugot@latest
go mod tidy
```

Check that it resolves without errors and that `go.mod` now has:
```
require (
    github.com/knights-analytics/hugot v...
    ...
)
```

**Verification**:
```
go build ./...
```

> If the pure Go (GoMLX) backend introduces large transitive deps, that is expected and acceptable.
> Do not add any `-tags ORT` or `-tags XLA` build flags.

---

## Task 4 — Implement `HugotProvider`

**Files created**: `indexer/embeddings/hugot.go`  
**Estimated time**: 10 minutes

The hugot API (confirmed from source):
- `hugot.NewGoSession() (*hugot.Session, error)` — creates pure-Go session, no CGO
- `hugot.DownloadModel(modelName, destDir string, opts hugot.DownloadOptions) (string, error)`
- `hugot.NewPipeline[T](session, config) (T, error)` — generic; returns concrete pipeline type
- `hugot.FeatureExtractionConfig{ModelPath, Name string}` — config type alias
- `(*pipelines.FeatureExtractionPipeline).RunPipeline([]string) (*pipelines.FeatureExtractionOutput, error)`
- `FeatureExtractionOutput.Embeddings [][]float32` — one row per input, already mean-pooled

```go
package embeddings

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/knights-analytics/hugot"
	"github.com/knights-analytics/hugot/pipelines"
)

const (
	// DefaultHugotModel is the HuggingFace repo slug for the default ONNX model.
	// KnightsAnalytics maintain a pre-exported ONNX version of all-MiniLM-L6-v2.
	DefaultHugotModel = "KnightsAnalytics/all-MiniLM-L6-v2"

	// DefaultHugotDims is the embedding dimension of the default model.
	DefaultHugotDims = 384
)

// HugotProvider runs ONNX sentence-embedding models fully in-process using the
// Hugot pure-Go backend (GoMLX). No daemon, no CGO, no data leaves the host.
//
// The provider is lazy: the session and pipeline are created on the first call
// to Embed. Model files are downloaded from HuggingFace on first use if not
// already present at modelPath.
//
// Thread-safe: multiple goroutines may call Embed concurrently.
type HugotProvider struct {
	modelPath string // local directory containing model.onnx + tokenizer.json
	modelName string // HuggingFace repo slug (e.g. "KnightsAnalytics/all-MiniLM-L6-v2")
	dims      int

	once     sync.Once
	session  *hugot.Session
	pipeline *pipelines.FeatureExtractionPipeline
	initErr  error
}

// NewHugot creates a HugotProvider.
//
//   - modelPath: local directory containing the ONNX model files.
//     If empty, defaults to ~/.axon/models/<model-slug>.
//     Created automatically and model downloaded on first Embed call if absent.
//
//   - model: HuggingFace repo slug (e.g. "KnightsAnalytics/all-MiniLM-L6-v2").
//     Defaults to DefaultHugotModel if empty.
func NewHugot(modelPath, model string) *HugotProvider {
	if model == "" {
		model = DefaultHugotModel
	}
	if modelPath == "" {
		home, _ := os.UserHomeDir()
		// "Org/Name" → "Org-Name" to avoid directory separator issues
		slug := strings.ReplaceAll(filepath.Base(model), "/", "-")
		modelPath = filepath.Join(home, ".axon", "models", slug)
	}
	return &HugotProvider{
		modelPath: modelPath,
		modelName: model,
		dims:      DefaultHugotDims,
	}
}

func (p *HugotProvider) Name() string    { return "hugot/" + p.modelName }
func (p *HugotProvider) Dimensions() int { return p.dims }

// Close destroys the underlying Hugot session and releases all resources.
// Safe to call on an uninitialized or already-closed provider.
func (p *HugotProvider) Close() error {
	if p.session != nil {
		return p.session.Destroy()
	}
	return nil
}

// initialize creates the Hugot session and feature-extraction pipeline.
// Called exactly once via sync.Once. Subsequent calls return p.initErr.
func (p *HugotProvider) initialize() {
	p.once.Do(func() {
		// Download model if model.onnx is not present
		onnxPath := filepath.Join(p.modelPath, "model.onnx")
		if _, err := os.Stat(onnxPath); os.IsNotExist(err) {
			fmt.Printf("hugot: downloading model %q to %s (one-time, ~90 MB)…\n",
				p.modelName, p.modelPath)
			destDir := filepath.Dir(p.modelPath)
			if err := os.MkdirAll(destDir, 0o755); err != nil {
				p.initErr = fmt.Errorf("hugot: mkdir %s: %w", destDir, err)
				return
			}
			if _, err := hugot.DownloadModel(p.modelName, destDir,
				hugot.NewDownloadOptions()); err != nil {
				p.initErr = fmt.Errorf("hugot: download %q: %w", p.modelName, err)
				return
			}
		}

		session, err := hugot.NewGoSession()
		if err != nil {
			p.initErr = fmt.Errorf("hugot: new session: %w", err)
			return
		}

		pipe, err := hugot.NewPipeline(session, hugot.FeatureExtractionConfig{
			ModelPath: p.modelPath,
			Name:      "axon-embed",
		})
		if err != nil {
			_ = session.Destroy()
			p.initErr = fmt.Errorf("hugot: new pipeline: %w", err)
			return
		}

		p.session = session
		p.pipeline = pipe
	})
}

// Embed generates an embedding for text. On the first call the model is loaded
// (and downloaded if necessary). Subsequent calls reuse the loaded session.
func (p *HugotProvider) Embed(_ context.Context, text string) ([]float32, error) {
	p.initialize()
	if p.initErr != nil {
		return nil, p.initErr
	}

	out, err := p.pipeline.RunPipeline([]string{text})
	if err != nil {
		return nil, fmt.Errorf("hugot: run pipeline: %w", err)
	}
	if len(out.Embeddings) == 0 {
		return nil, fmt.Errorf("hugot: empty embedding output")
	}
	return out.Embeddings[0], nil
}
```

**Verification**:
```
go build ./indexer/embeddings/
```

---

## Task 5 — Write `HugotProvider` unit tests

**Files created**: `indexer/embeddings/hugot_test.go`  
**Estimated time**: 5 minutes

```go
package embeddings

import (
	"context"
	"os"
	"testing"
)

// Compile-time interface check.
var _ Provider = (*HugotProvider)(nil)

func TestHugotProviderDefaults(t *testing.T) {
	p := NewHugot("", "")

	if p.Name() != "hugot/"+DefaultHugotModel {
		t.Errorf("Name: got %q, want %q", p.Name(), "hugot/"+DefaultHugotModel)
	}
	if p.Dimensions() != DefaultHugotDims {
		t.Errorf("Dimensions: got %d, want %d", p.Dimensions(), DefaultHugotDims)
	}

	// Close on uninitialized provider must be safe
	if err := p.Close(); err != nil {
		t.Errorf("Close on uninitialized: %v", err)
	}
}

func TestHugotProviderCustomModel(t *testing.T) {
	p := NewHugot("/tmp/mymodels/foo", "MyOrg/my-model")
	if p.modelPath != "/tmp/mymodels/foo" {
		t.Errorf("modelPath: got %q", p.modelPath)
	}
	if p.modelName != "MyOrg/my-model" {
		t.Errorf("modelName: got %q", p.modelName)
	}
}

// TestHugotProviderEmbed runs a real embedding. Skipped unless
// AXON_TEST_HUGOT=1 is set (downloads ~90MB model on first run).
func TestHugotProviderEmbed(t *testing.T) {
	if os.Getenv("AXON_TEST_HUGOT") == "" {
		t.Skip("set AXON_TEST_HUGOT=1 to run (downloads model on first run ~90MB)")
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

	// Sanity: at least some non-zero values
	nonZero := 0
	for _, v := range vec {
		if v != 0 {
			nonZero++
		}
	}
	if nonZero == 0 {
		t.Error("embedding is all zeros — model did not produce output")
	}
}
```

**Verification**:
```
go test ./indexer/embeddings/
```

All tests should pass. `TestHugotProviderEmbed` will be skipped.

---

## Task 6 — Create `cmd/axon/embed.go` (shared provider factory)

**Files created**: `cmd/axon/embed.go`  
**Estimated time**: 5 minutes

This is the single place in the CLI that knows how to construct providers from flags/env vars.
It is **not** part of the embeddings library — it is CLI wiring only.

```go
package main

import (
	"fmt"
	"os"

	"github.com/codewandler/axon/indexer/embeddings"
)

// resolveEmbeddingProvider constructs an embedding provider from flags and
// environment variables. providerFlag and modelPathFlag come from CLI flags;
// they override the corresponding env vars when set.
//
// Priority (highest to lowest):
//   CLI flag  >  AXON_EMBED_PROVIDER env var  >  "ollama" default
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
		// dims: default 768 for nomic-embed-text; override via AXON_OLLAMA_DIMS (not yet exposed)
		return embeddings.NewOllama(baseURL, model, 0), nil

	case "hugot":
		model := os.Getenv("AXON_HUGOT_MODEL")
		path := modelPathFlag
		if path == "" {
			path = os.Getenv("AXON_HUGOT_MODEL_PATH")
		}
		return embeddings.NewHugot(path, model), nil

	default:
		return nil, fmt.Errorf(
			"unknown embedding provider %q — valid values: ollama, hugot", name)
	}
}
```

**Verification**:
```
go build ./cmd/axon/
```

---

## Task 7 — Update `cmd/axon/index.go`

**Files modified**: `cmd/axon/index.go`  
**Estimated time**: 5 minutes

### 7a — Add new flags to the `var` block and `init()`

Add two new flag variables alongside the existing ones:

```go
var (
	flagNoGC           bool
	flagEmbed          bool
	flagEmbedProvider  string  // NEW
	flagEmbedModelPath string  // NEW
	flagWatch          bool
	flagWatchDebounce  time.Duration
	flagWatchQuiet     bool
)
```

In `init()`, register the new flags:

```go
indexCmd.Flags().StringVar(&flagEmbedProvider, "embed-provider", "",
	"Embedding provider: ollama|hugot (overrides AXON_EMBED_PROVIDER env var)")
indexCmd.Flags().StringVar(&flagEmbedModelPath, "embed-model-path", "",
	"Local model directory for hugot provider (default: ~/.axon/models/<model>)")
```

Update the existing `--embed` flag help text:

```go
indexCmd.Flags().BoolVar(&flagEmbed, "embed", false,
	"Generate embeddings after indexing (use --embed-provider to choose ollama or hugot)")
```

### 7b — Replace the provider switch in `runIndex`

Remove the old inline switch (lines 101–115 in the current file):

```go
// OLD — remove this block:
if flagEmbed {
    providerName := os.Getenv("AXON_EMBED_PROVIDER")
    if providerName == "" {
        providerName = "ollama"
    }
    switch providerName {
    case "ollama":
        baseURL := os.Getenv("AXON_OLLAMA_URL")
        model := os.Getenv("AXON_OLLAMA_MODEL")
        axCfg.EmbeddingProvider = embeddings.NewOllama(baseURL, model)
        fmt.Printf("Embedding provider: %s\n", axCfg.EmbeddingProvider.Name())
    default:
        return fmt.Errorf("unknown embedding provider %q (set AXON_EMBED_PROVIDER=ollama)", providerName)
    }
}
```

Replace with:

```go
if flagEmbed {
    provider, err := resolveEmbeddingProvider(flagEmbedProvider, flagEmbedModelPath)
    if err != nil {
        return err
    }
    axCfg.EmbeddingProvider = provider
    fmt.Printf("Embedding provider: %s\n", provider.Name())
}
```

Also remove the `embeddings` import since it is no longer used directly in this file.

**Verification**:
```
go build ./cmd/axon/
./bin/axon index --help   # confirm --embed-provider and --embed-model-path appear
```

---

## Task 8 — Update `cmd/axon/search.go`

**Files modified**: `cmd/axon/search.go`  
**Estimated time**: 3 minutes

### 8a — Remove the duplicate `resolveEmbeddingProvider` function

Delete the entire `resolveEmbeddingProvider` function (lines 231–244 in the current file):

```go
// DELETE this function — it now lives in embed.go
func resolveEmbeddingProvider() (embeddings.Provider, error) {
	providerName := os.Getenv("AXON_EMBED_PROVIDER")
	if providerName == "" {
		providerName = "ollama"
	}
	switch providerName {
	case "ollama":
		baseURL := os.Getenv("AXON_OLLAMA_URL")
		model := os.Getenv("AXON_OLLAMA_MODEL")
		return embeddings.NewOllama(baseURL, model), nil
	default:
		return nil, fmt.Errorf("unknown embedding provider %q (set AXON_EMBED_PROVIDER=ollama)", providerName)
	}
}
```

### 8b — Update the call site in `runSemanticSearch`

The call on line 177 currently passes no arguments:
```go
provider, err := resolveEmbeddingProvider()
```

Update to pass empty strings for the flags (search command has no provider flags — env vars only):
```go
provider, err := resolveEmbeddingProvider("", "")
```

### 8c — Remove the now-unused `embeddings` import if no longer needed

Check whether `embeddings` is still imported elsewhere in `search.go`. If not, remove it from the import block.

**Verification**:
```
go build ./cmd/axon/
```

---

## Task 9 — Write the benchmark

**Files created**: `indexer/embeddings/bench_test.go`  
**Estimated time**: 5 minutes

```go
package embeddings_test

import (
	"context"
	"os"
	"testing"

	"github.com/codewandler/axon/indexer/embeddings"
)

// benchCorpus is a representative set of texts drawn from axon's own codebase.
// Realistic for axon's use case: short symbol descriptions and doc comments.
var benchCorpus = []string{
	"Storage interface for the axon graph database",
	"PutNode upserts a node into the storage backend",
	"FindSimilar performs cosine similarity search over stored embeddings",
	"IndexWithProgress runs all indexers and emits structured progress events",
	"AQL parser converts query strings into an abstract syntax tree",
	"Edge represents a directed relationship between two nodes",
	"Generation string used to identify stale nodes after a re-index run",
	"DeleteStaleByURIPrefix removes nodes whose URI prefix matches and generation differs",
	"FeatureExtractionPipeline produces mean-pooled sentence embeddings from ONNX models",
	"Indexer interface defines Name, Schemes, Handles, Subscriptions, and Index methods",
	"EmitNode writes a node to the graph via the current generation emitter",
	"PostIndexer runs after all primary indexers complete, used for embedding generation",
	"WatchOptions configures debounce window and re-index callback for watch mode",
	"SQLite adapter buffers writes in 5000-item batches flushed every 100ms",
	"go:func node type represents a function definition in a Go source file",
	"md:section node type represents a heading-delimited section in a Markdown file",
	"vcs:repo node type represents the root of a git repository",
	"contains edge type expresses structural containment between parent and child nodes",
	"resolveDB walks parent directories looking for a .axon/graph.db database file",
	"buildNodeText combines name, type, labels, doc and signature into an embeddable string",
}

// BenchmarkOllama measures embedding throughput via the local Ollama daemon.
// Requires Ollama to be running with nomic-embed-text pulled.
// Enable with: AXON_BENCH_OLLAMA=1 go test -bench=BenchmarkOllama -benchtime=30s ./indexer/embeddings/
func BenchmarkOllama(b *testing.B) {
	if os.Getenv("AXON_BENCH_OLLAMA") == "" {
		b.Skip("set AXON_BENCH_OLLAMA=1 to run (requires Ollama daemon with nomic-embed-text)")
	}
	p := embeddings.NewOllama("", "", 0)
	defer p.Close()
	ctx := context.Background()

	// Warm up
	if _, err := p.Embed(ctx, benchCorpus[0]); err != nil {
		b.Fatalf("warmup: %v", err)
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		text := benchCorpus[i%len(benchCorpus)]
		if _, err := p.Embed(ctx, text); err != nil {
			b.Fatalf("Embed: %v", err)
		}
	}
}

// BenchmarkHugot measures embedding throughput via the in-process Hugot pure-Go backend.
// Downloads ~90MB model on first run; cached at ~/.axon/models/all-MiniLM-L6-v2.
// Enable with: AXON_BENCH_HUGOT=1 go test -bench=BenchmarkHugot -benchtime=30s ./indexer/embeddings/
func BenchmarkHugot(b *testing.B) {
	if os.Getenv("AXON_BENCH_HUGOT") == "" {
		b.Skip("set AXON_BENCH_HUGOT=1 to run (downloads ~90MB model on first run)")
	}
	p := embeddings.NewHugot("", "")
	defer p.Close()
	ctx := context.Background()

	// Warm up: first call triggers session + model load
	b.Log("loading model (may take a few seconds on first run)...")
	if _, err := p.Embed(ctx, benchCorpus[0]); err != nil {
		b.Fatalf("warmup: %v", err)
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		text := benchCorpus[i%len(benchCorpus)]
		if _, err := p.Embed(ctx, text); err != nil {
			b.Fatalf("Embed: %v", err)
		}
	}
}
```

**Verification**:
```
# Must skip cleanly with no errors:
go test -bench=. -run=^$ ./indexer/embeddings/

# Expected output:
# BenchmarkOllama-N   --- SKIP: set AXON_BENCH_OLLAMA=1 ...
# BenchmarkHugot-N    --- SKIP: set AXON_BENCH_HUGOT=1 ...
```

---

## Task 10 — Final build and full test suite

**Estimated time**: 3 minutes

```bash
# Full build — must pass with zero errors, no CGO required
go build ./...

# Full test suite — all tests must pass
go test ./...

# Benchmarks skip cleanly (no env vars set)
go test -bench=. -run=^$ ./indexer/embeddings/

# Check CLI help shows new flags
go run ./cmd/axon index --help
```

Expected `index --help` output includes:
```
  --embed-provider string     Embedding provider: ollama|hugot (overrides AXON_EMBED_PROVIDER env var)
  --embed-model-path string   Local model directory for hugot provider (default: ~/.axon/models/<model>)
```

---

## Task 11 — Update README

**Files modified**: `README.md`  
**Estimated time**: 5 minutes

Locate the existing embedding section in README.md (search for "embed" or "Ollama").
Add a new subsection below the Ollama instructions:

```markdown
### Hugot (in-process, no daemon required)

The `hugot` provider runs a sentence-embedding ONNX model fully inside the axon process.
No external service needed. Data never leaves the machine.

```bash
# Index with Hugot (downloads ~90MB model on first run, cached at ~/.axon/models/)
axon index --embed --embed-provider=hugot .

# Or via environment variable
AXON_EMBED_PROVIDER=hugot axon index --embed .

# Use a custom model directory
axon index --embed --embed-provider=hugot --embed-model-path=/data/models/MiniLM .

# Semantic search (use the same provider that was used to index)
AXON_EMBED_PROVIDER=hugot axon search --semantic "storage interface"
```

Default model: `KnightsAnalytics/all-MiniLM-L6-v2` (384 dimensions, ~90 MB, downloaded once).

| | Hugot | Ollama |
|---|---|---|
| External daemon | ❌ none | ✅ required |
| CGO | ❌ none | ❌ none |
| First-run download | ~90 MB model file | model via `ollama pull` |
| Throughput | ~0.4 ms/embed (CPU) | ~10 ms/embed (HTTP + daemon) |
| Best for | offline / CI / Docker | existing Ollama users |
```

---

## Summary of all changes

| File | Action |
|---|---|
| `indexer/embeddings/doc.go` | Create — package documentation + library boundary note |
| `indexer/embeddings/provider.go` | Rewrite — interface only, adds `Close() error` |
| `indexer/embeddings/null.go` | Create — `NullProvider` (was in `provider.go`) |
| `indexer/embeddings/ollama.go` | Create — `OllamaProvider` with configurable dims (was in `provider.go`) |
| `indexer/embeddings/hugot.go` | Create — `HugotProvider`, lazy init, sync.Once, model download |
| `indexer/embeddings/provider_test.go` | Update — cover `Close()`, compile-time interface checks |
| `indexer/embeddings/hugot_test.go` | Create — unit tests for `HugotProvider` |
| `indexer/embeddings/bench_test.go` | Create — `BenchmarkOllama` + `BenchmarkHugot` |
| `cmd/axon/embed.go` | Create — shared `resolveEmbeddingProvider(flag, modelPath)` |
| `cmd/axon/index.go` | Modify — new flags, use shared factory, remove duplicate switch |
| `cmd/axon/search.go` | Modify — remove duplicate factory fn, update call site |
| `go.mod` / `go.sum` | Modify — add `github.com/knights-analytics/hugot` |
| `README.md` | Modify — document Hugot provider usage |
