// Package aql provides a type-safe fluent API and parser for the Axon Query Language (AQL).
//
// AQL is a SQL-like query language designed for the Axon graph database with both
// flat table queries and graph pattern matching using Cypher-like syntax.
//
// The package provides two main APIs:
// 1. **Fluent Builder API** - Type-safe, chainable query construction (recommended)
// 2. **Parser API** - Parse query strings into AST (for dynamic queries)
//
// # Fluent Builder API (Type-Safe, Recommended)
//
// Build queries using type-safe constants and fluent method chaining:
//
//	// Basic table queries
//	result, err := aql.Nodes.
//	    Select(aql.Type, aql.Count()).
//	    Where(aql.Type.Eq(aql.NodeType.File)).
//	    GroupBy(aql.Type).
//	    OrderByCount(true).
//	    Build()
//
//	// JsonEach for JSON array unpacking
//	result, err := aql.Nodes.JsonEach(aql.Labels).
//	    Select(aql.Val, aql.Count()).
//	    Where(aql.Val.Ne("")).
//	    GroupBy(aql.Val).
//	    Build()
//
//	// Scoped queries (optimized with CTE+JOIN)
//	result, err := aql.Nodes.
//	    Select(aql.Type, aql.Count()).
//	    Where(aql.Nodes.ScopedTo(rootNodeID)).
//	    GroupBy(aql.Type).
//	    Build()
//
//	// Pattern queries
//	result, err := aql.FromPattern(
//	    aql.Pat(aql.N("dir").OfType(aql.NodeType.Dir).Build()).
//		    To(aql.Edge.Contains, aql.N("file").OfType(aql.NodeType.File).Build()).
//		    Build(),
//	).Select(aql.Var("file")).Build()
//
//	// Variable-length paths
//	result, err := aql.FromPattern(
//	    aql.Pat(aql.N("root").OfType(aql.NodeType.Dir).Build()).
//		    To(aql.Edge.Contains.WithHops(1, 3), aql.N("desc").Build()).
//		    Build(),
//	).Select(aql.Var("desc")).Build()
//
// Execute the query:
//
//	result, err := storage.Query(ctx, query)
//	if err != nil {
//	    log.Fatal(err)
//	}
//
//	switch result.Type {
//	case graph.ResultTypeNodes:
//	    for _, node := range result.Nodes {
//	        fmt.Println(node.Name, node.Type)
//	    }
//	case graph.ResultTypeCounts:
//	    for key, count := range result.Counts {
//	        fmt.Printf("%s: %d\n", key, count)
//	    }
//	}
//
// # Parser API (For Dynamic Queries)
//
// Parse query strings when you need dynamic query construction:
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
// Get query execution plan:
//
//	plan, err := storage.Explain(ctx, query)
//	if err != nil {
//	    log.Fatal(err)
//	}
//	fmt.Println("SQL:", plan.SQL)
//	fmt.Println("Plan:", plan.SQLitePlan)
//
// # Table Queries
//
// Query the flat nodes and edges tables:
//
//	// All files
//	aql.Nodes.SelectStar()
//
//	// Files with conditions
//	aql.Nodes.SelectStar().Where(aql.DataExt.Eq("go"))
//
//	// Count by type
//	aql.Nodes.Select(aql.Type, aql.Count()).GroupBy(aql.Type)
//
//	// Edge statistics
//	aql.Edges.Select(aql.Type, aql.Count()).GroupBy(aql.Type)
//
// # JSON Field Access
//
// Access nested data fields using dot notation:
//
//	aql.DataExt.Eq("go")              // data.ext = 'go'
//	aql.DataSize.Gt(1000)               // data.size > 1000
//	aql.DataMode.Between(400, 500)      // data.mode BETWEEN 400 AND 500
//
// The data field compiles to efficient json_extract() calls in SQLite.
//
// # Pattern Queries
//
// Build graph patterns using the fluent builder:
//
//	// Basic containment: (dir)-[:contains]->(file)
//	aql.Pat(aql.N("dir").OfType(aql.NodeType.Dir).Build()).
//	    To(aql.Edge.Contains, aql.N("file").OfType(aql.NodeType.File).Build()).
//	    Build()
//
//	// With edge variables: (a)-[e:contains]->(b)
//	aql.Pat(aql.N("a").Build()).
//	    To(aql.EOfType("e", "contains"), aql.N("b").Build()).
//	    Build()
//
//	// Multi-type edges: (parent)-[:contains|has]->(child)
//	aql.Pat(aql.N("parent").Build()).
//	    To(aql.EdgeTypes(aql.Edge.Contains, aql.Edge.Has), aql.N("child").Build()).
//	    Build()
//
//	// Incoming edges: (branch)<-[:has]-(repo)
//	aql.Pat(aql.N("branch").OfType(aql.NodeType.Branch).Build()).
//	    From(aql.Edge.Has, aql.N("repo").OfType(aql.NodeType.Repo).Build()).
//	    Build()
//
// # Variable-Length Paths
//
// Recursive graph traversal using CTEs:
//
//	// 1 to 3 hops: -[:contains*1..3]->
//	aql.Edge.Contains.WithHops(1, 3)
//
//	// Exactly 2 hops: -[:contains*2]->
//	aql.Edge.Contains.WithExactHops(2)
//
//	// 2 or more hops (unbounded): -[:contains*2..]->
//	aql.Edge.Contains.WithMinHops(2)
//
// Examples:
//
//	// Variable-length pattern
//	aql.Pat(aql.N("root").OfType(aql.NodeType.Dir).Build()).
//	    To(aql.Edge.Contains.WithHops(1, 3), aql.N("desc").Build()).
//	    Build()
//
//	// Multi-type recursive
//	aql.Pat(aql.N("root").Build()).
//	    To(aql.EdgeTypes(aql.Edge.Contains, aql.Edge.Has).WithHops(1, 5), aql.N("node").Build()).
//	    Build()
//
// # JSON Array Unpacking
//
// Use json_each to unpack JSON arrays into rows:
//
//	// Count all labels across nodes
//	aql.Nodes.JsonEach(aql.Labels).
//	    Select(aql.Val, aql.Count()).
//	    GroupBy(aql.Val).
//	    Build()
//
//	// List unique labels
//	aql.Nodes.JsonEach(aql.Labels).
//	    Select(aql.Val).
//	    Where(aql.Val.Ne("")).
//	    Distinct().
//	    Build()
//
// # Scoped Queries
//
// Use EXISTS patterns for efficient directory-scoped queries:
//
//	// Node types in directory scope
//	aql.Nodes.Select(aql.Type, aql.Count()).
//	    Where(aql.Nodes.ScopedTo(cwdNodeID)).
//	    GroupBy(aql.Type).
//	    Build()
//
//	// Edge types from scoped nodes
//	aql.Edges.Select(aql.Type, aql.Count()).
//	    Where(aql.Edges.ScopedTo(cwdNodeID)).
//	    GroupBy(aql.Type).
//	    Build()
//
//	// Labels in scope (combine json_each with scoped query)
//	aql.Nodes.JsonEach(aql.Labels).
//	    Select(aql.Val, aql.Count()).
//	    Where(aql.And(
//	        aql.Val.Ne(""),
//	        aql.Nodes.ScopedTo(cwdNodeID),
//	    )).
//	    GroupBy(aql.Val).
//	    Build()
//
// # Expressions
//
// WHERE and HAVING clauses support comprehensive expressions:
//
//	// Comparisons (auto-wrap values)
//	aql.Type.Eq("fs:file")              // type = 'fs:file'
//	aql.Type.Ne("fs:dir")               // type != 'fs:dir'
//	aql.DataSize.Gt(1000)               // data.size > 1000
//	aql.DataSize.Between(100, 1000)   // data.size BETWEEN 100 AND 1000
//
//	// Pattern matching
//	aql.Name.Like("README%")            // name LIKE 'README%'
//	aql.Type.Glob("fs:*")               // type GLOB 'fs:*'
//
//	// Set operations
//	aql.Type.In("fs:file", "fs:dir")    // type IN ('fs:file', 'fs:dir')
//	aql.DataSize.Between(100, 1000)     // data.size BETWEEN 100 AND 1000
//
//	// Null checks
//	aql.DataExt.IsNull()                // data.ext IS NULL
//	aql.DataExt.IsNotNull()             // data.ext IS NOT NULL
//
//	// Label operations
//	aql.Labels.ContainsAny("test", "code")    // labels CONTAINS ANY ('test', 'code')
//	aql.Labels.ContainsAll("important", "review") // labels CONTAINS ALL ('important', 'review')
//	aql.Labels.NotContains("archived")      // labels NOT CONTAINS ('archived')
//
//	// Existence checks
//	aql.Exists(pattern)                 // EXISTS pattern
//	aql.NotExists(pattern)              // NOT EXISTS pattern
//
// # Variable References
//
// Reference pattern variables in WHERE clauses:
//
//	// Variable field access
//	aql.Var("file").DataField("ext").Eq("go")     // file.data.ext = 'go'
//	aql.Var("file").Field("name").Glob("*.go")    // file.name GLOB '*.go'
//
//	// Variable as column
//	aql.Select(aql.Var("file"))                     // SELECT file
//	aql.Select(aql.Var("repo"), aql.Var("branch")) // SELECT repo, branch
//
// # Constants
//
// Predefined constants for common types and fields:
//
// Node types: aql.NodeType.File, aql.NodeType.Dir, aql.NodeType.Repo, etc.
// Edge types: aql.Edge.Contains, aql.Edge.Has, aql.Edge.LocatedAt, etc.
// Common fields: aql.Type, aql.Name, aql.URI, aql.Labels, aql.DataExt, aql.DataSize
// JsonEach fields: aql.Key, aql.Val
//
// # Migration from Old API
//
// Old string-based API → New type-safe fluent API:
//
//	SelectStar().From("nodes")                    → Nodes.SelectStar()
//	Select(...).From("edges")                     → Edges.Select(...)
//	FromJoined("nodes", "json_each", "labels")    → Nodes.JsonEach(Labels)
//	Col("type")                                   → Type
//	Col("data", "ext")                            → DataExt or Data.Field("ext")
//	Eq("type", String("fs:file"))                 → Type.Eq("fs:file")
//	Gt("data.size", Int(1000))                    → DataSize.Gt(1000)
//	ContainsAny("labels", String("test"))           → Labels.ContainsAny("test")
//	N("file")                                     → N("file").Build()
//	NodeType("file", "fs:file")                   → N("file").OfType(NodeType.File).Build()
//	AnyEdgeOfType("contains")                     → Edge.Contains
//	EdgeType("e", "contains")                     → EOfType("e", Edge.Contains)
//
// The new API is type-safe, eliminates magic strings, and provides better IDE support
// while maintaining full compatibility with the underlying AST and validation system.
package aql
