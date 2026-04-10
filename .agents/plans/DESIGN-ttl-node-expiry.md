# DESIGN: TTL (time-to-live) for Nodes and Edges — Issue #26

## Problem

Agents using Axon for custom memory (`axon_write_node`) have no way to express
that a piece of knowledge is time-bounded. Nodes and edges persist indefinitely.
For session notes, working hypotheses, and short-term observations the graph
accumulates stale context forever.

## Proposed Solution

Add an optional `ExpiresAt *time.Time` field to both `graph.Node` and `graph.Edge`.
Expired records are treated as non-existent by all read paths and are physically
removed by `axon gc` (on-demand GC).

A convenience builder method `WithTTL(d time.Duration)` sets `ExpiresAt = now + d`.
Callers that want immortal nodes (the default) set nothing — `ExpiresAt == nil`.

## Architecture

### Layer 1: Data model (`graph/node.go`, `graph/edge.go`)

```go
// Node
ExpiresAt *time.Time `json:"expires_at,omitempty"`

func (n *Node) WithTTL(d time.Duration) *Node {
    t := time.Now().Add(d)
    n.ExpiresAt = &t
    return n
}

// Edge — same pattern
```

`Clone()` in `node.go` is updated to copy `ExpiresAt`.

### Layer 2: Storage interface (`graph/storage.go`)

`StalenessManager` gains one new method:

```go
DeleteExpired(ctx context.Context) (int64, int64, error)
// returns (nodesDeleted, edgesDeleted, error)
```

### Layer 3: SQLite adapter (`adapters/sqlite/sqlite.go`)

- **Migration 11**: `ALTER TABLE nodes ADD COLUMN expires_at INTEGER` + index
  `CREATE INDEX idx_nodes_expires_at ON nodes(expires_at)`.
- **Migration 12**: same for edges.
- **Write path** (`tryFlushBatch`): store `expires_at` as unix epoch (nullable
  `sql.NullInt64`).
- **Read path** (`GetNode`, `GetNodeByURI`, `GetNodeByKey`, `FindNodes`,
  `CountNodes`, `GetEdge`, `GetEdgesFrom`, `GetEdgesTo`, `FindSimilar`):
  add `AND (expires_at IS NULL OR expires_at > unixepoch())` to every
  read query. `buildNodeFilterArgs` is the central place for node reads.
- **Scan functions** (`scanNode`, `scanEdges`): read the new column and
  populate the field.
- **`DeleteExpired`**: physically DELETE expired rows from both tables.
- **Orphaned-edge detection** (`FindOrphanedEdges`, `CountOrphanedEdges`,
  `DeleteOrphanedEdges`): NOT changed — they operate on the physical table
  (expired but not yet GC'd nodes are still real rows; orphan detection is
  about missing nodes entirely).
- **Stale cleanup** (`FindStaleByURIPrefix`, `DeleteStaleByURIPrefix`): NOT
  changed — generation-based cleanup is independent of TTL.

### Layer 4: AQL compiler (`adapters/sqlite/aql.go`)

The AQL compiler generates raw SQL against the `nodes` and `edges` tables.
Rather than threading a filter through every branch of the 3 000-line compiler,
we add two SQLite views in migration 11/12:

```sql
CREATE VIEW active_nodes AS
  SELECT * FROM nodes WHERE expires_at IS NULL OR expires_at > unixepoch();

CREATE VIEW active_edges AS
  SELECT * FROM edges WHERE expires_at IS NULL OR expires_at > unixepoch();
```

The AQL compiler switches from `nodes` / `edges` to `active_nodes` /
`active_edges` in all SELECT contexts (table names embedded as string
constants). INSERT / DELETE / UPDATE in `sqlite.go` continue to target the
actual tables.

### Layer 5: CLI — `axon write-node` command (`cmd/axon/write_node.go`)

New subcommand that lets CLI users (and the `axon_write_node` agent tool)
persist a custom node with optional TTL:

```
axon write-node \
  --uri  memory://session-abc/task \
  --type memory:note \
  --name "Current focus" \
  --data '{"text":"investigating auth bug"}' \
  --ttl  2h
```

Flags: `--uri` (required), `--type` (required), `--name`, `--data` (JSON),
`--ttl` (duration string, e.g. `"30m"`, `"2h"`, `"24h"`). Prints the node ID
on success.

### Layer 6: GC command (`cmd/axon/gc.go`)

`axon gc` now also calls `DeleteExpired` and prints:

```
Deleted N expired nodes, M expired edges
Deleted K orphaned edges
```

Dry-run counts expired records without deleting them.

### Layer 7: Watch mode (`axon.go`)

A background ticker in `Watch` calls `DeleteExpired` every 5 minutes
(configurable via `WatchOptions.GCInterval`, default 5 min; 0 = disabled).

## Key Decisions

| Decision | Rationale |
|---|---|
| `expires_at INTEGER` (unix epoch) | SQLite `unixepoch()` comparison is index-friendly; TEXT parsing is slower |
| Opt-in (nil = immortal) | No breaking change; indexer-owned nodes never receive a TTL |
| Views for AQL filtering | Avoids threading a filter through 3 000 lines of SQL generation; clean separation |
| GC on `axon gc` | Matches existing GC pattern for orphaned edges |
| Watch background ticker | Long-lived watch mode would otherwise accumulate expired records |
| `DeleteExpired` returns `(nodes, edges, error)` | GC can report both counts |
| No auto-renewal | Callers upsert via same URI to renew; keeps the model simple |

## Out of Scope

- Per-indexer TTL (index-owned nodes are never ephemeral by design)
- `axon_label` changes — labels on expired nodes are irrelevant (nodes invisible)
- Watch-mode GC interval exposed as CLI flag (can be added later)

## Files Changed

| File | Change |
|---|---|
| `graph/node.go` | Add `ExpiresAt` field, `WithTTL`, update `Clone` |
| `graph/edge.go` | Add `ExpiresAt` field, `WithTTL` |
| `graph/storage.go` | Add `DeleteExpired` to `StalenessManager` |
| `adapters/sqlite/sqlite.go` | Migrations 11+12, write path, read paths, `DeleteExpired` impl |
| `adapters/sqlite/aql.go` | Switch `FROM nodes`/`FROM edges` to views in SELECT |
| `cmd/axon/write_node.go` | New `write-node` subcommand |
| `cmd/axon/main.go` | Register `write-node` command |
| `cmd/axon/gc.go` | Call `DeleteExpired`, report count, dry-run support |
| `axon.go` | Watch-mode background GC ticker |
| `adapters/sqlite/sqlite_test.go` | TTL tests |
| `api_test.go` | TTL integration tests |
| `README.md` | TTL usage example, write-node command docs |
| `.agents/skills/axon/SKILL.md` | `write-node` command docs |
