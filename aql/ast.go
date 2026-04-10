package aql

import (
	"fmt"
	"strings"
)

// Position tracks source location for error messages.
type Position struct {
	Line   int
	Column int
	Offset int
}

// String returns a human-readable position string.
func (p Position) String() string {
	if p.Line == 0 {
		return ""
	}
	return fmt.Sprintf("%d:%d", p.Line, p.Column)
}

// Node is the interface implemented by all AST nodes.
type Node interface {
	Pos() Position
}

// ----------------------------------------------------------------------------
// Top Level
// ----------------------------------------------------------------------------

// Query is the top-level AST node representing a complete AQL query.
type Query struct {
	Position Position
	Select   *SelectStmt
}

func (q *Query) Pos() Position { return q.Position }

// SelectStmt represents a SELECT query.
type SelectStmt struct {
	Position Position
	Distinct bool
	Columns  []Column
	From     Source
	Where    Expression
	GroupBy  []Selector
	Having   Expression
	OrderBy  []OrderSpec
	Limit    *int
	Offset   *int
}

func (s *SelectStmt) Pos() Position { return s.Position }

// ----------------------------------------------------------------------------
// Columns
// ----------------------------------------------------------------------------

// Column represents a single column in SELECT clause.
type Column struct {
	Position Position
	Expr     ColumnExpr
	Alias    string
}

func (c *Column) Pos() Position { return c.Position }

// ColumnExpr is an expression that can appear in SELECT columns.
type ColumnExpr interface {
	Node
	columnExpr()
}

// Star represents SELECT *.
type Star struct {
	Position Position
}

func (s *Star) Pos() Position { return s.Position }
func (*Star) columnExpr()     {}

// CountCall represents COUNT(*).
type CountCall struct {
	Position Position
}

func (c *CountCall) Pos() Position { return c.Position }
func (*CountCall) columnExpr()     {}
func (*CountCall) expr()           {} // CountCall can also be used in expressions (HAVING)

// Selector represents a field selector like "name", "data.ext", or "repo.name".
type Selector struct {
	Position Position
	Parts    []string
}

func (s *Selector) Pos() Position { return s.Position }
func (*Selector) columnExpr()     {}
func (*Selector) expr()           {} // Selector can appear in expressions

// String returns the dot-joined selector path.
func (s *Selector) String() string {
	return strings.Join(s.Parts, ".")
}

// ----------------------------------------------------------------------------
// Source
// ----------------------------------------------------------------------------

// Source represents the FROM clause source.
type Source interface {
	Node
	source()
}

// TableSource represents FROM nodes or FROM edges.
type TableSource struct {
	Position Position
	Table    string // "nodes" or "edges"
}

func (t *TableSource) Pos() Position { return t.Position }
func (*TableSource) source()         {}

// PatternSource represents FROM with graph patterns.
type PatternSource struct {
	Position Position
	Patterns []*Pattern
}

func (p *PatternSource) Pos() Position { return p.Position }
func (*PatternSource) source()         {}

// JoinedTableSource represents FROM table, table_func(column) (implicit cross join).
// This is used for table-valued functions like json_each.
type JoinedTableSource struct {
	Position  Position
	Table     string     // Base table: "nodes" or "edges"
	TableFunc *TableFunc // Table-valued function to join with
}

func (j *JoinedTableSource) Pos() Position { return j.Position }
func (*JoinedTableSource) source()         {}

// TableFunc represents a table-valued function call like json_each(labels).
type TableFunc struct {
	Position Position
	Name     string    // Function name: "json_each"
	Arg      *Selector // Argument: column or data.field
	Alias    string    // Optional alias for the result
}

func (t *TableFunc) Pos() Position { return t.Position }

// ----------------------------------------------------------------------------
// Pattern
// ----------------------------------------------------------------------------

// Pattern represents a graph pattern like (a)-[:edge]->(b).
type Pattern struct {
	Position Position
	Elements []PatternElement
}

func (p *Pattern) Pos() Position { return p.Position }

// PatternElement is either a NodePattern or EdgePattern.
type PatternElement interface {
	Node
	patternElement()
}

// NodePattern represents a node in a pattern: (variable:type WHERE expr).
type NodePattern struct {
	Position Position
	Variable string     // optional (empty if not bound)
	Type     string     // optional, supports glob (fs:*, empty = any)
	Where    Expression // optional inline predicate
}

func (n *NodePattern) Pos() Position { return n.Position }
func (*NodePattern) patternElement() {}

