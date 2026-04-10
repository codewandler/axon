# DESIGN: Hugot Embedding Provider + Ollama/Hugot Benchmark

**Date**: 2026-04-10  
**Status**: Approved — ready to plan  

---

## Problem

Axon currently supports exactly one embedding provider: **Ollama**. Ollama requires a running daemon
(`ollama serve`) that the user must install and keep alive separately from axon. This is a hard
dependency for any user who wants semantic search.

Our requirement is that all data stays on `localhost`. That rules out all cloud/API providers.

---

## Goal

1. Add **Hugot** as a second embedding provider — fully in-process, no daemon, no CGO (pure Go
   backend), no data leaves the machine.
2. Add a **benchmark** (`go test -bench`) that measures and compares embedding throughput between
   Ollama and Hugot on a representative corpus, so we can make informed decisions about which
   provider to recommend.

---

## Scope

### In-scope
- `HugotProvider` implementing the existing `embeddings.Provider` interface
- Model download helper (`hugot.DownloadModel`) wired to a configurable local path
- CLI: `--embed-provider` flag on `axon index`; `AXON_EMBED_PROVIDER=hugot` env var
- CLI: `--embed-model-path` flag + `AXON_HUGOT_MODEL_PATH` env var for custom model location
- `resolveEmbeddingProvider()` updated in `cmd/axon/search.go`
- Benchmark file: `indexer/embeddings/bench_test.go`

### Out-of-scope
- Hugot ORT or XLA backends (CGO required — defer to future work)
- GPU support
- Model fine-tuning
- Any cloud/remote embedding provider

---

## Background: Hugot Pure Go Backend

`github.com/knights-analytics/hugot` (★590, active April 2026) runs HuggingFace ONNX transformer
models in-process via three pluggable backends:

| Backend | Build tag | CGO? | External dep | Notes |
|---|---|---|---|---|
| **Pure Go** (GoMLX) | *(default)* | ❌ | none | Our target. ≤32 inputs/call. |
| ORT | `-tags ORT` | ✅ | `libonnxruntime.so` | Fastest; deferred |
| XLA | `-tags XLA` | ✅ | go-xla | GPU/TPU; deferred |

**Default model**: `KnightsAnalytics/all-MiniLM-L6-v2` — a HuggingFace-hosted ONNX export of the
popular `sentence-transformers/all-MiniLM-L6-v2`. Produces 384-dimensional embeddings. ~90 MB.

Hugot's pure Go backend is designed for exactly this use case: CLI tools and embedded applications
where CGO / shared libs are not acceptable. Works on Linux, macOS, Windows.

---

## Design

### 1. `HugotProvider` (`indexer/embeddings/hugot.go`)

New file implementing `Provider`. No build tags — uses the default pure Go backend.

```go
package embeddings

import (
    "context"
    "fmt"
    "os"
    "path/filepath"
    "sync"

    "github.com/knights-analytics/hugot"
)

const (
    DefaultHugotModel     = "KnightsAnalytics/all-MiniLM-L6-v2"
    DefaultHugotModelPath = "" // resolved at runtime to ~/.axon/models
    HugotDimensions       = 384
)

// HugotProvider runs ONNX sentence-embedding models fully in-process using
// the Hugot pure Go backend (GoMLX). No daemon, no CGO, no data leaves the host.
type HugotProvider struct {
    modelPath string  // local directory containing model.onnx + tokenizer.json
    modelName string  // HuggingFace repo slug (for display / download)

    once     sync.Once
    session  *hugot.Session
    pipeline hugot.Pipeline // FeatureExtractionPipeline
    initErr  error
}

// NewHugot creates a HugotProvider.
//
//   - modelPath: local directory with the ONNX model files. If empty, defaults to
//     ~/.axon/models/<model>. The directory is created and the model downloaded from
//     HuggingFace on first use if it does not exist.
//   - model: HuggingFace repo slug, e.g. "KnightsAnalytics/all-MiniLM-L6-v2".
//     Defaults to DefaultHugotModel if empty.
func NewHugot(modelPath, model string) *HugotProvider {
    if model == "" {
        model = DefaultHugotModel
    }
    if modelPath == "" {
        home, _ := os.UserHomeDir()
        // slug "Org/Name" → directory "~/.axon/models/Org-Name"
        safe := filepath.Base(model)
        modelPath = filepath.Join(home, ".axon", "models", safe)
    }
    return &HugotProvider{modelPath: modelPath, modelName: model}
}

func (p *HugotProvider) Name() string    { return "hugot/" + p.modelName }
func (p *HugotProvider) Dimensions() int { return HugotDimensions }

// init lazily starts the Hugot session and loads the pipeline on first call.
// Thread-safe: uses sync.Once.
func (p *HugotProvider) init() error {
    p.once.Do(func() {
        // Download model if not already present
        if _, err := os.Stat(filepath.Join(p.modelPath, "model.onnx")); os.IsNotExist(err) {
            if _, err := hugot.DownloadModel(p.modelName, filepath.Dir(p.modelPath),
                hugot.NewDownloadOptions()); err != nil {
                p.initErr = fmt.Errorf("hugot: download model %q: %w", p.modelName, err)
                return
            }
        }

        session, err := hugot.NewGoSession()
        if err != nil {
            p.initErr = fmt.Errorf("hugot: create session: %w", err)
            return
        }

        pipe, err := hugot.NewPipeline(session, hugot.FeatureExtractionConfig{
            ModelPath: p.modelPath,
            Name:      "axon-embed",
        })
        if err != nil {
            _ = session.Destroy()
            p.initErr = fmt.Errorf("hugot: create pipeline: %w", err)
            return
        }

        p.session = session
        p.pipeline = pipe
    })
    return p.initErr
}

func (p *HugotProvider) Embed(ctx context.Context, text string) ([]float32, error) {
    if err := p.init(); err != nil {
        return nil, err
    }
    result, err := p.pipeline.RunPipeline([]string{text})
    if err != nil {
        return nil, fmt.Errorf("hugot embed: %w", err)
    }
    // RunPipeline returns []FeatureExtractionOutput; each has Embeddings [][]float32
    // We take the first (and only) output, mean-pooled embedding
    outputs := result.Embeddings // [][]float32 — one per token; hugot does mean-pooling
    if len(outputs) == 0 {
        return nil, fmt.Errorf("hugot embed: empty output")
    }
    return outputs[0], nil
}

// Close releases the underlying Hugot session. Call when done with the provider.
func (p *HugotProvider) Close() error {
    if p.session != nil {
        return p.session.Destroy()
    }
    return nil
}
```

