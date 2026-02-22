package aql

// scopedTo creates an EXISTS expression for scoped queries on a specific table.
// Internal helper - called by TableRef.ScopedTo() methods.
func scopedTo(nodeID string, targetTable string) Expression {
	// Build pattern: (root WHERE id = nodeID)-[:contains*0..]->(table)
	// Target variable must match table name for CTE+JOIN optimization
	rootNode := &NodePattern{
		Variable: "__scope_root",
		Where: &ComparisonExpr{
			Left:  &Selector{Parts: []string{"id"}},
			Op:    OpEq,
			Right: &StringLit{Value: nodeID},
		},
	}

	minHops := 0
	containsEdge := &EdgePattern{
		Type:      "contains",
		Direction: Outgoing,
		MinHops:   &minHops,
		MaxHops:   nil, // unbounded
	}

	// Target variable must match table name for optimization
	targetNode := &NodePattern{
		Variable: targetTable,
	}

	pattern := &Pattern{
		Elements: []PatternElement{rootNode, containsEdge, targetNode},
	}

	return &ExistsExpr{Not: false, Pattern: pattern}
}
