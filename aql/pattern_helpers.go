package aql

// This file contains convenience methods for building patterns with EdgeTypeRef.

// ToEdge adds an outgoing edge and target node, accepting EdgeTypeRef.
// Example: Pat(N("dir").Build()).ToEdge(Edge.Contains, N("file").Build())
func (p *PatternBuilder) ToEdge(edgeType EdgeTypeRef, node *NodePattern) *PatternBuilder {
	return p.To(edgeType.ToEdgePattern(), node)
}

// FromEdge adds an incoming edge and source node, accepting EdgeTypeRef.
// Example: Pat(N("file").Build()).FromEdge(Edge.Contains, N("dir").Build())
func (p *PatternBuilder) FromEdge(edgeType EdgeTypeRef, node *NodePattern) *PatternBuilder {
	return p.From(edgeType.ToEdgePattern(), node)
}

// EitherEdge adds an undirected edge and adjacent node, accepting EdgeTypeRef.
// Example: Pat(N("a").Build()).EitherEdge(Edge.References, N("b").Build())
func (p *PatternBuilder) EitherEdge(edgeType EdgeTypeRef, node *NodePattern) *PatternBuilder {
	return p.Either(edgeType.ToEdgePattern(), node)
}
