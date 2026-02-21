package indexer

import (
	"context"

	"github.com/codewandler/axon/graph"
	"github.com/codewandler/axon/types"
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

// EmitContainment creates bidirectional containment edges using any Emitter:
// - contains: parentID → childID (structural containment)
// - contained_by: childID → parentID (inverse)
// Use for physical/structural hierarchies (directories, nested structures).
func EmitContainment(ctx context.Context, e Emitter, parentID, childID string) error {
	containsEdge := graph.NewEdge(types.EdgeContains, parentID, childID)
	if err := e.EmitEdge(ctx, containsEdge); err != nil {
		return err
	}
	containedByEdge := graph.NewEdge(types.EdgeContainedBy, childID, parentID)
	return e.EmitEdge(ctx, containedByEdge)
}

// EmitOwnership creates bidirectional ownership edges using any Emitter:
// - has: ownerID → ownedID (logical ownership)
// - belongs_to: ownedID → ownerID (inverse)
// Use when the child cannot exist without the parent (repo→branch, doc→section).
func EmitOwnership(ctx context.Context, e Emitter, ownerID, ownedID string) error {
	hasEdge := graph.NewEdge(types.EdgeHas, ownerID, ownedID)
	if err := e.EmitEdge(ctx, hasEdge); err != nil {
		return err
	}
	belongsToEdge := graph.NewEdge(types.EdgeBelongsTo, ownedID, ownerID)
	return e.EmitEdge(ctx, belongsToEdge)
}

// GraphEmitter-specific methods (for convenience, delegate to package functions)

// EmitContainment creates bidirectional containment edges:
// - contains: parentID → childID (structural containment)
// - contained_by: childID → parentID (inverse)
// Use for physical/structural hierarchies (directories, nested structures).
func (e *GraphEmitter) EmitContainment(ctx context.Context, parentID, childID string) error {
	containsEdge := graph.NewEdge(types.EdgeContains, parentID, childID)
	if err := e.EmitEdge(ctx, containsEdge); err != nil {
		return err
	}
	containedByEdge := graph.NewEdge(types.EdgeContainedBy, childID, parentID)
	return e.EmitEdge(ctx, containedByEdge)
}

// EmitOwnership creates bidirectional ownership edges:
// - has: ownerID → ownedID (logical ownership)
// - belongs_to: ownedID → ownerID (inverse)
// Use when the child cannot exist without the parent (repo→branch, doc→section).
func (e *GraphEmitter) EmitOwnership(ctx context.Context, ownerID, ownedID string) error {
	hasEdge := graph.NewEdge(types.EdgeHas, ownerID, ownedID)
	if err := e.EmitEdge(ctx, hasEdge); err != nil {
		return err
	}
	belongsToEdge := graph.NewEdge(types.EdgeBelongsTo, ownedID, ownerID)
	return e.EmitEdge(ctx, belongsToEdge)
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

// EmitContainment creates bidirectional containment edges for testing.
func (e *CollectingEmitter) EmitContainment(ctx context.Context, parentID, childID string) error {
	containsEdge := graph.NewEdge(types.EdgeContains, parentID, childID)
	if err := e.EmitEdge(ctx, containsEdge); err != nil {
		return err
	}
	containedByEdge := graph.NewEdge(types.EdgeContainedBy, childID, parentID)
	return e.EmitEdge(ctx, containedByEdge)
}

// EmitOwnership creates bidirectional ownership edges for testing.
func (e *CollectingEmitter) EmitOwnership(ctx context.Context, ownerID, ownedID string) error {
	hasEdge := graph.NewEdge(types.EdgeHas, ownerID, ownedID)
	if err := e.EmitEdge(ctx, hasEdge); err != nil {
		return err
	}
	belongsToEdge := graph.NewEdge(types.EdgeBelongsTo, ownedID, ownerID)
	return e.EmitEdge(ctx, belongsToEdge)
}

// CountingEmitter wraps an Emitter and tracks node counts by type in the Context.
type CountingEmitter struct {
	inner Emitter
	ictx  *Context
}

// NewCountingEmitter creates an emitter that wraps inner and increments
// counters in ictx based on emitted node types.
func NewCountingEmitter(inner Emitter, ictx *Context) *CountingEmitter {
	return &CountingEmitter{inner: inner, ictx: ictx}
}

func (e *CountingEmitter) EmitNode(ctx context.Context, node *graph.Node) error {
	if err := e.inner.EmitNode(ctx, node); err != nil {
		return err
	}
	// Increment counter based on node type
	switch node.Type {
	case types.TypeFile:
		e.ictx.AddFilesIndexed(1)
	case types.TypeDir:
		e.ictx.AddDirsIndexed(1)
	case types.TypeRepo:
		e.ictx.AddReposIndexed(1)
	}
	return nil
}

func (e *CountingEmitter) EmitEdge(ctx context.Context, edge *graph.Edge) error {
	return e.inner.EmitEdge(ctx, edge)
}
