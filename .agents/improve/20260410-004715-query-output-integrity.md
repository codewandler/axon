# Axon Improvement Report — 2026-04-10

**Session focus**: Query output correctness, orphaned edge root causes, and CLI help accuracy  
**DB used**: `.axon/graph.db` (local)  
**Phases completed**: Phase 1 ✅ | Phase 2 ✅

---

## Summary

Full CLI audit of the axon codebase indexed against itself. Seven concrete findings were surfaced through CLI-only exploration (Phase 1) and confirmed via source reading (Phase 2). The most critical issues are in `cmd/axon/query.go`: aggregate query results use a Go `map[string]int` causing non-deterministic row order and wrong JSON structure. A secondary critical issue is in `indexer/golang/indexer.go`: the go indexer creates ~14,500 orphaned edges on every index run because `ExportedOnly: true` suppresses symbol node creation but not reference edge creation.

| # | Finding | Category | Severity |
|---|---------|----------|----------|
| 1 | `axon info` "Orphaned" label doesn't say "edges" | Documentation | 🟡 Low |
| 2 | `find` help examples for `vcs:*` types silently return 0 without `--global` | Documentation | 🟠 Medium |
| 3 | Go indexer creates ~14,500 orphaned reference edges per index run | Graph Integrity | 🔴 High |
| 4 | AQL rejects subqueries in `NOT IN (SELECT ...)` | AQL Limitation | 🟠 Medium |
| 5 | `axon query` GROUP BY results have non-deterministic row order | CLI Bug | 🔴 High |
| 6 | `axon query` table output ignores SELECT column order | CLI Bug | 🟠 Medium |
| 7 | `axon query --output json` produces wrong structure for aggregate queries | CLI Bug | 🔴 High |
| 8 | `axon query --output json` includes unselected `created_at`/`updated_at` fields | CLI Bug | 🟠 Medium |

---

## Finding 1: "Orphaned" label in `axon info` doesn't specify "edges"

**Category**: Documentation  
**Severity**: 🟡 Low

### Evidence

```bash
axon info
```

Actual output (relevant line):
```
Orphaned:      14,558 (run 'axon gc' to clean)
```

```bash
axon gc --dry-run
```

Actual output:
```
Would delete 14558 orphaned edges
```

Expected: The `axon info` line should say "Orphaned edges: 14,558" to match what gc confirms.

### Root Cause

In `cmd/axon/info.go` line 195:
```go
p.Printf("Orphaned:      %d (run 'axon gc' to clean)\n", data.OrphanedEdges)
```

The local struct field is `OrphanedEdges` (clearly an edge count), and the JSON key is `orphaned_edges`, but the text label just says "Orphaned:". A user reading the text output doesn't know whether these are orphaned nodes or edges. The JSON output is already correct.

### Suggested Fix

In `cmd/axon/info.go`, change line 195 from:
```go
p.Printf("Orphaned:      %d (run 'axon gc' to clean)\n", data.OrphanedEdges)
```
to:
```go
p.Printf("Orphaned edges: %d (run 'axon gc' to clean)\n", data.OrphanedEdges)
```

Also update line 197 from `fmt.Println("Orphaned:      0")` to `fmt.Println("Orphaned edges: 0")`.

### Related Patterns

The `axon gc --help` text also uses the unqualified word "orphaned": "Run garbage collection to clean up orphaned edges." — but in this case the sentence says "edges" so it's already correct.

---

## Finding 2: `axon find` help examples for `vcs:*` types silently return 0 results

**Category**: Documentation  
**Severity**: 🟠 Medium

### Evidence

```bash
axon find --type vcs:repo --count
```
Actual output: `0`

```bash
axon find --type vcs:branch --show-parent
```
Actual output: _(empty, no results)_

Expected: These should return the indexed repo and branch when run from within the repo directory.

With `--global` added, both work:
```bash
axon find --type vcs:repo --count --global   # → 1
axon find --type vcs:branch --global         # → [0MTKq7y] git+file://...
```

### Root Cause

In `cmd/axon/find.go` lines 52–58, the help text shows:
```
  # Find all branches (with wildcard query)
  axon find --type vcs:branch --query "feature*"

  # Count git repositories
  axon find --type vcs:repo --count

  # Show nodes with parent chain
  axon find --type vcs:branch --show-parent
```

