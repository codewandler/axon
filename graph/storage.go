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
	Generation  string   // Filter by exact generation ID (empty = any). Pass indexer.Context.Generation
	             // to scope a query to only nodes written in the current indexing run.
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

// NodeWithScore is a node with a similarity score for semantic search results.
type NodeWithScore struct {
	*Node
	Score float32
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

// AQLQuerier provides AQL (Axon Query Language) query execution.
type AQLQuerier interface {
	Query(ctx context.Context, query interface{}) (*QueryResult, error)
	Explain(ctx context.Context, query interface{}) (*QueryPlan, error)
}

// EmbeddingStore provides vector embedding storage and similarity search.
type EmbeddingStore interface {
	PutEmbedding(ctx context.Context, nodeID string, embedding []float32) error
	GetEmbedding(ctx context.Context, nodeID string) ([]float32, error)
	FindSimilar(ctx context.Context, query []float32, limit int, filter *NodeFilter) ([]*NodeWithScore, error)
}

// ResultType indicates the type of query result.
type ResultType int

const (
	ResultTypeNodes  ResultType = iota
	ResultTypeEdges
	ResultTypeCounts
	ResultTypeRows // Multi-variable or cross-variable field-selector pattern queries
)

// QueryResult holds the results of an AQL query execution.
// Fields are populated based on the result type:
// - ResultTypeNodes: Nodes slice is populated
// - ResultTypeEdges: Edges slice is populated
// - ResultTypeCounts: Counts slice is populated
// - ResultTypeRows: Rows slice is populated (multi-variable pattern SELECT)
//
// SelectedColumns is set for non-star SELECT queries, in SELECT order.
type QueryResult struct {
	Type            ResultType
	Nodes           []*Node
	Edges           []*Edge
	Counts          []CountItem      // For GROUP BY queries, in SQLite result order
	SelectedColumns []string         // Column names in SELECT order; nil means SELECT *
	Rows            []map[string]any // For ResultTypeRows: multi-variable pattern results
	GroupingColumn  string           // For ResultTypeCounts GROUP BY: the grouping column name
}

// Count returns the scalar count value for SELECT COUNT(*) queries.
// For scalar COUNT queries (no GROUP BY), looks for the "_count" sentinel item.
// For GROUP BY queries, returns the sum of all counts.
// Returns 0 if not a count query or if result is empty.
func (qr *QueryResult) Count() int {
	if qr.Type != ResultTypeCounts {
		return 0
	}

	// Check for scalar count (special "_count" sentinel)
	for _, item := range qr.Counts {
		if item.Name == "_count" {
			return item.Count
		}
	}

	// Fallback: sum all counts (for GROUP BY queries)
	total := 0
	for _, item := range qr.Counts {
		total += item.Count
	}
	return total
}

// QueryPlan holds the execution plan for an AQL query.
// Used for debugging and performance analysis.
type QueryPlan struct {
	SQL         string // Generated SQL query
	Args        []any  // Query arguments
	SQLitePlan  string // Output of EXPLAIN QUERY PLAN
	EstimatedMs int64  // Estimated execution time (if available)
}

// -----------------------------------------------------------------------------
// Full Storage Interface
// -----------------------------------------------------------------------------

// Storage defines the complete interface for graph persistence.
// It composes all the smaller interfaces for full functionality.
type Storage interface {
	NodeStore
	EdgeStore
	NodeQuerier
	EdgeQuerier
	StalenessManager
	IndexRunTracker
	Flusher
	DatabaseInfo
	AQLQuerier
	EmbeddingStore
}