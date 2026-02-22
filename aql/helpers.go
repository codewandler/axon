package aql

// This file contains compatibility helpers and convenience functions
// for the fluent builder API.

// ----------------------------------------------------------------------------
// Pattern Node Helpers
// ----------------------------------------------------------------------------

// AnyNode creates an anonymous node pattern (no variable, any type).
// Example: Pat(AnyNode()).To(Edge.Contains, N("file").Build())
func AnyNode() *NodePattern {
	return &NodePattern{}
}

// AnyNodeOfType creates an anonymous node pattern of a specific type.
// Example: Pat(N("a").Build()).To(Edge.Contains, AnyNodeOfType("fs:file"))
func AnyNodeOfType(nodeType string) *NodePattern {
	return &NodePattern{Type: nodeType}
}

// ----------------------------------------------------------------------------
// Pattern Edge Helpers
// ----------------------------------------------------------------------------

// EdgeTypes creates an edge pattern matching any of the given types (OR logic).
// Example: Pat(N("a").Build()).To(EdgeTypes("contains", "has"), N("b").Build())
func EdgeTypes(types ...string) *EdgePattern {
	return &EdgePattern{Types: types}
}

// ----------------------------------------------------------------------------
// Legacy Builder Helpers
// ----------------------------------------------------------------------------

// Select starts building a SELECT query with the given columns.
// This is a legacy helper - prefer using Nodes.Select() or Edges.Select().
func Select(cols ...Selectable) *Builder {
	columns := make([]Column, len(cols))
	for i, col := range cols {
		columns[i] = Column{Expr: convertToColumnExpr(col)}
	}

	return &Builder{
		stmt: &SelectStmt{
			Columns: columns,
		},
	}
}

// SelectStar starts building a SELECT * query.
// This is a legacy helper - prefer using Nodes.SelectStar() or Edges.SelectStar().
func SelectStar() *Builder {
	return &Builder{
		stmt: &SelectStmt{
			Columns: []Column{{Expr: &Star{}}},
		},
	}
}

// SelectDistinct starts building a SELECT DISTINCT query.
// This is a legacy helper - prefer using Nodes.SelectDistinct() or Edges.SelectDistinct().
func SelectDistinct(cols ...Selectable) *Builder {
	b := Select(cols...)
	b.stmt.Distinct = true
	return b
}

// ----------------------------------------------------------------------------
// Builder Extension Methods
// ----------------------------------------------------------------------------

// From sets the FROM clause to a table name.
// This extends Builder for legacy API support.
func (b *Builder) From(table string) *Builder {
	b.stmt.From = &TableSource{Table: table}
	return b
}

// FromPattern sets the FROM clause to graph patterns.
// This extends Builder for legacy API support.
func (b *Builder) FromPattern(patterns ...*Pattern) *Builder {
	b.stmt.From = &PatternSource{Patterns: patterns}
	return b
}

// FromJoined sets the FROM clause to a joined table with table function.
// Example: FromJoined("nodes", "json_each", "labels")
func (b *Builder) FromJoined(table, funcName, column string) *Builder {
	b.stmt.From = &JoinedTableSource{
		Table:     table,
		TableFunc: &TableFunc{Name: funcName, Arg: &Selector{Parts: []string{column}}},
	}
	return b
}

// Distinct sets the DISTINCT flag on the query.
func (b *Builder) Distinct() *Builder {
	b.stmt.Distinct = true
	return b
}

// GroupByCol sets the GROUP BY clause from column name strings.
// Example: GroupByCol("type", "name")
func (b *Builder) GroupByCol(columns ...string) *Builder {
	b.stmt.GroupBy = make([]Selector, len(columns))
	for i, col := range columns {
		b.stmt.GroupBy[i] = *parseFieldSelector(col)
	}
	return b
}

// OrderByExpr adds an ORDER BY clause for an expression.
func (b *Builder) OrderByExpr(expr ColumnExpr, desc bool) *Builder {
	b.stmt.OrderBy = append(b.stmt.OrderBy, OrderSpec{
		Expr:       expr,
		Descending: desc,
	})
	return b
}