None of these examples include `--global`. The design choice — `vcs:*` nodes use `git+file://` URIs while the local scope searches only via `file://` containment — means `vcs:*` nodes are always outside the default local scope. This is a correct design decision but the help text doesn't reflect it.

### Suggested Fix

In `cmd/axon/find.go`, add `--global` to all three `vcs:*` examples:
```
  # Find all branches (requires --global since vcs nodes use git+file:// URIs)
  axon find --type vcs:branch --global --query "feature*"

  # Count git repositories
  axon find --type vcs:repo --global --count

  # Show nodes with parent chain
  axon find --type vcs:branch --global --show-parent
```

### Related Patterns

The `--show-parent` flag may also behave unexpectedly for `vcs:*` nodes even with `--global`, since parent chain traversal might not produce meaningful filesystem paths. Worth a separate investigation.

---

## Finding 3: Go indexer creates ~14,500 orphaned reference edges per index run

**Category**: Graph Integrity  
**Severity**: 🔴 High

### Evidence

```bash
axon init --local .
# Output: Indexed 133 files, 31 directories, 1 git repos

axon gc --dry-run
# Output: Would delete 14558 orphaned edges

axon gc
# Output: Deleted 14558 orphaned edges

axon init --local .    # immediately re-index
axon gc --dry-run
# Output: Would delete 14558 orphaned edges   ← same count, recreated!
```

The 14,558 orphaned edges reappear after every `axon init`. Running `axon gc` is not a lasting fix.

```bash
axon query "SELECT type, COUNT(*) FROM edges GROUP BY type ORDER BY COUNT(*) DESC"
# Before gc: references  21,559
# After  gc: references   7,125
# Orphaned references: 14,434
```

### Root Cause

**Two compounding issues:**

**Issue A — ExportedOnly inconsistency in `indexer/golang/indexer.go`**:

The go indexer defaults to `ExportedOnly: true` (line 65), which skips creating symbol nodes for unexported functions, methods, and types. However, `indexReferences` (lines 919–998) creates `go:ref` nodes and `references` edges for ALL symbol usages within the module — including usages of unexported symbols.

When `ExportedOnly: true`, an unexported function like `helper()` has no node in the DB (skipped by line 707–709 in `indexFuncDecl`). But every call site to `helper()` still creates a `go:ref` node with a `references` edge pointing to `graph.IDFromURI(targetURI)` — an ID that will never exist. These are the orphaned edges.

The filter at line 943–947 correctly excludes references to external packages, but does NOT exclude references to unexported symbols within the module.

**Issue B — `axon init` GC is conditional on node deletions (`axon.go:370`)**:

```go
if !opts.SkipGC && ictx.NodesDeleted() > 0 {
    // ... run gc
}
```

If no files were added or removed between runs, `NodesDeleted() == 0`, so orphaned edge cleanup is completely skipped. The go-indexer-created orphaned edges are never caught by `axon init`'s built-in GC.

### Suggested Fix

**Primary fix** (prevents orphaned edges from being created):

In `indexer/golang/indexer.go`, inside `indexReferences` before creating the `references` edge, add a check when `ExportedOnly` is enabled:

```go
// Skip references to unexported symbols when ExportedOnly is set
if i.ExportedOnly && !obj.Exported() {
    continue
}
```

This check should be inserted after line 946 (the module prefix filter) and before the `classifyReference` call. This eliminates 14,434 spurious orphaned edges at the source.

**Secondary fix** (makes `axon init` always run orphaned edge GC):

Change the condition in `axon.go` line 370 from:
```go
if !opts.SkipGC && ictx.NodesDeleted() > 0 {
```
to:
```go
if !opts.SkipGC {
```

This ensures orphaned edges are always cleaned up after indexing, regardless of whether nodes were deleted. The performance cost is one extra SQL query (`DELETE FROM edges WHERE from_id NOT IN (...) OR to_id NOT IN (...)`), which is fast on modern SQLite with proper indices.

### Related Patterns

The `ExportedOnly` flag inconsistency may also affect `go:field` references (line 1065–1068 in `buildTargetURI` already skips field references). Check other reference types in `buildTargetURI` for similar cases where the target node type might not be created when `ExportedOnly: true`.

