package embeddings_test

import (
	"context"
	"os"
	"testing"

	"github.com/codewandler/axon/indexer/embeddings"
)

// benchCorpus is a representative set of texts drawn from axon's own codebase —
// function names, interface descriptions, doc comments — matching real embed workloads.
var benchCorpus = []string{
	"Storage interface for the axon graph database",
	"PutNode upserts a node into the storage backend",
	"FindSimilar performs cosine similarity search over stored embeddings",
	"IndexWithProgress runs all indexers and emits structured progress events",
	"AQL parser converts query strings into an abstract syntax tree",
	"Edge represents a directed relationship between two nodes in the graph",
	"Generation string used to identify and clean up stale nodes after re-index",
	"DeleteStaleByURIPrefix removes nodes whose URI prefix matches but generation differs",
	"FeatureExtractionPipeline produces mean-pooled sentence embeddings from ONNX models",
	"Indexer interface defines Name, Schemes, Handles, Subscriptions, and Index methods",
	"EmitNode writes a node to the graph via the current generation emitter",
	"PostIndexer runs after all primary indexers complete, used for embedding generation",
	"WatchOptions configures the debounce window and re-index callback for watch mode",
	"SQLite adapter buffers writes in 5000-item batches flushed every 100 milliseconds",
	"go:func node type represents a function definition in a Go source file",
	"md:section node type represents a heading-delimited section in a Markdown file",
	"vcs:repo node type represents the root of a git repository",
	"contains edge type expresses structural containment between parent and child nodes",
	"resolveDB walks parent directories looking for a .axon/graph.db database file",
	"buildNodeText combines name, type, labels, doc and signature for embedding input",
}

// BenchmarkOllama measures per-embedding throughput via the local Ollama daemon
// using single-item calls (one HTTP request per embed).
//
// Run with:
//
//	AXON_BENCH_OLLAMA=1 go test -bench=BenchmarkOllama -benchtime=30s ./indexer/embeddings/
func BenchmarkOllama(b *testing.B) {
	if os.Getenv("AXON_BENCH_OLLAMA") == "" {
		b.Skip("set AXON_BENCH_OLLAMA=1 to run (requires Ollama daemon with nomic-embed-text)")
	}

	p := embeddings.NewOllama("", "", 0)
	defer p.Close()
	ctx := context.Background()

	// Warm up: establish HTTP connection, ensure model is loaded in daemon
	if _, err := p.Embed(ctx, benchCorpus[0]); err != nil {
		b.Fatalf("warmup: %v", err)
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := p.Embed(ctx, benchCorpus[i%len(benchCorpus)]); err != nil {
			b.Fatalf("Embed: %v", err)
		}
	}
}

// BenchmarkOllamaBatch measures throughput when sending the full corpus in a
// single /api/embed request. Shows the gain from eliminating per-call HTTP overhead.
//
// Run with:
//
//	AXON_BENCH_OLLAMA=1 go test -bench=BenchmarkOllamaBatch -benchtime=30s ./indexer/embeddings/
func BenchmarkOllamaBatch(b *testing.B) {
	if os.Getenv("AXON_BENCH_OLLAMA") == "" {
		b.Skip("set AXON_BENCH_OLLAMA=1 to run (requires Ollama daemon with nomic-embed-text)")
	}

	p := embeddings.NewOllama("", "", 0)
	defer p.Close()
	ctx := context.Background()

	if _, err := p.EmbedBatch(ctx, benchCorpus[:1]); err != nil {
		b.Fatalf("warmup: %v", err)
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := p.EmbedBatch(ctx, benchCorpus); err != nil {
			b.Fatalf("EmbedBatch: %v", err)
		}
	}
	b.ReportMetric(float64(len(benchCorpus)), "texts/op")
}

// BenchmarkHugot measures per-embedding throughput via the in-process Hugot
// pure-Go backend using single-item calls.
//
// Run with:
//
//	AXON_BENCH_HUGOT=1 go test -bench=BenchmarkHugot -benchtime=30s ./indexer/embeddings/
func BenchmarkHugot(b *testing.B) {
	if os.Getenv("AXON_BENCH_HUGOT") == "" {
		b.Skip("set AXON_BENCH_HUGOT=1 to run (downloads ~90 MB model on first run)")
	}

	p := embeddings.NewHugot("", "")
	defer p.Close()
	ctx := context.Background()

	// Warm up: first call triggers session creation + model load (can take a few seconds)
	b.Log("loading model — may take a few seconds on first run...")
	if _, err := p.Embed(ctx, benchCorpus[0]); err != nil {
		b.Fatalf("warmup: %v", err)
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := p.Embed(ctx, benchCorpus[i%len(benchCorpus)]); err != nil {
			b.Fatalf("Embed: %v", err)
		}
	}
}

// BenchmarkHugotBatch measures throughput when the full corpus is sent as a
// single RunPipeline call — all texts tokenized and inferred in one pass.
//
// Run with:
//
//	AXON_BENCH_HUGOT=1 go test -bench=BenchmarkHugotBatch -benchtime=30s ./indexer/embeddings/
func BenchmarkHugotBatch(b *testing.B) {
	if os.Getenv("AXON_BENCH_HUGOT") == "" {
		b.Skip("set AXON_BENCH_HUGOT=1 to run (downloads ~90 MB model on first run)")
	}

	p := embeddings.NewHugot("", "")
	defer p.Close()
	ctx := context.Background()

	b.Log("loading model — may take a few seconds on first run...")
	if _, err := p.EmbedBatch(ctx, benchCorpus[:1]); err != nil {
		b.Fatalf("warmup: %v", err)
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := p.EmbedBatch(ctx, benchCorpus); err != nil {
			b.Fatalf("EmbedBatch: %v", err)
		}
	}
	b.ReportMetric(float64(len(benchCorpus)), "texts/op")
}
