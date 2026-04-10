# PLAN: TTL for Nodes and Edges — Issue #26

## Task 1: Data model — `graph/node.go` and `graph/edge.go`

**Files**: `graph/node.go`, `graph/edge.go`

### graph/node.go
- Add `ExpiresAt *time.Time \`json:"expires_at,omitempty"\`` to `Node` struct after `UpdatedAt`.
- Add `WithTTL(d time.Duration) *Node` method.
- Update `Clone()` to copy `ExpiresAt`.

### graph/edge.go
- Add `ExpiresAt *time.Time \`json:"expires_at,omitempty"\`` to `Edge` struct after `CreatedAt`.
- Add `WithTTL(d time.Duration) *Edge` method.

**Verification**: `go build ./graph/...`

---

## Task 2: Storage interface — `graph/storage.go`

**File**: `graph/storage.go`

Add `DeleteExpired(ctx context.Context) (int64, int64, error)` to `StalenessManager`:

```go
// DeleteExpired removes all nodes and edges whose ExpiresAt is in the past.
// Returns (nodesDeleted, edgesDeleted, error).
DeleteExpired(ctx context.Context) (int64, int64, error)
```

**Verification**: `go build ./graph/...`

---

## Task 3: SQLite migrations and write path

**File**: `adapters/sqlite/sqlite.go`

### 3a. Migrations
Add migrations 11 and 12:

```sql
-- Migration 11
ALTER TABLE nodes ADD COLUMN expires_at INTEGER;
CREATE INDEX IF NOT EXISTS idx_nodes_expires_at ON nodes(expires_at);

CREATE VIEW IF NOT EXISTS active_nodes AS
  SELECT * FROM nodes WHERE expires_at IS NULL OR expires_at > unixepoch();

-- Migration 12
ALTER TABLE nodes ADD COLUMN root TEXT; -- compatibility guard (skip if exists)
ALTER TABLE edges ADD COLUMN expires_at INTEGER;
CREATE INDEX IF NOT EXISTS idx_edges_expires_at ON edges(expires_at);

CREATE VIEW IF NOT EXISTS active_edges AS
  SELECT * FROM edges WHERE expires_at IS NULL OR expires_at > unixepoch();
```

> Note: migrations 11 and 12 are separate so each can be guarded individually.

### 3b. Write path — `tryFlushBatch`
- Node INSERT: add `expires_at` column; use `sql.NullInt64{Int64: n.ExpiresAt.Unix(), Valid: n.ExpiresAt != nil}`.
- Edge INSERT: same for edge.
- Update the `ON CONFLICT ... DO UPDATE` to include `expires_at = excluded.expires_at`.

### 3c. Scan functions
- `scanNode`: select `expires_at` (nullable integer); parse into `node.ExpiresAt`.
- `scanEdges` / inline scan in `GetEdge`: same.

### 3d. Implement `DeleteExpired`

```go
func (s *Storage) DeleteExpired(ctx context.Context) (int64, int64, error) {
    if err := s.Flush(ctx); err != nil {
        return 0, 0, err
    }
    nr, err := s.db.ExecContext(ctx,
        `DELETE FROM nodes WHERE expires_at IS NOT NULL AND expires_at <= unixepoch()`)
    // ... er for edges ...
}
```

**Verification**: `go build ./adapters/sqlite/...`

---

## Task 4: Read-path TTL filtering in `sqlite.go`

**File**: `adapters/sqlite/sqlite.go`

### 4a. Node read queries
Change all node SELECT queries to use `active_nodes` instead of `nodes`:
- `GetNode`: `FROM active_nodes WHERE id = ?`
- `GetNodeByURI`: `FROM active_nodes WHERE uri = ?`
- `GetNodeByKey`: `FROM active_nodes WHERE type = ? AND key = ?`
- `FindNodes`: `FROM active_nodes WHERE 1=1` (the main query string)
- `CountNodes`: all variants use `active_nodes`
- `FindStaleByURIPrefix`: keep `FROM nodes` (stale cleanup is TTL-independent)
- `FindSimilar`: uses `GetNode` → automatically filtered

### 4b. Edge read queries
- `GetEdge`: `FROM active_edges WHERE id = ?`
- `GetEdgesFrom`: `FROM active_edges WHERE from_id = ?`
- `GetEdgesTo`: `FROM active_edges WHERE to_id = ?`
- `CountEdges` (both variants): use `active_edges e` or `JOIN active_edges e`
- `FindOrphanedEdges` / `CountOrphanedEdges` / `DeleteOrphanedEdges`: keep `FROM edges` (orphan check must see all physical rows)

**Verification**: `go test ./adapters/sqlite/... -run TestTTL` (written in Task 5)

---

## Task 5: Tests — SQLite adapter and API

**Files**: `adapters/sqlite/sqlite_test.go`, `api_test.go`

### sqlite_test.go — table-driven TTL tests

```go
func TestTTL_NodeExpiry(t *testing.T) {
    // 1. Write node with TTL already elapsed (expires_at in the past)
    // 2. GetNode → storage.ErrNodeNotFound
    // 3. FindNodes → empty
    // 4. DeleteExpired → 1 node deleted
    // 5. Physical row should be gone
}

func TestTTL_NodeVisibleBeforeExpiry(t *testing.T) {
    // Write node with TTL = 1h → GetNode succeeds
}

func TestTTL_NilTTLIsImmortal(t *testing.T) {
    // nil ExpiresAt → always visible
}

func TestTTL_EdgeExpiry(t *testing.T) {
    // Same pattern for edges
}

func TestTTL_DeleteExpiredCounts(t *testing.T) {
    // Write 3 expired nodes + 1 live → DeleteExpired returns 3 nodes, 0 edges
}

func TestTTL_GCEdges(t *testing.T) {
    // Expired edge with live nodes → GetEdgesFrom returns empty; DeleteExpired removes it
}
```

