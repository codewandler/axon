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
// Table queries - query the flat nodes or edges tables:
//
//	SELECT * FROM nodes WHERE type = 'fs:file'
//	SELECT * FROM edges WHERE type = 'contains'
//	SELECT type, COUNT(*) FROM nodes GROUP BY type
//	SELECT name, type FROM nodes WHERE data.ext = 'go'
//
// Pattern queries - use graph pattern matching:
//
//	SELECT file FROM (dir:fs:dir)-[:contains]->(file:fs:file)
//	SELECT branch FROM (repo:vcs:repo)-[:has]->(branch:vcs:branch)
//	SELECT e FROM (a)-[e:contains]->(b)
//	SELECT child FROM (parent)-[:contains|has]->(child)
//	SELECT repo FROM (branch:vcs:branch)<-[:has]-(repo:vcs:repo)
//	SELECT file FROM (dir)-[:contains]->(file) WHERE file.data.ext = 'go'
//	SELECT b FROM (a:fs:dir)-[:contains*1..3]->(b:fs:file)    -- variable-length paths
//	SELECT b FROM (a)-[:contains*2]->(b)                      -- exactly 2 hops
//	SELECT b FROM (a)-[:contains*2..]->(b)                    -- 2 or more hops (unbounded)
//
// # JSON Field Access
//
// The data field can be queried using dot notation:
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
//	-[:type1|type2]->            - multi-type edge
//	-[variable:type]->           - edge variable binding
//
// # Variable-Length Paths
//
// Recursive graph traversal using SQLite CTEs:
//
//	-[:type*min..max]->          - bounded variable-length
//	-[:type*n]->                 - exact hops
//	-[:type*min..]->             - unbounded
//
// Examples:
//
//	SELECT file FROM (dir)-[:contains*1..3]->(file)      -- 1 to 3 hops
//	SELECT node FROM (root)-[:contains*2]->(node)         -- exactly 2 hops
//	SELECT desc FROM (root)-[:contains*2..]->(desc)       -- 2 or more hops
//	SELECT n FROM (a)-[:has|contains*1..5]->(n)           -- multi-type with recursion
//
// WHERE clauses can reference pattern variables:
//
//	SELECT file FROM (dir:fs:dir)-[:contains]->(file:fs:file)
//	WHERE file.data.ext = 'go' AND dir.name = 'src'
//
// Multiple patterns are comma-separated and share variables (implicit JOIN):
//
//	SELECT file FROM (repo:vcs:repo)-[:located_at]->(dir:fs:dir), (dir)-[:contains]->(file:fs:file)
//
// ORDER BY and GROUP BY work with pattern variables:
//
//	SELECT file FROM (dir)-[:contains]->(file) ORDER BY file.name
//	SELECT dir.name, COUNT(*) FROM (dir)-[:contains]->(file) GROUP BY dir.name
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
//	// Basic pattern: (dir:fs:dir)-[:contains]->(file:fs:file)
//	pattern := aql.Pat(aql.NodeType("dir", "fs:dir")).
//	    To(aql.AnyEdgeOfType("contains"), aql.NodeType("file", "fs:file")).
//	    Build()
//	q := aql.Select(aql.Col("file")).
//	    FromPattern(pattern).
//	    Build()
//
//	// Edge variable: (a)-[e:contains]->(b)
//	pattern := aql.Pat(aql.N("a")).
//	    To(aql.EdgeType("e", "contains"), aql.N("b")).
//	    Build()
//	q := aql.Select(aql.Col("e")).FromPattern(pattern).Build()
//
//	// Multi-type edge: (parent)-[:contains|has]->(child)
//	pattern := aql.Pat(aql.N("parent")).
//	    To(aql.EdgeTypes("contains", "has"), aql.N("child")).
//	    Build()
//	q := aql.Select(aql.Col("child")).FromPattern(pattern).Build()
//
//	// Incoming edge: (branch:vcs:branch)<-[:has]-(repo:vcs:repo)
//	pattern := aql.Pat(aql.NodeType("branch", "vcs:branch")).
//	    From(aql.AnyEdgeOfType("has"), aql.NodeType("repo", "vcs:repo")).
//	    Build()
//	q := aql.Select(aql.Col("repo")).FromPattern(pattern).Build()
//
//	// Pattern with WHERE: file.data.ext = 'go'
//	pattern := aql.Pat(aql.NodeType("dir", "fs:dir")).
//	    To(aql.AnyEdgeOfType("contains"), aql.NodeType("file", "fs:file")).
//	    Build()
//	q := aql.Select(aql.Col("file")).
//	    FromPattern(pattern).
//	    Where(aql.Eq("file.data.ext", aql.String("go"))).  // dot notation for JSON fields
//	    Limit(10).
//	    Build()
//
//	// Undirected edge: (a)-[:references]-(b)
//	pattern := aql.Pat(aql.N("a")).
//	    Either(aql.AnyEdgeOfType("references"), aql.N("b")).
//	    Build()
//	q := aql.Select(aql.Col("a"), aql.Col("b")).FromPattern(pattern).Build()
//
//	// Multiple patterns: (repo)-[:located_at]->(dir), (dir)-[:contains]->(file)
//	p1 := aql.Pat(aql.NodeType("repo", "vcs:repo")).
//	    To(aql.AnyEdgeOfType("located_at"), aql.NodeType("dir", "fs:dir")).Build()
//	p2 := aql.Pat(aql.N("dir")).
//	    To(aql.AnyEdgeOfType("contains"), aql.NodeType("file", "fs:file")).Build()
//	q := aql.Select(aql.Col("file")).FromPattern(p1, p2).Build()
//
//	// Pattern with ORDER BY
//	pattern := aql.Pat(aql.NodeType("dir", "fs:dir")).
//	    To(aql.AnyEdgeOfType("contains"), aql.NodeType("file", "fs:file")).Build()
//	q := aql.Select(aql.Col("file")).
//	    FromPattern(pattern).
//	    OrderBy("file.name").  // dot notation works
//	    Build()
//
//	// Pattern with GROUP BY
//	pattern := aql.Pat(aql.NodeType("dir", "fs:dir")).
//	    To(aql.AnyEdgeOfType("contains"), aql.NodeType("file", "fs:file")).Build()
//	q := aql.Select(aql.Col("dir", "name"), aql.Count()).
//	    FromPattern(pattern).
//	    GroupByCol("dir.name").  // dot notation works
//	    Build()
//
//	// Variable-length path: 1-3 hops
//	pattern := aql.Pat(aql.NodeType("root", "fs:dir")).
//	    To(aql.AnyEdgeOfType("contains").WithHops(1, 3), aql.N("descendant")).Build()
//	q := aql.Select(aql.Col("descendant")).
//	    FromPattern(pattern).
//	    Build()
//
//	// Variable-length path: exactly 2 hops
//	pattern := aql.Pat(aql.N("start")).
//	    To(aql.AnyEdgeOfType("contains").WithHops(2, 2), aql.N("end")).Build()
//	q := aql.Select(aql.Col("end")).FromPattern(pattern).Build()
//
//	// Variable-length path: 2 or more hops (unbounded)
//	pattern := aql.Pat(aql.N("start")).
//	    To(aql.AnyEdgeOfType("contains").WithMinHops(2), aql.N("end")).Build()
//	q := aql.Select(aql.Col("end")).FromPattern(pattern).Build()
//
//	// Variable-length with multi-type edges
//	pattern := aql.Pat(aql.N("root")).
//	    To(aql.EdgeTypes("has", "contains").WithHops(1, 5), aql.N("node")).Build()
//	q := aql.Select(aql.Col("node")).FromPattern(pattern).Build()
//
// # Table Functions
//
// json_each unpacks JSON arrays into rows with 'key' (index) and 'value' columns:
//
//	SELECT value, COUNT(*) FROM nodes, json_each(labels)
//	GROUP BY value
//
// Using the builder with FromJoined:
//
//	q := aql.Select(aql.Col("value"), aql.Count()).
//	    FromJoined("nodes", "json_each", "labels").
//	    Where(aql.Ne("value", aql.String(""))).
//	    GroupByCol("value").
//	    Build()
//
// # Scoped Queries with EXISTS
//
// Combine EXISTS with variable-length paths for efficient scoped counting.
// The pattern uses *0.. to include the root node itself (0 or more hops):
//
//	// Build scope pattern: (cwd WHERE id = rootID)-[:contains*0..]->(nodes)
//	cwdPattern := aql.N("cwd").WithWhere(aql.Eq("id", aql.String(rootID)))
//	containsEdge := aql.AnyEdgeOfType("contains").WithMinHops(0)
//	pattern := aql.Pat(cwdPattern).To(containsEdge, aql.N("nodes")).Build()
//
//	// Count node types in scope
//	q := aql.Select(aql.Col("type"), aql.Count()).
//	    From("nodes").
//	    Where(aql.Exists(pattern)).
//	    GroupByCol("type").
//	    Build()
//
// For the edges table, EXISTS correlates on from_id (the edge's source node):
//
//	// Count edge types from scoped nodes
//	q := aql.Select(aql.Col("type"), aql.Count()).
//	    From("edges").
//	    Where(aql.Exists(pattern)).
//	    GroupByCol("type").
//	    Build()
//
// Combine json_each with EXISTS for scoped label counting:
//
//	q := aql.Select(aql.Col("value"), aql.Count()).
//	    FromJoined("nodes", "json_each", "labels").
//	    Where(aql.And(
//	        aql.Ne("value", aql.String("")),
//	        aql.Exists(pattern),
//	    )).
//	    GroupByCol("value").
//	    Build()
package aql
