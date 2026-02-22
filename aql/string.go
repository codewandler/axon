package aql

import (
	"fmt"
	"strings"
)

// ----------------------------------------------------------------------------
// String() Methods for AQL Serialization
// ----------------------------------------------------------------------------

// String returns the AQL query as a string.
func (q *Query) String() string {
	if q.Select == nil {
		return ""
	}
	return q.Select.String()
}

// String returns the SELECT statement as AQL syntax.
func (s *SelectStmt) String() string {
	var parts []string

	// SELECT [DISTINCT] columns
	selectPart := "SELECT"
	if s.Distinct {
		selectPart += " DISTINCT"
	}

	// Columns
	var cols []string
	for _, col := range s.Columns {
		cols = append(cols, col.String())
	}
	selectPart += " " + strings.Join(cols, ", ")
	parts = append(parts, selectPart)

	// FROM
	if s.From != nil {
		parts = append(parts, "FROM "+sourceString(s.From))
	}

	// WHERE
	if s.Where != nil {
		parts = append(parts, "WHERE "+expressionString(s.Where))
	}

	// GROUP BY
	if len(s.GroupBy) > 0 {
		var groupCols []string
		for _, sel := range s.GroupBy {
			groupCols = append(groupCols, sel.String())
		}
		parts = append(parts, "GROUP BY "+strings.Join(groupCols, ", "))
	}

	// HAVING
	if s.Having != nil {
		parts = append(parts, "HAVING "+expressionString(s.Having))
	}

	// ORDER BY
	if len(s.OrderBy) > 0 {
		var orderCols []string
		for _, spec := range s.OrderBy {
			orderCols = append(orderCols, spec.String())
		}
		parts = append(parts, "ORDER BY "+strings.Join(orderCols, ", "))
	}

	// LIMIT
	if s.Limit != nil {
		parts = append(parts, fmt.Sprintf("LIMIT %d", *s.Limit))
	}

	// OFFSET
	if s.Offset != nil {
		parts = append(parts, fmt.Sprintf("OFFSET %d", *s.Offset))
	}

	return strings.Join(parts, " ")
}

// String returns the column as AQL syntax.
func (c *Column) String() string {
	s := columnExprString(c.Expr)
	if c.Alias != "" {
		s += " AS " + c.Alias
	}
	return s
}

// columnExprString returns the string representation of a ColumnExpr.
func columnExprString(expr ColumnExpr) string {
	switch e := expr.(type) {
	case *Star:
		return e.String()
	case *CountCall:
		return e.String()
	case *Selector:
		return e.String()
	default:
		return "?"
	}
}

// sourceString returns the string representation of a Source.
func sourceString(src Source) string {
	switch s := src.(type) {
	case *TableSource:
		return s.String()
	case *PatternSource:
		return s.String()
	default:
		return "?"
	}
}

// expressionString returns the string representation of an Expression.
func expressionString(expr Expression) string {
	switch e := expr.(type) {
	case *BinaryExpr:
		return e.String()
	case *UnaryExpr:
		return e.String()
	case *ComparisonExpr:
		return e.String()
	case *InExpr:
		return e.String()
	case *BetweenExpr:
		return e.String()
	case *LabelExpr:
		return e.String()
	case *IsNullExpr:
		return e.String()
	case *ExistsExpr:
		return e.String()
	case *ParenExpr:
		return e.String()
	case *Selector:
		return e.String()
	case *CountCall:
		return e.String()
	default:
		return "?"
	}
}

// valueString returns the string representation of a Value.
func valueString(val Value) string {
	switch v := val.(type) {
	case *StringLit:
		return v.String()
	case *NumberLit:
		return v.String()
	case *BoolLit:
		return v.String()
	case *Parameter:
		return v.String()
	default:
		return "?"
	}
}

// String for Star.
func (s *Star) String() string {
	return "*"
}

// String for CountCall.
func (c *CountCall) String() string {
	return "COUNT(*)"
}

// String for TableSource.
func (t *TableSource) String() string {
	return t.Table
}

// String for PatternSource.
func (p *PatternSource) String() string {
	var patterns []string
	for _, pat := range p.Patterns {
		patterns = append(patterns, pat.String())
	}
	return strings.Join(patterns, ", ")
}

// String for Pattern.
func (p *Pattern) String() string {
	var parts []string
	for _, elem := range p.Elements {
		switch e := elem.(type) {
		case *NodePattern:
			parts = append(parts, e.String())
		case *EdgePattern:
			parts = append(parts, e.String())
		}
	}
	return strings.Join(parts, "")
}

