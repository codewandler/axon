package aql

// scopedTo creates an EXISTS expression for scoped queries on a specific table.
// Internal helper - called by TableRef.ScopedTo() methods.
//
// The traversal follows both "contains" and "has" edges so that non-filesystem
// nodes (e.g. md:document, md:section, go:struct) that are owned by files
// within the directory are correctly considered in-scope.
func scopedTo(nodeID string, targetTable string) Expression {
	// Build pattern: (root WHERE id = nodeID)-[:contains|has*0..]->(table)
	// Using Types for multi-type traversal: contains for filesystem hierarchy,
	// has for owned nodes (markdown sections, Go symbols, etc.).
	// Target variable must match table name for CTE+JOIN optimization.
	rootNode := &NodePattern{
		Variable: "__scope_root",
		Where: &ComparisonExpr{
			Left:  &Selector{Parts: []string{"id"}},
			Op:    OpEq,
			Right: &StringLit{Value: nodeID},
		},
	}

	minHops := 0
	containsOrHasEdge := &EdgePattern{
		Types:     []string{"contains", "has"},
		Direction: Outgoing,
		MinHops:   &minHops,
		MaxHops:   nil, // unbounded
	}

	// Target variable must match table name for optimization
	targetNode := &NodePattern{
		Variable: targetTable,
	}

	pattern := &Pattern{
		Elements: []PatternElement{rootNode, containsOrHasEdge, targetNode},
	}

	return &ExistsExpr{Not: false, Pattern: pattern}
}
