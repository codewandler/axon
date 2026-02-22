# AQL Grammar Specification

AQL (Axon Query Language) is a SQL-like query language with graph pattern matching
capabilities for querying the Axon graph database.

## Overview

AQL provides a unified SELECT syntax where the data source can be either:
- Flat tables (`nodes`, `edges`)
- Graph patterns with relationship traversal

```sql
-- Flat query
SELECT * FROM nodes WHERE type = 'fs:file'

-- Pattern query
SELECT file FROM (dir:fs:dir)-[:contains]->(file:fs:file) WHERE dir.name = 'cmd'
```

## Lexical Elements

### Keywords (case-insensitive)

```
SELECT, DISTINCT, FROM, WHERE, AND, OR, NOT, IN, BETWEEN, LIKE, GLOB,
IS, NULL, TRUE, FALSE, EXISTS, CONTAINS, ANY, ALL, GROUP, BY, HAVING,
ORDER, ASC, DESC, LIMIT, OFFSET, COUNT, AS
```

### Identifiers

```
Identifier = Letter { Letter | Digit | "_" }
Letter     = "a".."z" | "A".."Z" | "_"
Digit      = "0".."9"
```

### Type Patterns

Node and edge types support glob patterns:

```
TypePattern = Identifier ":" ( Identifier | "*" | GlobPattern )

Examples:
  fs:file       -- exact type
  fs:*          -- any type in fs domain
  vcs:bran*     -- glob pattern
```

### Literals

```
StringLit  = "'" { Character | "''" } "'"    -- single quotes, '' to escape
NumberLit  = Integer | Float
Integer    = Digit { Digit }
Float      = Integer "." Integer
BoolLit    = "true" | "false"
```

### Parameters

```
Parameter  = "$" Identifier    -- named: $name, $type
           | "$" Integer       -- positional: $1, $2
```

### Comments

```
Comment = "--" { Character } NewLine    -- line comment
```

### Whitespace

Spaces, tabs, and newlines are ignored except as token separators.

## Grammar (EBNF)

### Top Level

```ebnf
Query = SelectStmt ;

SelectStmt = "SELECT" [ "DISTINCT" ] Columns
             "FROM" Source
             [ "WHERE" Expression ]
             [ "GROUP" "BY" SelectorList ]
             [ "HAVING" Expression ]
             [ "ORDER" "BY" OrderList ]
             [ "LIMIT" Integer [ "OFFSET" Integer ] ] ;
```

### Columns

```ebnf
Columns    = "*" | ColumnList ;
ColumnList = Column { "," Column } ;
Column     = ( Selector | CountCall ) [ "AS" Identifier ] ;
CountCall  = "COUNT" "(" "*" ")" ;
```

### Source

```ebnf
Source      = TableSource | JoinedSource | PatternList ;
TableSource = "nodes" | "edges" ;
JoinedSource = TableSource "," TableFunc ;
TableFunc   = Identifier "(" Selector ")" [ "AS" Identifier ] ;
PatternList = Pattern { "," Pattern } ;
```

**Table functions** allow unpacking JSON arrays:
- `json_each(column)` - unpacks JSON array into rows with `key` and `value` columns

Example:
```sql
-- Count all labels across nodes
SELECT value, COUNT(*) FROM nodes, json_each(labels) GROUP BY value

-- Unpack nested JSON array
SELECT value FROM nodes, json_each(data.tags) WHERE value LIKE 'important%'
```

### Pattern

A pattern is a sequence of nodes connected by edges:

```ebnf
Pattern       = NodePattern { EdgePattern NodePattern } ;

NodePattern   = "(" [ NodeInner ] ")" ;
NodeInner     = Variable [ ":" TypePattern ] [ "WHERE" Expression ]
              | ":" TypePattern [ "WHERE" Expression ] ;

Variable      = Identifier ;
TypePattern   = Identifier ":" ( Identifier | GlobPattern | "*" ) ;
```

### Edge Pattern

```ebnf
EdgePattern   = OutgoingEdge | IncomingEdge | UndirectedEdge ;

OutgoingEdge   = "-" "[" [ EdgeInner ] "]" "->" ;
IncomingEdge   = "<-" "[" [ EdgeInner ] "]" "-" ;
UndirectedEdge = "-" "[" [ EdgeInner ] "]" "-" ;

EdgeInner     = [ Variable ] [ ":" EdgeType ] [ Quantifier ] ;
EdgeType      = Identifier { "|" Identifier } | GlobPattern | "*" ;
Quantifier    = "*" [ HopRange ] ;
HopRange      = Integer [ ".." Integer ] ;
```

