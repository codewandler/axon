package aql

import "fmt"

// colType represents a column or field path (e.g., "type", "data.ext").
// It implements the Selectable interface so columns can be used in SELECT clauses.
type colType struct {
	parts []string
}

// Ensure colType implements Selectable
func (c colType) selectable() {}

// toSelector converts colType to AST Selector for query building.
func (c colType) toSelector() *Selector {
	return &Selector{Parts: c.parts}
}

// Field creates a sub-field accessor (for nested paths).
func (c colType) Field(name string) colType {
	parts := make([]string, len(c.parts)+1)
	copy(parts, c.parts)
	parts[len(c.parts)] = name
	return colType{parts: parts}
}

// Common node columns
var (
	ID         = colType{[]string{"id"}}
	Type       = colType{[]string{"type"}}
	Name       = colType{[]string{"name"}}
	URI        = colType{[]string{"uri"}}
	Labels     = colType{[]string{"labels"}}
	DataCol    = colType{[]string{"data"}} // The data JSON column itself (for NULL checks)
	Generation = colType{[]string{"generation"}}
	CreatedAt  = colType{[]string{"created_at"}}
)

// Edge-specific columns
var (
	FromID = colType{[]string{"from_id"}}
	ToID   = colType{[]string{"to_id"}}
)

// json_each output columns
var (
	Key = colType{[]string{"key"}}
	Val = colType{[]string{"value"}} // Val instead of Value to avoid conflict with AST Value interface
)

// dataAccessor provides access to JSON data fields.
type dataAccessor struct{}

// Data is the accessor for JSON data fields (data.ext, data.size, etc.)
var Data = dataAccessor{}

// Field creates a column reference for a data field.
// Example: Data.Field("ext") produces data.ext
func (dataAccessor) Field(name string) colType {
	return colType{[]string{"data", name}}
}

// Predefined common data fields
var (
	DataExt  = Data.Field("ext")
	DataSize = Data.Field("size")
	DataMode = Data.Field("mode")
)

// ----------------------------------------------------------------------------
// Auto-wrap helper for values
// ----------------------------------------------------------------------------

// wrapValue automatically converts Go values to AQL Value types.
func wrapValue(v any) Value {
	switch x := v.(type) {
	case string:
		return &StringLit{Value: x}
	case int:
		return &NumberLit{Value: float64(x), IsInt: true}
	case int64:
		return &NumberLit{Value: float64(x), IsInt: true}
	case float64:
		return &NumberLit{Value: x, IsInt: false}
	case bool:
		return &BoolLit{Value: x}
	case Value:
		return x
	case NodeTypeRef:
		return &StringLit{Value: x.name}
	case EdgeTypeRef:
		return &StringLit{Value: x.name}
	default:
		panic(fmt.Sprintf("unsupported value type: %T", v))
	}
}

// wrapValues converts a slice of any to a slice of Value.
func wrapValues(vals ...any) []Value {
	result := make([]Value, len(vals))
	for i, v := range vals {
		result[i] = wrapValue(v)
	}
	return result
}

// ----------------------------------------------------------------------------
// Expression methods on colType (auto-wrap values)
// ----------------------------------------------------------------------------

// Eq creates an equality comparison: column = value.
func (c colType) Eq(v any) Expression {
	return &ComparisonExpr{
		Left:  c.toSelector(),
		Op:    OpEq,
		Right: wrapValue(v),
	}
}

// Ne creates a not-equal comparison: column != value.
func (c colType) Ne(v any) Expression {
	return &ComparisonExpr{
		Left:  c.toSelector(),
		Op:    OpNe,
		Right: wrapValue(v),
	}
}

// Lt creates a less-than comparison: column < value.
func (c colType) Lt(v any) Expression {
	return &ComparisonExpr{
		Left:  c.toSelector(),
		Op:    OpLt,
		Right: wrapValue(v),
	}
}

// Le creates a less-than-or-equal comparison: column <= value.
func (c colType) Le(v any) Expression {
	return &ComparisonExpr{
		Left:  c.toSelector(),
		Op:    OpLe,
		Right: wrapValue(v),
	}
}

// Gt creates a greater-than comparison: column > value.
func (c colType) Gt(v any) Expression {
	return &ComparisonExpr{
		Left:  c.toSelector(),
		Op:    OpGt,
		Right: wrapValue(v),
	}
}

// Ge creates a greater-than-or-equal comparison: column >= value.
func (c colType) Ge(v any) Expression {
	return &ComparisonExpr{
		Left:  c.toSelector(),
		Op:    OpGe,
		Right: wrapValue(v),
	}
}

// Like creates a LIKE comparison: column LIKE pattern.
func (c colType) Like(pattern string) Expression {
	return &ComparisonExpr{
		Left:  c.toSelector(),
		Op:    OpLike,
		Right: &StringLit{Value: pattern},
	}
}

// Glob creates a GLOB comparison: column GLOB pattern.
func (c colType) Glob(pattern string) Expression {
	return &ComparisonExpr{
		Left:  c.toSelector(),
		Op:    OpGlob,
		Right: &StringLit{Value: pattern},
	}
}

// In creates an IN expression: column IN (values...).
func (c colType) In(vals ...any) Expression {
	return &InExpr{
		Left:   c.toSelector(),
		Values: wrapValues(vals...),
	}
}

// Between creates a BETWEEN expression: column BETWEEN low AND high.
func (c colType) Between(low, high any) Expression {
	return &BetweenExpr{
		Left: c.toSelector(),
		Low:  wrapValue(low),
		High: wrapValue(high),
	}
}

// IsNull creates an IS NULL expression.
func (c colType) IsNull() Expression {
	return &IsNullExpr{
		Selector: c.toSelector(),
		Not:      false,
	}
}

// IsNotNull creates an IS NOT NULL expression.
func (c colType) IsNotNull() Expression {
	return &IsNullExpr{
		Selector: c.toSelector(),
		Not:      true,
	}
}

// ContainsAny creates a CONTAINS ANY expression for label arrays.
func (c colType) ContainsAny(labels ...string) Expression {
	vals := make([]Value, len(labels))
	for i, l := range labels {
		vals[i] = &StringLit{Value: l}
	}
	return &LabelExpr{
		Selector: c.toSelector(),
		Op:       OpContainsAny,
		Labels:   vals,
	}
}

// ContainsAll creates a CONTAINS ALL expression for label arrays.
func (c colType) ContainsAll(labels ...string) Expression {
	vals := make([]Value, len(labels))
	for i, l := range labels {
		vals[i] = &StringLit{Value: l}
	}
	return &LabelExpr{
		Selector: c.toSelector(),
		Op:       OpContainsAll,
		Labels:   vals,
	}
}

// NotContains creates a NOT CONTAINS expression for label arrays.
func (c colType) NotContains(labels ...string) Expression {
	vals := make([]Value, len(labels))
	for i, l := range labels {
		vals[i] = &StringLit{Value: l}
	}
	return &LabelExpr{
		Selector: c.toSelector(),
		Op:       OpNotContains,
		Labels:   vals,
	}
}
