package aql

import "fmt"

// ValidationError represents a validation error with position information.
type ValidationError struct {
	Position Position
	Message  string
}

// Error implements the error interface.
func (e *ValidationError) Error() string {
	if e.Position.Line > 0 {
		return fmt.Sprintf("%d:%d: %s", e.Position.Line, e.Position.Column, e.Message)
	}
	return e.Message
}

// Validate checks the AST for semantic errors and returns a list of validation errors.
// If the returned slice is empty, the query is valid.
func Validate(q *Query) []*ValidationError {
	v := &validator{
		definedVars: make(map[string]Position),
	}
	v.validateQuery(q)
	return v.errors
}

type validator struct {
	errors      []*ValidationError
	definedVars map[string]Position // variables defined in patterns
	inPattern   bool                // whether we're validating inside a pattern context
}

func (v *validator) addError(pos Position, format string, args ...any) {
	v.errors = append(v.errors, &ValidationError{
		Position: pos,
		Message:  fmt.Sprintf(format, args...),
	})
}

func (v *validator) validateQuery(q *Query) {
	if q == nil {
		return
	}
	if q.Select == nil {
		v.addError(q.Position, "query must have a SELECT statement")
		return
	}
	v.validateSelect(q.Select)
}

func (v *validator) validateSelect(s *SelectStmt) {
	// Must have FROM
	if s.From == nil {
		v.addError(s.Position, "SELECT requires FROM clause")
		return
	}

	// Validate source
	switch src := s.From.(type) {
	case *TableSource:
		if src.Table != "nodes" && src.Table != "edges" {
			v.addError(src.Position, "FROM must be 'nodes', 'edges', or a pattern, got '%s'", src.Table)
		}
	case *PatternSource:
		v.validatePatternSource(src)
		v.inPattern = true
	}

	// Must have at least one column
	if len(s.Columns) == 0 {
		v.addError(s.Position, "SELECT requires at least one column")
	}

	// Validate column references (for patterns)
	if v.inPattern {
		for _, col := range s.Columns {
			v.validateColumnExpr(col.Expr)
		}
	}

	// Validate WHERE
	if s.Where != nil {
		v.validateExpression(s.Where)
	}

	// HAVING requires GROUP BY
	if s.Having != nil && len(s.GroupBy) == 0 {
		v.addError(s.Position, "HAVING requires GROUP BY")
	}

	// Validate HAVING
	if s.Having != nil {
		v.validateExpression(s.Having)
	}

	// Validate ORDER BY
	for _, o := range s.OrderBy {
		v.validateColumnExpr(o.Expr)
	}
}

func (v *validator) validatePatternSource(ps *PatternSource) {
	if len(ps.Patterns) == 0 {
		v.addError(ps.Position, "pattern source must have at least one pattern")
		return
	}

	hasVariable := false
	for _, p := range ps.Patterns {
		if v.validatePattern(p) {
			hasVariable = true
		}
	}

	// At least one variable is required for selection
	if !hasVariable {
		v.addError(ps.Position, "pattern must bind at least one variable for selection")
	}
}

func (v *validator) validatePattern(p *Pattern) bool {
	hasVariable := false

	// Track variables defined within THIS pattern only (for duplicate detection)
	patternVars := make(map[string]Position)

	for _, elem := range p.Elements {
		switch e := elem.(type) {
		case *NodePattern:
			if e.Variable != "" {
				// Check for duplicate within the same pattern
				if prev, exists := patternVars[e.Variable]; exists {
					v.addError(e.Position, "variable '%s' already defined at %d:%d", e.Variable, prev.Line, prev.Column)
				} else {
					patternVars[e.Variable] = e.Position
					// Also record in definedVars if not already present (for cross-pattern JOINs)
					if _, exists := v.definedVars[e.Variable]; !exists {
						v.definedVars[e.Variable] = e.Position
					}
				}
				hasVariable = true
			}
			if e.Where != nil {
				v.validateExpression(e.Where)
			}
		case *EdgePattern:
			if e.Variable != "" {
				// Check for duplicate within the same pattern
				if prev, exists := patternVars[e.Variable]; exists {
					v.addError(e.Position, "variable '%s' already defined at %d:%d", e.Variable, prev.Line, prev.Column)
				} else {
					patternVars[e.Variable] = e.Position
					// Also record in definedVars if not already present
					if _, exists := v.definedVars[e.Variable]; !exists {
						v.definedVars[e.Variable] = e.Position
					}
					hasVariable = true
				}
			}
		}
	}

	return hasVariable
}

func (v *validator) validateColumnExpr(expr ColumnExpr) {
	switch e := expr.(type) {
	case *Selector:
		v.validateSelector(e)
	case *Star:
		// * is always valid
	case *CountCall:
		// COUNT(*) is always valid
	}
}

func (v *validator) validateSelector(s *Selector) {
	if !v.inPattern {
		return // No variable checking for flat queries
	}

	if len(s.Parts) == 0 {
		return
	}

	// For pattern queries, first part should be a defined variable
	// unless it's a special name like COUNT(*)
	varName := s.Parts[0]
	if varName == "*" || varName == "COUNT(*)" {
		return
	}

	if _, exists := v.definedVars[varName]; !exists {
		v.addError(s.Position, "undefined variable '%s'", varName)
	}
}

func (v *validator) validateExpression(expr Expression) {
	if expr == nil {
		return
	}

	switch e := expr.(type) {
	case *BinaryExpr:
		v.validateExpression(e.Left)
		v.validateExpression(e.Right)
	case *UnaryExpr:
		v.validateExpression(e.Operand)
	case *ComparisonExpr:
		v.validateSelector(e.Left)
	case *InExpr:
		v.validateSelector(e.Left)
	case *BetweenExpr:
		v.validateSelector(e.Left)
	case *LabelExpr:
		v.validateSelector(e.Selector)
	case *IsNullExpr:
		v.validateSelector(e.Selector)
	case *ExistsExpr:
		// EXISTS has its own scope - variables don't leak out
		// But it can reference outer variables
		v.validatePattern(e.Pattern)
	case *ParenExpr:
		v.validateExpression(e.Inner)
	case *Selector:
		v.validateSelector(e)
	}
}
