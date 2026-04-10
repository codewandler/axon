# Axon Improvement Report — 2026-04-10

**Session focus**: Non-string `data.*` fields return null in AQL SELECT; empty-result JSON outputs; help text drift
**DB used**: `.axon/graph.db` (local)
**Phases completed**: Phase 1 ✅ | Phase 2 ✅

---

## Summary

This session focused on AQL query result correctness and CLI output integrity. Four new bugs were found:
`data.*` fields with integer or boolean JSON values always return `null` in SELECT output (a type-assertion bug in the SQLite scanner); `axon find --output json` produces zero bytes (not `[]`) on empty results; `axon query --output json` produces `null` (not `[]`) on empty results; and the `axon query --help` contains an example that silently returns no rows. Additionally, SQLITE_BUSY write errors are silently swallowed with exit 0. Two previously reported issues (aggregate column names, internal timestamp fields in JSON) remain unfixed.

| # | Finding | Category | Severity |
|---|---------|----------|----------|
| 1 | `data.*` integer/boolean fields return null in SELECT | AQL Bug | 🔴 High |
| 2 | Pattern query `SELECT var.field` returns null (single-var) | AQL Bug | 🔴 High |
| 3 | `axon find --output json` produces 0 bytes on empty results | CLI Bug | 🟠 Medium |
| 4 | `axon query --output json` produces `null` on empty results | CLI Bug | 🟠 Medium |
| 5 | `axon query --help` example `data.ext = 'go'` returns 0 rows | Documentation | 🟠 Medium |
| 6 | SQLITE_BUSY write errors swallowed; exit code stays 0 | Error Handling | 🟠 Medium |
| 7 | Aggregate SELECT column names hardcoded to "key"/"count" | AQL Bug | 🟡 Low (prev. reported) |

---

## Finding 1: `data.*` integer/boolean fields return null in SELECT

**Category**: AQL Bug
**Severity**: 🔴 High

### Evidence

```bash
axon query "SELECT data.size, data.mode FROM nodes WHERE type = 'fs:file' LIMIT 3" --output json
```

Actual output:
```json
[
  { "data.size": null, "data.mode": null },
  { "data.size": null, "data.mode": null },
  { "data.size": null, "data.mode": null }
]
```

Expected: actual integer values (e.g. `2489`, `420`). Verified the values exist in the DB:
```bash
sqlite3 .axon/graph.db "SELECT json_extract(data, '$.size') FROM nodes WHERE type = 'fs:file' LIMIT 3"
# → 2489, 5777, 13408
```

String fields work correctly:
```bash
axon query "SELECT data.ext, data.modified FROM nodes WHERE type = 'fs:file' LIMIT 2" --output json
# → {"data.ext": ".md", "data.modified": "..."}  ✓
```

Boolean fields (go:field `exported`, `embedded`) also return null.

WHERE clause filtering still works correctly — only SELECT output is affected:
```bash
axon query "SELECT name FROM nodes WHERE data.size > 50000"
# → returns correct rows, but SELECT data.size in the same query is null
```

### Root Cause

File: `adapters/sqlite/aql.go`, function `scanNodePartial`, line 2961–2973.

When AQL compiles `SELECT data.size FROM nodes`, it generates SQL:
```sql
SELECT json_extract(data, '$.size') FROM nodes ...
```

SQLite returns this column with the name `json_extract(data, '$.size')` and the value as `int64` (2489). In `scanNodePartial`, the default case handles these columns:

```go
if strings.HasPrefix(col, "json_extract") {
    if str, ok := val.(string); ok && str != "" {   // ← BUG
        if match := jsonExtractRegex.FindStringSubmatch(col); match != nil {
            fieldName := match[1]
            if node.Data == nil {
                node.Data = make(map[string]any)
            }
            if dataMap, ok := node.Data.(map[string]any); ok {
                dataMap[fieldName] = str
            }
        }
    }
}
```

The inner `if str, ok := val.(string); ok && str != ""` only handles string-typed values. When `json_extract` returns an `int64` (for `size`, `mode`) or `bool` (for `exported`, `embedded`), the type assertion `val.(string)` fails silently and the value is discarded. String fields (`.ext`, `.name`, `.modified`) happen to pass because SQLite returns them as `string`.

### Suggested Fix

