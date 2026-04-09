# Plan: Pattern SELECT correctness, go:ref URIs, path display

**Source report**: `.agents/improve/20260410-011723-pattern-select-and-path-display.md`  
**Findings addressed**: F1 (full fix), F2, F3, F4  
**Scope decision**: F1 → medium-term full aliased-column fix

---

## Task 1 — Multi-variable pattern SELECT (F1, full fix)

**Root cause**: `compilePatternSelect` emits `n0.*, n1.*`, producing duplicate column names
(`id, type, …, id, type, …`). `scanNodePartial` maps by column name → last value wins.

### Step 1 — Add `ResultTypeRows` to `graph/storage.go`

```go
const (
    ResultTypeNodes  ResultType = iota
    ResultTypeEdges
    ResultTypeCounts
    ResultTypeRows   // Multi-variable or multi-field-selector pattern queries
)
```

Add `Rows` field to `QueryResult`:
```go
type QueryResult struct {
    Type            ResultType
    Nodes           []*Node
    Edges           []*Edge
    Counts          []CountItem
    SelectedColumns []string
    Rows            []map[string]any // For ResultTypeRows: ordered by SelectedColumns
}
```

### Step 2 — `adapters/sqlite/aql.go`: detect multi-variable SELECT

Add helper `detectMultiVarSelect(stmt, nodeAliases, edgeAliases) bool`:
- Collect distinct variable names referenced in SELECT columns
- Returns `true` when > 1 distinct variable name appears
- Whole-variable reference (`SELECT a`) counts as that variable name
- Field reference (`SELECT a.name`) extracts `"a"` as the variable name

### Step 3 — `adapters/sqlite/aql.go`: update `compilePatternQuery`

After existing result type detection, add:
```go
if detectMultiVarSelect(q.Select, nodeAliases, edgeAliases) {
    resultType = graph.ResultTypeRows
}
```

### Step 4 — `adapters/sqlite/aql.go`: update `compilePatternSelect`

Change signature to `(stmt, nodeAliases, edgeAliases) (sql string, err error)` (unchanged
signature; internal logic changes based on whether multi-var is detected):

When `resultType == ResultTypeRows` (signalled via a boolean parameter or detected inline):
- Whole-variable SELECT (`SELECT a` where `a` is a node alias `n0`):
  Expand to aliased columns with `"var.field"` aliases:
  ```sql
  n0.id AS "a.id", n0.type AS "a.type", n0.uri AS "a.uri",
  n0.key AS "a.key", n0.name AS "a.name", n0.labels AS "a.labels",
  n0.data AS "a.data", n0.generation AS "a.generation"
  ```
  (8 fields per node variable; skip `root`, `created_at`, `updated_at`)
- Field-level selector (`SELECT a.name` → `n0.name`):
  Alias as `n0.name AS "a.name"`

In single-variable mode (unchanged): emit `n0.*` as before.

Pass the multi-var flag into `compilePatternSelect` via a bool parameter:
```go
selectSQL, err := s.compilePatternSelect(q.Select, nodeAliases, edgeAliases, isMultiVar)
```

### Step 5 — `adapters/sqlite/aql.go`: add `executeRowsQuery`

```go
func (s *Storage) executeRowsQuery(ctx context.Context, sqlQuery string, args []any) ([]string, []map[string]any, error)
```

- `rows.Columns()` → column order (`["a.id", "a.type", "a.name", "b.id", ...]`)
- For each row: scan into `[]any`, build `map[string]any` with column name as key
- Parse `labels` values that look like JSON arrays into `[]any`
- Parse `data` values that look like JSON objects into `map[string]any`
- Return `(cols, rows, err)`

### Step 6 — `adapters/sqlite/aql.go`: update `executeQuery`

```go
case graph.ResultTypeRows:
    cols, rows, err := s.executeRowsQuery(ctx, sql, args)
    if err != nil { return nil, err }
    result.Rows = rows
    result.SelectedColumns = cols
```

### Step 7 — `cmd/axon/query.go`: handle `ResultTypeRows`

`printQueryResultTable`:
```go
case graph.ResultTypeRows:
    return printRowsTable(result.Rows, result.SelectedColumns)
```

`printQueryResultJSON`:
```go
case graph.ResultTypeRows:
    data = result.Rows
```

`printQueryResultCount`:
```go
case graph.ResultTypeRows:
    count = len(result.Rows)
```

New `printRowsTable(rows []map[string]any, cols []string) error`:
- Headers from `cols` (display as-is: `"a.id"`, `"a.name"`, etc.)
- Each row: iterate `cols`, look up `row[col]`, format as string
- Use `tabwriter` like existing table functions

### Step 8 — Tests

`TestQuery_MultiVariablePatternSelect` in `adapters/sqlite/aql_test.go`:
1. Setup: nodes + edges
2. `SELECT a, b FROM (a:fs:dir)-[:contains]->(b:fs:file)`:
   - `result.Type == ResultTypeRows`
   - Each row map has both `"a.id"`, `"a.type"`, `"a.name"`, `"b.id"`, `"b.type"`, `"b.name"`
   - `a.*` fields ≠ `b.*` fields (no overwriting)