// EdgePattern represents an edge in a pattern: -[:type*min..max]->.
type EdgePattern struct {
	Position  Position
	Variable  string    // optional
	Type      string    // single type, optional, supports glob (*, con*)
	Types     []string  // multiple types with OR logic (e.g., [:contains|has])
	Direction Direction // Outgoing, Incoming, Undirected
	MinHops   *int      // nil = 1 (single hop)
	MaxHops   *int      // nil = unbounded (only valid when MinHops != nil)
}

func (e *EdgePattern) Pos() Position { return e.Position }
func (*EdgePattern) patternElement() {}

// IsVariableLength returns true if this is a variable-length edge pattern.
func (e *EdgePattern) IsVariableLength() bool {
	return e.MinHops != nil
}

// HasMultipleTypes returns true if this edge matches multiple types (OR logic).
func (e *EdgePattern) HasMultipleTypes() bool {
	return len(e.Types) > 1
}

// AllTypes returns all edge types this pattern matches.
// Returns Types if set, otherwise returns a single-element slice with Type (or empty if no type).
func (e *EdgePattern) AllTypes() []string {
	if len(e.Types) > 0 {
		return e.Types
	}
	if e.Type != "" {
		return []string{e.Type}
	}
	return nil
}

// WithHops sets variable-length edge hops (min..max).
// Use -1 for unbounded max (e.g., WithHops(1, -1) for 1..* hops).
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

// Direction represents edge direction in a pattern.
type Direction int

const (
	Outgoing   Direction = iota // ->
	Incoming                    // <-
	Undirected                  // -
)

func (d Direction) String() string {
	switch d {
	case Outgoing:
		return "->"
	case Incoming:
		return "<-"
	case Undirected:
		return "-"
	default:
		return "?"
	}
}

// ----------------------------------------------------------------------------
// Order
// ----------------------------------------------------------------------------

// OrderSpec represents an ORDER BY item.
type OrderSpec struct {
	Position   Position
	Expr       ColumnExpr // Selector or CountCall
	Descending bool
}

func (o *OrderSpec) Pos() Position { return o.Position }

// ----------------------------------------------------------------------------
// Expressions
// ----------------------------------------------------------------------------

// Expression represents a boolean expression in WHERE/HAVING clauses.
type Expression interface {
	Node
	expr()
}

// BinaryExpr represents AND/OR expressions.
type BinaryExpr struct {
	Position Position
	Left     Expression
	Op       BinaryOp
	Right    Expression
}

func (b *BinaryExpr) Pos() Position { return b.Position }
func (*BinaryExpr) expr()           {}

// BinaryOp is the operator for binary expressions.
type BinaryOp int

const (
	OpAnd BinaryOp = iota
	OpOr
)

func (op BinaryOp) String() string {
	switch op {
	case OpAnd:
		return "AND"
	case OpOr:
		return "OR"
	default:
		return "?"
	}
}

// UnaryExpr represents NOT expressions.
type UnaryExpr struct {
	Position Position
	Op       UnaryOp
	Operand  Expression
}

func (u *UnaryExpr) Pos() Position { return u.Position }
func (*UnaryExpr) expr()           {}

// UnaryOp is the operator for unary expressions.
type UnaryOp int

const (
	OpNot UnaryOp = iota
)

func (op UnaryOp) String() string {
	switch op {
	case OpNot:
		return "NOT"
	default:
		return "?"
	}
}

// ComparisonExpr represents comparison expressions like a = b, a LIKE b.
type ComparisonExpr struct {
	Position Position
	Left     *Selector
	Op       ComparisonOp
	Right    Value
}

func (c *ComparisonExpr) Pos() Position { return c.Position }
func (*ComparisonExpr) expr()           {}

// ComparisonOp is the operator for comparisons.
type ComparisonOp int

const (
	OpEq      ComparisonOp = iota // =
	OpNe                          // !=
	OpLt                          // <
	OpLe                          // <=
	OpGt                          // >
	OpGe                          // >=
	OpLike                        // LIKE
	OpGlob                        // GLOB
	OpNotLike                     // NOT LIKE
	OpNotGlob                     // NOT GLOB
)

func (op ComparisonOp) String() string {
	switch op {
	case OpEq:
		return "="
	case OpNe:
		return "!="
	case OpLt:
		return "<"
	case OpLe:
		return "<="
	case OpGt:
		return ">"
	case OpGe:
		return ">="
	case OpLike:
		return "LIKE"
	case OpGlob:
		return "GLOB"
	case OpNotLike:
		return "NOT LIKE"
	case OpNotGlob:
		return "NOT GLOB"
	default:
		return "?"
	}
}

