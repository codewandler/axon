# Design: Schema Discovery / Introspection (`Describe`)

**Issue**: https://github.com/codewandler/axon/issues/3  
**Branch**: `feature/describe-schema-introspection`  
**Worktree**: `./worktrees/describe-schema`  
**Date**: 2025-07-01

---

## Problem

There is no way to discover what node types, edge types, or fields exist in the graph.  
Agents using `axon_search` and `axon_query` must guess type strings (`go:func`, `vcs:commit`, …)
and field names from convention and trial-and-error.

---

## Proposed Solution

Add a `Describe(ctx context.Context) (*SchemaDescription, error)` method to the `*Axon` struct.
This becomes the programmatic API for schema introspection. A companion CLI command `axon describe`
exposes it to the terminal, and the future `axon_describe` tool will expose it to AI agents.

---

## Output Shape

```go
type SchemaDescription struct {
    NodeTypes []NodeTypeInfo `json:"node_types"`
    EdgeTypes []EdgeTypeInfo `json:"edge_types"`
}

type NodeTypeInfo struct {
    Type   string   `json:"type"`
    Count  int      `json:"count"`
    Fields []string `json:"fields,omitempty"` // top-level JSON keys found in `data`
}

type EdgeTypeInfo struct {
    Type        string           `json:"type"`
    Count       int              `json:"count"`
    Connections []EdgeConnection `json:"connections"` // from/to type pairs, ordered by count desc
}

type EdgeConnection struct {
    From  string `json:"from"`
    To    string `json:"to"`
    Count int    `json:"count"`
}
```

The JSON output matches the structure proposed in the issue.

---

## Architecture

### Layer 1 — graph package: types + optional interface

New file `graph/schema.go` defines the types above and a `Describer` interface:

```go
type Describer interface {
    DescribeSchema(ctx context.Context) (*SchemaDescription, error)
}
```

The `Describer` interface is **not** added to the monolithic `Storage` interface — it is kept
separate so that existing mock/test storage implementations don't need to change.

### Layer 2 — SQLite adapter: raw SQL implementation

New file `adapters/sqlite/describe.go` implements `DescribeSchema` on `*Storage` using two
raw SQL queries:

**Node types + counts + fields**
```sql
-- Step 1: types and counts
SELECT type, COUNT(*) FROM nodes GROUP BY type ORDER BY COUNT(*) DESC

-- Step 2: distinct JSON object keys per type (sampled, max 500 nodes per type)
SELECT DISTINCT je.key
FROM (SELECT data FROM nodes WHERE type = ? LIMIT 500) n,
     json_each(n.data) je
```

**Edge types + counts + from/to pairs**
```sql
SELECT e.type, nf.type AS from_type, nt.type AS to_type, COUNT(*) AS count
FROM edges e
JOIN nodes nf ON e.from_id = nf.id
JOIN nodes nt ON e.to_id   = nt.id
GROUP BY e.type, from_type, to_type
ORDER BY e.type, count DESC
```

Both queries are read-only and require no schema changes.

### Layer 3 — Axon public method

`Axon.Describe` (new file `describe.go` in root package):
```go
func (a *Axon) Describe(ctx context.Context) (*graph.SchemaDescription, error) {
    if err := a.graph.Storage().Flush(ctx); err != nil {
        return nil, fmt.Errorf("describe: flush: %w", err)
    }
    d, ok := a.graph.Storage().(graph.Describer)
    if !ok {
        return nil, fmt.Errorf("describe: storage does not support schema introspection")
    }
    return d.DescribeSchema(ctx)
}
```

### Layer 4 — CLI command `axon describe`

New file `cmd/axon/describe.go` with flags:
- `--global` / `-g` — use entire graph (default: scoped to CWD, same as `axon stats`)
- `--output` / `-o` — `text` (default) or `json`
- `--fields` / `-f` — include field names per node type (slightly slower; default: false)

Text output example:
```
Node Types (12 types, 15,432 nodes total):
  go:func          8,234
  fs:file          3,421
  go:struct          892
  ...

Edge Types (8 types, 42,100 edges):
  contains        15,000
    fs:dir → fs:file          12,000
    fs:dir → fs:dir            3,000
  calls            5,000
    go:func → go:func          5,000
  ...
```

With `--fields`:
```
Fields by node type:
  go:func    name, signature, doc, file, line, exported, recv
  fs:file    name, ext, size, is_binary
  ...
```

---

## Key Design Decisions

| Decision | Rationale |
|----------|-----------|
| `graph.Describer` is optional (not in `Storage`) | Keeps existing test mocks valid |
| Fields discovered from live data, not type registry | Reflects what is actually stored; handles custom indexers |
| Field discovery samples max 500 nodes per type | Bounded cost on large graphs; fields are stable per type |
| Edge connections show all from/to pairs | More accurate than a single "from"/"to" shown in the issue |
| `Flush()` before describe | SQLite adapter buffers writes; ensures fresh data |
| `--fields` flag off by default on CLI | Avoids N+1 queries unless explicitly needed |

---

## Out of Scope

- Cardinality stats (min/max/avg edges per node type)
- Field value ranges or histograms
- AQL-based schema queries (future `axon_describe` agent tool)
- Scoped describe (per directory) — global only for now; simpler first pass

---

## Files Changed

| File | Action |
|------|--------|
| `graph/schema.go` | NEW — types + `Describer` interface |
| `adapters/sqlite/describe.go` | NEW — `DescribeSchema` SQL implementation |
| `adapters/sqlite/describe_test.go` | NEW — tests for describe |
| `describe.go` | NEW — `Axon.Describe` public method |
| `describe_test.go` | NEW — integration test for `Axon.Describe` |
| `cmd/axon/describe.go` | NEW — CLI command |
| `cmd/axon/main.go` | MODIFY — register `describeCmd` |
| `AGENTS.md` | MODIFY — document `axon describe` command |
| `README.md` | MODIFY — document `axon describe` command |
