package graph

import "context"

// NodeFilter specifies criteria for finding nodes.
type NodeFilter struct {
	Type        string   // Filter by exact node type (empty = any)
	TypePattern string   // Filter by node type with glob pattern (empty = any)
	URIPrefix   string   // Filter by URI prefix (empty = any)
	Name        string   // Filter by exact name (empty = any)
	NamePattern string   // Filter by name with glob pattern (empty = any)
	Labels      []string // Filter by labels (OR logic - node must have at least one)
	Extensions  []string // Filter by file extension without dot (OR logic, e.g., "go", "py")
}

// EdgeFilter specifies criteria for finding/counting edges.
type EdgeFilter struct {
	Type string      // Filter by exact edge type (empty = any)
	From *NodeFilter // Filter by from-node properties (nil = any)
	To   *NodeFilter // Filter by to-node properties (nil = any)
}

// QueryOptions specifies aggregation, ordering, and limiting for queries.
type QueryOptions struct {
	GroupBy string // "type", "label", or "" for no grouping
	OrderBy string // "count", "name" for counts; "name", "updated", "type" for nodes
	Desc    bool   // true for descending order
	Limit   int    // 0 for no limit
}

// Storage defines the interface for graph persistence.
type Storage interface {
	// Node operations
	PutNode(ctx context.Context, node *Node) error
	GetNode(ctx context.Context, id string) (*Node, error)
	GetNodeByURI(ctx context.Context, uri string) (*Node, error)
	GetNodeByKey(ctx context.Context, nodeType, key string) (*Node, error)
	DeleteNode(ctx context.Context, id string) error

	// Edge operations
	PutEdge(ctx context.Context, edge *Edge) error
	GetEdge(ctx context.Context, id string) (*Edge, error)
	DeleteEdge(ctx context.Context, id string) error

	// Traversal
	GetEdgesFrom(ctx context.Context, nodeID string) ([]*Edge, error)
	GetEdgesTo(ctx context.Context, nodeID string) ([]*Edge, error)

	// Queries
	FindNodes(ctx context.Context, filter NodeFilter, opts QueryOptions) ([]*Node, error)

	// CountNodes returns node counts. With GroupBy="", returns {"": total}.
	// With GroupBy="type", returns counts per type. With GroupBy="label", returns counts per label.
	CountNodes(ctx context.Context, filter NodeFilter, opts QueryOptions) (map[string]int, error)

	// CountEdges returns edge counts. With GroupBy="", returns {"": total}.
	// With GroupBy="type", returns counts per edge type.
	CountEdges(ctx context.Context, filter EdgeFilter, opts QueryOptions) (map[string]int, error)

	// Staleness management (used by indexers for cleanup)

	// FindStaleByURIPrefix returns nodes matching the URI prefix that don't have the current generation.
	FindStaleByURIPrefix(ctx context.Context, uriPrefix, currentGen string) ([]*Node, error)

	// DeleteStaleByURIPrefix removes nodes matching the URI prefix that don't have the current generation.
	// Returns the number of deleted nodes.
	DeleteStaleByURIPrefix(ctx context.Context, uriPrefix, currentGen string) (int, error)

	// DeleteByURIPrefix removes all nodes matching the URI prefix regardless of generation.
	// Returns the number of deleted nodes.
	DeleteByURIPrefix(ctx context.Context, uriPrefix string) (int, error)

	// DeleteStaleEdges removes edges that don't have the current generation.
	// Returns the number of deleted edges.
	DeleteStaleEdges(ctx context.Context, currentGen string) (int, error)

	// DeleteOrphanedEdges removes edges where either endpoint node no longer exists.
	// Returns the number of deleted edges.
	DeleteOrphanedEdges(ctx context.Context) (int, error)

	// CountOrphanedEdges returns the number of edges where either endpoint node no longer exists.
	CountOrphanedEdges(ctx context.Context) (int, error)

	// Flush writes any buffered data to persistent storage.
	// Implementations without buffering can no-op.
	Flush(ctx context.Context) error
}