---

## Finding 4: AQL rejects `NOT IN (SELECT ...)` subqueries

**Category**: AQL Limitation  
**Severity**: 🟠 Medium

### Evidence

```bash
axon query "SELECT id, type, name FROM nodes WHERE id NOT IN (SELECT from_id FROM edges) LIMIT 20"
```

Actual output:
```
Error: parse error: 1:51: unexpected token "SELECT" (expected ValueGrammar ("," ValueGrammar)* ")")
```

Expected: Valid SQL that returns nodes with no outgoing edges.

### Root Cause

The AQL grammar (`aql/parser.go`) does not support subqueries in `IN (...)` or `NOT IN (...)` value positions. The grammar's `ValueGrammar` production handles literal values, strings, and numbers, but not nested `SELECT` statements. This is an intentional grammar limitation, not a parser bug.

### Suggested Fix

Supporting subqueries in `IN` clauses requires extending the AQL grammar to allow a `SELECT` clause as a value in `IN (...)`. Changes would span:
1. `aql/ast.go` — add `SubqueryExpr` node
2. `aql/parser.go` — extend `ValueGrammar` to accept `(SELECT ...)`
3. `adapters/sqlite/aql.go` — compile `SubqueryExpr` to SQL subquery

A simpler short-term workaround: document in `aql/grammar.md` that subqueries are not supported in `IN` clauses, and provide equivalent AQL patterns using `EXISTS`.

---

## Finding 5: `axon query` GROUP BY results have non-deterministic row order

**Category**: CLI Bug  
**Severity**: 🔴 High

### Evidence

Run 5 times, observe 3 different orderings:
```bash
# Run 1 & 2:
Key           Count
go:ref        21566
go:field      431
...

# Run 3 & 5:
Key           Count
md:section    198
md:codeblock  169
go:ref        21566    ← should be first for DESC
...
```

```bash
# SQLite returns correct order:
sqlite3 .axon/graph.db "SELECT type, COUNT(*) FROM nodes GROUP BY type ORDER BY COUNT(*) DESC LIMIT 5"
go:ref|21566
go:field|431
go:method|363
```

SQLite is sorting correctly. The bug is in the rendering layer.

### Root Cause

In `cmd/axon/query.go`, the `printCountsTable` function (lines 310–325):

```go
func printCountsTable(counts map[string]int) error {
    ...
    for key, count := range counts {   // ← Go map iteration: non-deterministic!
        fmt.Fprintf(w, "%s\t%d\n", key, count)
    }
}
```

The `executeCountQuery` function in `adapters/sqlite/aql.go` (lines 2937–2971) returns `map[string]int`. Rows from SQLite arrive in correct ORDER BY order, but stuffing them into a Go map immediately loses the ordering. All other commands (`axon stats`, `axon types`, `axon edges`, `axon labels`) handle this correctly by converting to `CountResult` and calling `SortByCount()` explicitly.

### Suggested Fix

In `cmd/axon/query.go`, replace `printCountsTable`:

```go
func printCountsTable(counts map[string]int) error {
    if len(counts) == 0 {
        fmt.Println("No results")
        return nil
    }

    // Convert to sorted slice to get deterministic output
    var result CountResult
    result.FromMap(counts)
    result.SortByCount()  // descending by count, then ascending by name

    w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
    defer w.Flush()

    fmt.Fprintln(w, "Key\tCount")
    for _, item := range result.Items {
        fmt.Fprintf(w, "%s\t%d\n", item.Name, item.Count)
    }
    return nil
}
```

**Deeper fix**: Change `Counts map[string]int` in `graph.QueryResult` to `[]CountItem` to preserve SQLite's ORDER BY at the data layer. This requires updating `executeCountQuery`, all consumers of `result.Counts`, and the `QueryResult` struct. It would also fix the JSON output issue (Finding 7). The map-based approach should be considered a design debt.

### Related Patterns

Same bug exists in `printQueryResultCount` (lines 157–160): `for _, c := range result.Counts` also iterates a map. For scalar `COUNT(*)`, this works (only one entry `_count`), but for GROUP BY count, it would sum incorrectly if the order mattered. Low risk but worth fixing for consistency.

---

## Finding 6: `axon query` table output ignores SELECT column order

