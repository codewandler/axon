# Deep Code Review: Axon Repository

## Executive Summary

Overall, the codebase demonstrates solid Go practices with clean architecture, proper error handling in most places, and good test coverage. However, there are several areas needing attention, particularly around error handling in the critical SQLite storage layer and concurrency patterns.

## Critical Issues (Must Fix)

### 1. Silent Error Swallowing in SQLite Adapter
**Location:** `adapters/sqlite/sqlite.go:435-493`

The `flushBatch` function silently discards critical errors:
- Transaction begin failures (line 435-437)
- Statement preparation failures (line 454-456, 467-469)
- `ExecContext` return values completely ignored (lines 476-479, 487-490)
- Transaction commit error ignored (line 493)

**Impact:** Data loss can occur without any indication. This is the most critical issue in the codebase.

**Recommendation:**
```go
var batchErr error
for _, op := range batch {
    if op.Node != nil {
        if _, err := nodeStmt.ExecContext(...); err != nil {
            batchErr = err
            break
        }
    }
}
if batchErr != nil {
    return // defer will rollback
}
if err := tx.Commit(); err != nil {
    log.Printf("sqlite: batch commit failed: %v", err)
}
```

### 2. Goroutines Don't Check Context Cancellation
**Location:** `axon.go:184-219, 225-239`

Subscriber and dispatcher goroutines don't check `ctx.Done()`. If context is cancelled, goroutines continue processing until channels close.

**Recommendation:**
```go
for {
    select {
    case event, ok := <-eventCh:
        if !ok {
            return
        }
        // process event...
    case <-ctx.Done():
        return
    }
}
```

### 3. Shared Mutable Node Across Subscribers
**Location:** `indexer/events.go`, subscribers in `indexer/tagger`, `indexer/markdown`

When events carry `*graph.Node`, multiple subscribers may concurrently modify the same node (e.g., `node.AddLabels()`). This is a potential data race.

**Recommendation:** Either:
- Clone nodes before passing to each subscriber
- Use immutable node patterns
- Add mutex to Node struct

## High Priority Issues

### 4. Missing Error Wrapping
**Locations:** Throughout codebase

Many errors returned without context, making debugging harder:
- `axon.go:48-50, 136-138`
- `cmd/axon/*.go` (various `return err` statements)
- `adapters/sqlite/sqlite.go:70-72, 113-122`

**Recommendation:** Use `fmt.Errorf("operation %s: %w", detail, err)` pattern consistently.

### 5. Storage Interface Too Large
**Location:** `graph/storage.go:69-140`

The `Storage` interface has 18 methods, violating Go's small interface principle.

**Recommendation:** Compose from smaller interfaces:
```go
type NodeReader interface { GetNode, GetNodeByURI, GetNodeByKey }
type NodeWriter interface { PutNode, DeleteNode }
type NodeStore interface { NodeReader; NodeWriter }
// ...
type Storage interface { NodeStore; EdgeStore; QueryStore; ... }
```

### 6. Missing Context in SQLite Init/Migrate
**Location:** `adapters/sqlite/sqlite.go:113-119, 131-135, 292-303`

All `Exec` and `Query` calls in initialization use non-context variants.

**Recommendation:** Use `ExecContext` with timeout context for all init SQL.

### 7. CLI Boilerplate Duplication
**Location:** `cmd/axon/*.go`

Nearly identical DB initialization code in every command file (~10 commands).

**Recommendation:** Extract helper:
```go
func openAxon(cwd string) (*axon.Axon, *DBLocation, func(), error)
```

## Medium Priority Issues

### 8. IndexWithOptions Too Long (~200 lines)
**Location:** `axon.go:125-326`

Single function handles: path resolution, context setup, goroutine creation, indexing, shutdown, post-processing, GC, stats recording.

**Recommendation:** Extract into smaller functions.

### 9. Silent Error Suppression in Graph Methods
**Location:** `graph/graph.go:129-132, 151-154, 174-177`

`Neighbors()`, `Children()`, `Parents()` silently skip nodes that can't be found.

**Recommendation:** Return error or log warning.

### 10. Inconsistent Scope Resolution
**Location:** `cmd/axon/`

Some commands use new `resolveScopeTraversal` (types, stats), others use deprecated `resolveScope` (labels, edges).

