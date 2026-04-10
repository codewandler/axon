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

// Indexer is a PostIndexer that generates and stores embeddings for nodes.
type Indexer struct {
	Provider Provider
	Types    []string // Node types to embed; defaults to DefaultEmbedTypes
}

// New creates a new embedding Indexer.
func New(provider Provider) *Indexer {
	return &Indexer{
		Provider: provider,
		Types:    DefaultEmbedTypes,
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

// PostIndex implements the PostIndexer interface.
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

	for _, nodeType := range embedTypes {
		nodes, err := storage.FindNodes(ctx, graph.NodeFilter{Type: nodeType}, graph.QueryOptions{})
		if err != nil {
			return err
		}

		for _, node := range nodes {
			text := buildNodeText(node)
			embedding, err := i.Provider.Embed(ctx, text)
			if err != nil {
				// Log but continue
				continue
			}
			if err := embStore.PutEmbedding(ctx, node.ID, embedding); err != nil {
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