// String for NodePattern.
func (n *NodePattern) String() string {
	var parts []string

	// Variable
	if n.Variable != "" {
		parts = append(parts, n.Variable)
	}

	// Type
	if n.Type != "" {
		if n.Variable != "" {
			parts = append(parts, ":"+n.Type)
		} else {
			parts = append(parts, ":"+n.Type)
		}
	}

	// WHERE
	if n.Where != nil {
		wherePart := " WHERE " + expressionString(n.Where)
		if len(parts) > 0 {
			parts = append(parts, wherePart)
		} else {
			parts = append(parts, strings.TrimSpace(wherePart))
		}
	}

	content := strings.Join(parts, "")
	return "(" + content + ")"
}

// String for EdgePattern.
func (e *EdgePattern) String() string {
	var parts []string

	// Left arrow for incoming
	if e.Direction == Incoming {
		parts = append(parts, "<")
	}

	parts = append(parts, "-")

	// Edge spec [variable:type(s)*hops]
	if e.Variable != "" || e.Type != "" || len(e.Types) > 0 || e.IsVariableLength() {
		edgeSpec := "["

		if e.Variable != "" {
			edgeSpec += e.Variable
		}

		if e.Type != "" {
			edgeSpec += ":" + e.Type
		} else if len(e.Types) > 0 {
			edgeSpec += ":" + strings.Join(e.Types, "|")
		}

		// Variable length
		if e.IsVariableLength() {
			edgeSpec += "*"
			if e.MinHops != nil {
				edgeSpec += fmt.Sprintf("%d", *e.MinHops)
			}
			if e.MaxHops != nil {
				edgeSpec += fmt.Sprintf("..%d", *e.MaxHops)
			} else if e.MinHops != nil {
				edgeSpec += ".."
			}
		}

		edgeSpec += "]"
		parts = append(parts, edgeSpec)
	}

	parts = append(parts, "-")

	// Right arrow for outgoing
	if e.Direction == Outgoing {
		parts = append(parts, ">")
	}

	return strings.Join(parts, "")
}

// String for OrderSpec.
func (o *OrderSpec) String() string {
	s := columnExprString(o.Expr)
	if o.Descending {
		s += " DESC"
	}
	return s
}

// String for BinaryExpr (AND/OR).
func (b *BinaryExpr) String() string {
	return fmt.Sprintf("%s %s %s", expressionString(b.Left), b.Op.String(), expressionString(b.Right))
}

// String for UnaryExpr (NOT).
func (u *UnaryExpr) String() string {
	return fmt.Sprintf("%s %s", u.Op.String(), expressionString(u.Operand))
}

// String for ComparisonExpr (=, !=, LIKE, GLOB, etc.).
func (c *ComparisonExpr) String() string {
	return fmt.Sprintf("%s %s %s", c.Left.String(), c.Op.String(), valueString(c.Right))
}

// String for InExpr.
func (i *InExpr) String() string {
	var values []string
	for _, v := range i.Values {
		values = append(values, valueString(v))
	}
	return fmt.Sprintf("%s IN (%s)", i.Left.String(), strings.Join(values, ", "))
}

// String for BetweenExpr.
func (b *BetweenExpr) String() string {
	return fmt.Sprintf("%s BETWEEN %s AND %s", b.Left.String(), valueString(b.Low), valueString(b.High))
}

// String for LabelExpr.
func (l *LabelExpr) String() string {
	var labels []string
	for _, v := range l.Labels {
		labels = append(labels, valueString(v))
	}
	return fmt.Sprintf("%s %s (%s)", l.Selector.String(), l.Op.String(), strings.Join(labels, ", "))
}

// String for IsNullExpr.
func (i *IsNullExpr) String() string {
	if i.Not {
		return fmt.Sprintf("%s IS NOT NULL", i.Selector.String())
	}
	return fmt.Sprintf("%s IS NULL", i.Selector.String())
}

// String for ExistsExpr.
func (e *ExistsExpr) String() string {
	if e.Not {
		return fmt.Sprintf("NOT EXISTS %s", e.Pattern.String())
	}
	return fmt.Sprintf("EXISTS %s", e.Pattern.String())
}

// String for ParenExpr.
func (p *ParenExpr) String() string {
	return fmt.Sprintf("(%s)", expressionString(p.Inner))
}

// String for StringLit.
func (s *StringLit) String() string {
	// Escape single quotes by doubling them
	escaped := strings.ReplaceAll(s.Value, "'", "''")
	return fmt.Sprintf("'%s'", escaped)
}

// String for NumberLit.
func (n *NumberLit) String() string {
	if n.IsInt {
		return fmt.Sprintf("%d", n.IntValue())
	}
	return fmt.Sprintf("%g", n.Value)
}

// String for BoolLit.
func (b *BoolLit) String() string {
	if b.Value {
		return "TRUE"
	}
	return "FALSE"
}
