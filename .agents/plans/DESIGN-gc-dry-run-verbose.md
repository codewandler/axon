# DESIGN: Enhanced `axon gc --dry-run` with Verbose Listing

**Issue:** #27  
**Branch:** feature/gc-dry-run-verbose

---

## Problem

`axon gc --dry-run` currently only prints a single count line:

```
Would delete 3 orphaned edges
```

It does not list *which* records would be removed, does not cover stale nodes or
TTL-expired records, and offers no quiet mode for scripted use.

---

## Proposed Solution

1. **Verbose listing** — `--dry-run` lists each orphaned edge (type, from-node,
   to-node) by default. The normal run (`axon gc`) also lists what was deleted.
2. **`--quiet` / `-q` flag** — suppresses per-record lines; shows only the
   summary totals (for CI / scripts).
3. **`FindOrphanedEdges`** — new storage method that returns the full list of
   orphaned edge objects (reusing the existing SQL predicate).
4. **TTL-expired records** — deferred to issue #26; no placeholder output added
   (the issue is still open, nothing to show yet).

---

## Architecture

### Layers changed

| File | Change |
|---|---|
| `graph/storage.go` | Add `FindOrphanedEdges` to `StalenessManager` interface |
| `adapters/sqlite/sqlite.go` | Implement `FindOrphanedEdges` — SELECT with same predicate as `Count`/`Delete` variants |
| `adapters/sqlite/sqlite_test.go` | Tests for `FindOrphanedEdges` |
| `cmd/axon/gc.go` | Add `--quiet`/`-q`, verbose listing, unified output helper |
| `cmd/axon/gc_test.go` | New file — tests for dry-run, normal run, quiet flag |

### Storage method

```go
// StalenessManager (graph/storage.go)
FindOrphanedEdges(ctx context.Context) ([]*Edge, error)
```

Implementation selects all edges whose `from_id` or `to_id` is absent from the
`nodes` table (identical predicate to `CountOrphanedEdges` / `DeleteOrphanedEdges`),
scanning rows with the existing `scanEdges` helper.

### Command flow

```
runGC(cmd, args)
  │
  ├─ dry-run=false, quiet=false
  │     FindOrphanedEdges → print each edge with resolved node info
  │     DeleteOrphanedEdges → print "Deleted N orphaned edges"
  │
  ├─ dry-run=true, quiet=false
  │     FindOrphanedEdges → print each edge with resolved node info
  │     (no delete) → print "Would delete N orphaned edges  (dry run)"
  │
  ├─ quiet=true (either mode)
  │     DeleteOrphanedEdges / CountOrphanedEdges → print only summary line
  │
  └─ nothing found
        "No orphaned edges found"
```

### Output format

```
Orphaned edges (3):
  [contains]  (from: <missing>)              →  (to: fs:file /src/old.go)
  [has]       (from: vcs:repo .)             →  (to: <missing>)
  [defines]   (from: <missing>)              →  (to: <missing>)

Would delete 3 orphaned edges  (dry run — no changes made)
```

Node resolution: call `GetNode(edge.From)` and `GetNode(edge.To)`; if not found
(expected for orphaned edges), show `<missing>`. If found, show `type name` (or
just `type` if name is blank).

---

## Key Decisions

1. **Always `FindOrphanedEdges` for verbose mode** — simple, readable. GC is not
   performance-critical so the double-pass (find + delete) is fine.
2. **Quiet mode skips `FindOrphanedEdges`** — falls back to `CountOrphanedEdges`
   (dry-run) or `DeleteOrphanedEdges` (normal) for minimal DB load.
3. **No TTL placeholder** — adding `[requires #26]` rows for zero items clutters
   the output with information about unreleased features. Skip until #26 merges.
4. **Verbose output in both modes** — the issue requests "same structured summary
   but says 'Deleted' instead of 'Would delete'" for normal runs. This means the
   listing is always shown in verbose mode regardless of dry-run.

---

## Out of Scope

- Exit code signal (mentioned as optional, follow-up)
- TTL-expired nodes/edges (depends on #26)

---

## Files Changed

| File | Action |
|---|---|
| `graph/storage.go` | Add `FindOrphanedEdges` to `StalenessManager` |
| `adapters/sqlite/sqlite.go` | Implement `FindOrphanedEdges` |
| `adapters/sqlite/sqlite_test.go` | Add `TestFindOrphanedEdges` tests |
| `cmd/axon/gc.go` | Rewrite `runGC`, add `--quiet`/`-q` |
| `cmd/axon/gc_test.go` | New file with command-level tests |
| `README.md` | Update `gc` command description |
| `.agents/skills/axon/SKILL.md` | Update `gc` entry |
