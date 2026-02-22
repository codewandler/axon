package aql

// varRef represents a reference to a pattern variable.
// Use Var("name") to create references to pattern variables in WHERE clauses.
type varRef struct {
	name string
}

// Var creates a variable reference for use in WHERE clauses and SELECT.
// Example: Var("file").DataField("ext").Eq("go")
func Var(name string) varRef {
	return varRef{name: name}
}

// Implements Selectable so variables can be selected
func (v varRef) selectable() {}

// toSelector converts varRef to *Selector for the AST.
func (v varRef) toSelector() *Selector {
	return &Selector{Parts: []string{v.name}}
}

// Field creates a column reference for a variable's field.
// Example: Var("file").Field("data", "ext") → file.data.ext
func (v varRef) Field(parts ...string) colType {
	allParts := make([]string, len(parts)+1)
	allParts[0] = v.name
	copy(allParts[1:], parts)
	return colType{parts: allParts}
}

// DataField is shorthand for accessing data.X on a variable.
// Example: Var("file").DataField("ext") → file.data.ext
func (v varRef) DataField(name string) colType {
	return colType{parts: []string{v.name, "data", name}}
}

// Col returns just the variable as a column (for SELECT variable).
func (v varRef) Col() colType {
	return colType{parts: []string{v.name}}
}
