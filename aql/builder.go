package aql

// Builder provides a fluent API for constructing AQL queries programmatically.
// Use Nodes.Select() or Edges.Select() to start building a query.
//
// Example:
//
//	q := aql.Nodes.
//	    Select(aql.Type, aql.Count()).
//	    Where(aql.Type.Eq(aql.NodeType.File)).
//	    GroupBy(aql.Type).
//	    Build()
type Builder struct {
	stmt *SelectStmt
}

// Where sets the WHERE clause.
func (b *Builder) Where(expr Expression) *Builder {
	b.stmt.Where = expr
	return b
}

// GroupBy sets the GROUP BY clause.
func (b *Builder) GroupBy(cols ...colType) *Builder {
	b.stmt.GroupBy = make([]Selector, len(cols))
	for i, col := range cols {
		b.stmt.GroupBy[i] = *col.toSelector()
	}
	return b
}

// Having sets the HAVING clause.
func (b *Builder) Having(expr Expression) *Builder {
	b.stmt.Having = expr
	return b
}

// OrderBy adds ORDER BY clauses (ascending by default).
func (b *Builder) OrderBy(cols ...colType) *Builder {
	for _, col := range cols {
		b.stmt.OrderBy = append(b.stmt.OrderBy, OrderSpec{
			Expr:       col.toSelector(),
			Descending: false,
		})
	}
	return b
}

// OrderByDesc adds descending ORDER BY clauses.
func (b *Builder) OrderByDesc(cols ...colType) *Builder {
	for _, col := range cols {
		b.stmt.OrderBy = append(b.stmt.OrderBy, OrderSpec{
			Expr:       col.toSelector(),
			Descending: true,
		})
	}
	return b
}

// OrderByCount adds ORDER BY COUNT(*) clause.
func (b *Builder) OrderByCount(desc bool) *Builder {
	b.stmt.OrderBy = append(b.stmt.OrderBy, OrderSpec{
		Expr:       &CountCall{},
		Descending: desc,
	})
	return b
}

// Limit sets the LIMIT clause.
func (b *Builder) Limit(n int) *Builder {
	b.stmt.Limit = &n
	return b
}

// Offset sets the OFFSET clause.
func (b *Builder) Offset(n int) *Builder {
	b.stmt.Offset = &n
	return b
}

// Build returns the completed Query.
func (b *Builder) Build() *Query {
	return &Query{Select: b.stmt}
}

// ----------------------------------------------------------------------------
// Pattern Building
// ----------------------------------------------------------------------------

// PatternBuilder helps construct graph patterns.
type PatternBuilder struct {
	elements []PatternElement
}

// Pat starts building a pattern with an initial node.
func Pat(node *NodePattern) *PatternBuilder {
	return &PatternBuilder{
		elements: []PatternElement{node},
	}
}

// To adds an outgoing edge and target node to the pattern.
func (p *PatternBuilder) To(edge *EdgePattern, node *NodePattern) *PatternBuilder {
	edge.Direction = Outgoing
	p.elements = append(p.elements, edge, node)
	return p
}

// From adds an incoming edge and source node to the pattern.
func (p *PatternBuilder) From(edge *EdgePattern, node *NodePattern) *PatternBuilder {
	edge.Direction = Incoming
	p.elements = append(p.elements, edge, node)
	return p
}

// Either adds an undirected edge and adjacent node to the pattern.
func (p *PatternBuilder) Either(edge *EdgePattern, node *NodePattern) *PatternBuilder {
	edge.Direction = Undirected
	p.elements = append(p.elements, edge, node)
	return p
}

// Build returns the completed Pattern.
func (p *PatternBuilder) Build() *Pattern {
	return &Pattern{Elements: p.elements}
}

// ----------------------------------------------------------------------------
// Boolean Logic
// ----------------------------------------------------------------------------

// And combines expressions with AND.
func And(exprs ...Expression) Expression {
	if len(exprs) == 0 {
		return nil
	}
	if len(exprs) == 1 {
		return exprs[0]
	}
	result := exprs[0]
	for _, e := range exprs[1:] {
		result = &BinaryExpr{Left: result, Op: OpAnd, Right: e}
	}
	return result
}

// Or combines expressions with OR.
func Or(exprs ...Expression) Expression {
	if len(exprs) == 0 {
		return nil
	}
	if len(exprs) == 1 {
		return exprs[0]
	}
	result := exprs[0]
	for _, e := range exprs[1:] {
		result = &BinaryExpr{Left: result, Op: OpOr, Right: e}
	}
	return result
}

// Not negates an expression.
func Not(expr Expression) *UnaryExpr {
	return &UnaryExpr{Op: OpNot, Operand: expr}
}

// Paren wraps an expression in parentheses.
func Paren(expr Expression) *ParenExpr {
	return &ParenExpr{Inner: expr}
}

// ----------------------------------------------------------------------------
// Special Expressions
// ----------------------------------------------------------------------------

// Exists creates an EXISTS expression.
func Exists(pattern *Pattern) *ExistsExpr {
	return &ExistsExpr{Not: false, Pattern: pattern}
}

// NotExists creates a NOT EXISTS expression.
func NotExists(pattern *Pattern) *ExistsExpr {
	return &ExistsExpr{Not: true, Pattern: pattern}
}

// ----------------------------------------------------------------------------
// Value Builders (kept for direct use if needed)
// ----------------------------------------------------------------------------

// String creates a string literal value.
func String(s string) *StringLit {
	return &StringLit{Value: s}
}

// Int creates an integer literal value.
func Int(n int) *NumberLit {
	return &NumberLit{Value: float64(n), IsInt: true}
}

// Float creates a float literal value.
func Float(f float64) *NumberLit {
	return &NumberLit{Value: f, IsInt: false}
}

// Bool creates a boolean literal value.
func Bool(b bool) *BoolLit {
	return &BoolLit{Value: b}
}

// True creates a true boolean literal.
func True() *BoolLit {
	return &BoolLit{Value: true}
}

// False creates a false boolean literal.
func False() *BoolLit {
	return &BoolLit{Value: false}
}

// Param creates a named parameter ($name).
func Param(name string) *Parameter {
	return &Parameter{Name: name}
}

// ParamN creates a positional parameter ($1, $2, etc.).
func ParamN(index int) *Parameter {
	return &Parameter{Index: index}
}