// OrderByString adds ascending ORDER BY clauses from column name strings.
// This is a convenience method for backwards compatibility.
// Example: OrderByString("name", "type")
func (b *Builder) OrderByString(columns ...string) *Builder {
	for _, col := range columns {
		b.stmt.OrderBy = append(b.stmt.OrderBy, OrderSpec{
			Expr:       parseFieldSelector(col),
			Descending: false,
		})
	}
	return b
}

// OrderByDescString adds descending ORDER BY clauses from column name strings.
// This is a convenience method for backwards compatibility.
func (b *Builder) OrderByDescString(columns ...string) *Builder {
	for _, col := range columns {
		b.stmt.OrderBy = append(b.stmt.OrderBy, OrderSpec{
			Expr:       parseFieldSelector(col),
			Descending: true,
		})
	}
	return b
}

// ----------------------------------------------------------------------------
// Helper Functions for Expression Building
// ----------------------------------------------------------------------------

// Col creates a column type from parts for use in SELECT.
// Example: Col("file", "data", "ext") → file.data.ext
func Col(parts ...string) colType {
	return colType{parts: parts}
}

// Sel creates a Selector from parts for use in expressions.
// This is for compatibility with code that needs *Selector.
// Example: Sel("file", "data", "ext") → &Selector{Parts: []string{"file", "data", "ext"}}
func Sel(parts ...string) *Selector {
	return &Selector{Parts: parts}
}

// parseFieldSelector parses a dot-separated field path into a Selector.
// Example: "file.data.ext" → &Selector{Parts: []string{"file", "data", "ext"}}
func parseFieldSelector(field string) *Selector {
	parts := []string{field}
	// Simple implementation - could be enhanced to split on "."
	for i := 0; i < len(field); i++ {
		if field[i] == '.' {
			parts = []string{field[:i]}
			rest := field[i+1:]
			for len(rest) > 0 {
				idx := 0
				for idx < len(rest) && rest[idx] != '.' {
					idx++
				}
				parts = append(parts, rest[:idx])
				if idx < len(rest) {
					rest = rest[idx+1:]
				} else {
					rest = ""
				}
			}
			return &Selector{Parts: parts}
		}
	}
	return &Selector{Parts: parts}
}

// Eq creates an equality comparison expression.
// Example: Eq("type", "fs:file")
func Eq(field string, value any) *ComparisonExpr {
	return &ComparisonExpr{
		Left:  parseFieldSelector(field),
		Op:    OpEq,
		Right: wrapValue(value),
	}
}

// Ne creates a not-equal comparison expression.
func Ne(field string, value any) *ComparisonExpr {
	return &ComparisonExpr{
		Left:  parseFieldSelector(field),
		Op:    OpNe,
		Right: wrapValue(value),
	}
}

// Lt creates a less-than comparison expression.
func Lt(field string, value any) *ComparisonExpr {
	return &ComparisonExpr{
		Left:  parseFieldSelector(field),
		Op:    OpLt,
		Right: wrapValue(value),
	}
}

// Le creates a less-than-or-equal comparison expression.
func Le(field string, value any) *ComparisonExpr {
	return &ComparisonExpr{
		Left:  parseFieldSelector(field),
		Op:    OpLe,
		Right: wrapValue(value),
	}
}

// Gt creates a greater-than comparison expression.
func Gt(field string, value any) *ComparisonExpr {
	return &ComparisonExpr{
		Left:  parseFieldSelector(field),
		Op:    OpGt,
		Right: wrapValue(value),
	}
}

// Ge creates a greater-than-or-equal comparison expression.
func Ge(field string, value any) *ComparisonExpr {
	return &ComparisonExpr{
		Left:  parseFieldSelector(field),
		Op:    OpGe,
		Right: wrapValue(value),
	}
}

// Between creates a BETWEEN expression.
func Between(field string, low, high any) *BetweenExpr {
	return &BetweenExpr{
		Left: parseFieldSelector(field),
		Low:  wrapValue(low),
		High: wrapValue(high),
	}
}

// IsNull creates an IS NULL expression.
func IsNull(field string) *IsNullExpr {
	return &IsNullExpr{
		Selector: parseFieldSelector(field),
		Not:      false,
	}
}

// IsNotNull creates an IS NOT NULL expression.
func IsNotNull(field string) *IsNullExpr {
	return &IsNullExpr{
		Selector: parseFieldSelector(field),
		Not:      true,
	}
}
