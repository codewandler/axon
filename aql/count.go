package aql

// countCall wraps CountCall to make it Selectable.
type countCall struct{}

// Ensure countCall implements Selectable
func (countCall) selectable() {}

// toCountCall converts to AST CountCall.
func (countCall) toCountCall() *CountCall {
	return &CountCall{}
}

// Count creates a COUNT(*) expression for use in SELECT clauses.
// Example: Nodes.Select(Type, Count())
func Count() countCall {
	return countCall{}
}