**Recommendation:** Migrate all commands to new traversal-based approach.

### 11. O(N²) Worst Case in Tree Rendering
**Location:** `render/tree.go:114-126`

Type filtering causes double traversal via `hasDescendantsMatching()`.

**Recommendation:** Pre-compute matching nodes in single pass.

### 12. Event Channel Blocking Risk
**Location:** `axon.go:231`

If subscriber channel is full, dispatcher blocks:
```go
info.eventCh <- event  // Could block if buffer full
```

**Recommendation:** Add select with context cancellation or document as intentional backpressure.

## Low Priority Issues

### 13. Magic Numbers
**Location:** `axon.go:78, 150, 181`

Hardcoded values like `100` for channel buffers, ignore patterns as inline strings.

**Recommendation:** Extract to named constants.

### 14. Missing Package Documentation
**Location:** `graph/graph.go`, `progress/*.go`

No package-level doc comments.

### 15. Unused Code
- `cmd/axon/stats.go:372-380`: `extractExtension` function never called
- `progress/ui.go:29`: `startTime` field set but never read

### 16. Inconsistent Edge Registration
**Location:** `types/edges.go:78-109`

`RegisterCommonEdges` registers only 6 of 10 defined edge constants. `contains`, `contained_by`, `links_to`, `located_at` are missing.

### 17. Test Gaps
- Missing concurrent write tests for SQLite
- Missing symlink/permission error tests for FS indexer
- Missing malformed markdown tests

## Architecture Observations

### Good Patterns to Preserve
1. **Builder pattern** for Node/Edge construction (`graph/node.go:65-102`)
2. **Functional options** for configuration
3. **Interface-based storage** enabling `:memory:` testing
4. **Event-driven indexer architecture** with subscriptions
5. **Consistent t.Helper()/t.TempDir()/t.Cleanup()** in tests

### Potential Improvements
1. Consider breaking `Indexer` interface into `PrimaryIndexer` vs `EventIndexer`
2. Add streaming support for large tree operations
3. Consider error aggregation for batch indexing failures
4. Add context support to progress coordinator

## Summary by Package

| Package | Quality | Key Issues |
|---------|---------|------------|
| `axon.go` | Good | Long function, goroutine ctx checks |
| `graph/` | Good | Large interface, silent errors |
| `adapters/sqlite/` | Critical Issues | Silent error swallowing in batch writes |
| `indexer/` | Good | Interface could be split |
| `progress/` | Good | Missing context support |
| `cmd/axon/` | Needs Work | Heavy duplication, inconsistent patterns |
| `types/` | Good | Minor registration inconsistencies |
| `render/` | Good | Performance concerns for large trees |

## Recommended Fix Order

1. **Critical:** Fix SQLite `flushBatch` error handling
2. **Critical:** Add context cancellation to goroutines in `axon.go`
3. **High:** Wrap errors consistently
4. **High:** Extract CLI boilerplate
5. **Medium:** Refactor `IndexWithOptions` into smaller functions
6. **Medium:** Migrate all commands to new scope traversal
7. **Low:** Clean up magic numbers, unused code, documentation

---

## TODO

- [x] 1. Fix SQLite `flushBatch` silent error swallowing
- [x] 2. Add context cancellation checks to goroutines in `axon.go`
- [ ] 3. Fix shared mutable node across subscribers (potential data race)
- [ ] 4. Add consistent error wrapping throughout codebase
- [ ] 5. Break up Storage interface into smaller composable interfaces
- [ ] 6. Add context to SQLite init/migrate operations
- [ ] 7. Extract CLI boilerplate into shared helpers
- [ ] 8. Refactor `IndexWithOptions` into smaller functions
- [ ] 9. Fix silent error suppression in graph `Neighbors`/`Children`/`Parents`
- [ ] 10. Migrate all CLI commands to new traversal-based scope resolution
- [ ] 11. Optimize tree rendering O(N²) worst case
- [ ] 12. Fix event channel blocking risk in dispatcher
- [ ] 13. Extract magic numbers to named constants
- [ ] 14. Add missing package documentation
- [ ] 15. Remove unused code (`extractExtension`, `startTime`)
- [ ] 16. Fix inconsistent edge registration in types package
- [ ] 17. Add missing tests (concurrent writes, symlinks, malformed markdown)