In `adapters/sqlite/aql.go` in `scanNodePartial`, replace the string-only check with native value storage:

```go
if strings.HasPrefix(col, "json_extract") {
    // Remove the `val.(string)` guard — store native type directly
    if match := jsonExtractRegex.FindStringSubmatch(col); match != nil {
        fieldName := match[1]
        if node.Data == nil {
            node.Data = make(map[string]any)
        }
        if dataMap, ok := node.Data.(map[string]any); ok {
            dataMap[fieldName] = val  // val is already int64, bool, string, etc.
        }
    }
}
```

Note: the nil check (`if val == nil { continue }` at line 2903) already guards against null DB values, so no additional nil guard is needed here.

### Related Patterns

The same string-only pattern appears in the `go:field`/`go:func`/`go:struct` `data` fields used by the Go indexer — any numeric field (line numbers, column positions) queried via `SELECT data.line` will also return null. This affects any query that selects numeric or boolean JSON data fields.

---

## Finding 2: Pattern query `SELECT var.field` returns null for single-variable queries

**Category**: AQL Bug
**Severity**: 🔴 High

### Evidence

```bash
axon query "SELECT file.name FROM (dir:fs:dir)-[:contains]->(file:fs:file) LIMIT 5" --output json
```

Actual output:
```json
[
  { "file.name": null },
  { "file.name": null },
  { "file.name": null }
]
```

Expected: actual file names. The SQL generated is correct and returns real values:
```bash
axon query --explain "SELECT file.name FROM (dir:fs:dir)-[:contains]->(file:fs:file) LIMIT 5"
# SQL: SELECT n1.name FROM nodes AS n0 JOIN edges AS e0 ON ...
# Verified directly: sqlite3 ... "SELECT n1.name FROM ..." → "indexer.go", "indexer_test.go" ...
```

`SELECT *` in pattern queries returns an error:
```
Error: query error: compile error: unsupported SELECT expression in pattern: *aql.Star
```

Multi-variable pattern queries (`SELECT dir, file`) also return all-null values.

### Root Cause

File: `adapters/sqlite/aql.go`, function `compilePatternSelect` and `scanNodePartial` / `nodeFieldRaw`.

**Issue 1 — single-variable field selectors get no SQL alias:**

`detectMultiVarSelect` (line 1645) returns `true` only when two or more distinct variables are selected. For `SELECT file.name` (one variable), `multiVar` is `false`. In `compilePatternSelect` (line 1719–1724):

```go
if multiVar {
    // Alias as "var.field" so rows.Columns() returns the dotted name
    aliasName := strings.Join(sel.Parts, ".")
    sqlBuilder.WriteString(fmt.Sprintf(`%s AS "%s"`, resolved, aliasName))
} else {
    sqlBuilder.WriteString(resolved)   // ← no alias for single-var
}
```

So the SQL is `SELECT n1.name` — SQLite returns the column as `name`, not `file.name`.

**Issue 2 — result mapper can't find the value:**

Since `multiVar=false`, the result type stays `ResultTypeNodes` and `scanNodePartial` is called. `scanNodePartial` correctly maps the `name` SQL column to `node.Name`. But `extractSelectedColumns(query)` returns `["file.name"]`, and in the rendering layer, `nodeFieldRaw(node, "file.name")` hits the `default` case:

```go
default:
    if strings.HasPrefix(col, "data.") {   // "file.name" doesn't start with "data."
        ...
    }
    return nil   // ← always nil for "file.name"
```

### Suggested Fix

Two complementary changes are needed:

**Option A (preferred):** Always emit SQL aliases in `compilePatternSelect` for field selectors, regardless of `multiVar`:

```go
// In compilePatternSelect, for field selectors:
aliasName := strings.Join(sel.Parts, ".")
sqlBuilder.WriteString(fmt.Sprintf(`%s AS "%s"`, resolved, aliasName))
// (remove the multiVar guard)
```

This ensures `rows.Columns()` always returns `file.name` as the column name for both single-var and multi-var queries.

**Also fix `executeNodeQuery`:** When field selectors are present, the result type should be `ResultTypeRows` (which uses `executeRowsQuery` and the correct column-name-keyed map) rather than `ResultTypeNodes`. Alternatively, update `nodeFieldRaw` in `query.go` to handle `var.field` keys by stripping the variable prefix:

