package aql

// Builder provides a fluent API for constructing AQL queries programmatically.
// Use Select() to start building a query.
//
// Example:
//
//	q := Select(Col("name"), Col("type")).
//	    From("nodes").
//	    Where(Eq("type", String("fs:file"))).
//	    OrderBy("name").
//	    Limit(10).
//	    Build()
type Builder struct {
	stmt *SelectStmt
}

// Select starts building a SELECT query with the given columns.
func Select(columns ...ColumnExpr) *Builder {
	cols := make([]Column, len(columns))
	for i, c := range columns {
		cols[i] = Column{Expr: c}
	}
	return &Builder{
		stmt: &SelectStmt{
			Columns: cols,
		},
	}
}

// SelectDistinct starts building a SELECT DISTINCT query.
func SelectDistinct(columns ...ColumnExpr) *Builder {
	b := Select(columns...)
	b.stmt.Distinct = true
	return b
}

// SelectStar starts building a SELECT * query.
func SelectStar() *Builder {
	return Select(&Star{})
}

// Distinct sets the DISTINCT flag on the query.
func (b *Builder) Distinct() *Builder {
	b.stmt.Distinct = true
	return b
}

// From sets the FROM clause to a table name ("nodes" or "edges").
func (b *Builder) From(table string) *Builder {
	b.stmt.From = &TableSource{Table: table}
	return b
}

// FromPattern sets the FROM clause to graph patterns.
func (b *Builder) FromPattern(patterns ...*Pattern) *Builder {
	b.stmt.From = &PatternSource{Patterns: patterns}
	return b
}

// Where sets the WHERE clause.
func (b *Builder) Where(expr Expression) *Builder {
	b.stmt.Where = expr
	return b
}

// GroupBy sets the GROUP BY clause.
func (b *Builder) GroupBy(selectors ...Selector) *Builder {
	b.stmt.GroupBy = selectors
	return b
}

// GroupByCol sets the GROUP BY clause using column name strings.
func (b *Builder) GroupByCol(columns ...string) *Builder {
	b.stmt.GroupBy = make([]Selector, len(columns))
	for i, c := range columns {
		b.stmt.GroupBy[i] = Selector{Parts: []string{c}}
	}
	return b
}

// Having sets the HAVING clause.
func (b *Builder) Having(expr Expression) *Builder {
	b.stmt.Having = expr
	return b
}

// OrderBy adds ORDER BY clauses (ascending by default).
func (b *Builder) OrderBy(columns ...string) *Builder {
	for _, c := range columns {
		b.stmt.OrderBy = append(b.stmt.OrderBy, OrderSpec{
			Expr:       &Selector{Parts: []string{c}},
			Descending: false,
		})
	}
	return b
}

// OrderByDesc adds descending ORDER BY clauses.
func (b *Builder) OrderByDesc(columns ...string) *Builder {
	for _, c := range columns {
		b.stmt.OrderBy = append(b.stmt.OrderBy, OrderSpec{
			Expr:       &Selector{Parts: []string{c}},
			Descending: true,
		})
	}
	return b
}

