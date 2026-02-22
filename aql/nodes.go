package aql

// NodeBuilder provides a fluent API for building node patterns.
// Use Node("varname") to start building a node pattern.
type NodeBuilder struct {
	variable string
	nodeType string
	where    Expression
}

// N creates a node pattern builder with a variable name.
// Example: N("file") creates a pattern node with variable "file".
func N(variable string) *NodeBuilder {
	return &NodeBuilder{variable: variable}
}

// OfType sets the node type using a NodeTypeRef.
// Example: Node("file").OfType(NodeType.File)
func (n *NodeBuilder) OfType(t NodeTypeRef) *NodeBuilder {
	n.nodeType = t.name
	return n
}

// OfTypeStr sets the node type from a string (for custom types).
// Example: Node("file").OfTypeStr("custom:thing")
func (n *NodeBuilder) OfTypeStr(t string) *NodeBuilder {
	n.nodeType = t
	return n
}

// Where adds an inline WHERE condition to the node pattern.
// Example: N("file").Where(DataExt.Eq("go"))
func (n *NodeBuilder) Where(expr Expression) *NodeBuilder {
	n.where = expr
	return n
}

// WithWhere is an alias for Where (for compatibility).
func (n *NodeBuilder) WithWhere(expr Expression) *NodeBuilder {
	return n.Where(expr)
}

// Build returns the NodePattern for use in Pat().To() chains.
func (n *NodeBuilder) Build() *NodePattern {
	return &NodePattern{
		Variable: n.variable,
		Type:     n.nodeType,
		Where:    n.where,
	}
}