3. `SELECT a.name, b.name FROM (a:fs:dir)-[:contains]->(b:fs:file)`:
   - `result.Rows[i]["a.name"]` = dir name, `result.Rows[i]["b.name"]` = file name
   - Values differ between the two variables (confirming no overwrite)

---

## Task 2 — `go:ref` URI double-slash (F2)

**Root cause**: `pos.Filename` is an absolute path; appending to `moduleURI + "/ref/"` produces
a double slash. `pkg.Module.Dir` (already in scope inside `indexReferences`) is the module root.

**File**: `indexer/golang/indexer.go`, `indexReferences()`, line 974.

```go
// Before:
refURI := fmt.Sprintf("%s/ref/%s:%d:%d", moduleURI, pos.Filename, pos.Line, pos.Column)

// After:
relFilename := strings.TrimPrefix(pos.Filename, pkg.Module.Dir+string(filepath.Separator))
refURI := fmt.Sprintf("%s/ref/%s:%d:%d", moduleURI, relFilename, pos.Line, pos.Column)
```

No signature change needed. `pkg.Module.Dir` is the correct absolute module root.

**Migration**: existing databases will have old malformed URIs. They'll be auto-replaced on the
next `axon init` run via generation-based cleanup (`DeleteStaleByURIPrefix`).

**Tests**: add assertion in `indexer/golang` tests that no `go:ref` URI contains `//`.

---

## Task 3 — `shortenPath` heuristics → `filepath.Rel` (F3)

**Root cause**: two independent implementations both use unreliable heuristics.

### `cmd/axon/search.go`

Replace `shortenPath` (line 1153) with `os.Getwd()`-based relative path:

```go
func shortenPath(path string) string {
    if cwd, err := os.Getwd(); err == nil {
        if rel, err := filepath.Rel(cwd, path); err == nil && !strings.HasPrefix(rel, "..") {
            return rel
        }
    }
    // Fallback: last 3 path components
    parts := strings.Split(path, string(filepath.Separator))
    if len(parts) > 3 {
        return strings.Join(parts[len(parts)-3:], string(filepath.Separator))
    }
    return path
}
```

Add `"os"` and `"path/filepath"` to imports if not present.

### `context/format.go`

Replace `shortenPath` (line 180) with the same pattern:

```go
func shortenPath(path string) string {
    if cwd, err := os.Getwd(); err == nil {
        if rel, err := filepath.Rel(cwd, path); err == nil && !strings.HasPrefix(rel, "..") {
            return rel
        }
    }
    parts := strings.Split(path, string(filepath.Separator))
    if len(parts) > 3 {
        return strings.Join(parts[len(parts)-3:], string(filepath.Separator))
    }
    return path
}
```

Add `"os"` to imports (`"path/filepath"` is already imported).

---

## Task 4 — `axon context` diagnostic on empty results (F4)

**Root cause**: `Walk()` finds symbols (e.g. `"AQL"`) but no Go node named `"AQL"` exists.
The result shows "0 files, 0 tokens" with no explanation.

**File**: `context/context.go`, `Gather()`, after the `Walk()` call.

```go
items, err := Walk(ctx, storage, task, walkOpts)
if err != nil {
    return "", fmt.Errorf("walking graph: %w", err)
}

// Emit a hint when symbols were extracted but nothing was found in the graph.
// Common cause: acronyms (AQL, HTTP), lowercase names, or misspellings.
var symbolHint string
if len(items) == 0 && len(task.Symbols) > 0 {
    symbolHint = fmt.Sprintf(
        "> **Note**: No Go symbols found matching %v. "+
        "Try exact symbol names like `Parser`, `Query`, or `NewNode`. "+
        "Use `axon search \"list structs\"` to browse available symbols.\n\n",
        task.Symbols,
    )
}
```

Prepend `symbolHint` to the formatted output before returning.

---

## Implementation Order

| # | Task | Files | Risk |
|---|------|-------|------|
| 1 | F2: go:ref URI | `indexer/golang/indexer.go` | Low |
| 2 | F3: shortenPath | `cmd/axon/search.go`, `context/format.go` | Low |
| 3 | F4: diagnostic | `context/context.go` | Low |
| 4 | F1: multi-var SELECT | `graph/storage.go`, `adapters/sqlite/aql.go`, `cmd/axon/query.go` | Medium |

Do F2, F3, F4 first (independent, low-risk), then F1 (data model + compiler change).

Final verification:
```bash
go build ./...
go test -count=1 ./...
# Verify F1:
axon query "SELECT a, b FROM (a:fs:dir)-[:contains]->(b:fs:file) WHERE b.name = 'README.md'"
# Expected: both axon dir and README.md file in output
# Verify F2:
axon query "SELECT uri FROM nodes WHERE type = 'go:ref' LIMIT 3"
# Expected: no double-slash in URIs
# Verify F3:
axon search "where is NewNode defined"
# Expected: "graph/node.go:XX" not "axon/graph/node.go:XX"
# Verify F4:
axon context --task "how does AQL parsing work" --tokens 1000
# Expected: note about no symbols found, with suggestions
```