// InExpr represents field IN (value1, value2, ...) or field NOT IN (...).
// Exactly one of Values or Subquery is set.
type InExpr struct {
	Position Position
	Left     *Selector
	Values   []Value // set for IN ('a', 'b') — literal list
	Subquery *Query  // set for IN (SELECT ...) — subquery
	Not      bool    // NOT IN
}

func (i *InExpr) Pos() Position { return i.Position }
func (*InExpr) expr()           {}

// BetweenExpr represents field BETWEEN low AND high.
type BetweenExpr struct {
	Position Position
	Left     *Selector
	Low      Value
	High     Value
}

func (b *BetweenExpr) Pos() Position { return b.Position }
func (*BetweenExpr) expr()           {}

// LabelExpr represents label set operations.
type LabelExpr struct {
	Position Position
	Selector *Selector
	Op       LabelOp
	Labels   []Value
}

func (l *LabelExpr) Pos() Position { return l.Position }
func (*LabelExpr) expr()           {}

// LabelOp is the operator for label expressions.
type LabelOp int

const (
	OpContainsAny    LabelOp = iota // CONTAINS ANY
	OpContainsAll                   // CONTAINS ALL
	OpNotContains                   // NOT CONTAINS
	OpNotContainsAny                // NOT CONTAINS ANY
	OpNotContainsAll                // NOT CONTAINS ALL
)

func (op LabelOp) String() string {
	switch op {
	case OpContainsAny:
		return "CONTAINS ANY"
	case OpContainsAll:
		return "CONTAINS ALL"
	case OpNotContains:
		return "NOT CONTAINS"
	case OpNotContainsAny:
		return "NOT CONTAINS ANY"
	case OpNotContainsAll:
		return "NOT CONTAINS ALL"
	default:
		return "?"
	}
}

// IsNullExpr represents field IS [NOT] NULL.
type IsNullExpr struct {
	Position Position
	Selector *Selector
	Not      bool
}

func (i *IsNullExpr) Pos() Position { return i.Position }
func (*IsNullExpr) expr()           {}

// ExistsExpr represents [NOT] EXISTS pattern.
type ExistsExpr struct {
	Position Position
	Not      bool
	Pattern  *Pattern
}

func (e *ExistsExpr) Pos() Position { return e.Position }
func (*ExistsExpr) expr()           {}

// ParenExpr represents a parenthesized expression.
type ParenExpr struct {
	Position Position
	Inner    Expression
}

func (p *ParenExpr) Pos() Position { return p.Position }
func (*ParenExpr) expr()           {}

// ----------------------------------------------------------------------------
// Values
// ----------------------------------------------------------------------------

// Value represents a literal value or parameter.
type Value interface {
	Node
	value()
}

// StringLit represents a string literal.
type StringLit struct {
	Position Position
	Value    string
}

func (s *StringLit) Pos() Position { return s.Position }
func (*StringLit) value()          {}

// NumberLit represents a numeric literal.
type NumberLit struct {
	Position Position
	Value    float64
	IsInt    bool
}

func (n *NumberLit) Pos() Position { return n.Position }
func (*NumberLit) value()          {}

// IntValue returns the integer value if IsInt is true.
func (n *NumberLit) IntValue() int {
	return int(n.Value)
}

// BoolLit represents a boolean literal.
type BoolLit struct {
	Position Position
	Value    bool
}

func (b *BoolLit) Pos() Position { return b.Position }
func (*BoolLit) value()          {}

// Parameter represents a query parameter ($name or $1).
type Parameter struct {
	Position Position
	Name     string // for $name (empty if positional)
	Index    int    // for $1 (1-based, 0 if named)
}

func (p *Parameter) Pos() Position { return p.Position }
func (*Parameter) value()          {}

// IsNamed returns true if this is a named parameter.
func (p *Parameter) IsNamed() bool {
	return p.Name != ""
}

// IsPositional returns true if this is a positional parameter.
func (p *Parameter) IsPositional() bool {
	return p.Index > 0
}

// String returns the parameter as it would appear in a query.
func (p *Parameter) String() string {
	if p.IsNamed() {
		return "$" + p.Name
	}
	return fmt.Sprintf("$%d", p.Index)
}
