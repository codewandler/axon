package indexer

import (
	"context"

	"github.com/codewandler/axon/graph"
)

// Emitter receives discovered nodes and edges during indexing.
type Emitter interface {
	// EmitNode adds or updates a node in the graph.
	EmitNode(ctx context.Context, node *graph.Node) error

	// EmitEdge adds or updates an edge in the graph.
	EmitEdge(ctx context.Context, edge *graph.Edge) error
}

// GraphEmitter emits nodes and edges directly to a graph.
type GraphEmitter struct {
	graph      *graph.Graph
	generation string
}

// NewGraphEmitter creates an emitter that writes to the given graph.
func NewGraphEmitter(g *graph.Graph, generation string) *GraphEmitter {
	return &GraphEmitter{
		graph:      g,
		generation: generation,
	}
}

func (e *GraphEmitter) EmitNode(ctx context.Context, node *graph.Node) error {
	node.Generation = e.generation
	return e.graph.Storage().PutNode(ctx, node)
}

func (e *GraphEmitter) EmitEdge(ctx context.Context, edge *graph.Edge) error {
	edge.Generation = e.generation
	return e.graph.Storage().PutEdge(ctx, edge)
}

// CollectingEmitter collects nodes and edges for later processing.
// Useful for testing or batch operations.
type CollectingEmitter struct {
	Nodes []*graph.Node
	Edges []*graph.Edge
}

func (e *CollectingEmitter) EmitNode(ctx context.Context, node *graph.Node) error {
	e.Nodes = append(e.Nodes, node)
	return nil
}

func (e *CollectingEmitter) EmitEdge(ctx context.Context, edge *graph.Edge) error {
	e.Edges = append(e.Edges, edge)
	return nil
}
