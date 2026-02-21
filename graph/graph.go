// Package graph provides the core graph data structures and operations for Axon.
//
// The graph consists of nodes and edges. Nodes represent entities (files, directories,
// git repos, markdown documents, etc.) and edges represent relationships between them
// (contains, has, links_to, etc.).
//
// The Graph type wraps a Storage implementation and provides high-level operations
// like AddNode, AddEdge, Neighbors, Children, and Parents. It also handles type
// validation through a Registry.
//
// Storage implementations (like SQLite) handle persistence and querying. The Storage
// interface is broken into smaller composable interfaces (NodeReader, NodeWriter, etc.)
// for flexibility.
package graph

import (
	"context"
	"log"
)

// Direction specifies the traversal direction for neighbors.
type Direction int

const (
	// Outgoing follows edges from the node (node -> neighbors).
	Outgoing Direction = iota
	// Incoming follows edges to the node (neighbors -> node).
	Incoming
	// Both follows edges in both directions.
	Both
)

// Graph provides high-level operations over the storage layer.
type Graph struct {
	storage  Storage
	registry *Registry
}

// New creates a new graph with the given storage and registry.
func New(s Storage, r *Registry) *Graph {
	return &Graph{
		storage:  s,
		registry: r,
	}
}

// Storage returns the underlying storage.
func (g *Graph) Storage() Storage {
	return g.storage
}

// Registry returns the type registry.
func (g *Graph) Registry() *Registry {
	return g.registry
}

// AddNode adds a node to the graph after validating its type.
func (g *Graph) AddNode(ctx context.Context, n *Node) error {
	if err := g.registry.ValidateNode(n); err != nil {
		return err
	}
	return g.storage.PutNode(ctx, n)
}

// AddEdge adds an edge to the graph after validating its type and endpoints.
func (g *Graph) AddEdge(ctx context.Context, e *Edge) error {
	fromNode, err := g.storage.GetNode(ctx, e.From)
	if err != nil {
		return err
	}
	toNode, err := g.storage.GetNode(ctx, e.To)
	if err != nil {
		return err
	}
	if err := g.registry.ValidateEdge(e, fromNode, toNode); err != nil {
		return err
	}
	return g.storage.PutEdge(ctx, e)
}

// GetNode retrieves a node by ID.
func (g *Graph) GetNode(ctx context.Context, id string) (*Node, error) {
	return g.storage.GetNode(ctx, id)
}

// GetNodeByURI retrieves a node by its URI.
func (g *Graph) GetNodeByURI(ctx context.Context, uri string) (*Node, error) {
	return g.storage.GetNodeByURI(ctx, uri)
}

// GetEdge retrieves an edge by ID.
func (g *Graph) GetEdge(ctx context.Context, id string) (*Edge, error) {
	return g.storage.GetEdge(ctx, id)
}

// DeleteNode removes a node from the graph.
func (g *Graph) DeleteNode(ctx context.Context, id string) error {
	return g.storage.DeleteNode(ctx, id)
}

// DeleteEdge removes an edge from the graph.
func (g *Graph) DeleteEdge(ctx context.Context, id string) error {
	return g.storage.DeleteEdge(ctx, id)
}

// Neighbors returns all nodes connected to the given node in the specified direction.
func (g *Graph) Neighbors(ctx context.Context, nodeID string, dir Direction) ([]*Node, error) {
	var edges []*Edge
	var err error

	switch dir {
	case Outgoing:
		edges, err = g.storage.GetEdgesFrom(ctx, nodeID)
	case Incoming:
		edges, err = g.storage.GetEdgesTo(ctx, nodeID)
	case Both:
		outgoing, err1 := g.storage.GetEdgesFrom(ctx, nodeID)
		incoming, err2 := g.storage.GetEdgesTo(ctx, nodeID)
		if err1 != nil {
			return nil, err1
		}
		if err2 != nil {
			return nil, err2
		}
		edges = append(outgoing, incoming...)
	}

	if err != nil {
		return nil, err
	}

	// Collect unique neighbor IDs
	neighborIDs := make(map[string]bool)
	for _, e := range edges {
		if e.From == nodeID {
			neighborIDs[e.To] = true
		} else {
			neighborIDs[e.From] = true
		}
	}

	// Fetch nodes
	nodes := make([]*Node, 0, len(neighborIDs))
	for id := range neighborIDs {
		node, err := g.storage.GetNode(ctx, id)
		if err != nil {
			// Log warning - this indicates a data integrity issue (orphaned edge)
			log.Printf("graph: Neighbors: failed to get node %s: %v (possible orphaned edge)", id, err)
			continue
		}
		nodes = append(nodes, node)
	}

	return nodes, nil
}

// Children returns all nodes that this node contains or has (owns).
// Filters to only "contains" and "has" edges (parent→child relationships).
func (g *Graph) Children(ctx context.Context, nodeID string) ([]*Node, error) {
	edges, err := g.storage.GetEdgesFrom(ctx, nodeID)
	if err != nil {
		return nil, err
	}

	// Filter to only parent→child edge types
	nodes := make([]*Node, 0)
	for _, e := range edges {
		if e.Type == "contains" || e.Type == "has" {
			node, err := g.storage.GetNode(ctx, e.To)
			if err != nil {
				// Log warning - this indicates a data integrity issue (orphaned edge)
				log.Printf("graph: Children: failed to get node %s: %v (possible orphaned edge)", e.To, err)
				continue
			}
			nodes = append(nodes, node)
		}
	}

	return nodes, nil
}

// Parents returns all nodes that contain or own this node.
// Filters to only "contained_by" and "belongs_to" edges (child→parent relationships).
func (g *Graph) Parents(ctx context.Context, nodeID string) ([]*Node, error) {
	edges, err := g.storage.GetEdgesFrom(ctx, nodeID)
	if err != nil {
		return nil, err
	}

	// Filter to only child→parent edge types
	nodes := make([]*Node, 0)
	for _, e := range edges {
		if e.Type == "contained_by" || e.Type == "belongs_to" {
			node, err := g.storage.GetNode(ctx, e.To)
			if err != nil {
				// Log warning - this indicates a data integrity issue (orphaned edge)
				log.Printf("graph: Parents: failed to get node %s: %v (possible orphaned edge)", e.To, err)
				continue
			}
			nodes = append(nodes, node)
		}
	}

	return nodes, nil
}

// FindNodes finds nodes matching the given filter.
func (g *Graph) FindNodes(ctx context.Context, filter NodeFilter, opts QueryOptions) ([]*Node, error) {
	return g.storage.FindNodes(ctx, filter, opts)
}

// CountNodes returns node counts grouped by the specified field.
func (g *Graph) CountNodes(ctx context.Context, filter NodeFilter, opts QueryOptions) (map[string]int, error) {
	return g.storage.CountNodes(ctx, filter, opts)
}

// CountEdges returns edge counts grouped by the specified field.
func (g *Graph) CountEdges(ctx context.Context, filter EdgeFilter, opts QueryOptions) (map[string]int, error) {
	return g.storage.CountEdges(ctx, filter, opts)
}

// GetEdgesFrom returns all edges originating from the given node.
func (g *Graph) GetEdgesFrom(ctx context.Context, nodeID string) ([]*Edge, error) {
	return g.storage.GetEdgesFrom(ctx, nodeID)
}

// GetEdgesTo returns all edges pointing to the given node.
func (g *Graph) GetEdgesTo(ctx context.Context, nodeID string) ([]*Edge, error) {
	return g.storage.GetEdgesTo(ctx, nodeID)
}
