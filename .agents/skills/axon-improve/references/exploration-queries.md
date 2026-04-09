# Exploration Queries Reference

A catalogue of AQL queries and CLI invocations useful for exploring the axon
codebase with axon itself.

---

## Codebase Structure

```bash
# Top-level directory tree
axon tree --depth 2 --types

# All Go packages (directories containing .go files)
axon query "
  SELECT DISTINCT parent.name, parent.uri
  FROM nodes AS parent
  JOIN edges ON edges.from_id = parent.id
  JOIN nodes AS child ON child.id = edges.to_id
  WHERE parent.type = 'fs:dir'
    AND child.type = 'fs:file'
    AND child.data.ext = 'go'
    AND edges.type = 'contains'
  ORDER BY parent.uri
"

# File count per directory
axon query "
  SELECT parent.name, COUNT(*) as files
  FROM nodes AS parent
  JOIN edges ON edges.from_id = parent.id
  JOIN nodes AS child ON child.id = edges.to_id
  WHERE parent.type = 'fs:dir'
    AND child.type = 'fs:file'
    AND edges.type = 'contains'
  GROUP BY parent.id
  ORDER BY files DESC
"

# Largest Go source files
axon query "
  SELECT name, data.size
  FROM nodes
  WHERE type = 'fs:file' AND data.ext = 'go'
  ORDER BY data.size DESC
  LIMIT 20
"

# All test files
axon find --ext go --global | grep _test
# or via AQL:
axon query "
  SELECT name, uri
  FROM nodes
  WHERE type = 'fs:file' AND name GLOB '*_test.go'
  ORDER BY name
"
```

---

## Code Organisation

```bash
# All CLI command files
axon query "
  SELECT name, uri
  FROM nodes
  WHERE type = 'fs:file'
    AND data.ext = 'go'
    AND uri GLOB '*cmd/axon*'
  ORDER BY name
"

# All adapter files
axon query "
  SELECT name, uri
  FROM nodes
  WHERE type = 'fs:file'
    AND data.ext = 'go'
    AND uri GLOB '*adapters*'
  ORDER BY name
"

# All indexer files (not tests)
axon query "
  SELECT name, uri
  FROM nodes
  WHERE type = 'fs:file'
    AND data.ext = 'go'
    AND uri GLOB '*indexer*'
    AND name NOT GLOB '*_test.go'
  ORDER BY name
"

# AQL-related files
axon query "
  SELECT name, uri, data.size
  FROM nodes
  WHERE type = 'fs:file'
    AND data.ext = 'go'
    AND uri GLOB '*aql*'
  ORDER BY data.size DESC
"
```

---

## Documentation Coverage

```bash
# All markdown documents
axon find --type md:document --global --output table

# All headings across all docs
axon query "
  SELECT name, uri
  FROM nodes
  WHERE type = 'md:heading'
  ORDER BY uri, name
"

# Sections in README
axon query "
  SELECT name
  FROM nodes
  WHERE type = 'md:heading'
    AND uri GLOB '*README*'
  ORDER BY name
"

# Commands mentioned in README
axon search "what CLI commands are documented in README.md"

# Does the README mention the 'ask' command?
axon query "
  SELECT name, uri
  FROM nodes
  WHERE type = 'md:section'
    AND name GLOB '*ask*'
"
```

---

## Graph Health Checks

