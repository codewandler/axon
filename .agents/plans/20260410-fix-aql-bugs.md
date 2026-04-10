# Fix Plan: AQL Bug Fixes & CLI Output Correctness

**Source**: `.agents/improve/20260410-014525-data-field-query-nulls.md`
**Validated**: All findings confirmed against live DB and source code.

---

## Validated Findings

| # | Finding | Severity | Confirmed |
|---|---------|----------|-----------|
| 1 | `data.*` integer/boolean fields return null in SELECT | 🔴 High | ✅ |
| 2 | Pattern query `SELECT var.field` returns null | 🔴 High | ✅ |
| 3 | `axon find --output json` produces 0 bytes on empty results | 🟠 Medium | ✅ |
| 4 | `axon query --output json` produces `null` on empty results | 🟠 Medium | ✅ |
| 5 | `axon query --help` example `data.ext = 'go'` returns 0 rows | 🟠 Medium | ✅ |
| 6 | SQLITE_BUSY write errors swallowed; exit 0 | 🟠 Medium | ✅ (code review) |
| 7 | Aggregate SELECT column names hardcoded to `key`/`count` | 🟡 Low | ✅ |

---

## Task 1 — Fix `data.*` numeric/boolean fields returning null in SELECT

**File**: `adapters/sqlite/aql.go`
**Location**: `scanNodePartial`, ~line 2961

**Root cause confirmed**: The `default:` branch in the column scanner does:
```go
if strings.HasPrefix(col, "json_extract") {
    if str, ok := val.(string); ok && str != "" {   // ← string-only guard
```
When SQLite returns `int64` (for `data.size`, `data.mode`) or `bool`, the type assertion `val.(string)` fails silently and the value is discarded.

**Fix**: Remove the `val.(string)` guard; store `val` directly (the nil-guard at the top of the loop already handles NULL):
```go
if strings.HasPrefix(col, "json_extract") {
    if match := jsonExtractRegex.FindStringSubmatch(col); match != nil {
        fieldName := match[1]
        if node.Data == nil {
            node.Data = make(map[string]any)
        }
        if dataMap, ok := node.Data.(map[string]any); ok {
            dataMap[fieldName] = val
        }
    }
}
```

**Test to write** (in `adapters/sqlite/aql_test.go`):
- Index a node with integer `data.size` and boolean `data.exported` fields
- `SELECT data.size, data.exported FROM nodes WHERE type = 'fs:file' LIMIT 1` must return actual values, not null

---

## Task 2 — Fix pattern query `SELECT var.field` returning null

**Files**: `adapters/sqlite/aql.go`, `cmd/axon/query.go`

**Root cause confirmed** (two parts):

**Part A** — `compilePatternSelect` (~line 1719) only emits SQL aliases (`n1.name AS "file.name"`) when `multiVar=true`. For single-variable queries it emits bare `n1.name`, so `rows.Columns()` returns `"name"` instead of `"file.name"`.

**Fix A**: Always emit the alias in `compilePatternSelect` for field selectors, regardless of `multiVar`:
```go
// Remove the multiVar guard; always alias:
aliasName := strings.Join(sel.Parts, ".")
sqlBuilder.WriteString(fmt.Sprintf(`%s AS "%s"`, resolved, aliasName))
```

**Part B** — `nodeFieldRaw` in `cmd/axon/query.go` (~line 455) only handles `data.*` prefixes; `"file.name"` falls through to `return nil`.

**Fix B**: Add a fallback in `nodeFieldRaw` to strip the variable prefix and recurse:
```go
default:
    if strings.HasPrefix(col, "data.") {
        field := strings.TrimPrefix(col, "data.")
        if m, ok := node.Data.(map[string]any); ok {
            return m[field]
        }
    } else if idx := strings.LastIndex(col, "."); idx >= 0 {
        // var.field selector — strip variable prefix and recurse
        return nodeFieldRaw(node, col[idx+1:])
    }
    return nil
```

**Test to write**:
- `SELECT file.name FROM (dir:fs:dir)-[:contains]->(file:fs:file) LIMIT 3` must return actual file names
- `SELECT dir.name, file.name FROM (dir:fs:dir)-[:contains]->(file:fs:file) LIMIT 3` (multi-var) must also work

---

## Task 3 — Fix `axon find --output json` producing 0 bytes on empty results

**File**: `cmd/axon/find.go`
**Location**: ~line 268

**Root cause confirmed**: The early return fires before the output-format switch:
```go
if len(allNodes) == 0 {
    return nil   // ← exits before outputJSON / outputTable are called
}
```

**Fix**: Make the early return format-aware:
```go
if len(allNodes) == 0 {
    switch findOutput {
    case "json":
        fmt.Println("[]")
        return nil
    case "table":
        // fall through to outputTable which handles empty gracefully
    default:
        return nil
    }
}
```

**Test to write**:
- `axon find --type "vcs:nonexistent" --global --output json` must output `[]`
- `axon find --type "vcs:nonexistent" --global --output table` must output headers (or "No results")
- `axon find --type "vcs:nonexistent" --global --output path` must produce no output (acceptable)

---

## Task 4 — Fix `axon query --output json` producing `null` on empty results

**File**: `cmd/axon/query.go`
**Location**: `printQueryResultJSON`, ~line 148

**Root cause confirmed**: When no rows are found, `result.Nodes` / `result.Edges` / `result.Rows` are nil (not empty slices). `json.Encode(nil)` → `null`.

