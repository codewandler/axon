package embeddings

import (
	"context"
	"strings"

	"github.com/codewandler/axon/graph"
	"github.com/codewandler/axon/indexer"
	"github.com/codewandler/axon/progress"
	"github.com/codewandler/axon/types"
)

// DefaultEmbedTypes are the node types embedded by default.
var DefaultEmbedTypes = []string{
	types.TypeGoFunc,
	types.TypeGoStruct,
	types.TypeGoInterface,
	"md:section",
	"vcs:commit",
	types.TypeTodo,
}

// DefaultBatchSize is the number of texts sent to the provider in one
// EmbedBatch call. 32 balances GPU utilisation against memory pressure and
// works well for Hugot's pure-Go backend.
const DefaultBatchSize = 32

// Indexer is a PostIndexer that generates and stores embeddings for nodes.
type Indexer struct {
	Provider  Provider
	Types     []string // Node types to embed; defaults to DefaultEmbedTypes
	BatchSize int      // Number of texts per EmbedBatch call; defaults to DefaultBatchSize
}

// New creates a new embedding Indexer.
func New(provider Provider) *Indexer {
	return &Indexer{
		Provider:  provider,
		Types:     DefaultEmbedTypes,
		BatchSize: DefaultBatchSize,
	}
}

// Ensure Indexer implements the PostIndexer interface.
var _ indexer.PostIndexer = (*Indexer)(nil)

func (i *Indexer) Name() string                          { return "embeddings" }
func (i *Indexer) Schemes() []string                     { return nil }
func (i *Indexer) Handles(uri string) bool               { return false }
func (i *Indexer) Subscriptions() []indexer.Subscription { return nil }
func (i *Indexer) Index(ctx context.Context, ictx *indexer.Context) error {
	return nil
}
func (i *Indexer) HandleEvent(ctx context.Context, ictx *indexer.Context, event indexer.Event) error {
	return nil
}

// PostIndex implements the PostIndexer interface. It embeds only the nodes that
// were written during the current indexing run (filtered by ictx.Generation),
// so re-index triggered by a file watcher does not re-embed the entire corpus.
//
// Nodes are embedded in batches of [Indexer.BatchSize] using [Provider.EmbedBatch],
// which sends a single request/inference pass per batch instead of one per node.
// Progress is reported on ictx.Progress under the "Vectorizing" phase.
func (i *Indexer) PostIndex(ctx context.Context, ictx *indexer.Context) error {
	storage := ictx.Graph.Storage()

	// Check if storage supports embeddings
	embStore, ok := storage.(interface {
		PutEmbedding(ctx context.Context, nodeID string, embedding []float32) error
	})
	if !ok {
		return nil // Storage doesn't support embeddings
	}

	embedsTypes := i.Types
	if len(embedsTypes) == 0 {
		embedsTypes = DefaultEmbedTypes
	}
	batchSize := i.BatchSize
	if batchSize <= 0 {
		batchSize = DefaultBatchSize
	}

	// Collect all nodes to embed across every type in one pass.
	type entry struct {
		nodeID string
		text   string
	}
	var entries []entry

	for _, nodeType := range embedsTypes {
		nodes, err := storage.FindNodes(ctx, graph.NodeFilter{
			Type:       nodeType,
			Generation: ictx.Generation,
		}, graph.QueryOptions{})
		if err != nil {
			return err
		}
		for _, node := range nodes {
			entries = append(entries, entry{node.ID, buildNodeText(node)})
		}
	}

	if len(entries) == 0 {
		return nil // nothing to embed — skip progress noise
	}

	sendProgress(ictx.Progress, progress.StartedInPhase("embeddings", "Vectorizing"))

	// Embed in batches.
	embedded := 0
	for start := 0; start < len(entries); start += batchSize {
		end := min(start+batchSize, len(entries))
		batch := entries[start:end]

		texts := make([]string, len(batch))
		for j, e := range batch {
			texts[j] = e.text
		}

		embeddings, err := i.Provider.EmbedBatch(ctx, texts)
		if err != nil {
			// Non-fatal: skip this batch rather than aborting the whole run.
			embedded += len(batch)
			sendProgress(ictx.Progress, progress.ProgressWithTotal(
				"embeddings", embedded, len(entries), ""))
			continue
		}

		for j, e := range batch {
			if j >= len(embeddings) {
				break
			}
			if err := embStore.PutEmbedding(ctx, e.nodeID, embeddings[j]); err != nil {
				return err
			}
		}

		embedded += len(batch)

		// Use the first word of the last entry's text as the display item (node name).
		lastItem := batch[len(batch)-1].text
		if idx := strings.Index(lastItem, " "); idx > 0 {
			lastItem = lastItem[:idx]
		}
		sendProgress(ictx.Progress, progress.ProgressWithTotal(
			"embeddings", embedded, len(entries), lastItem))
	}

	sendProgress(ictx.Progress, progress.Completed("embeddings", len(entries)))
	return nil
}

// sendProgress sends a progress event; no-op if the channel is nil.
func sendProgress(ch chan<- progress.Event, evt progress.Event) {
	if ch == nil {
		return
	}
	ch <- evt
}

// BuildNodeText builds a text representation of a node for embedding.
// It is exported so callers that write nodes programmatically (e.g. via
// (*Axon).WriteNode) can generate consistent embeddings without re-implementing
// the same logic.
func BuildNodeText(node *graph.Node) string {
	return buildNodeText(node)
}

// buildNodeText builds a text representation of a node for embedding.
func buildNodeText(node *graph.Node) string {
	parts := []string{node.Name, node.Type}
	if len(node.Labels) > 0 {
		parts = append(parts, strings.Join(node.Labels, " "))
	}
	if m, ok := node.Data.(map[string]interface{}); ok {
		// Go symbols: doc comment + signature
		if doc, ok := m["doc"].(string); ok && doc != "" {
			parts = append(parts, doc)
		}
		if sig, ok := m["signature"].(string); ok && sig != "" {
			parts = append(parts, sig)
		}
		// VCS commits: subject line + optional body
		if msg, ok := m["message"].(string); ok && msg != "" {
			parts = append(parts, msg)
		}
		if body, ok := m["body"].(string); ok && body != "" {
			parts = append(parts, body)
		}
		// code:todo annotations: surrounding context line adds semantic signal
		if ctx, ok := m["context"].(string); ok && ctx != "" {
			parts = append(parts, ctx)
		}
	}
	return strings.Join(parts, " ")
}