To simulate "past expiry" without sleeping, write a node with:
```go
past := time.Now().Add(-1 * time.Second)
node.ExpiresAt = &past
```

### api_test.go — integration test via `Axon.WriteNode`

```go
func TestAxon_WriteNode_WithTTL(t *testing.T) {
    // Write node with past-TTL → not findable → gc → gone
}
```

**Verification**: `go test -race ./adapters/sqlite/... -run TestTTL`

---

## Task 6: AQL filtering — `adapters/sqlite/aql.go`

**File**: `adapters/sqlite/aql.go`

The AQL compiler uses the string `"nodes"` and `"edges"` as table references in
all SELECT contexts. Replace every `"nodes"` → `"active_nodes"` and `"edges"` →
`"active_edges"` within SELECT SQL generation.

Key locations to update (search for `FROM nodes`, `FROM edges`, `table: nodes`):
- `compileTableQuery` (~line 412)
- `compilePatternQuery` (~line 1114, 1462, 1464)
- `compileScopedExistsFilter` (~line 2311)
- Any other SELECT-generating functions

Do NOT change DELETE, INSERT, or the orphan-detection sub-selects that reference
actual physical rows.

**Verification**: `go test ./adapters/sqlite/... -run TestQuery`

---

## Task 7: `axon write-node` command

**Files**: `cmd/axon/write_node.go` (new), `cmd/axon/main.go`

```go
var writeNodeCmd = &cobra.Command{
    Use:   "write-node",
    Short: "Persist a custom node to the graph",
    Long: `Persist a custom node to the graph (upsert by URI).
Accepts an optional --ttl flag to set a time-to-live on the node.`,
    RunE: runWriteNode,
}

var (
    flagWriteNodeURI    string
    flagWriteNodeType   string
    flagWriteNodeName   string
    flagWriteNodeData   string
    flagWriteNodeTTL    string
    flagWriteNodeLabels []string
)

func init() {
    writeNodeCmd.Flags().StringVar(&flagWriteNodeURI,  "uri",  "", "node URI (required, same URI = upsert)")
    writeNodeCmd.Flags().StringVar(&flagWriteNodeType, "type", "", "node type (required)")
    writeNodeCmd.Flags().StringVar(&flagWriteNodeName, "name", "", "human-readable name")
    writeNodeCmd.Flags().StringVar(&flagWriteNodeData, "data", "", "JSON data payload")
    writeNodeCmd.Flags().StringVar(&flagWriteNodeTTL,  "ttl",  "", `time-to-live duration (e.g. "30m", "2h", "24h")`)
    writeNodeCmd.Flags().StringArrayVar(&flagWriteNodeLabels, "label", nil, "label to add (repeatable)")
    writeNodeCmd.MarkFlagRequired("uri")
    writeNodeCmd.MarkFlagRequired("type")
}
```

The command parses flags, builds a `graph.Node`, calls `Axon.WriteNode`, and
prints the node ID.

**Verification**: `go build ./cmd/axon/... && ./bin/axon write-node --uri test:// --type test:node`

---

## Task 8: GC — report expired records

**File**: `cmd/axon/gc.go`

Extend `runGC` to also call `DeleteExpired`:

```
$ axon gc
Using database: /path/to/db

Deleted 3 expired nodes
Deleted 0 expired edges
Deleted 2 orphaned edges
```

For dry-run, query count without deleting.

Add `CountExpired(ctx) (int64, int64, error)` as a private helper or as an
additional method on the storage (simpler: count in `gc.go` using a direct SQL
approach via `DeleteExpired` with `--dry-run` mode).

For dry-run we can run a SELECT COUNT(*) instead of DELETE.

**Verification**: `go build ./cmd/axon/... && go test ./cmd/axon/... -run TestGC`

---

## Task 9: Watch-mode background GC

**File**: `axon.go`

In `Watch()`, after the initial index, start a background goroutine that calls
`DeleteExpired` on a ticker (default 5 min). Honour `ctx.Done()` for clean
shutdown.

```go
go func() {
    ticker := time.NewTicker(5 * time.Minute)
    defer ticker.Stop()
    for {
        select {
        case <-ticker.C:
            _, _, _ = a.storage.DeleteExpired(ctx)
        case <-ctx.Done():
            return
        }
    }
}()
```

**Verification**: `go build ./...`

---

## Task 10: Documentation

**Files**: `README.md`, `.agents/skills/axon/SKILL.md`

### README.md

Under "Writing Nodes", add a TTL example:

```go
// Ephemeral node — auto-expires in 2 hours
node := graph.NewNode("memory:note").
    WithName("Current focus").
    WithURI("memory://session/current-task").
    WithData(map[string]any{"text": "Investigating auth bug"}).
    WithTTL(2 * time.Hour)

if err := ax.WriteNode(ctx, node); err != nil {
    log.Fatal(err)
}
```

Also add the `axon write-node` CLI reference:

```
axon write-node --uri memory://s1/task --type memory:note --name "task" --ttl 2h
axon gc    # removes expired nodes + orphaned edges
```

### SKILL.md

Add `write-node` command under a new "Writing and Tagging Nodes" section.

**Verification**: review rendered diff

---

## Execution Order

1 → 2 → 3 → 4 → 5 → 6 → 7 → 8 → 9 → 10

Each task verifies independently before proceeding to the next.