**Category**: CLI Bug  
**Severity**: 🟠 Medium

### Evidence

```bash
axon query "SELECT uri, name, type, id FROM nodes LIMIT 3"
```

Actual output:
```
ID                      Type    Name     URI
nI3NDos2Cv-720e4Bwa8gw  fs:dir  axon     file:///...
```

Expected: Columns in SELECT order `uri, name, type, id` — not the hardcoded `ID, Type, Name, URI`.

### Root Cause

In `cmd/axon/query.go`, `printNodesTable` (lines 182–278) has a hardcoded column order: ID → Type → Name → URI → Key → Labels → Data. This order is always applied regardless of the user's SELECT clause.

The function receives `[]*graph.Node` (fully-populated or partially-populated structs). The SELECT projection is available in the AQL AST but is not passed down to the rendering layer — only the Node slice is returned, losing the column-selection metadata.

### Suggested Fix

Pass the list of selected column names (in order) alongside the result nodes, and use that to drive the table header and row construction.

This requires threading the selected columns through:
1. `graph.QueryResult` — add `SelectedColumns []string` field
2. `adapters/sqlite/aql.go` — populate `SelectedColumns` from the AQL AST's `Select.Columns`
3. `cmd/axon/query.go` — `printNodesTable` takes an additional `[]string` parameter and renders columns in that order

For the short term (no struct changes), at minimum the help text and examples in `query.go` should note that column order in `SELECT` is not preserved in the output.

### Related Patterns

Column order is only meaningful for multi-column node queries. Edge queries (`printEdgesTable`) have a hardcoded 5-column layout (`ID, Type, From, To, Data`) and don't have this issue since all edge fields are always shown.

---

## Finding 7: `axon query --output json` produces wrong structure for aggregate queries

**Category**: CLI Bug  
**Severity**: 🔴 High

### Evidence

```bash
axon query --output json "SELECT type, COUNT(*) FROM nodes GROUP BY type ORDER BY COUNT(*) DESC LIMIT 5"
```

Actual output:
```json
{
  "go:field": 431,
  "go:method": 363,
  "go:ref": 21566,
  "md:codeblock": 169,
  "md:section": 198
}
```

Problems:
1. **Wrong structure**: flat `{key: count}` map instead of an ordered array of `{key, count}` objects
2. **Wrong key order**: alphabetically sorted (`go:field` before `go:ref`) instead of by count descending
3. **Wrong result count**: showing 5 rows but count values are in wrong order

Expected output:
```json
[
  {"key": "go:ref", "count": 21566},
  {"key": "go:field", "count": 431},
  {"key": "go:method", "count": 363},
  {"key": "md:section", "count": 198},
  {"key": "md:codeblock", "count": 169}
]
```

### Root Cause

In `cmd/axon/query.go` lines 136–139:
```go
case graph.ResultTypeCounts:
    data = result.Counts   // result.Counts is map[string]int
```

`json.Marshal(map[string]int{...})` produces a JSON object with alphabetically-sorted keys — standard Go JSON behavior. The ORDER BY from SQLite is lost because `Counts` is a map (same root cause as Finding 5).

### Suggested Fix

In `printQueryResultJSON`, convert `result.Counts` to an ordered slice before marshaling:

```go
case graph.ResultTypeCounts:
    var cr CountResult
    cr.FromMap(result.Counts)
    cr.SortByCount()
    // Marshal as array of {key, count} objects
    type countRow struct {
        Key   string `json:"key"`
        Count int    `json:"count"`
    }
    rows := make([]countRow, len(cr.Items))
    for i, item := range cr.Items {
        rows[i] = countRow{Key: item.Name, Count: item.Count}
    }
    data = rows
```

The **deeper fix** is to change `Counts map[string]int` to an ordered type at the `graph.QueryResult` level (see Finding 5 deep fix suggestion).

---

## Finding 8: `axon query --output json` includes unselected timestamp fields

**Category**: CLI Bug  
**Severity**: 🟠 Medium

### Evidence

```bash
axon query --output json "SELECT name, type FROM nodes WHERE type = 'fs:file' LIMIT 2"
```

Actual output:
```json
[
  {
    "type": "fs:file",
    "name": ".gitignore",
    "created_at": "0001-01-01T00:00:00Z",
    "updated_at": "0001-01-01T00:00:00Z"
  }
]
```

