package graph

import "context"

// NodeTypeInfo describes a node type in the graph.
type NodeTypeInfo struct {
	Type   string   `json:"type"`
	Count  int      `json:"count"`
	Fields []string `json:"fields,omitempty"`
}

// EdgeConnection describes a from→to node-type pair for an edge type.
type EdgeConnection struct {
	From  string `json:"from"`
	To    string `json:"to"`
	Count int    `json:"count"`
}

// EdgeTypeInfo describes an edge type in the graph.
type EdgeTypeInfo struct {
	Type        string           `json:"type"`
	Count       int              `json:"count"`
	Connections []EdgeConnection `json:"connections"`
}

// SchemaDescription is the result of schema introspection. It describes all
// node types and edge types currently present in the graph, along with their
// counts and (optionally) the data field names available on each node type.
type SchemaDescription struct {
	NodeTypes []NodeTypeInfo `json:"node_types"`
	EdgeTypes []EdgeTypeInfo `json:"edge_types"`
}

// Describer is an optional interface that storage implementations may satisfy
// to provide schema introspection. It is intentionally NOT embedded in Storage
// so that existing test mocks are not affected.
//
// includeFields, when true, causes an additional per-type query to discover
// the JSON data field names actually stored in nodes of each type. This samples
// up to 500 nodes per type and may be slightly slower on large graphs.
type Describer interface {
	DescribeSchema(ctx context.Context, includeFields bool) (*SchemaDescription, error)
}
