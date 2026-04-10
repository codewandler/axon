# Plan: Schema Discovery / Introspection (`Describe`)

**Design**: `.agents/plans/DESIGN-describe-schema.md`  
**Branch**: `feature/describe-schema-introspection`  
**Worktree**: `./worktrees/describe-schema`  
**Date**: 2025-07-01  
**Estimated total time**: ~25 minutes

---

## Task 1 — Define schema types and `Describer` interface

**Files created**: `worktrees/describe-schema/graph/schema.go`  
**Estimated time**: 3 minutes

```go
// graph/schema.go
package graph

import "context"

// NodeTypeInfo describes a node type in the graph.
type NodeTypeInfo struct {
    Type   string   `json:"type"`
    Count  int      `json:"count"`
    Fields []string `json:"fields,omitempty"`
}

// EdgeConnection describes a from→to node-type pair for an edge type.
type EdgeConnection struct {
    From  string `json:"from"`
    To    string `json:"to"`
    Count int    `json:"count"`
}

// EdgeTypeInfo describes an edge type in the graph.
type EdgeTypeInfo struct {
    Type        string           `json:"type"`
    Count       int              `json:"count"`
    Connections []EdgeConnection `json:"connections"`
}

// SchemaDescription is the result of schema introspection.
type SchemaDescription struct {
    NodeTypes []NodeTypeInfo `json:"node_types"`
    EdgeTypes []EdgeTypeInfo `json:"edge_types"`
}

// Describer is an optional interface that storage implementations may satisfy
// to provide schema introspection. It is intentionally NOT part of Storage so
// existing test mocks are not affected.
type Describer interface {
    DescribeSchema(ctx context.Context, includeFields bool) (*SchemaDescription, error)
}
```

**Verification**:
```
go build ./graph/...
```

---

## Task 2 — Implement `DescribeSchema` in the SQLite adapter

**Files created**: `worktrees/describe-schema/adapters/sqlite/describe.go`  
**Estimated time**: 8 minutes

Implementation uses three raw SQL queries:
1. Node types + counts via `SELECT type, COUNT(*) FROM nodes GROUP BY type ORDER BY COUNT(*) DESC`
2. (Optional) Distinct JSON keys per type via `json_each(data)` sampled at 500 nodes
3. Edge connections via 3-way join: `edges JOIN nodes(from) JOIN nodes(to) GROUP BY e.type, from_type, to_type ORDER BY e.type, count DESC`

After building the raw data, group edge connections under their edge type.

**Verification**:
```
go build ./adapters/sqlite/...
go test -v -run TestDescribeSchema ./adapters/sqlite/...
```

---

## Task 3 — Write tests for `DescribeSchema` (SQLite adapter)

**Files created**: `worktrees/describe-schema/adapters/sqlite/describe_test.go`  
**Estimated time**: 5 minutes

Table-driven test cases:
- Empty graph → `SchemaDescription{NodeTypes: nil, EdgeTypes: nil}` (or empty slices)
- Graph with nodes of two types, no edges → correct counts, no edge types
- Graph with edges → correct connections, counts, from/to types
- `includeFields=true` → fields populated; `includeFields=false` → fields nil

Use `setupTestDB(t)` helper that already exists in the package.

**Verification**:
```
go test -v -run TestDescribeSchema ./adapters/sqlite/...
```

---

## Task 4 — Add `Axon.Describe` public method

**Files created**: `worktrees/describe-schema/describe.go`  
**Estimated time**: 3 minutes

```go
// describe.go
package axon

import (
    "context"
    "fmt"

    "github.com/codewandler/axon/graph"
)

// Describe returns a schema description of the graph: all node types with
// counts and (optionally) field names, plus all edge types with from/to
// node-type pairs and counts.
//
// includeFields causes an additional per-type query to discover JSON field
// names found in the data payload. This samples up to 500 nodes per type and
// is slightly slower on large graphs.
func (a *Axon) Describe(ctx context.Context, includeFields bool) (*graph.SchemaDescription, error) {
    if err := a.graph.Storage().Flush(ctx); err != nil {
        return nil, fmt.Errorf("describe: flush: %w", err)
    }
    d, ok := a.graph.Storage().(graph.Describer)
    if !ok {
        return nil, fmt.Errorf("describe: storage backend does not support schema introspection")
    }
    return d.DescribeSchema(ctx, includeFields)
}
```

**Verification**:
```
go build ./...
```

---

## Task 5 — Write integration test for `Axon.Describe`

**Files created**: `worktrees/describe-schema/describe_test.go`  
**Estimated time**: 4 minutes

Test:
- Creates an in-memory Axon instance
- Indexes a small temp directory with some Go files
- Calls `Describe(ctx, true)` and `Describe(ctx, false)`
- Asserts:
  - At least one `fs:file` node type in the result
  - `includeFields=false` → `Fields == nil` for all node types
  - `includeFields=true` → `Fields` non-empty for known types like `fs:file`
  - Total node count matches sum of individual type counts

**Verification**:
```
go test -v -run TestAxon_Describe ./...
```

---

## Task 6 — Add `axon describe` CLI command

**Files created**: `worktrees/describe-schema/cmd/axon/describe.go`  
**Files modified**: `worktrees/describe-schema/cmd/axon/main.go`  
**Estimated time**: 5 minutes

Flags:
- `--output/-o` — `text` (default) or `json`
- `--fields/-f` — include field names (calls `Describe(ctx, true)`)
- `--global/-g` — ignored for now (describe is always global); present for consistency

Text rendering:
```
Node Types (N types, M nodes total):
  <type>    <count>
  ...

Edge Types (N types, M edges total):
  <type>    <count>
    <from> → <to>    <count>
    ...

Fields by node type:           ← only shown with --fields
  <type>: field1, field2, ...
```

Register in `main.go`:
```go
rootCmd.AddCommand(describeCmd)
```

**Verification**:
```
go build -o ./bin/axon ./cmd/axon
./bin/axon describe --help
./bin/axon describe -o json
./bin/axon describe --fields
```

---

## Task 7 — Update AGENTS.md and README.md

**Files modified**:
- `worktrees/describe-schema/AGENTS.md` — add `describe` to the available commands list
- `worktrees/describe-schema/README.md` — add `axon describe` section to CLI reference

**Estimated time**: 2 minutes

**AGENTS.md change** — in the "Available commands" list:
```
- `describe` - Show graph schema: node types, edge types, fields, and connection patterns
```

**README.md change** — add to CLI reference section:
```markdown
### axon describe

Show the schema of the graph: all node types with counts, edge types with
from/to connection patterns, and (with `--fields`) the JSON data fields
available on each node type.

    axon describe              # schema overview (text)
    axon describe -o json      # machine-readable JSON
    axon describe --fields     # include data field names per node type
```

**Verification**:
```
go build ./...
go test ./...
```

---

## Task 8 — Final verification

```bash
go build ./...
go vet ./...
go test ./...
./bin/axon describe -o json   # smoke-test on the axon repo itself
```

All tests must pass. `go vet` must be clean.