**Edge types** support multiple types with OR semantics:
- `:contains` - single type
- `:contains|has` - matches either "contains" OR "has" edges
- `:contains|has|located_at` - matches any of the three types

**Edge quantifiers** for variable-length paths:
- `*` - one or more hops (1..unlimited)
- `*3` - exactly 3 hops
- `*1..3` - between 1 and 3 hops
- `*..5` - up to 5 hops (1..5)
- `*2..` - at least 2 hops (2..unlimited)

### Expressions

```ebnf
Expression = OrExpr ;
OrExpr     = AndExpr { "OR" AndExpr } ;
AndExpr    = NotExpr { "AND" NotExpr } ;
NotExpr    = "NOT" NotExpr | Primary ;

Primary    = Comparison
           | InExpr
           | BetweenExpr
           | LabelExpr
           | IsNullExpr
           | ExistsExpr
           | "(" Expression ")" ;
```

### Comparison Operators

```ebnf
Comparison = Selector CompOp Value ;
CompOp     = "=" | "!=" | "<" | ">" | "<=" | ">=" | "LIKE" | "GLOB" ;
```

- `=`, `!=` - equality
- `<`, `>`, `<=`, `>=` - ordering
- `LIKE` - SQL pattern matching (`%` = any chars, `_` = single char)
- `GLOB` - shell-style pattern matching (`*` = any chars, `?` = single char)

### Set Operations

```ebnf
InExpr      = Selector "IN" "(" ValueList ")" ;
BetweenExpr = Selector "BETWEEN" Value "AND" Value ;
```

### Label Operations

```ebnf
LabelExpr = Selector "CONTAINS" ( "ANY" | "ALL" ) "(" ValueList ")"
          | Selector "NOT" "CONTAINS" "(" ValueList ")" ;
```

- `CONTAINS ANY` - has at least one of the labels
- `CONTAINS ALL` - has all of the labels
- `NOT CONTAINS` - has none of the labels

### NULL Checks

```ebnf
IsNullExpr = Selector "IS" [ "NOT" ] "NULL" ;
```

Note: `= NULL` is not allowed; use `IS NULL` instead.

### Existence Checks

```ebnf
ExistsExpr = [ "NOT" ] "EXISTS" Pattern ;
```

EXISTS checks whether a pattern matches without binding variables to the outer scope.
The pattern can reference variables from the outer scope.

### Order By

```ebnf
OrderList = OrderSpec { "," OrderSpec } ;
OrderSpec = ( Selector | CountCall ) [ "ASC" | "DESC" ] ;
```

Default order is ASC.

### Selectors and Values

```ebnf
SelectorList = Selector { "," Selector } ;
Selector     = Identifier { "." Identifier } ;

ValueList    = Value { "," Value } ;
Value        = StringLit | NumberLit | BoolLit | Parameter ;
```

Selectors support dot notation for nested field access:
- `name` - simple field
- `data.ext` - nested JSON field
- `repo.name` - variable.field in patterns

## Examples

### Flat Queries

```sql
-- All files
SELECT * FROM nodes WHERE type = 'fs:file'

-- Files with specific extension
SELECT * FROM nodes 
WHERE type = 'fs:file' AND data.ext = 'go'

-- Count by type
SELECT type, COUNT(*) FROM nodes GROUP BY type ORDER BY COUNT(*) DESC

-- Complex boolean logic
SELECT * FROM nodes
WHERE (type = 'fs:file' OR type = 'fs:dir')
  AND labels CONTAINS ANY ('important', 'reviewed')
  AND labels NOT CONTAINS ('archived')
  AND data.size > 1000

-- Edge statistics
SELECT type, COUNT(*) FROM edges GROUP BY type

-- Label statistics using json_each
SELECT value, COUNT(*) FROM nodes, json_each(labels) 
WHERE value != '' 
GROUP BY value 
ORDER BY COUNT(*) DESC

-- With parameters
SELECT * FROM nodes WHERE type = $type AND name LIKE $1
```

### Pattern Queries