> **Note on `RunPipeline` output**: The `FeatureExtractionPipeline` in Hugot returns
> `FeatureExtractionOutput` which carries the mean-pooled sentence embedding as `Embeddings[0]`
> when batch size is 1. The exact field shape must be verified against the hugot API when
> implementing — the sketch above may need minor adjustment.

---

### 2. CLI integration (`cmd/axon/index.go`)

**New flag**: `--embed-provider` (string, default `"ollama"`)  
**New flag**: `--embed-model-path` (string, default `""` → resolved by `NewHugot`)

```go
// New flags
var (
    flagEmbedProvider  string
    flagEmbedModelPath string
)

func init() {
    // existing flags ...
    indexCmd.Flags().StringVar(&flagEmbedProvider, "embed-provider", "",
        "Embedding provider: ollama|hugot (default: $AXON_EMBED_PROVIDER or ollama)")
    indexCmd.Flags().StringVar(&flagEmbedModelPath, "embed-model-path", "",
        "Local model directory for hugot provider (default: ~/.axon/models/<model>)")
}
```

**Updated provider resolution** (extracted into shared `resolveEmbeddingProvider()` in a new
`cmd/axon/embed.go` so both `index.go` and `search.go` use the same logic):

```go
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
        return embeddings.NewOllama(baseURL, model), nil

    case "hugot":
        model := os.Getenv("AXON_HUGOT_MODEL")  // e.g. "KnightsAnalytics/all-MiniLM-L6-v2"
        path := modelPathFlag
        if path == "" {
            path = os.Getenv("AXON_HUGOT_MODEL_PATH")
        }
        return embeddings.NewHugot(path, model), nil

    default:
        return nil, fmt.Errorf("unknown embedding provider %q (valid: ollama, hugot)", name)
    }
}
```

Both `index.go` and `search.go` call this helper. The duplicate switch in `search.go` is removed.

---

### 3. Benchmark (`indexer/embeddings/bench_test.go`)

A standard Go benchmark file. Both providers are benchmarked against the same fixed corpus of
realistic texts (code symbol descriptions similar to what axon actually embeds).

```
go test -bench=. -benchtime=30s ./indexer/embeddings/
```

**Corpus**: 20 representative strings — function names + doc comments — extracted from axon itself.

**Guards**:
- Ollama benchmark: skipped if `AXON_BENCH_OLLAMA=1` is not set (avoids CI failures when Ollama
  is not running)
- Hugot benchmark: skipped if `AXON_BENCH_HUGOT=1` is not set (model download can be slow on
  first run, so opt-in)

