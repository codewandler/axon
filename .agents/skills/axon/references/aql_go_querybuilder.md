# AQL Go Query Builder - Cheat Sheet

Comprehensive reference for building AQL queries programmatically using the Go builder API.

## Table of Contents

1. [Basic Query Construction](#basic-query-construction)
2. [WHERE Clause Builders](#where-clause-builders)
3. [Pattern Matching](#pattern-matching)
4. [Aggregation & Grouping](#aggregation--grouping)
5. [Common CLI Patterns](#common-cli-patterns)
6. [NodeFilter to AQL Translation](#nodefilter-to-aql-translation)

---

## Basic Query Construction

### Select Everything

```go
// SELECT * FROM nodes
query := aql.SelectStar().
    From("nodes").
    Build()
```

### Select Specific Columns

```go
// SELECT name, type FROM nodes
query := aql.Select(aql.Col("name"), aql.Col("type")).
    From("nodes").
    Build()
```

### Select with Alias

```go
// SELECT name AS file_name, type AS node_type FROM nodes
query := aql.Select(
    aql.As(aql.Col("name"), "file_name"),
    aql.As(aql.Col("type"), "node_type"),
).From("nodes").Build()
```

### Limit and Offset

```go
// SELECT * FROM nodes LIMIT 10 OFFSET 20
query := aql.SelectStar().
    From("nodes").
    Limit(10).
    Offset(20).
    Build()
```

### Distinct

```go
// SELECT DISTINCT type FROM nodes
query := aql.SelectDistinct(aql.Col("type")).
    From("nodes").
    Build()
```

---

## WHERE Clause Builders

### Comparison Operators

```go
// Equality: type = 'fs:file'
aql.Eq("type", aql.String("fs:file"))

// Inequality: type != 'fs:dir'
aql.Ne("type", aql.String("fs:dir"))

// Greater than: data.size > 1000
aql.Gt("data.size", aql.Int(1000))

// Greater or equal: data.size >= 1000
aql.Ge("data.size", aql.Int(1000))

// Less than: data.size < 5000
aql.Lt("data.size", aql.Int(5000))

// Less or equal: data.size <= 5000
aql.Le("data.size", aql.Int(5000))
```

### Pattern Matching

```go
// LIKE: name LIKE 'README%'
aql.Like("name", aql.String("README%"))

// GLOB (uses index!): id GLOB 'abc*'
aql.Glob("id", aql.String("abc*"))

// GLOB with wildcards: name GLOB '*test*'
aql.Glob("name", aql.String("*test*"))
```

### Set Operations

```go
// IN: type IN ('fs:file', 'fs:dir')
aql.In("type", aql.String("fs:file"), aql.String("fs:dir"))

// BETWEEN: data.size BETWEEN 100 AND 1000
aql.Between("data.size", aql.Int(100), aql.Int(1000))
```

### NULL Checks

```go
// IS NULL: name IS NULL
aql.IsNull("name")

// IS NOT NULL: name IS NOT NULL
aql.IsNotNull("name")
```

### Label Operations

```go
// CONTAINS ANY: labels CONTAINS ANY ('test:file', 'ci:config')
aql.ContainsAny("labels", aql.String("test:file"), aql.String("ci:config"))

// CONTAINS ALL: labels CONTAINS ALL ('important', 'reviewed')
aql.ContainsAll("labels", aql.String("important"), aql.String("reviewed"))

// NOT CONTAINS: labels NOT CONTAINS ('archived')
aql.NotContains("labels", aql.String("archived"))
```

### Boolean Logic

```go
// AND: type = 'fs:file' AND data.ext = 'go'
aql.And(
    aql.Eq("type", aql.String("fs:file")),
    aql.Eq("data.ext", aql.String("go")),
)

// OR: type = 'fs:file' OR type = 'fs:dir'
aql.Or(
    aql.Eq("type", aql.String("fs:file")),
    aql.Eq("type", aql.String("fs:dir")),
)

// NOT: NOT type = 'fs:file'
aql.Not(aql.Eq("type", aql.String("fs:file")))

// Parentheses for grouping
aql.And(
    aql.Or(
        aql.Eq("type", aql.String("fs:file")),
        aql.Eq("type", aql.String("fs:dir")),
    ),
    aql.Gt("data.size", aql.Int(1000)),
)
```

### JSON Field Access

Always use dot notation for nested data fields:

```go
// data.ext = 'go'
aql.Eq("data.ext", aql.String("go"))

// data.size BETWEEN 100 AND 1000
aql.Between("data.size", aql.Int(100), aql.Int(1000))

// data.mode = 755
aql.Eq("data.mode", aql.Int(755))
```

---

## Pattern Matching

### Basic Patterns

```go
// (dir:fs:dir)-[:contains]->(file:fs:file)
pattern := aql.Pat(aql.NodeType("dir", "fs:dir")).
    To(aql.AnyEdgeOfType("contains"), aql.NodeType("file", "fs:file")).
    Build()

query := aql.Select(aql.Col("file")).
    FromPattern(pattern).
    Build()
```

### Pattern with WHERE

```go
// SELECT file FROM (dir:fs:dir)-[:contains]->(file:fs:file)
// WHERE file.data.ext = 'go'
pattern := aql.Pat(aql.NodeType("dir", "fs:dir")).
    To(aql.AnyEdgeOfType("contains"), aql.NodeType("file", "fs:file")).
    Build()

query := aql.Select(aql.Col("file")).
    FromPattern(pattern).
    Where(aql.Eq("file.data.ext", aql.String("go"))).
    Build()
```

### Multi-Type Edges

```go
// (parent)-[:contains|has]->(child)
pattern := aql.Pat(aql.N("parent")).
    To(aql.EdgeTypes("contains", "has"), aql.N("child")).
    Build()

query := aql.Select(aql.Col("child")).
    FromPattern(pattern).
    Build()
```

### Edge Variables

```go
// (from)-[e:contains]->(to)
pattern := aql.Pat(aql.N("from")).
    To(aql.EdgeType("e", "contains"), aql.N("to")).
    Build()

query := aql.Select(aql.Col("e")).
    FromPattern(pattern).
    Build()
```

### Direction: Incoming Edge

```go
// (branch:vcs:branch)<-[:has]-(repo:vcs:repo)
pattern := aql.Pat(aql.NodeType("branch", "vcs:branch")).
    From(aql.AnyEdgeOfType("has"), aql.NodeType("repo", "vcs:repo")).
    Build()

query := aql.Select(aql.Col("repo")).
    FromPattern(pattern).
    Build()
```

### Direction: Undirected

```go
// (a)-[:references]-(b)
pattern := aql.Pat(aql.N("a")).
    Either(aql.AnyEdgeOfType("references"), aql.N("b")).
    Build()

query := aql.Select(aql.Col("a"), aql.Col("b")).
    FromPattern(pattern).
    Build()
```

### Variable-Length Paths

```go
// 1 to 3 hops: -[:contains*1..3]->
pattern := aql.Pat(aql.NodeType("root", "fs:dir")).
    To(aql.AnyEdgeOfType("contains").WithHops(1, 3), aql.N("descendant")).
    Build()

// Exactly 2 hops: -[:contains*2]->
pattern := aql.Pat(aql.N("start")).
    To(aql.AnyEdgeOfType("contains").WithExactHops(2), aql.N("end")).
    Build()

// 2 or more hops (unbounded): -[:contains*2..]->
pattern := aql.Pat(aql.N("start")).
    To(aql.AnyEdgeOfType("contains").WithMinHops(2), aql.N("end")).
    Build()

// Multi-type with variable-length
pattern := aql.Pat(aql.N("root")).
    To(aql.EdgeTypes("has", "contains").WithHops(1, 5), aql.N("node")).
    Build()
```

### Multiple Patterns (JOIN)

```go
// (repo:vcs:repo)-[:located_at]->(dir:fs:dir),
// (dir)-[:contains]->(file:fs:file)
p1 := aql.Pat(aql.NodeType("repo", "vcs:repo")).
    To(aql.AnyEdgeOfType("located_at"), aql.NodeType("dir", "fs:dir")).
    Build()

p2 := aql.Pat(aql.N("dir")).
    To(aql.AnyEdgeOfType("contains"), aql.NodeType("file", "fs:file")).
    Build()

query := aql.Select(aql.Col("file")).
    FromPattern(p1, p2).
    Build()
```

---

## Aggregation & Grouping

### COUNT(*)

```go
// SELECT type, COUNT(*) FROM nodes GROUP BY type
query := aql.Select(aql.Col("type"), aql.Count()).
    From("nodes").
    GroupByCol("type").
    Build()
```

### GROUP BY with HAVING

```go
// SELECT type, COUNT(*) FROM nodes 
// GROUP BY type 
// HAVING COUNT(*) > 10
query := aql.Select(aql.Col("type"), aql.Count()).
    From("nodes").
    GroupByCol("type").
    Having(aql.Gt("COUNT(*)", aql.Int(10))).
    Build()
```

### ORDER BY

```go
// ORDER BY name ASC
query := aql.SelectStar().
    From("nodes").
    OrderBy("name").
    Build()

// ORDER BY count DESC
query := aql.Select(aql.Col("type"), aql.Count()).
    From("nodes").
    GroupByCol("type").
    OrderByDesc("COUNT(*)").
    Build()
```

### Pattern with GROUP BY

```go
// SELECT dir.name, COUNT(*) 
// FROM (dir:fs:dir)-[:contains]->(file:fs:file)
// GROUP BY dir.name
pattern := aql.Pat(aql.NodeType("dir", "fs:dir")).
    To(aql.AnyEdgeOfType("contains"), aql.NodeType("file", "fs:file")).
    Build()

query := aql.Select(aql.Col("dir", "name"), aql.Count()).
    FromPattern(pattern).
    GroupByCol("dir.name").
    Build()
```

---

## Table Functions (json_each)

### FromJoined for JSON Array Unpacking

```go
// SELECT value, COUNT(*) FROM nodes, json_each(labels) GROUP BY value
query := aql.Select(aql.Col("value"), aql.Count()).
    FromJoined("nodes", "json_each", "labels").
    GroupByCol("value").
    Build()
```

### Filter Empty Values

```go
// Exclude empty labels
query := aql.Select(aql.Col("value"), aql.Count()).
    FromJoined("nodes", "json_each", "labels").
    Where(aql.Ne("value", aql.String(""))).
    GroupByCol("value").
    Build()
```

---

## Scoped Queries with EXISTS

For directory-scoped statistics, use EXISTS with variable-length paths.

### Build the Scope Pattern

```go
// Pattern: (cwd WHERE id = rootID)-[:contains*0..]->(nodes)
// *0.. means "0 or more hops" - includes the root itself
cwdPattern := aql.N("cwd").WithWhere(aql.Eq("id", aql.String(rootNodeID)))
containsEdge := aql.AnyEdgeOfType("contains").WithMinHops(0)
pattern := aql.Pat(cwdPattern).To(containsEdge, aql.N("nodes")).Build()
```

### Scoped Node Type Counting

```go
q := aql.Select(aql.Col("type"), aql.Count()).
    From("nodes").
    Where(aql.Exists(pattern)).
    GroupByCol("type").
    Build()
```

### Scoped Edge Type Counting

```go
// For edges, EXISTS correlates on from_id automatically
q := aql.Select(aql.Col("type"), aql.Count()).
    From("edges").
    Where(aql.Exists(pattern)).
    GroupByCol("type").
    Build()
```

### Scoped Extension Counting

```go
q := aql.Select(aql.Col("data", "ext"), aql.Count()).
    From("nodes").
    Where(aql.And(
        aql.IsNotNull("data.ext"),
        aql.Exists(pattern),
    )).
    GroupByCol("data.ext").
    Build()
```

### Scoped Label Counting (json_each + EXISTS)

```go
q := aql.Select(aql.Col("value"), aql.Count()).
    FromJoined("nodes", "json_each", "labels").
    Where(aql.And(
        aql.Ne("value", aql.String("")),
        aql.Exists(pattern),
    )).
    GroupByCol("value").
    Build()
```

---

## Common CLI Patterns

### Find by Type

```go
// axon find --type fs:file
query := aql.SelectStar().
    From("nodes").
    Where(aql.Eq("type", aql.String("fs:file"))).
    Build()
```

### Find by Type Pattern (GLOB)

```go
// axon find --type "fs:*"
query := aql.SelectStar().
    From("nodes").
    Where(aql.Glob("type", aql.String("fs:*"))).
    Build()
```

### Find by Name

```go
// axon find --name "README.md"
query := aql.SelectStar().
    From("nodes").
    Where(aql.Eq("name", aql.String("README.md"))).
    Build()
```

### Find by Name Pattern

```go
// axon find --query "README*"
query := aql.SelectStar().
    From("nodes").
    Where(aql.Glob("name", aql.String("README*"))).
    Build()
```

### Find by Extension

```go
// axon find --ext go
query := aql.SelectStar().
    From("nodes").
    Where(aql.Eq("data.ext", aql.String(".go"))).
    Build()

// Multiple extensions: --ext go --ext py
query := aql.SelectStar().
    From("nodes").
    Where(aql.In("data.ext", aql.String(".go"), aql.String(".py"))).
    Build()
```

### Find by Label

```go
// axon find --label test:file
query := aql.SelectStar().
    From("nodes").
    Where(aql.ContainsAny("labels", aql.String("test:file"))).
    Build()

// Multiple labels (OR logic): --label test:file --label ci:config
query := aql.SelectStar().
    From("nodes").
    Where(aql.ContainsAny("labels", 
        aql.String("test:file"), 
        aql.String("ci:config"))).
    Build()
```

### Find by ID Prefix

```go
// axon show abc123
query := aql.SelectStar().
    From("nodes").
    Where(aql.Glob("id", aql.String("abc123*"))).
    Limit(100).
    Build()
```

### Combined Filters

```go
// axon find --type fs:file --ext go --label test:file
query := aql.SelectStar().
    From("nodes").
    Where(aql.And(
        aql.Eq("type", aql.String("fs:file")),
        aql.Eq("data.ext", aql.String(".go")),
        aql.ContainsAny("labels", aql.String("test:file")),
    )).
    Build()
```

### Find with Limit

```go
// axon find --type fs:file --limit 10
query := aql.SelectStar().
    From("nodes").
    Where(aql.Eq("type", aql.String("fs:file"))).
    Limit(10).
    Build()
```

---

## NodeFilter to AQL Translation

Complete mapping from `graph.NodeFilter` struct fields to AQL builder patterns.

### Type (Exact Match)

```go
// NodeFilter
filter := graph.NodeFilter{
    Type: "fs:file",
}

// AQL Equivalent
query := aql.SelectStar().
    From("nodes").
    Where(aql.Eq("type", aql.String("fs:file"))).
    Build()
```

### TypePattern (Glob)

```go
// NodeFilter
filter := graph.NodeFilter{
    TypePattern: "fs:*",
}

// AQL Equivalent
query := aql.SelectStar().
    From("nodes").
    Where(aql.Glob("type", aql.String("fs:*"))).
    Build()
```

### URIPrefix

```go
// NodeFilter
filter := graph.NodeFilter{
    URIPrefix: "file:///home/user/project",
}

// AQL Equivalent
query := aql.SelectStar().
    From("nodes").
    Where(aql.Glob("uri", aql.String("file:///home/user/project*"))).
    Build()
```

### Name (Exact Match)

```go
// NodeFilter
filter := graph.NodeFilter{
    Name: "README.md",
}

// AQL Equivalent
query := aql.SelectStar().
    From("nodes").
    Where(aql.Eq("name", aql.String("README.md"))).
    Build()
```

### NamePattern (Glob)

```go
// NodeFilter
filter := graph.NodeFilter{
    NamePattern: "*test*",
}

// AQL Equivalent
query := aql.SelectStar().
    From("nodes").
    Where(aql.Glob("name", aql.String("*test*"))).
    Build()
```

### Labels (OR Logic)

```go
// NodeFilter
filter := graph.NodeFilter{
    Labels: []string{"test:file", "ci:config"},
}

// AQL Equivalent
query := aql.SelectStar().
    From("nodes").
    Where(aql.ContainsAny("labels", 
        aql.String("test:file"), 
        aql.String("ci:config"))).
    Build()
```

### Extensions (OR Logic)

```go
// NodeFilter
filter := graph.NodeFilter{
    Extensions: []string{"go", "py"},
}

// AQL Equivalent
query := aql.SelectStar().
    From("nodes").
    Where(aql.In("data.ext", aql.String(".go"), aql.String(".py"))).
    Build()
```

### NodeIDs (OR Logic)

```go
// NodeFilter
filter := graph.NodeFilter{
    NodeIDs: []string{"abc123", "def456"},
}

// AQL Equivalent
query := aql.SelectStar().
    From("nodes").
    Where(aql.In("id", aql.String("abc123"), aql.String("def456"))).
    Build()
```

### Root (Flag)

```go
// NodeFilter
filter := graph.NodeFilter{
    Root: true,
}

// AQL Equivalent
query := aql.SelectStar().
    From("nodes").
    Where(aql.Eq("root", aql.Bool(true))).
    Build()
```

### Complex Combined Filter

```go
// NodeFilter with multiple conditions
filter := graph.NodeFilter{
    Type:       "fs:file",
    Labels:     []string{"test:file"},
    Extensions: []string{"go", "py"},
}

// AQL Equivalent
query := aql.SelectStar().
    From("nodes").
    Where(aql.And(
        aql.Eq("type", aql.String("fs:file")),
        aql.ContainsAny("labels", aql.String("test:file")),
        aql.In("data.ext", aql.String(".go"), aql.String(".py")),
    )).
    Build()
```

---

## Tips & Best Practices

### 1. Use GLOB Instead of LIKE for Prefix Matching

```go
// ❌ LIKE doesn't use index
aql.Like("id", aql.String("abc%"))

// ✅ GLOB uses PRIMARY KEY index
aql.Glob("id", aql.String("abc*"))
```

### 2. Always Add LIMIT for Prefix Queries

```go
// Prevent accidentally loading too many results
query := aql.SelectStar().
    From("nodes").
    Where(aql.Glob("id", aql.String("a*"))).
    Limit(100).  // ✅ Safety limit
    Build()
```

### 3. Use Dot Notation for JSON Fields

```go
// ✅ Compiles to efficient json_extract()
aql.Eq("data.ext", aql.String(".go"))
aql.Gt("data.size", aql.Int(1000))
aql.Between("data.mode", aql.Int(400), aql.Int(500))
```

### 4. Combine Multiple Conditions with AND

```go
// Build complex filters step by step
conditions := []aql.Expression{}

if typeFilter != "" {
    conditions = append(conditions, aql.Eq("type", aql.String(typeFilter)))
}

if nameFilter != "" {
    conditions = append(conditions, aql.Eq("name", aql.String(nameFilter)))
}

query := aql.SelectStar().
    From("nodes").
    Where(aql.And(conditions...)).
    Build()
```

### 5. Pattern Variables Use Dot Notation

```go
// In patterns, reference fields with variable.field
pattern := aql.Pat(aql.NodeType("file", "fs:file")).Build()

query := aql.Select(aql.Col("file")).
    FromPattern(pattern).
    Where(aql.Eq("file.data.ext", aql.String("go"))).  // ✅ variable.data.field
    OrderBy("file.name").  // ✅ variable.field
    Build()
```

---

## Quick Reference Table

| Operation | Builder Method | Example |
|-----------|---------------|---------|
| Equality | `aql.Eq(field, value)` | `aql.Eq("type", aql.String("fs:file"))` |
| Not Equal | `aql.Ne(field, value)` | `aql.Ne("type", aql.String("fs:dir"))` |
| Greater | `aql.Gt(field, value)` | `aql.Gt("data.size", aql.Int(1000))` |
| Less | `aql.Lt(field, value)` | `aql.Lt("data.size", aql.Int(5000))` |
| Like | `aql.Like(field, pattern)` | `aql.Like("name", aql.String("README%"))` |
| Glob | `aql.Glob(field, pattern)` | `aql.Glob("type", aql.String("fs:*"))` |
| In | `aql.In(field, values...)` | `aql.In("type", aql.String("fs:file"), aql.String("fs:dir"))` |
| Between | `aql.Between(field, low, high)` | `aql.Between("data.size", aql.Int(100), aql.Int(1000))` |
| Is Null | `aql.IsNull(field)` | `aql.IsNull("name")` |
| Not Null | `aql.IsNotNull(field)` | `aql.IsNotNull("name")` |
| Contains Any | `aql.ContainsAny(field, values...)` | `aql.ContainsAny("labels", aql.String("test"))` |
| Contains All | `aql.ContainsAll(field, values...)` | `aql.ContainsAll("labels", aql.String("a"), aql.String("b"))` |
| Not Contains | `aql.NotContains(field, values...)` | `aql.NotContains("labels", aql.String("archived"))` |
| And | `aql.And(exprs...)` | `aql.And(expr1, expr2)` |
| Or | `aql.Or(exprs...)` | `aql.Or(expr1, expr2)` |
| Not | `aql.Not(expr)` | `aql.Not(expr)` |
| Exists | `aql.Exists(pattern)` | `aql.Exists(pattern)` |
| FromJoined | `FromJoined(table, func, col)` | `FromJoined("nodes", "json_each", "labels")` |
| WithMinHops | `edge.WithMinHops(n)` | `aql.AnyEdgeOfType("contains").WithMinHops(0)` |
| WithHops | `edge.WithHops(min, max)` | `aql.AnyEdgeOfType("contains").WithHops(1, 3)` |

---

## Execution

### Query Execution

```go
// Execute query through storage
result, err := storage.Query(ctx, query)
if err != nil {
    return fmt.Errorf("query failed: %w", err)
}

// Access results
switch result.Type {
case graph.ResultTypeNodes:
    for _, node := range result.Nodes {
        fmt.Println(node.Name, node.Type)
    }
case graph.ResultTypeCounts:
    for key, count := range result.Counts {
        fmt.Printf("%s: %d\n", key, count)
    }
}
```

### Query Explanation (Debugging)

```go
// See the generated SQL and execution plan
plan, err := storage.Explain(ctx, query)
if err != nil {
    return err
}

fmt.Println("SQL:", plan.SQL)
fmt.Println("Args:", plan.Args)
fmt.Println("Plan:", plan.SQLitePlan)
```

---

**Last Updated**: 2026-02-22
**Axon Version**: main branch
