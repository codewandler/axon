package graph

import (
	"context"
	"time"
)

// NodeFilter specifies criteria for finding nodes.
type NodeFilter struct {
	Type        string   // Filter by exact node type (empty = any)
	TypePattern string   // Filter by node type with glob pattern (empty = any)
	URIPrefix   string   // Filter by URI prefix (empty = any)
	Name        string   // Filter by exact name (empty = any)
	NamePattern string   // Filter by name with glob pattern (empty = any)
	Labels      []string // Filter by labels (OR logic - node must have at least one)
	Extensions  []string // Filter by file extension without dot (OR logic, e.g., "go", "py")
	NodeIDs     []string // Filter to specific node IDs (OR logic)
	Root        bool     // Only nodes with no incoming containment edges (top-level roots)
}

// EdgeFilter specifies criteria for finding/counting edges.
type EdgeFilter struct {
	Type      string      // Filter by exact edge type (empty = any)
	Types     []string    // Filter by multiple edge types (OR logic, empty = any)
	Direction string      // For traversal: "outgoing", "incoming", "both" (default: "outgoing")
	From      *NodeFilter // Filter by from-node properties (nil = any)
	To        *NodeFilter // Filter by to-node properties (nil = any)
}

// QueryOptions specifies aggregation, ordering, and limiting for queries.
type QueryOptions struct {
	GroupBy string // "type", "label", "extension" or "" for no grouping
	OrderBy string // "count", "name" for counts; "name", "updated", "type" for nodes
	Desc    bool   // true for descending order
	Limit   int    // 0 for no limit
}

// TraverseOptions controls graph traversal.
type TraverseOptions struct {
	Seed        NodeFilter   // finds starting nodes
	MaxDepth    int          // 0 = unlimited
	NodeFilter  NodeFilter   // filter which nodes to include in results
	EdgeFilters []EdgeFilter // edges to follow (multiple allows different directions per edge type)
}

// TraverseResult represents a visited node during traversal.
type TraverseResult struct {
	Node  *Node // the visited node
	Depth int   // distance from seed node (0 for seed nodes)
	Via   *Edge // edge that led here (nil for seed nodes)
	Err   error // if set, signals error and end of traversal
}

// IndexRunRecord represents a single indexing run for tracking history.
type IndexRunRecord struct {
	ID           int64
	StartedAt    time.Time
	FinishedAt   time.Time
	DurationMs   int64
	RootPath     string
	FilesIndexed int
	DirsIndexed  int
	ReposIndexed int
	StaleRemoved int
	Generation   string
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

	// Traverse walks the graph from seed nodes, yielding visited nodes via channel.
	// Uses BFS, respects MaxDepth, applies NodeFilter to results, follows EdgeFilters.
	// Closes channel when traversal completes or context is cancelled.
	Traverse(ctx context.Context, opts TraverseOptions) (<-chan TraverseResult, error)

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

	// Index run tracking

	// RecordIndexRun saves a record of an indexing run.
	RecordIndexRun(ctx context.Context, run IndexRunRecord) error

	// GetLastIndexRun returns the most recent index run, or nil if none.
	GetLastIndexRun(ctx context.Context) (*IndexRunRecord, error)

	// GetDatabasePath returns the path to the database file.
	GetDatabasePath() string
}