```go
default:
    if strings.HasPrefix(col, "data.") {
        // ... existing data.* handler
    } else if idx := strings.LastIndex(col, "."); idx >= 0 {
        // var.field selector like "file.name" → look up by field name
        return nodeFieldRaw(node, col[idx+1:])
    }
    return nil
```

**For `SELECT *` in patterns:** Add `*aql.Star` handling in `compilePatternSelect` that expands to the relevant node's columns (e.g. `n1.*`). Currently returns an error.

### Related Patterns

The `SELECT var` (whole node) case generates `SELECT n1.*` which also returns null values because the result mapper expects a single column named `file` but gets the expanded columns `id`, `type`, `name`, etc.

---

## Finding 3: `axon find --output json` produces 0 bytes on empty results

**Category**: CLI Bug
**Severity**: 🟠 Medium

### Evidence

```bash
axon find --type "vcs:nonexistent" --global --output json | wc -c
# 0
```

Expected: `[]` (empty JSON array). Similarly `--output table` produces no output (not even headers or "No results").

```bash
axon find --type "vcs:nonexistent" --global --output path
# (empty - this is OK for path/uri output)
```

### Root Cause

File: `cmd/axon/find.go`, lines 268–270.

```go
if len(allNodes) == 0 {
    return nil   // ← early return before the output format switch
}

switch findOutput {
case "json":
    return outputJSON(allNodes)
// ...
}
```

The early return fires before the output-format switch, so `outputJSON`, `outputTable`, etc. are never called. For path/uri output this is acceptable Unix behaviour, but for structured formats (json, table), consumers expect a valid empty structure.

### Suggested Fix

In `cmd/axon/find.go`, replace the blanket early return with format-aware handling:

```go
if len(allNodes) == 0 {
    switch findOutput {
    case "json":
        fmt.Println("[]")
    case "table":
        // Print header even for empty results so scripts can detect the format
        outputTable(allNodes)  // outputTable already handles empty slices gracefully
    }
    return nil
}
```

Or alternatively: remove the early return entirely and let each output function handle the empty case itself (requires adding "No results" handling to `outputTable`).

### Related Patterns

`outputTable` (`cmd/axon/find.go` line 401) always prints the header row even when `nodes` is empty. If the early-return guard were removed, table output would at least print the column headers — which is arguably correct. The early-return guard is overly broad.

---

## Finding 4: `axon query --output json` produces `null` on empty results

**Category**: CLI Bug
**Severity**: 🟠 Medium

### Evidence

```bash
axon query "SELECT * FROM nodes WHERE type = 'nonexistent'" --output json
# null
```

Expected: `[]` (empty JSON array).

```bash
axon query "SELECT * FROM edges WHERE type = 'nonexistent'" --output json
# null
```

### Root Cause

File: `cmd/axon/query.go`, function `printQueryResultJSON`, lines 131–165.

For `ResultTypeNodes`, the code path when `result.Nodes` is nil (no rows) is:

```go
case graph.ResultTypeNodes:
    if len(result.SelectedColumns) > 0 {
        projected := make([]map[string]any, len(result.Nodes))  // len 0, returns []
        // ...
        data = projected   // ← would be [] if reached
    } else {
        data = result.Nodes   // ← result.Nodes is nil → data = nil
    }
```

When `SELECT *` finds no rows, `result.Nodes` is nil (the SQLite scanner returns nil, not an empty slice). `data = nil` then encodes as `null` via `json.Encode`.

Same issue for `ResultTypeEdges` (`data = result.Edges`) and `ResultTypeRows` (`data = result.Rows`).

### Suggested Fix

In `printQueryResultJSON` in `cmd/axon/query.go`, guard against nil slices:

```go
case graph.ResultTypeNodes:
    if len(result.SelectedColumns) > 0 {
        projected := make([]map[string]any, len(result.Nodes))
        // ... projection loop
        data = projected
    } else {
        if result.Nodes == nil {
            data = []*graph.Node{}
        } else {
            data = result.Nodes
        }
    }
case graph.ResultTypeEdges:
    if result.Edges == nil {
        data = []*graph.Edge{}
    } else {
        data = result.Edges
    }
case graph.ResultTypeRows:
    if result.Rows == nil {
        data = []map[string]any{}
    } else {
        data = result.Rows
    }
```

