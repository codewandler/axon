package aql

// EdgeTypeRef represents a typed edge type for patterns.
// Can be used in queries like Edge.Contains or Edge.Has.
type EdgeTypeRef struct {
	name string
}

// String returns the edge type as a string.
func (e EdgeTypeRef) String() string { return e.name }

// ToEdgePattern converts EdgeTypeRef to *EdgePattern for use in patterns.
// This is the exported version that can be used in tests and external code.
func (e EdgeTypeRef) ToEdgePattern() *EdgePattern {
	return &EdgePattern{Type: e.name}
}

// toEdgePattern is an internal alias for ToEdgePattern.
func (e EdgeTypeRef) toEdgePattern() *EdgePattern {
	return e.ToEdgePattern()
}

// WithHops sets variable-length edge hops (min..max).
// Example: Edge.Contains.WithHops(1, 3) matches 1-3 hops.
func (e EdgeTypeRef) WithHops(min, max int) *EdgePattern {
	return e.toEdgePattern().WithHops(min, max)
}

// WithMinHops sets minimum hops with unbounded maximum.
// Example: Edge.Contains.WithMinHops(0) matches 0 or more hops.
func (e EdgeTypeRef) WithMinHops(min int) *EdgePattern {
	return e.toEdgePattern().WithMinHops(min)
}

// WithExactHops sets exact number of hops.
// Example: Edge.Contains.WithExactHops(2) matches exactly 2 hops.
func (e EdgeTypeRef) WithExactHops(n int) *EdgePattern {
	return e.toEdgePattern().WithExactHops(n)
}

// Edge namespace with predefined edge types.
var Edge = struct {
	Contains    EdgeTypeRef
	ContainedBy EdgeTypeRef
	Has         EdgeTypeRef
	BelongsTo   EdgeTypeRef
	LocatedAt   EdgeTypeRef
	LinksTo     EdgeTypeRef
	References  EdgeTypeRef
	DependsOn   EdgeTypeRef
	Imports     EdgeTypeRef
	Defines     EdgeTypeRef
	ParentOf    EdgeTypeRef
	Modifies    EdgeTypeRef
}{
	Contains:    EdgeTypeRef{"contains"},
	ContainedBy: EdgeTypeRef{"contained_by"},
	Has:         EdgeTypeRef{"has"},
	BelongsTo:   EdgeTypeRef{"belongs_to"},
	LocatedAt:   EdgeTypeRef{"located_at"},
	LinksTo:     EdgeTypeRef{"links_to"},
	References:  EdgeTypeRef{"references"},
	DependsOn:   EdgeTypeRef{"depends_on"},
	Imports:     EdgeTypeRef{"imports"},
	Defines:     EdgeTypeRef{"defines"},
	ParentOf:    EdgeTypeRef{"parent_of"},
	Modifies:    EdgeTypeRef{"modifies"},
}

// EdgeTypeOf creates a custom edge type reference.
// Use this for edge types not in the predefined Edge namespace.
func EdgeTypeOf(name string) EdgeTypeRef {
	return EdgeTypeRef{name: name}
}

// EdgesOf creates an edge pattern matching any of the given types (OR logic).
// Example: EdgesOf(Edge.Contains, Edge.Has) matches [:contains|has]
func EdgesOf(types ...EdgeTypeRef) *EdgePattern {
	names := make([]string, len(types))
	for i, t := range types {
		names[i] = t.name
	}
	return &EdgePattern{Types: names}
}

// E creates an edge pattern with a variable and type.
// Example: E("e", Edge.Contains) creates [e:contains]
func E(variable string, edgeType EdgeTypeRef) *EdgePattern {
	return &EdgePattern{
		Variable: variable,
		Type:     edgeType.name,
	}
}

// EOfType creates an edge pattern with a variable and custom type string.
// Example: EOfType("e", "custom:edge") creates [e:custom:edge]
func EOfType(variable string, edgeType string) *EdgePattern {
	return &EdgePattern{
		Variable: variable,
		Type:     edgeType,
	}
}