**Fix**: Use empty-slice literals as fallbacks:
```go
case graph.ResultTypeNodes:
    if len(result.SelectedColumns) > 0 {
        projected := make([]map[string]any, len(result.Nodes))
        // ... existing projection loop ...
        data = projected
    } else {
        if result.Nodes != nil {
            data = result.Nodes
        } else {
            data = []*graph.Node{}
        }
    }
case graph.ResultTypeEdges:
    if result.Edges != nil {
        data = result.Edges
    } else {
        data = []*graph.Edge{}
    }
case graph.ResultTypeRows:
    if result.Rows != nil {
        data = result.Rows
    } else {
        data = []map[string]any{}
    }
```

Alternatively (cleaner): initialise slices in `executeNodeQuery` / `executeEdgeQuery` / `executeRowsQuery` with `make([]..., 0)` instead of `var nodes []*graph.Node`.

**Test to write**:
- `axon query "SELECT * FROM nodes WHERE type = 'nonexistent'" --output json` must output `[]`
- `axon query "SELECT * FROM edges WHERE type = 'nonexistent'" --output json` must output `[]`

---

## Task 5 — Fix `axon query --help` example with wrong `data.ext` value

**File**: `cmd/axon/query.go`
**Location**: line 38 (Long help text)

**Root cause confirmed**: The example uses `data.ext = 'go'` but extensions are stored with a leading dot (`.go`). Verified: `data.ext = 'go'` returns 0 rows; `data.ext = '.go'` returns 114 results.

**Fix**: Change the one-liner:
```go
// Before:
axon query "SELECT * FROM nodes WHERE data.ext = 'go'"
// After:
axon query "SELECT * FROM nodes WHERE data.ext = '.go'"
```

Also add a note that `axon find --ext go` normalises the dot automatically, but AQL queries require the literal stored value.

**Additional check**: Grep README.md and AGENTS.md for `data.ext.*'go'` — update any matching examples.

---

## Task 6 — Add SQLITE_BUSY retry in `flushBatch`

**File**: `adapters/sqlite/sqlite.go`
**Location**: `flushBatch`, ~line 443

**Root cause confirmed** (code review): `flushBatch` logs SQLITE_BUSY and returns without retry; `Flush()` returns nil regardless; `init` reports success. The async write channel means callers cannot observe the error.

**Fix**: Add retry with exponential backoff for `SQLITE_BUSY`:
```go
func isSQLiteBusy(err error) bool {
    var sqliteErr sqlite3.Error
    return errors.As(err, &sqliteErr) && sqliteErr.Code == sqlite3.ErrBusy
}
```

In `flushBatch`, wrap the commit attempt:
```go
for attempt := 0; attempt < 3; attempt++ {
    err = tx.Commit()
    if err == nil {
        break
    }
    if isSQLiteBusy(err) && attempt < 2 {
        tx.Rollback()
        time.Sleep(time.Duration(50*(attempt+1)) * time.Millisecond)
        tx, err = s.db.BeginTx(ctx, nil)
        if err != nil { /* log and return */ }
        // re-execute batch into new tx
        continue
    }
    log.Printf("sqlite: batch write failed, rolling back %d operations: %v", len(batch), err)
    return
}
```

Additionally: add a `writeErr` field to `Storage` so persistent failures can be surfaced to `Flush()` callers, allowing the `init` command to exit non-zero.

**Note**: This is the most complex task. The retry logic for re-executing the batch into a new transaction requires refactoring `flushBatch` to extract the per-op execution loop. Implement this last.

---

## Task 7 — Fix aggregate SELECT column names hardcoded to `key`/`count`

**File**: `cmd/axon/query.go`
**Location**: ~line 152 (JSON), ~line 183 (table count header)

**Root cause confirmed**: `ResultTypeCounts` uses a hardcoded `countRow{Key, Count}` struct, ignoring the actual SELECT column names from the query.

**Fix**:
1. Extend `graph.QueryResult` (or `graph.CountItem`) to carry the grouping column name from the AQL compiler
2. In `printQueryResultJSON`, use the actual column name as the JSON key:
```go
case graph.ResultTypeCounts:
    groupingCol := result.GroupingColumn  // new field, defaults to "key"
    if groupingCol == "" {
        groupingCol = "key"
    }
    rows := make([]map[string]any, len(result.Counts))
    for i, item := range result.Counts {
        rows[i] = map[string]any{
            groupingCol: item.Name,
            "count":     item.Count,
        }
    }
    data = rows
```
3. Update `printCountsTable` to use the same column name for the header

**AQL compiler change needed**: In `adapters/sqlite/aql.go`, when compiling a `GROUP BY col` + `COUNT(*)` query, store the grouping column name in `QueryResult.GroupingColumn`.

---

## Execution Order

```
Task 5  →  docs-only, 1 line, do first (zero risk)
Task 3  →  find.go early-return, isolated change
Task 4  →  query.go nil-slice guard, isolated change
Task 1  →  aql.go type assertion, one-line core fix + test
Task 2  →  aql.go + query.go, two-file fix + test
Task 7  →  graph + query.go + aql.go, multi-file, moderate scope
Task 6  →  sqlite.go retry logic, most complex, do last
```

After each task: `go build ./...` + `go test ./...` must pass.
After Tasks 1 & 2: also run `go test -v ./adapters/sqlite -run TestQuery`.
