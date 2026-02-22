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
//	query, err := aql.Parse("SELECT * FROM nodes WHERE type = 'fs:file'")
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
// Execute a query against the storage:
//
//	result, err := storage.Query(ctx, query)
//	if err != nil {
//	    log.Fatal(err)
//	}
//
//	// Access results based on type
//	switch result.Type {
//	case graph.ResultTypeNodes:
//	    for _, node := range result.Nodes {
//	        fmt.Println(node.Name, node.Type)
//	    }
//	case graph.ResultTypeEdges:
//	    for _, edge := range result.Edges {
//	        fmt.Println(edge.Type, edge.From, edge.To)
//	    }
//	case graph.ResultTypeCounts:
//	    for key, count := range result.Counts {
//	        fmt.Printf("%s: %d\n", key, count)
//	    }
//	}
//
// Get query execution plan:
//
//	plan, err := storage.Explain(ctx, query)
//	if err != nil {
//	    log.Fatal(err)
//	}
//	fmt.Println("SQL:", plan.SQL)
//	fmt.Println("Plan:", plan.SQLitePlan)
//
// # Query Syntax
//
// AQL supports two types of sources in the FROM clause:
//
// Table queries - query the flat nodes or edges tables (Phase 1 - implemented):
//
//	SELECT * FROM nodes WHERE type = 'fs:file'
//	SELECT * FROM edges WHERE type = 'contains'
//	SELECT type, COUNT(*) FROM nodes GROUP BY type
//	SELECT name, type FROM nodes WHERE data.ext = 'go'
//
// Pattern queries - use graph pattern matching (Phase 2/3 - planned):
//
//	SELECT file FROM (dir:fs:dir)-[:contains]->(file:fs:file)
//	SELECT a, b FROM (a:fs:dir)-[:contains*1..3]->(b:fs:file)
//
// # JSON Field Access
//
// The data field can be queried using dot notation (Phase 1):
//
//	SELECT * FROM nodes WHERE data.ext = 'go'
//	SELECT * FROM nodes WHERE data.size > 1000
//	SELECT * FROM nodes WHERE data.mode BETWEEN 400 AND 500
//
// This compiles to efficient json_extract() calls in SQLite
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
// # Partial Field Selection
//
// When selecting specific columns instead of *, the result nodes/edges will
// only have those fields populated. Other fields will have zero values:
//
//	SELECT name, type FROM nodes WHERE type = 'fs:file'
//	// Returns nodes with only name and type populated
//	// node.ID == "", node.URI == "", etc.
//
// # Parameters
//
// Both named and positional parameters are supported (Phase 2 - planned):
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
//	        aql.Gt("data.size", aql.Int(1000)),  // dot notation works in builders
//	    )).
//	    OrderBy("name").
//	    Limit(10).
//	    Build()
//
// For JSON field access, use dot notation directly:
//
//	q := aql.SelectStar().
//	    From("nodes").
//	    Where(aql.And(
//	        aql.Eq("data.ext", aql.String("go")),
//	        aql.Between("data.size", aql.Int(100), aql.Int(1000)),
//	    )).
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