Alternatively, ensure the SQLite execute functions always return empty slices rather than nil when there are no results. The fix in `executeNodeQuery` / `executeEdgeQuery` etc. would be to initialise with `make([]..., 0)` instead of `var nodes []*graph.Node`.

---

## Finding 5: `axon query --help` example `data.ext = 'go'` returns 0 rows

**Category**: Documentation
**Severity**: 🟠 Medium

### Evidence

```bash
axon query "SELECT * FROM nodes WHERE data.ext = 'go'"
# No results
```

Expected: all Go source files. The correct query is:

```bash
axon query "SELECT * FROM nodes WHERE data.ext = '.go'"
# → 114 results ✓
```

Extensions are stored with a leading dot in the `data.ext` field (`".go"`, `".md"`, `".mod"`). The `axon find --ext go` command correctly normalises by adding the leading dot internally (see `find.go` line 201), but AQL users querying `data.ext` directly must include the dot.

### Root Cause

File: `cmd/axon/query.go`, line 38, in the `Long` help text:

```go
  axon query "SELECT * FROM nodes WHERE data.ext = 'go'"
```

Should be:

```go
  axon query "SELECT * FROM nodes WHERE data.ext = '.go'"
```

### Suggested Fix

In `cmd/axon/query.go`, change line 38:

```
  # Query with JSON field access
  axon query "SELECT * FROM nodes WHERE data.ext = '.go'"
```

Consider also adding a note about the dot convention, since it's a common point of confusion given that `axon find --ext go` works without the dot.

### Related Patterns

The README.md and AGENTS.md may contain similar examples. A quick grep for `data.ext.*'go'` across docs would catch any others.

---

## Finding 6: SQLITE_BUSY write errors swallowed; process exits with 0

**Category**: Error Handling
**Severity**: 🟠 Medium

### Evidence

Observed once during concurrent `axon init` + `axon query` (rare but reproducible):

```
2026/04/10 01:34:18 sqlite: failed to insert node nI3NDos2Cv-720e4Bwa8gw: database is locked (5) (SQLITE_BUSY)
2026/04/10 01:34:18 sqlite: batch write failed, rolling back 1 operations
Indexed 137 files, 33 directories, 1 git repos
Removed 335 stale entries
```

Exit code: `0`. The "Indexed 137 files..." summary appears as if the index is complete, but one or more write batches were rolled back due to the lock.

### Root Cause

File: `adapters/sqlite/sqlite.go`, function `flushBatch` (line 443 onwards).

The SQLite adapter uses an async write buffer: `PutNode`/`PutEdge` enqueue operations to `s.writeCh`, and `flushBatch` processes them in a background goroutine. The comment at line 444 documents this explicitly:

```go
// flushBatch writes a batch of operations to the database.
// Errors are logged since this runs asynchronously and cannot return errors to callers.
func (s *Storage) flushBatch(batch []writeOp) {
```

When SQLITE_BUSY occurs:
1. `flushBatch` logs the error to stderr and rolls back the transaction
2. The rolled-back nodes/edges are lost (not re-queued for retry)
3. `Flush()` returns `nil` — it only waits for the flush goroutine to signal "done", not for success
4. The indexer continues and reports success

There is no retry mechanism for transient lock errors.

### Suggested Fix

In `adapters/sqlite/sqlite.go` in `flushBatch`, add retry logic for `SQLITE_BUSY`:

Describe the change: before rolling back on error, check if the error is `SQLITE_BUSY` (using the `github.com/mattn/go-sqlite3` error type check). If so, wait briefly and retry the batch up to N times (e.g. 3 retries with 50ms backoff). Only roll back and log on permanent failure.

```go
// Pseudocode for retry:
for attempt := 0; attempt < 3; attempt++ {
    err = tryFlushBatch(tx, batch)
    if err == nil {
        break
    }
    if isSQLiteBusy(err) && attempt < 2 {
        tx.Rollback()
        time.Sleep(50ms * (attempt + 1))
        tx, _ = s.db.BeginTx(ctx, nil)
        continue
    }
    // permanent error or max retries
    log.Printf("sqlite: batch write failed: %v", err)
    return
}
```

Additionally, the `init` command should propagate a non-zero exit code if any write batches failed, rather than printing "Indexed X files..." as if all succeeded.