```sql
-- Files in directories
SELECT file
FROM (dir:fs:dir)-[:contains]->(file:fs:file)
WHERE file.data.ext = 'go'

-- Branches in repos
SELECT repo.name, branch.name
FROM (repo:vcs:repo)-[:has]->(branch:vcs:branch)
WHERE branch.name LIKE 'feature%'

-- Edge variables
SELECT e.type, from.name, to.name
FROM (from)-[e:contains]->(to)
WHERE from.type = 'fs:dir'

-- Transitive closure (all descendants)
SELECT file
FROM (root:fs:dir)-[:contains*]->(file:fs:file)
WHERE root.name = 'src'

-- Bounded depth
SELECT child
FROM (parent:fs:dir)-[:contains*1..3]->(child)
WHERE parent.name = 'project'

-- Multi-type edges (OR logic)
SELECT child
FROM (parent)-[:contains|has]->(child)
WHERE parent.type = 'fs:dir'

-- Incoming edges
SELECT repo
FROM (branch:vcs:branch)<-[:has]-(repo:vcs:repo)
WHERE branch.name = 'main'

-- Undirected (either direction)
SELECT other
FROM (node:fs:file)-[:references]-(other:fs:file)
WHERE node.name = 'main.go'
```

### Multiple Patterns

Multiple patterns are comma-separated and share variables (implicit JOIN):

```sql
-- Repos with specific branch AND readme
SELECT repo, branch, doc
FROM (repo:vcs:repo)-[:has]->(branch:vcs:branch),
     (repo)-[:located_at]->(dir:fs:dir)-[:contains]->(doc:md:document)
WHERE branch.name = 'main' AND doc.name = 'README.md'

-- Directories with specific structure
SELECT foo, doc
FROM (foo:fs:dir)-[:contains]->(bar:fs:dir),
     (bar)-[:contains]->(doc:md:document)
WHERE foo.name LIKE '%foo' AND bar.name = 'bar'
```

### EXISTS / NOT EXISTS

EXISTS checks for pattern existence without returning matched nodes:

```sql
-- Directories containing Go files
SELECT dir
FROM (dir:fs:dir)
WHERE EXISTS (dir)-[:contains]->(:fs:file WHERE name LIKE '%.go')

-- Directories with Go files but no tests
SELECT dir
FROM (dir:fs:dir)-[:contains]->(file:fs:file)
WHERE file.data.ext = 'go'
  AND NOT EXISTS (dir)-[:contains]->(:fs:file WHERE name LIKE '*_test.go')

-- Repos with at least one branch
SELECT repo
FROM (repo:vcs:repo)
WHERE EXISTS (repo)-[:has]->(:vcs:branch)
```

### Inline WHERE

WHERE can be placed inside node patterns for convenience:

```sql
-- Equivalent queries:
SELECT file FROM (dir:fs:dir WHERE dir.name = 'cmd')-[:contains]->(file:fs:file)
SELECT file FROM (dir:fs:dir)-[:contains]->(file:fs:file) WHERE dir.name = 'cmd'

-- Complex inline conditions
SELECT file
FROM (dir:fs:dir WHERE dir.name LIKE 'src%' AND dir.labels CONTAINS ANY ('important'))
     -[:contains]->
     (file:fs:file WHERE file.data.ext = 'go')
```

### Aggregations

```sql
-- Count branches per repo
SELECT repo.name, COUNT(*)
FROM (repo:vcs:repo)-[:has]->(branch:vcs:branch)
GROUP BY repo.name
ORDER BY COUNT(*) DESC

-- Repos with many branches
SELECT repo.name, COUNT(*)
FROM (repo:vcs:repo)-[:has]->(branch:vcs:branch)
GROUP BY repo.name
HAVING COUNT(*) > 5

-- Files imported by multiple files
SELECT file.name, COUNT(*)
FROM (importer:fs:file)-[:imports]->(file:fs:file)
GROUP BY file.name
HAVING COUNT(*) >= 2
```

## Semantic Rules

1. **FROM Required**: Every query must have a FROM clause.

2. **Valid Source**: FROM must be `nodes`, `edges`, or a pattern list.

3. **Variable Binding**: If FROM is a pattern, at least one node or edge must
   have a variable for selection.

4. **Variable Uniqueness**: Variable names must be unique within a query.

5. **Variable References**: Variables used in WHERE, SELECT, ORDER BY must be
   defined in the pattern.

6. **HAVING Requires GROUP BY**: Cannot use HAVING without GROUP BY.

7. **EXISTS Scoping**: Patterns in EXISTS can reference variables from the
   outer scope but do not export new variables.

8. **NULL Comparisons**: Use `IS NULL` / `IS NOT NULL`, not `= NULL`.

## Type Coercion

- String comparisons are case-sensitive
- Numeric comparisons follow standard ordering
- Boolean values are `true` and `false` (case-insensitive)
- Parameters are type-checked at execution time