```bash
# Total node and edge counts
axon query "SELECT COUNT(*) FROM nodes"
axon query "SELECT COUNT(*) FROM edges"

# Node type distribution
axon query "
  SELECT type, COUNT(*) as count
  FROM nodes
  GROUP BY type
  ORDER BY count DESC
"

# Edge type distribution
axon query "
  SELECT type, COUNT(*) as count
  FROM edges
  GROUP BY type
  ORDER BY count DESC
"

# Nodes with no outgoing edges
axon query "
  SELECT id, type, name
  FROM nodes
  WHERE NOT EXISTS (
    SELECT 1 FROM edges WHERE from_id = nodes.id
  )
  ORDER BY type, name
  LIMIT 30
"

# Nodes with no incoming edges (possible roots or orphans)
axon query "
  SELECT id, type, name
  FROM nodes
  WHERE NOT EXISTS (
    SELECT 1 FROM edges WHERE to_id = nodes.id
  )
  ORDER BY type, name
  LIMIT 30
"

# Duplicate URIs (should always be empty)
axon query "
  SELECT uri, COUNT(*) as c
  FROM nodes
  GROUP BY uri
  HAVING c > 1
"

# Self-referencing edges (from_id = to_id)
axon query "
  SELECT id, type, from_id, to_id
  FROM edges
  WHERE from_id = to_id
"
```

---

## Label Analysis

```bash
# Most common labels
axon query "
  SELECT value, COUNT(*) as count
  FROM nodes, json_each(labels)
  WHERE value != ''
  GROUP BY value
  ORDER BY count DESC
  LIMIT 30
"

# Nodes with no labels
axon query "
  SELECT type, COUNT(*) as count
  FROM nodes
  WHERE labels = '[]' OR labels IS NULL
  GROUP BY type
  ORDER BY count DESC
"

# All label namespaces (prefix before ':')
axon query "
  SELECT value
  FROM nodes, json_each(labels)
  WHERE value GLOB '*:*'
  GROUP BY value
  ORDER BY value
"
```

---

## Relationship Exploration

```bash
# All contains relationships between dirs
axon query "
  SELECT p.name as parent, c.name as child
  FROM nodes p
  JOIN edges e ON e.from_id = p.id
  JOIN nodes c ON c.id = e.to_id
  WHERE p.type = 'fs:dir'
    AND c.type = 'fs:dir'
    AND e.type = 'contains'
  ORDER BY p.name, c.name
"

# Git repo → branch relationships
axon query "
  SELECT r.name as repo, b.name as branch
  FROM nodes r
  JOIN edges e ON e.from_id = r.id
  JOIN nodes b ON b.id = e.to_id
  WHERE r.type = 'vcs:repo'
    AND b.type = 'vcs:branch'
    AND e.type = 'has'
  ORDER BY r.name, b.name
"

# Pattern query: files inside adapters/sqlite
axon query "
  SELECT file
  FROM (dir)-[:contains]->(file)
  WHERE dir.uri GLOB '*adapters/sqlite*'
    AND file.type = 'fs:file'
"

# Variable-length: all descendants of cmd/axon
axon query "
  SELECT child
  FROM (root)-[:contains*1..5]->(child)
  WHERE root.uri GLOB '*cmd/axon*'
    AND root.type = 'fs:dir'
"
```

---

## `ask` Queries for Architecture Understanding

```bash
# Core data model
axon search "what is Node"
axon search "what is Edge"
axon search "what is Graph"

# Storage layer
axon search "what is the Storage interface"
axon search "what implements Storage"
axon search "methods of Storage"

# Indexer system
axon search "what is the Indexer interface"
axon search "what implements Indexer"
axon search "explain the indexer subscription system"
axon search "how does event routing work"

# AQL
axon search "describe the AQL compiler"
axon search "what is the AQL builder"
axon search "explain query validation"
axon search "list structs in the aql package"

# CLI
axon search "how does db resolution work"
axon search "explain the output formatting system"
axon search "what is the results package"
```

---

## `context` Queries for Deep Dives

```bash
# Full AQL pipeline
axon context --task "trace an AQL query from CLI input to SQLite execution" --tokens 12000

# Storage interface and implementation
axon context --task "Storage interface and SQLite adapter implementation" --tokens 10000

# CLI command pattern
axon context --task "how a CLI command is structured, flags, db lookup, output" --tokens 8000

# Indexer system
axon context --task "indexer interface, event system, and subscription routing" --tokens 10000

# Error handling
axon context --task "error handling patterns across the codebase" --tokens 8000

# Testing patterns
axon context --task "test helpers and common test patterns" --tokens 6000
```
