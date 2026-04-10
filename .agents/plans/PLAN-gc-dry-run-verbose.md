# PLAN: Enhanced `axon gc --dry-run` with Verbose Listing

**Issue:** #27  
**Design:** `.agents/plans/DESIGN-gc-dry-run-verbose.md`

---

## Task 1 — Add `FindOrphanedEdges` to storage interface

**File:** `graph/storage.go`

Add to `StalenessManager` interface:

```go
FindOrphanedEdges(ctx context.Context) ([]*Edge, error)
```

**Verify:** `go build ./graph/...`

---

## Task 2 — Implement `FindOrphanedEdges` in SQLite adapter

**File:** `adapters/sqlite/sqlite.go`

Add after `CountOrphanedEdges`:

```go
func (s *Storage) FindOrphanedEdges(ctx context.Context) ([]*graph.Edge, error) {
    if err := s.Flush(ctx); err != nil {
        return nil, err
    }
    rows, err := s.db.QueryContext(ctx, `
        SELECT id, type, from_id, to_id, data, generation, created_at
        FROM edges
        WHERE from_id NOT IN (SELECT id FROM nodes)
           OR to_id   NOT IN (SELECT id FROM nodes)
    `)
    if err != nil {
        return nil, err
    }
    defer rows.Close()
    return s.scanEdges(rows)
}
```

**Verify:** `go build ./adapters/sqlite/...`

---

## Task 3 — Write failing tests for `FindOrphanedEdges`

**File:** `adapters/sqlite/sqlite_test.go`

Add:

```go
func TestFindOrphanedEdges(t *testing.T) {
    // create nodes and edge, delete one node, verify FindOrphanedEdges returns edge
}

func TestFindOrphanedEdges_NoOrphans(t *testing.T) {
    // create nodes and edge, don't delete anything, verify empty result
}
```

**Verify:** `go test -run TestFindOrphanedEdges ./adapters/sqlite/` → FAIL

---

## Task 4 — Make Task 3 tests pass

Implement `FindOrphanedEdges` (Task 2 already done). Run tests.

**Verify:** `go test -run TestFindOrphanedEdges ./adapters/sqlite/` → PASS

---

## Task 5 — Rewrite `cmd/axon/gc.go`

**File:** `cmd/axon/gc.go`

Changes:
- Add `flagGCQuiet bool` flag (`--quiet` / `-q`)
- Helper `resolveNodeDesc(ctx, storage, id) string` — returns `"type name"` or `"<missing>"`
- `runGC` refactored:
  - If not quiet: call `FindOrphanedEdges`, print each with resolved endpoints
  - If dry-run: print "Would delete N orphaned edges  (dry run — no changes made)"
  - If not dry-run: call `DeleteOrphanedEdges`, print "Deleted N orphaned edges"
  - If quiet + dry-run: `CountOrphanedEdges`, print summary only
  - If nothing found: "No orphaned edges found"

**Verify:** `go build ./cmd/axon/...`

---

## Task 6 — Write failing command tests

**File:** `cmd/axon/gc_test.go` (new)

Tests:
- `TestGC_DryRun_NoWrites` — dry-run produces no deletes
- `TestGC_NormalRun_DeletesOrphans` — normal run removes orphaned edges
- `TestGC_Quiet_SuppressesDetail` — quiet flag shows only summary

**Verify:** `go test -run TestGC_ ./cmd/axon/` → FAIL

---

## Task 7 — Make Task 6 tests pass + full suite

Run all tests with race detector.

**Verify:**
```
go test -race ./...
go vet ./...
```

---

## Task 8 — Update documentation

**Files:** `README.md`, `.agents/skills/axon/SKILL.md`

Update the `gc` command description in both files to mention verbose listing and `--quiet`.

---

## Task 9 — Pre-flight

```bash
go build ./...
go vet ./...
go test -race ./...
./bin/axon gc --help
```
