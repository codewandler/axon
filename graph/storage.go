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

// -----------------------------------------------------------------------------
// Composable Storage Interfaces
// -----------------------------------------------------------------------------

// NodeReader provides read access to nodes.
type NodeReader interface {
	GetNode(ctx context.Context, id string) (*Node, error)
	GetNodeByURI(ctx context.Context, uri string) (*Node, error)
	GetNodeByKey(ctx context.Context, nodeType, key string) (*Node, error)
}

// NodeWriter provides write access to nodes.
type NodeWriter interface {
	PutNode(ctx context.Context, node *Node) error
	DeleteNode(ctx context.Context, id string) error
}

// NodeStore combines read and write access to nodes.
type NodeStore interface {
	NodeReader
	NodeWriter
}

// EdgeReader provides read access to edges.
type EdgeReader interface {
	GetEdge(ctx context.Context, id string) (*Edge, error)
	GetEdgesFrom(ctx context.Context, nodeID string) ([]*Edge, error)
	GetEdgesTo(ctx context.Context, nodeID string) ([]*Edge, error)
}

// EdgeWriter provides write access to edges.
type EdgeWriter interface {
	PutEdge(ctx context.Context, edge *Edge) error
	DeleteEdge(ctx context.Context, id string) error
}

// EdgeStore combines read and write access to edges.
type EdgeStore interface {
	EdgeReader
	EdgeWriter
}

// GraphTraverser provides graph traversal capabilities.
type GraphTraverser interface {
	Traverse(ctx context.Context, opts TraverseOptions) (<-chan TraverseResult, error)
}

// NodeQuerier provides node query capabilities.
type NodeQuerier interface {
	FindNodes(ctx context.Context, filter NodeFilter, opts QueryOptions) ([]*Node, error)
	CountNodes(ctx context.Context, filter NodeFilter, opts QueryOptions) (map[string]int, error)
}

// EdgeQuerier provides edge query capabilities.
type EdgeQuerier interface {
	CountEdges(ctx context.Context, filter EdgeFilter, opts QueryOptions) (map[string]int, error)
}

// StalenessManager handles generation-based cleanup for indexers.
type StalenessManager interface {
	FindStaleByURIPrefix(ctx context.Context, uriPrefix, currentGen string) ([]*Node, error)
	DeleteStaleByURIPrefix(ctx context.Context, uriPrefix, currentGen string) (int, error)
	DeleteByURIPrefix(ctx context.Context, uriPrefix string) (int, error)
	DeleteStaleEdges(ctx context.Context, currentGen string) (int, error)
	DeleteOrphanedEdges(ctx context.Context) (int, error)
	CountOrphanedEdges(ctx context.Context) (int, error)
}

// IndexRunTracker tracks indexing run history.
type IndexRunTracker interface {
	RecordIndexRun(ctx context.Context, run IndexRunRecord) error
	GetLastIndexRun(ctx context.Context) (*IndexRunRecord, error)
}

// Flusher provides buffered write flushing.
type Flusher interface {
	Flush(ctx context.Context) error
}

// DatabaseInfo provides database metadata.
type DatabaseInfo interface {
	GetDatabasePath() string
}

// -----------------------------------------------------------------------------
// Full Storage Interface
// -----------------------------------------------------------------------------

// Storage defines the complete interface for graph persistence.
// It composes all the smaller interfaces for full functionality.
type Storage interface {
	NodeStore
	EdgeStore
	GraphTraverser
	NodeQuerier
	EdgeQuerier
	StalenessManager
	IndexRunTracker
	Flusher
	DatabaseInfo
}
