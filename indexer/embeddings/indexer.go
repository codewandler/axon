package embeddings

import (
	"context"
	"strings"

	"github.com/codewandler/axon/graph"
	"github.com/codewandler/axon/indexer"
	"github.com/codewandler/axon/types"
)

// DefaultEmbedTypes are the node types embedded by default.
var DefaultEmbedTypes = []string{
	types.TypeGoFunc,
	types.TypeGoStruct,
	types.TypeGoInterface,
	"md:section",
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
func (i *Indexer) PostIndex(ctx context.Context, ictx *indexer.Context) error {
	storage := ictx.Graph.Storage()

	// Check if storage supports embeddings
	embStore, ok := storage.(interface {
		PutEmbedding(ctx context.Context, nodeID string, embedding []float32) error
	})
	if !ok {
		return nil // Storage doesn't support embeddings
	}

	embedTypes := i.Types
	if len(embedTypes) == 0 {
		embedTypes = DefaultEmbedTypes
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

	for _, nodeType := range embedTypes {
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

	// Embed in batches.
	for start := 0; start < len(entries); start += batchSize {
		end := min(start+batchSize, len(entries))
		batch := entries[start:end]

		texts := make([]string, len(batch))
		for j, e := range batch {
			texts[j] = e.text
		}

		embeddings, err := i.Provider.EmbedBatch(ctx, texts)
		if err != nil {
			// Non-fatal: log and skip this batch rather than aborting the whole run.
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
	}

	return nil
}

// buildNodeText builds a text representation of a node for embedding.
func buildNodeText(node *graph.Node) string {
	parts := []string{node.Name, node.Type}
	if len(node.Labels) > 0 {
		parts = append(parts, strings.Join(node.Labels, " "))
	}
	// Extract doc/signature from Data if available
	if m, ok := node.Data.(map[string]interface{}); ok {
		if doc, ok := m["doc"].(string); ok && doc != "" {
			parts = append(parts, doc)
		}
		if sig, ok := m["signature"].(string); ok && sig != "" {
			parts = append(parts, sig)
		}
	}
	return strings.Join(parts, " ")
}