// OrderByExpr adds an ORDER BY clause with a custom expression.
func (b *Builder) OrderByExpr(expr ColumnExpr, desc bool) *Builder {
	b.stmt.OrderBy = append(b.stmt.OrderBy, OrderSpec{
		Expr:       expr,
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
// Column Expressions
// ----------------------------------------------------------------------------

// Col creates a selector for a column name.
func Col(parts ...string) *Selector {
	return &Selector{Parts: parts}
}

// Star creates a * selector.
func StarExpr() *Star {
	return &Star{}
}

// Count creates a COUNT(*) expression.
func Count() *CountCall {
	return &CountCall{}
}

// As wraps an expression with an alias and returns a Column.
func As(expr ColumnExpr, alias string) Column {
	return Column{Expr: expr, Alias: alias}
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
// Node Pattern Builders
// ----------------------------------------------------------------------------

// N creates a node pattern with just a variable.
func N(variable string) *NodePattern {
	return &NodePattern{Variable: variable}
}

// NodeType creates a node pattern with variable and type.
func NodeType(variable, nodeType string) *NodePattern {
	return &NodePattern{Variable: variable, Type: nodeType}
}

// AnyNode creates an anonymous node pattern (no variable, no type).
func AnyNode() *NodePattern {
	return &NodePattern{}
}

// AnyNodeOfType creates an anonymous node pattern with a type.
func AnyNodeOfType(nodeType string) *NodePattern {
	return &NodePattern{Type: nodeType}
}

// WithWhere adds an inline WHERE clause to a node pattern.
func (n *NodePattern) WithWhere(expr Expression) *NodePattern {
	n.Where = expr
	return n
}

// ----------------------------------------------------------------------------
// Edge Pattern Builders
// ----------------------------------------------------------------------------

// Edge creates an edge pattern with just a variable.
func Edge(variable string) *EdgePattern {
	return &EdgePattern{Variable: variable}
}

// EdgeType creates an edge pattern with variable and type.
func EdgeType(variable, edgeType string) *EdgePattern {
	return &EdgePattern{Variable: variable, Type: edgeType}
}

// AnyEdge creates an anonymous edge pattern (no variable, no type).
func AnyEdge() *EdgePattern {
	return &EdgePattern{}
}

// AnyEdgeOfType creates an anonymous edge pattern with a type.
func AnyEdgeOfType(edgeType string) *EdgePattern {
	return &EdgePattern{Type: edgeType}
}

// EdgeTypes creates an anonymous edge pattern matching any of the given types (OR logic).
// Example: EdgeTypes("contains", "has") matches [:contains|has]
func EdgeTypes(types ...string) *EdgePattern {
	return &EdgePattern{Types: types}
}

// WithTypes sets multiple edge types (OR logic) and returns the edge pattern for chaining.
// Example: Edge("e").WithTypes("contains", "has") matches [e:contains|has]
func (e *EdgePattern) WithTypes(types ...string) *EdgePattern {
	e.Types = types
	e.Type = "" // Clear single type when multi-type is set
	return e
}

// WithHops sets variable-length edge hops (min..max).
// Use -1 for unbounded max (e.g., VarHops(1, -1) for 1..* hops).
func (e *EdgePattern) WithHops(min, max int) *EdgePattern {
	e.MinHops = &min
	if max >= 0 {
		e.MaxHops = &max
	}
	return e
}

// WithMinHops sets minimum hops with unbounded maximum.
func (e *EdgePattern) WithMinHops(min int) *EdgePattern {
	e.MinHops = &min
	e.MaxHops = nil
	return e
}

// WithExactHops sets exact number of hops.
func (e *EdgePattern) WithExactHops(n int) *EdgePattern {
	e.MinHops = &n
	e.MaxHops = &n
	return e
}

// ----------------------------------------------------------------------------
// Expression Builders - Comparisons
// ----------------------------------------------------------------------------

// Eq creates an equality comparison: selector = value.
func Eq(field string, value Value) *ComparisonExpr {
	return &ComparisonExpr{
		Left:  Col(field),
		Op:    OpEq,
		Right: value,
	}
}

// Ne creates a not-equal comparison: selector != value.
func Ne(field string, value Value) *ComparisonExpr {
	return &ComparisonExpr{
		Left:  Col(field),
		Op:    OpNe,
		Right: value,
	}
}

// Lt creates a less-than comparison: selector < value.
func Lt(field string, value Value) *ComparisonExpr {
	return &ComparisonExpr{
		Left:  Col(field),
		Op:    OpLt,
		Right: value,
	}
}

// Le creates a less-than-or-equal comparison: selector <= value.
func Le(field string, value Value) *ComparisonExpr {
	return &ComparisonExpr{
		Left:  Col(field),
		Op:    OpLe,
		Right: value,
	}
}

// Gt creates a greater-than comparison: selector > value.
func Gt(field string, value Value) *ComparisonExpr {
	return &ComparisonExpr{
		Left:  Col(field),
		Op:    OpGt,
		Right: value,
	}
}

// Ge creates a greater-than-or-equal comparison: selector >= value.
func Ge(field string, value Value) *ComparisonExpr {
	return &ComparisonExpr{
		Left:  Col(field),
		Op:    OpGe,
		Right: value,
	}
}

// Like creates a LIKE comparison: selector LIKE value.
func Like(field string, value Value) *ComparisonExpr {
	return &ComparisonExpr{
		Left:  Col(field),
		Op:    OpLike,
		Right: value,
	}
}

// Glob creates a GLOB comparison: selector GLOB value.
func Glob(field string, value Value) *ComparisonExpr {
	return &ComparisonExpr{
		Left:  Col(field),
		Op:    OpGlob,
		Right: value,
	}
}

// ----------------------------------------------------------------------------
// Expression Builders - Boolean Logic
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
// Expression Builders - Special Expressions
// ----------------------------------------------------------------------------

// In creates an IN expression: selector IN (values...).
func In(field string, values ...Value) *InExpr {
	return &InExpr{
		Left:   Col(field),
		Values: values,
	}
}

// Between creates a BETWEEN expression: selector BETWEEN low AND high.
func Between(field string, low, high Value) *BetweenExpr {
	return &BetweenExpr{
		Left: Col(field),
		Low:  low,
		High: high,
	}
}

// IsNull creates an IS NULL expression.
func IsNull(field string) *IsNullExpr {
	return &IsNullExpr{
		Selector: Col(field),
		Not:      false,
	}
}

// IsNotNull creates an IS NOT NULL expression.
func IsNotNull(field string) *IsNullExpr {
	return &IsNullExpr{
		Selector: Col(field),
		Not:      true,
	}
}

// Exists creates an EXISTS expression.
func Exists(pattern *Pattern) *ExistsExpr {
	return &ExistsExpr{Not: false, Pattern: pattern}
}

// NotExists creates a NOT EXISTS expression.
func NotExists(pattern *Pattern) *ExistsExpr {
	return &ExistsExpr{Not: true, Pattern: pattern}
}

// ----------------------------------------------------------------------------
// Expression Builders - Label Operations
// ----------------------------------------------------------------------------

// ContainsAny creates a CONTAINS ANY label expression.
func ContainsAny(field string, labels ...Value) *LabelExpr {
	return &LabelExpr{
		Selector: Col(field),
		Op:       OpContainsAny,
		Labels:   labels,
	}
}

// ContainsAll creates a CONTAINS ALL label expression.
func ContainsAll(field string, labels ...Value) *LabelExpr {
	return &LabelExpr{
		Selector: Col(field),
		Op:       OpContainsAll,
		Labels:   labels,
	}
}

// NotContains creates a NOT CONTAINS label expression.
func NotContains(field string, labels ...Value) *LabelExpr {
	return &LabelExpr{
		Selector: Col(field),
		Op:       OpNotContains,
		Labels:   labels,
	}
}

// ----------------------------------------------------------------------------
// Value Builders
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
