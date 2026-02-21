// Package aql provides a parser and AST for the Axon Query Language (AQL).
//
// AQL is a SQL-like query language designed for the Axon graph database.
// It supports both flat table queries (on nodes/edges tables) and graph pattern
// matching using Cypher-like syntax.
//
// # Basic Usage
//
// Parse a query string into an AST:
//
//	p := aql.NewParser()
//	query, err := p.Parse("SELECT * FROM nodes WHERE type = 'fs:file'")
//	if err != nil {
//	    log.Fatal(err)
//	}
//
// Validate the AST for semantic errors:
//
//	errs := aql.Validate(query)
//	if len(errs) > 0 {
//	    for _, e := range errs {
//	        log.Printf("validation error: %s", e)
//	    }
//	}
//
// # Query Syntax
//
// AQL supports two types of sources in the FROM clause:
//
// Table queries - query the flat nodes or edges tables:
//
//	SELECT * FROM nodes WHERE type = 'fs:file'
//	SELECT * FROM edges WHERE type = 'contains'
//
// Pattern queries - use graph pattern matching:
//
//	SELECT file FROM (dir:fs:dir)-[:contains]->(file:fs:file)
//	SELECT a, b FROM (a:fs:dir)-[:contains*1..3]->(b:fs:file)
//
// # Pattern Matching
//
// Patterns follow Cypher-like syntax:
//
//	(variable:type)              - node pattern
//	-[:type]->                   - outgoing edge
//	<-[:type]-                   - incoming edge
//	-[:type]-                    - undirected edge
//	-[:type*min..max]->          - variable-length path
//
// Multiple patterns are comma-separated and share variables (implicit JOIN):
//
//	SELECT a, c FROM (a)-[:x]->(b), (b)-[:y]->(c)
//
// # Expressions
//
// WHERE and HAVING clauses support:
//
//	field = value               - equality
//	field != value              - inequality
//	field < <= > >=             - comparisons
//	field LIKE pattern          - pattern matching
//	field GLOB pattern          - glob matching
//	field IN (v1, v2, ...)      - set membership
//	field BETWEEN a AND b       - range
//	field IS NULL               - null check
//	field IS NOT NULL           - not null check
//	labels CONTAINS ANY (...)   - label set operations
//	labels CONTAINS ALL (...)
//	labels NOT CONTAINS (...)
//	EXISTS pattern              - subquery existence
//	NOT EXISTS pattern
//	expr AND expr               - boolean AND
//	expr OR expr                - boolean OR
//	NOT expr                    - boolean NOT
//	(expr)                      - grouping
//
// # Parameters
//
// Both named and positional parameters are supported:
//
//	SELECT * FROM nodes WHERE type = $type      -- named
//	SELECT * FROM nodes WHERE type = $1         -- positional
//
// # Builder API
//
// For programmatic AST construction, use the fluent builder:
//
//	q := aql.Select(aql.Col("name"), aql.Col("type")).
//	    From("nodes").
//	    Where(aql.And(
//	        aql.Eq("type", aql.String("fs:file")),
//	        aql.Gt("size", aql.Int(1000)),
//	    )).
//	    OrderBy("name").
//	    Limit(10).
//	    Build()
//
// Pattern queries with the builder:
//
//	pattern := aql.Pat(aql.NodeType("dir", "fs:dir")).
//	    To(aql.AnyEdgeOfType("contains"), aql.NodeType("file", "fs:file")).
//	    Build()
//
//	q := aql.Select(aql.Col("file")).
//	    FromPattern(pattern).
//	    Build()
package aql
