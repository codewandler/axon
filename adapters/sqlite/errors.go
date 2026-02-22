package sqlite

import (
	"fmt"

	"github.com/codewandler/axon/aql"
)

// QueryError wraps compilation/execution errors with the AQL query context.
// This provides better debugging information by showing which query failed and at what phase.
type QueryError struct {
	Query *aql.Query // Original AQL query AST
	Phase string     // "validate", "compile", or "execute"
	Err   error      // Underlying error
}

// Error implements the error interface.
func (e *QueryError) Error() string {
	// TODO: When aql.Query.String() is implemented, use it for better error messages:
	// return fmt.Sprintf("%s error: %v\nQuery: %s", e.Phase, e.Err, e.Query.String())
	return fmt.Sprintf("%s error: %v", e.Phase, e.Err)
}

// Unwrap returns the underlying error for errors.Is/As support.
func (e *QueryError) Unwrap() error {
	return e.Err
}