```go
package embeddings_test

import (
    "context"
    "os"
    "testing"
    "github.com/codewandler/axon/indexer/embeddings"
)

var benchCorpus = []string{
    "Storage interface for the axon graph database",
    "PutNode upserts a node into the graph",
    "FindSimilar performs cosine similarity search over stored embeddings",
    "IndexWithProgress runs the indexer and emits progress events",
    "AQL parser converts query strings into an abstract syntax tree",
    // ... (15 more representative strings)
}

func BenchmarkOllama(b *testing.B) {
    if os.Getenv("AXON_BENCH_OLLAMA") == "" {
        b.Skip("set AXON_BENCH_OLLAMA=1 to run (requires Ollama daemon)")
    }
    p := embeddings.NewOllama("", "")
    ctx := context.Background()
    corpus := benchCorpus
    b.ResetTimer()
    for i := 0; i < b.N; i++ {
        text := corpus[i%len(corpus)]
        if _, err := p.Embed(ctx, text); err != nil {
            b.Fatal(err)
        }
    }
}

func BenchmarkHugot(b *testing.B) {
    if os.Getenv("AXON_BENCH_HUGOT") == "" {
        b.Skip("set AXON_BENCH_HUGOT=1 to run (downloads model on first run ~90MB)")
    }
    p := embeddings.NewHugot("", "")
    defer p.Close()
    ctx := context.Background()
    // Warm up (first call triggers model load)
    if _, err := p.Embed(ctx, "warmup"); err != nil {
        b.Fatal(err)
    }
    corpus := benchCorpus
    b.ResetTimer()
    for i := 0; i < b.N; i++ {
        text := corpus[i%len(corpus)]
        if _, err := p.Embed(ctx, text); err != nil {
            b.Fatal(err)
        }
    }
}
```

**Expected output** (illustrative):

```
BenchmarkOllama-8    120    9_800_000 ns/op    (≈9.8ms/embed — network + model inference in daemon)
BenchmarkHugot-8    2500      400_000 ns/op    (≈0.4ms/embed — in-process, no network)
```

---

## Files Changed

| File | Action | Description |
|---|---|---|
| `indexer/embeddings/hugot.go` | **Create** | `HugotProvider` implementation |
| `indexer/embeddings/bench_test.go` | **Create** | Ollama + Hugot benchmarks |
| `cmd/axon/embed.go` | **Create** | Shared `resolveEmbeddingProvider()` helper |
| `cmd/axon/index.go` | **Modify** | Add `--embed-provider`, `--embed-model-path` flags; use shared helper |
| `cmd/axon/search.go` | **Modify** | Remove duplicate switch; use shared helper |
| `go.mod` / `go.sum` | **Modify** | Add `github.com/knights-analytics/hugot` dependency |

---

## Configuration Reference

| Env var | Default | Description |
|---|---|---|
| `AXON_EMBED_PROVIDER` | `ollama` | `ollama` or `hugot` |
| `AXON_OLLAMA_URL` | `http://localhost:11434` | Ollama base URL |
| `AXON_OLLAMA_MODEL` | `nomic-embed-text` | Ollama model name |
| `AXON_HUGOT_MODEL` | `KnightsAnalytics/all-MiniLM-L6-v2` | HuggingFace repo slug |
| `AXON_HUGOT_MODEL_PATH` | `~/.axon/models/<model>` | Local model directory |

CLI flags override env vars for `--embed-provider` and `--embed-model-path`.

---

## Key Risks & Mitigations

| Risk | Mitigation |
|---|---|
| Hugot `FeatureExtractionOutput` field shape differs from sketch | Verify against hugot source / `hugot_test.go` before implementing |
| Model download on first use is slow (~90MB) | Print `"Downloading model…"` message; download is one-time and cached |
| `sync.Once` hides init errors from second call | Store and return `p.initErr` on every call after once |
| Hugot pure Go backend is slower than ORT | Document clearly; add ORT support as follow-up with build tag |
| `go.mod` pulls in large transitive deps from hugot | Audit with `go mod graph`; acceptable given they are compile-time only |

---

## Acceptance Criteria

- [ ] `go build ./...` passes with no CGO required
- [ ] `go test ./indexer/embeddings/` passes (unit tests, no benchmark)
- [ ] `AXON_EMBED_PROVIDER=hugot axon index --embed .` produces embeddings in the DB
- [ ] `AXON_EMBED_PROVIDER=hugot axon search --semantic "storage interface"` returns results
- [ ] `AXON_BENCH_HUGOT=1 go test -bench=BenchmarkHugot -benchtime=30s ./indexer/embeddings/` runs and reports ns/op
- [ ] `AXON_BENCH_OLLAMA=1 go test -bench=BenchmarkOllama -benchtime=30s ./indexer/embeddings/` runs and reports ns/op (requires Ollama running)
- [ ] Both benchmarks skip cleanly without the env var set (no failures in CI)
- [ ] README updated with Hugot provider docs
