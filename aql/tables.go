package aql

// Selectable is implemented by types that can appear in SELECT clauses.
// This includes Column, CountCall, and VarRef.
type Selectable interface {
	selectable()
}

// TableRef represents a queryable table (nodes or edges).
type TableRef struct {
	name string
}

// Table constants for querying nodes and edges tables.
var (
	Nodes = TableRef{"nodes"}
	Edges = TableRef{"edges"}
)

// Select starts building a SELECT query on this table with the given columns.
func (t TableRef) Select(cols ...Selectable) *Builder {
	columns := make([]Column, len(cols))
	for i, col := range cols {
		columns[i] = Column{Expr: convertToColumnExpr(col)}
	}

	return &Builder{
		stmt: &SelectStmt{
			Columns: columns,
			From:    &TableSource{Table: t.name},
		},
	}
}

// SelectStar starts building a SELECT * query on this table.
func (t TableRef) SelectStar() *Builder {
	return &Builder{
		stmt: &SelectStmt{
			Columns: []Column{{Expr: &Star{}}},
			From:    &TableSource{Table: t.name},
		},
	}
}

// SelectDistinct starts building a SELECT DISTINCT query on this table.
func (t TableRef) SelectDistinct(cols ...Selectable) *Builder {
	b := t.Select(cols...)
	b.stmt.Distinct = true
	return b
}

// JsonEach creates a joined query builder for unpacking JSON arrays.
// Example: Nodes.JsonEach(Labels) produces FROM nodes, json_each(labels)
func (t TableRef) JsonEach(col colType) *JoinedBuilder {
	return &JoinedBuilder{
		table:     t.name,
		tableFunc: &TableFunc{Name: "json_each", Arg: col.toSelector()},
	}
}

// ScopedTo creates an EXISTS expression for directory-scoped queries.
// This builds an optimized CTE+JOIN pattern for fast scoped counting.
// Example: Nodes.Select(Type, Count()).Where(Nodes.ScopedTo(rootID))
func (t TableRef) ScopedTo(nodeID string) Expression {
	return scopedTo(nodeID, t.name)
}

// JoinedBuilder builds queries with table-valued functions like json_each.
type JoinedBuilder struct {
	table     string
	tableFunc *TableFunc
}

// Select starts building a SELECT query on the joined table.
func (j *JoinedBuilder) Select(cols ...Selectable) *Builder {
	columns := make([]Column, len(cols))
	for i, col := range cols {
		columns[i] = Column{Expr: convertToColumnExpr(col)}
	}

	return &Builder{
		stmt: &SelectStmt{
			Columns: columns,
			From:    &JoinedTableSource{Table: j.table, TableFunc: j.tableFunc},
		},
	}
}

// SelectDistinct starts building a SELECT DISTINCT query on the joined table.
func (j *JoinedBuilder) SelectDistinct(cols ...Selectable) *Builder {
	b := j.Select(cols...)
	b.stmt.Distinct = true
	return b
}

// convertToColumnExpr converts a Selectable to ColumnExpr for the AST.
func convertToColumnExpr(s Selectable) ColumnExpr {
	switch v := s.(type) {
	case colType:
		return v.toSelector()
	case countCall:
		return v.toCountCall()
	case varRef:
		return v.toSelector()
	default:
		panic("unsupported Selectable type")
	}
}

// FromPattern starts building a query from graph patterns.
func FromPattern(patterns ...*Pattern) *Builder {
	return &Builder{
		stmt: &SelectStmt{
			From: &PatternSource{Patterns: patterns},
		},
	}
}
