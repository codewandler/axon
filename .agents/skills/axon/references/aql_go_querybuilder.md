# AQL Go Query Builder - Complete Reference

Comprehensive reference for building AQL queries programmatically using the type-safe fluent API.

## Table of Contents

1. [Table Queries](#table-queries)
2. [Column References](#column-references)
3. [Expressions](#expressions)
4. [Pattern Matching](#pattern-matching)
5. [JSON Array Unpacking](#json-array-unpacking)
6. [Scoped Queries](#scoped-queries)
7. [Variable References](#variable-references)
8. [Common CLI Patterns](#common-cli-patterns)
9. [Migration Guide](#migration-guide)

---

## Table Queries

### Basic Table Queries

```go
// SELECT * FROM nodes
aql.Nodes.SelectStar()

// SELECT * FROM edges
aql.Edges.SelectStar()

// SELECT specific columns
aql.Nodes.Select(aql.Type, aql.Name)
aql.Edges.Select(aql.Type, aql.FromID, aql.ToID)

// With WHERE conditions
aql.Nodes.SelectStar().Where(aql.Type.Eq("fs:file"))
aql.Edges.SelectStar().Where(aql.Type.Eq("contains"))

// With LIMIT/OFFSET
aql.Nodes.SelectStar().Limit(10).Offset(20)

// With ORDER BY
aql.Nodes.SelectStar().OrderBy(aql.Name)
aql.Nodes.SelectStar().OrderByDesc(aql.DataSize)

// With DISTINCT
aql.Nodes.SelectDistinct(aql.Type)
```

### Aggregation Queries

```go
// Count by type
aql.Nodes.Select(aql.Type, aql.Count()).
    GroupBy(aql.Type).
    OrderByCount(true).
    Build()

// Count edges by type
aql.Edges.Select(aql.Type, aql.Count()).
    GroupBy(aql.Type).
    Build()

// HAVING clause
aql.Nodes.Select(aql.Type, aql.Count()).
    GroupBy(aql.Type).
    Having(aql.Gt("COUNT(*)", aql.Int(10))).
    Build()

// Complex aggregation
aql.Nodes.Select(aql.DataExt, aql.Count()).
    Where(aql.DataExt.IsNotNull()).
    GroupBy(aql.DataExt).
    OrderByCount(true).
    Build()
```

---

## Column References

### Common Node Columns

```go
aql.ID         // "id"
aql.Type       // "type" 
aql.Name       // "name"
aql.URI        // "uri"
aql.Labels     // "labels"
aql.DataCol    // "data" (for NULL checks)
aql.Generation // "generation"
aql.CreatedAt  // "created_at"
```

### Edge-Specific Columns

```go
aql.FromID     // "from_id"
aql.ToID       // "to_id"
```

### Data Field Access

```go
// Predefined data fields
aql.DataExt    // "data.ext"
aql.DataSize   // "data.size"
aql.DataMode   // "data.mode"

// Dynamic data field access
aql.Data.Field("ext")     // "data.ext"
aql.Data.Field("size")   // "data.size"
aql.Data.Field("mode")    // "data.mode"
```

### JsonEach Output Columns

```go
aql.Key   // "key" (array index)
aql.Val   // "value" (array element)
```

---

## Expressions

### Comparisons (Auto-wrap values)

```go
// Equality
aql.Type.Eq("fs:file")              // type = 'fs:file'
aql.Type.Ne("fs:dir")               // type != 'fs:dir'
aql.Name.Eq("README.md")            // name = 'README.md'

// Numeric comparisons
aql.DataSize.Gt(1000)               // data.size > 1000
aql.DataSize.Lt(5000)               // data.size < 5000
aql.DataSize.Ge(1000)               // data.size >= 1000
aql.DataSize.Le(5000)               // data.size <= 5000
aql.DataSize.Between(100, 1000)   // data.size BETWEEN 100 AND 1000

// Pattern matching
aql.Name.Like("README%")            // name LIKE 'README%'
aql.Type.Glob("fs:*")               // type GLOB 'fs:*'
aql.Name.Glob("*.go")               // name GLOB '*.go'

// Set operations
aql.Type.In("fs:file", "fs:dir")    // type IN ('fs:file', 'fs:dir')
aql.DataSize.Between(100, 1000)     // data.size BETWEEN 100 AND 1000
```

### Label Operations

```go
// CONTAINS ANY
aql.Labels.ContainsAny("test", "code")

// CONTAINS ALL
aql.Labels.ContainsAll("important", "reviewed")

// NOT CONTAINS
aql.Labels.NotContains("archived")
```

### Null Checks

```go
aql.DataExt.IsNull()
aql.DataExt.IsNotNull()
aql.DataCol.IsNull()        // Check if entire data field is NULL
```

### Boolean Logic

```go
// AND
aql.And(
    aql.Type.Eq("fs:file"),
    aql.DataExt.Eq("go"),
)

// OR
aql.Or(
    aql.Type.Eq("fs:file"),
    aql.Type.Eq("fs:dir"),
)

// NOT
aql.Not(aql.Type.Eq("fs:file"))

// Complex combinations
aql.And(
    aql.Or(
        aql.Type.Eq("fs:file"),
        aql.Type.Eq("fs:dir"),
    ),
    aql.DataSize.Gt(1000),
)
```

### Existence Checks

```go
// EXISTS pattern
aql.Exists(pattern)

// NOT EXISTS pattern
aql.NotExists(pattern)
```

---

## Pattern Matching

### Basic Patterns

```go
// (dir:fs:dir)-[:contains]->(file:fs:file)
aql.Pat(aql.N("dir").OfType(aql.NodeType.Dir).Build()).
    To(aql.Edge.Contains, aql.N("file").OfType(aql.NodeType.File).Build()).
    Build()

// (repo:vcs:repo)-[:has]->(branch:vcs:branch)
aql.Pat(aql.N("repo").OfType(aql.NodeType.Repo).Build()).
    To(aql.Edge.Has, aql.N("branch").OfType(aql.NodeType.Branch).Build()).
    Build()

// With edge variables: (a)-[e:contains]->(b)
aql.Pat(aql.N("a").Build()).
    To(aql.EOfType("e", "contains"), aql.N("b").Build()).
    Build()
```

### Multi-Type Edges

```go
// (parent)-[:contains|has]->(child)
aql.Pat(aql.N("parent").Build()).
    To(aql.EdgeTypes(aql.Edge.Contains, aql.Edge.Has), aql.N("child").Build()).
    Build()

// Incoming edges: (branch)<-[:has]-(repo)
aql.Pat(aql.N("branch").OfType(aql.NodeType.Branch).Build()).
    From(aql.Edge.Has, aql.N("repo").OfType(aql.NodeType.Repo).Build()).
    Build()

// Undirected edges: (a)-[:references]-(b)
aql.Pat(aql.N("a").Build()).
    Either(aql.Edge.References, aql.N("b").Build()).
    Build()
```

### Variable-Length Paths

```go
// 1 to 3 hops: -[:contains*1..3]->
aql.Edge.Contains.WithHops(1, 3)

// Exactly 2 hops: -[:contains*2]->
aql.Edge.Contains.WithExactHops(2)

// 2 or more hops (unbounded): -[:contains*2..]->
aql.Edge.Contains.WithMinHops(2)

// Multi-type with variable-length
aql.EdgeTypes(aql.Edge.Contains, aql.Edge.Has).WithHops(1, 5)
```

**Examples:**

```go
// Variable-length pattern
aql.Pat(aql.N("root").OfType(aql.NodeType.Dir).Build()).
    To(aql.Edge.Contains.WithHops(1, 3), aql.N("desc").Build()).
    Build()

// Multi-type recursive
aql.Pat(aql.N("root").Build()).
    To(aql.EdgeTypes(aql.Edge.Contains, aql.Edge.Has).WithHops(1, 5), aql.N("node").Build()).
    Build()
```

---

## JSON Array Unpacking

### JsonEach Table Function

```go
// Count all labels across nodes
aql.Nodes.JsonEach(aql.Labels).
    Select(aql.Val, aql.Count()).
    Where(aql.Val.Ne("")).
    GroupBy(aql.Val).
    Build()

// List unique labels
aql.Nodes.JsonEach(aql.Labels).
    Select(aql.Val).
    Where(aql.Val.Ne("")).
    Distinct().
    Build()

// Scoped label counting
aql.Nodes.JsonEach(aql.Labels).
    Select(aql.Val, aql.Count()).
    Where(aql.And(
        aql.Val.Ne(""),
        aql.Nodes.ScopedTo(cwdNodeID),
    )).
    GroupBy(aql.Val).
    Build()
```

### JsonEach with Data Fields

```go
// Unpack data.tags array
aql.Nodes.JsonEach(aql.Data.Field("tags")).
    Select(aql.Val, aql.Count()).
    GroupBy(aql.Val).
    Build()

// Filter empty values
aql.Nodes.JsonEach(aql.Data.Field("tags")).
    Select(aql.Val, aql.Count()).
    Where(aql.Val.Ne("")).
    GroupBy(aql.Val).
    Build()
```

---

## Scoped Queries

Use EXISTS patterns for efficient directory-scoped queries:

```go
// Node types in directory scope
aql.Nodes.Select(aql.Type, aql.Count()).
    Where(aql.Nodes.ScopedTo(cwdNodeID)).
    GroupBy(aql.Type).
    Build()

// Edge types from scoped nodes
aql.Edges.Select(aql.Type, aql.Count()).
    Where(aql.Edges.ScopedTo(cwdNodeID)).
    GroupBy(aql.Type).
    Build()

// Extensions in scope
aql.Nodes.Select(aql.DataExt, aql.Count()).
    Where(aql.And(
        aql.DataExt.IsNotNull(),
        aql.Nodes.ScopedTo(cwdNodeID),
    )).
    GroupBy(aql.DataExt).
    Build()

// Labels in scope (combine json_each with scoped query)
aql.Nodes.JsonEach(aql.Labels).
    Select(aql.Val, aql.Count()).
    Where(aql.And(
        aql.Val.Ne(""),
        aql.Nodes.ScopedTo(cwdNodeID),
    )).
    GroupBy(aql.Val).
    Build()
```

---

## Variable References

Reference pattern variables in WHERE clauses:

```go
// Variable field access
aql.Var("file").DataField("ext").Eq("go")     // file.data.ext = 'go'
aql.Var("file").Field("name").Glob("*.go")    // file.name GLOB '*.go'

// Variable as column
aql.Select(aql.Var("file"))                     // SELECT file
aql.Select(aql.Var("repo"), aql.Var("branch")) // SELECT repo, branch

// Complex variable references
aql.Var("repo").DataField("name").Eq("myproject")
aql.Var("branch").Field("name").Like("main%")
```

---

## Common CLI Patterns

### Find Commands

```go
// Find by type
aql.Nodes.SelectStar().Where(aql.Type.Eq("fs:file"))

// Find by extension
aql.Nodes.SelectStar().Where(aql.DataExt.Eq("go"))

// Find by name pattern
aql.Nodes.SelectStar().Where(aql.Name.Glob("README*"))

// Find by labels
aql.Nodes.SelectStar().Where(aql.Labels.ContainsAny("test"))

// Combined filters
aql.Nodes.SelectStar().Where(aql.And(
    aql.Type.Eq("fs:file"),
    aql.DataExt.Eq("go"),
    aql.Labels.ContainsAny("test"),
))

// With limit
aql.Nodes.SelectStar().
    Where(aql.Type.Eq("fs:file")).
    Limit(10).
    Build()
```

### Statistics Commands

```go
// Global statistics
aql.Nodes.Select(aql.Type, aql.Count()).GroupBy(aql.Type)
aql.Edges.Select(aql.Type, aql.Count()).GroupBy(aql.Type)

// Extension statistics
aql.Nodes.Select(aql.DataExt, aql.Count()).
    Where(aql.DataExt.IsNotNull()).
    GroupBy(aql.DataExt).
    OrderByCount(true).
    Build()

// Label statistics
aql.Nodes.JsonEach(aql.Labels).
    Select(aql.Val, aql.Count()).
    Where(aql.Val.Ne("")).
    GroupBy(aql.Val).
    OrderByCount(true).
    Build()
```

### Scoped Statistics

```go
// Node types in directory scope
aql.Nodes.Select(aql.Type, aql.Count()).
    Where(aql.Nodes.ScopedTo(cwdNodeID)).
    GroupBy(aql.Type).
    Build()

// Edge types from scoped nodes
aql.Edges.Select(aql.Type, aql.Count()).
    Where(aql.Edges.ScopedTo(cwdNodeID)).
    GroupBy(aql.Type).
    Build()
```

---

## Migration Guide

### Old → New API Mapping

**Table References:**
```go
// OLD: String-based
SelectStar().From("nodes")
Select(Col("type"), Count()).From("nodes")
FromJoined("nodes", "json_each", "labels")

// NEW: Type-safe fluent
Nodes.SelectStar()
Nodes.Select(Type, Count())
Nodes.JsonEach(Labels)
```

**Column References:**
```go
// OLD: Magic strings
Col("type")
Col("data", "ext")
Col("file", "data", "ext")

// NEW: Type-safe constants
Type
DataExt
Var("file").DataField("ext")
```

**Expressions (Auto-wrap values):**
```go
// OLD: Manual value wrapping
Eq("type", String("fs:file"))
Gt("data.size", Int(1000))
ContainsAny("labels", String("test"))

// NEW: Auto-wrap values
Type.Eq("fs:file")
DataSize.Gt(1000)
Labels.ContainsAny("test")
```

**Pattern Building:**
```go
// OLD: String-based patterns
N("file")
NodeType("file", "fs:file")
AnyEdgeOfType("contains")
EdgeType("e", "contains")

// NEW: Type-safe pattern building
N("file").Build()
N("file").OfType(NodeType.File).Build()
Edge.Contains
EOfType("e", Edge.Contains)
```

**Complex Pattern Example:**
```go
// OLD: Manual pattern construction
pattern := aql.Pat(aql.N("dir").WithWhere(aql.Eq("id", aql.String(rootID)))).
    To(aql.AnyEdgeOfType("contains").WithMinHops(0), aql.N("nodes")).Build()
q := aql.SelectStar().From("nodes").Where(aql.Exists(pattern)).Build()

// NEW: Optimized scoped helper
q := aql.Nodes.SelectStar().
    Where(aql.Nodes.ScopedTo(rootID)).
    Build()
```

### Complete Migration Examples

**1. Basic File Query:**
```go
// OLD
q := aql.SelectStar().
    From("nodes").
    Where(aql.Eq("type", aql.String("fs:file"))).
    Build()

// NEW
q := aql.Nodes.SelectStar().
    Where(aql.Type.Eq("fs:file")).
    Build()
```

**2. Label Counting:**
```go
// OLD
q := aql.Select(aql.Col("value"), aql.Count()).
    FromJoined("nodes", "json_each", "labels").
    Where(aql.Ne("value", aql.String(""))).
    GroupByCol("value").
    Build()

// NEW
q := aql.Nodes.JsonEach(aql.Labels).
    Select(aql.Val, aql.Count()).
    Where(aql.Val.Ne("")).
    GroupBy(aql.Val).
    Build()
```

**3. Pattern Query:**
```go
// OLD
pattern := aql.Pat(aql.NodeType("dir", "fs:dir")).
    To(aql.AnyEdgeOfType("contains"), aql.NodeType("file", "fs:file")).
    Build()
q := aql.Select(aql.Col("file")).FromPattern(pattern).Build()

// NEW
pattern := aql.Pat(aql.N("dir").OfType(aql.NodeType.Dir).Build()).
    To(aql.Edge.Contains, aql.N("file").OfType(aql.NodeType.File).Build()).
    Build()
q := aql.FromPattern(pattern).Select(aql.Var("file")).Build()
```

**4. Scoped Statistics:**
```go
// OLD
cwdPattern := aql.N("cwd").WithWhere(aql.Eq("id", aql.String(cwdNodeID)))
containsEdge := aql.AnyEdgeOfType("contains").WithMinHops(0)
pattern := aql.Pat(cwdPattern).To(containsEdge, aql.N("nodes")).Build()
q := aql.Select(aql.Col("type"), aql.Count()).
    From("nodes").
    Where(aql.Exists(pattern)).
    GroupByCol("type").
    Build()

// NEW
q := aql.Nodes.Select(aql.Type, aql.Count()).
    Where(aql.Nodes.ScopedTo(cwdNodeID)).
    GroupBy(aql.Type).
    Build()
```

### Benefits of New API

1. **Type Safety**: Compile-time validation prevents typos
2. **Auto-completion**: IDEs suggest valid options
3. **Cleaner Code**: More readable and maintainable
4. **Performance**: Automatic CTE+JOIN optimizations
5. **Better Error Messages**: Clear compile-time errors
6. **IDE Support**: Full autocomplete and documentation

The new API maintains full compatibility with the existing AST and validation system while providing a much cleaner, safer way to build queries.