### Related Patterns

The same swallowing pattern applies to ALL `flushBatch` error paths (not just SQLITE_BUSY): marshal errors, prepare-statement errors, and commit errors all result in `log.Printf` + silent continue. If the DB file is corrupt or disk is full, the user sees stderr output but exit code 0 and a normal completion message.

---

## Finding 7: Aggregate SELECT column names hardcoded to "key"/"count" (previously reported)

**Category**: AQL Bug
**Severity**: 🟡 Low

> **Note**: This was identified in the previous report (`20260410-004715-query-output-integrity.md`, Finding 6 and 7). It remains unfixed.

### Evidence

```bash
axon query "SELECT type, COUNT(*) FROM nodes GROUP BY type" --output json
# [{"key": "go:ref", "count": 22295}, ...]
```

Expected: `[{"type": "go:ref", "COUNT(*)": 22295}, ...]` (or at least `"type"` as key, not `"key"`).

Same for table output which shows "Key\tCount" header regardless of what columns were selected.

### Root Cause

File: `cmd/axon/query.go`, lines 152–162 (JSON) and line 504 (table). The `ResultTypeCounts` rendering hardcodes the column names:

```go
type countRow struct {
    Key   string `json:"key"`    // hardcoded
    Count int    `json:"count"`  // hardcoded
}
```

The actual column names from the AQL SELECT clause are not passed to the count renderer. `graph.CountItem` has `Name` and `Count` fields which map to the hardcoded "key"/"count".

### Suggested Fix

Pass the actual column names from the SELECT to the count rendering functions. The first non-COUNT column in the SELECT clause should become the JSON key name. This requires:
1. Extending `graph.CountItem` or `graph.QueryResult` to carry the grouping column name
2. Using it in `printCountsTable` and the `ResultTypeCounts` JSON path

---

## Observations Without Findings

- **`axon info` vs `axon stats -v` discrepancy** is expected and intentional: `info` shows DB-level globals, `stats` (default) is scoped to CWD. The only issue is that neither output explains the scope difference explicitly, which can confuse users comparing the two.

- **Scoped `axon types` excludes `go:*` and `vcs:*` nodes** even when running from the project root. This is because scoping follows `contains`/`has` edges, and Go/VCS nodes are linked via `defines`/`located_at` edges which are not in the traversal set. This is consistent behaviour, though potentially surprising.

- **`SELECT 1 FROM edges WHERE ...` in subqueries is unsupported** — AQL's EXISTS clause only accepts graph pattern syntax, not SQL-style subqueries. This is a documented design decision.

- **Table output has no column width cap** for the auto-detect mode (`SELECT *`): `md:section` nodes have very long names (frontmatter content), making the Name column extremely wide. This is a UX issue but not a bug.

- **`axon find --output count` errors** while `axon query --output count` works — inconsistency by design; `axon find` uses `--count` flag instead. Not a bug but worth noting in documentation.

- **`--show-query` output has misleading operator precedence** for multi-label queries: the displayed AQL shows `... AND label1 OR label2` without parentheses, suggesting a precedence issue. However the compiled SQL correctly scopes both conditions via CTE. The visual representation is misleading but the runtime is correct.

- **Graph integrity**: no duplicate URIs, no orphaned edges after `gc`. Graph is healthy.

---

## Recommended Next Steps

1. **Fix `scanNodePartial` string-only type assertion** (Finding 1) — one-line change in `adapters/sqlite/aql.go` line 2962; high impact on data field queries. Write a test: `SELECT data.size FROM nodes WHERE type = 'fs:file'` should return integer values.

2. **Fix `axon find --output json` empty result** (Finding 3) — add format-aware handling before the early return in `find.go` line 268; straightforward. Prevents broken JSON pipelines.

3. **Fix `axon query --output json` null on empty result** (Finding 4) — init empty slices instead of nil in `executeNodeQuery`/`executeEdgeQuery`; prevents `null` in JSON APIs.

4. **Fix pattern query SELECT null values** (Finding 2) — emit SQL aliases unconditionally in `compilePatternSelect`, and extend `nodeFieldRaw` to handle `var.field` keys. This unblocks the primary pattern-query use case.

5. **Fix `axon query --help` example** (Finding 5) — single string change; prevents user confusion.