Expected: Only `name` and `type` fields (as selected). No `created_at`/`updated_at`.

### Root Cause

**Two separate issues:**

**Issue A — `time.Time` ignores `omitempty`** (`graph/node.go` lines 21–22):
```go
CreatedAt  time.Time `json:"created_at,omitempty"`
UpdatedAt  time.Time `json:"updated_at,omitempty"`
```

`time.Time{}` (zero value) is NOT considered "empty" by Go's JSON encoder. Unlike strings or pointers, a zero `time.Time` still serializes as `"0001-01-01T00:00:00Z"`. This is a known Go gotcha.

When `SELECT name, type FROM nodes` is used (non-star query), `scanNodePartial` in `adapters/sqlite/aql.go` only populates `node.Name` and `node.Type`. The `CreatedAt` and `UpdatedAt` fields remain as zero values. But because `omitempty` doesn't work for `time.Time`, they appear in the JSON output.

**Issue B — JSON output ignores SELECT projection**:

Even if the `time.Time` zero-value issue were fixed, the JSON output still serializes the entire `graph.Node` struct rather than only the projected columns. There's no mechanism to serialize only the fields that appear in the SELECT clause.

### Suggested Fix

**For Issue A (quick fix)**: Change `time.Time` to `*time.Time` in `graph/node.go`:
```go
CreatedAt  *time.Time `json:"created_at,omitempty"`
UpdatedAt  *time.Time `json:"updated_at,omitempty"`
```

Pointer-to-time is `nil` when not set, and `nil` is truly empty for `omitempty`. Update all code that sets/reads these fields to use pointer syntax (`node.CreatedAt = &now`).

**For Issue B (deep fix)**: Introduce a `ProjectedRow` type in `graph` that holds only the selected columns as a generic `map[string]any`, preserving column order via `[]string` column names and `[]any` values. The JSON output for node queries would then serialize this projection map rather than the full struct.

This requires significant changes to `executeNodeQuery`, `QueryResult`, and the rendering layer, but would make `axon query` behave like a proper projection-aware query tool.

---

## Observations Without Findings

- **`axon show` error message repeats**: when a node is not found, the error text appears both inline and in the command's usage. Not a bug — this is cobra's default error display behavior — but could be improved by disabling `SilenceUsage` for "not found" errors.

- **`axon info` vs `axon stats`**: `axon info` shows global DB counts; `axon stats` without `-g` shows local scoped counts. The behavior is different but both are intentional. The distinction could be clearer in the help text.

- **`axon tree --depth` works correctly**: the tree command with `--depth 2` and `--types` filters work as documented.

- **URI deduplication is correct**: `SELECT uri, COUNT(*) FROM nodes GROUP BY uri HAVING c > 1` returns no results — no duplicate URIs.

---

## Recommended Next Steps

Priority-ordered actions for a developer:

1. **Fix orphaned reference edges** (`indexer/golang/indexer.go`): Add `!obj.Exported()` check in `indexReferences` when `ExportedOnly` is true. This eliminates 14,000+ phantom edges per index run and makes the graph integrity clean. High impact, low risk.

2. **Fix `printCountsTable` and JSON aggregate output** (`cmd/axon/query.go`): Convert `result.Counts` map to a sorted `CountResult` slice in both `printCountsTable` and `printQueryResultJSON`. This fixes the non-deterministic `ORDER BY` behavior and the wrong JSON structure for aggregate queries. These are user-visible correctness bugs that make `axon query` unreliable.

3. **Fix `time.Time` zero-value JSON serialization** (`graph/node.go`): Change `CreatedAt` and `UpdatedAt` to `*time.Time`. Quick one-liner change that eliminates confusing `"0001-01-01T00:00:00Z"` in partial-column JSON output.

4. **Fix `axon info` orphaned label** (`cmd/axon/info.go`): Change "Orphaned:" to "Orphaned edges:" in the text output. Two-line change, eliminates user confusion.

5. **Fix help examples for `vcs:*` types** (`cmd/axon/find.go`): Add `--global` to the three `vcs:*` examples in the `find` command's `Long` help text. Trivial change, eliminates silent zero-result surprises.
