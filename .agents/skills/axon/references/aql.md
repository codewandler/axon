# AQL Cheatsheet

Axon Query Language for querying graph data.

## Table Queries

Query nodes/edges as flat tables:

```sql
-- All files
SELECT * FROM nodes WHERE type = 'fs:file'

-- Go files larger than 1KB
SELECT * FROM nodes 
WHERE type = 'fs:file' AND data.ext = 'go' AND data.size > 1000

-- Count by type
SELECT type, COUNT(*) FROM nodes GROUP BY type ORDER BY COUNT(*) DESC

-- Top 10 largest files
SELECT name, data.size FROM nodes 
WHERE type = 'fs:file' 
ORDER BY data.size DESC 
LIMIT 10
```

## JSON Field Access

Use dot notation for `data` field:

```sql
SELECT * FROM nodes WHERE data.ext = 'go'
SELECT * FROM nodes WHERE data.size BETWEEN 100 AND 1000
SELECT * FROM nodes WHERE data.mode = 755
```

## Operators

- **Comparison**: `=`, `!=`, `<`, `>`, `<=`, `>=`
- **Pattern**: `LIKE` (SQL), `GLOB` (shell)
- **Set**: `IN (v1, v2, ...)`, `BETWEEN a AND b`
- **Null**: `IS NULL`, `IS NOT NULL`
- **Boolean**: `AND`, `OR`, `NOT`

## Label Operations

```sql
SELECT * FROM nodes WHERE labels CONTAINS ANY ('important', 'reviewed')
SELECT * FROM nodes WHERE labels CONTAINS ALL ('test', 'verified')
SELECT * FROM nodes WHERE labels NOT CONTAINS ('archived')
```

## Pattern Queries

Graph pattern matching with Cypher-like syntax:

```sql
-- Files in directories
SELECT file FROM (dir:fs:dir)-[:contains]->(file:fs:file)

-- Go files in 'cmd' directory
SELECT file 
FROM (dir:fs:dir)-[:contains]->(file:fs:file)
WHERE dir.name = 'cmd' AND file.data.ext = 'go'

-- Branches in repositories
SELECT branch 
FROM (repo:vcs:repo)-[:has]->(branch:vcs:branch)
WHERE repo.name = 'myproject'
```

## Pattern Syntax

- `(var:type)` - Node with variable and type
- `->` - Outgoing edge
- `<-` - Incoming edge
- `-` - Undirected (either direction)
- `[var:type]` - Edge variable
- `[:type1|type2]` - Multi-type edge (OR)

## Variable-Length Paths

Recursive traversal:

```sql
-- All descendants (1 or more hops)
SELECT desc FROM (root:fs:dir)-[:contains*]->(desc)

-- 1 to 3 hops
SELECT child FROM (parent:fs:dir)-[:contains*1..3]->(child)

-- Exactly 2 hops
SELECT node FROM (start)-[:contains*2]->(node)

-- At least 2 hops (unbounded)
SELECT desc FROM (root)-[:contains*2..]->(desc)

-- Multi-type recursive
SELECT node FROM (root)-[:contains|has*1..5]->(node)
```

## Multiple Patterns

Comma-separated patterns share variables (implicit JOIN):

```sql
-- Files in repos at specific location
SELECT file
FROM (repo:vcs:repo)-[:located_at]->(dir:fs:dir),
     (dir)-[:contains]->(file:fs:file)
WHERE repo.name = 'myrepo' AND file.data.ext = 'go'
```

## Aggregation

```sql
-- Count files per directory
SELECT dir.name, COUNT(*)
FROM (dir:fs:dir)-[:contains]->(file:fs:file)
GROUP BY dir.name
ORDER BY COUNT(*) DESC

-- Directories with many files
SELECT dir.name, COUNT(*)
FROM (dir:fs:dir)-[:contains]->(file:fs:file)
GROUP BY dir.name
HAVING COUNT(*) > 10
```

## Existence Checks

Test pattern existence:

```sql
-- Directories with Go files
SELECT dir
FROM (dir:fs:dir)
WHERE EXISTS (dir)-[:contains]->(:fs:file WHERE data.ext = 'go')

-- Repos without branches
SELECT repo
FROM (repo:vcs:repo)
WHERE NOT EXISTS (repo)-[:has]->(:vcs:branch)
```

## Edge Variables

Examine edge properties:

```sql
SELECT e.type, from.name, to.name
FROM (from)-[e:contains]->(to)
WHERE from.type = 'fs:dir'
```

## Inline WHERE

Filter inside patterns:

```sql
SELECT file
FROM (dir:fs:dir WHERE dir.name = 'src')
     -[:contains]->
     (file:fs:file WHERE file.data.ext = 'go')
```

## Query Modifiers

```sql
-- DISTINCT
SELECT DISTINCT type FROM nodes

-- ORDER BY
SELECT * FROM nodes ORDER BY name ASC
SELECT * FROM nodes ORDER BY data.size DESC

-- LIMIT / OFFSET
SELECT * FROM nodes LIMIT 10
SELECT * FROM nodes LIMIT 10 OFFSET 20

-- GROUP BY / HAVING
SELECT type, COUNT(*) FROM nodes 
GROUP BY type 
HAVING COUNT(*) > 5
```

## Common Patterns

**Find all Go files**:
```sql
SELECT * FROM nodes WHERE type = 'fs:file' AND data.ext = 'go'
```

**Files in specific directory**:
```sql
SELECT file FROM (dir:fs:dir)-[:contains]->(file:fs:file)
WHERE dir.name = 'cmd'
```

**All descendants of a directory**:
```sql
SELECT desc FROM (root:fs:dir)-[:contains*]->(desc)
WHERE root.name = 'src'
```

**Repos with their branches**:
```sql
SELECT repo.name, branch.name
FROM (repo:vcs:repo)-[:has]->(branch:vcs:branch)
```

**Files that import other files**:
```sql
SELECT file, imported
FROM (file:fs:file)-[:imports]->(imported:fs:file)
```

**Cross-references between files**:
```sql
SELECT a.name, b.name
FROM (a:fs:file)-[:references]-(b:fs:file)
WHERE a.data.ext = 'go'
```
