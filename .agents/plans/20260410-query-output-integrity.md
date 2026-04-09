# Plan: Query Output Integrity

**Source report**: `.agents/improve/20260410-004715-query-output-integrity.md`  
**Findings addressed**: F1, F2, F3, F4 (full impl), F5, F6, F7, F8A, F8B  
**Scope decisions**: Q1→a (implement subqueries), Q2→b (deep fix Counts type), Q3→a (full projection)

---

## Task 1 — Documentation fixes (F1, F2)

Low-risk, independent, no tests needed.

### F1 — `axon info` "Orphaned" label

**File**: `cmd/axon/info.go`

Change two lines:
```go
// Line 195
p.Printf("Orphaned:      %d (run 'axon gc' to clean)\n", data.OrphanedEdges)
// → 
p.Printf("Orphaned edges: %d (run 'axon gc' to clean)\n", data.OrphanedEdges)

// Line 197
fmt.Println("Orphaned:      0")
// →
fmt.Println("Orphaned edges: 0")
```

### F2 — `axon find` help examples for `vcs:*` types

**File**: `cmd/axon/find.go`

Update three examples in the `Long` help string (lines 51–58):
```
  # Find all branches (requires --global since vcs nodes use git+file:// URIs)
  axon find --type vcs:branch --global --query "feature*"

  # Count git repositories
  axon find --type vcs:repo --global --count

  # Show nodes with parent chain
  axon find --type vcs:branch --global --show-parent
```

---

## Task 2 — Go indexer: prevent orphaned reference edges (F3)

Two targeted fixes; independent of the other tasks.

### F3A — Primary: skip unexported symbol refs when `ExportedOnly`

**File**: `indexer/golang/indexer.go`, inside `indexReferences()` (around line 946).

After the module-prefix filter and before `classifyReference`, add:
```go
// Skip references to unexported symbols when ExportedOnly is set.
// Unexported symbol nodes are never created, so these edges would be orphaned.
if i.ExportedOnly && !obj.Exported() {
    continue
}
```

The `obj.Exported()` call is on `types.Object` which all items in `pkg.TypesInfo.Uses` satisfy.

### F3B — Secondary: always run orphaned-edge GC after indexing

**File**: `axon.go`, line 370.

```go
// Before:
if !opts.SkipGC && ictx.NodesDeleted() > 0 {

// After:
if !opts.SkipGC {
```

Update the comment above it to read:
```go
// Clean up orphaned edges (edges pointing to deleted/missing nodes).
// Always run unless explicitly skipped — the cost is a single fast SQL DELETE.
```

**Tests**: Run `go test ./indexer/golang/...` and `go test ./...` to confirm no regressions.  
**Manual verification**: `axon init --local . && axon gc --dry-run` should show 0 orphaned edges.

---

## Task 3 — `time.Time` zero-value JSON fix (F8A)

**Files**: `graph/node.go`, `adapters/sqlite/aql.go`, `graph/node_test.go`

### Step 1 — Change `graph/node.go`

```go
// Before:
CreatedAt  time.Time `json:"created_at,omitempty"`
UpdatedAt  time.Time `json:"updated_at,omitempty"`

// After:
CreatedAt  *time.Time `json:"created_at,omitempty"`
UpdatedAt  *time.Time `json:"updated_at,omitempty"`
```

Update `NewNode()`:
```go
now := time.Now()
return &Node{
    ID:        NewID(),
    Type:      nodeType,
    CreatedAt: &now,
    UpdatedAt: &now,
}
```

Update `Clone()` to copy pointer fields:
```go
if n.CreatedAt != nil {
    t := *n.CreatedAt
    clone.CreatedAt = &t
}
if n.UpdatedAt != nil {
    t := *n.UpdatedAt
    clone.UpdatedAt = &t
}
```

### Step 2 — Update `adapters/sqlite/aql.go`

In `executeNodeQuery` SELECT * path (around line 2701):
```go
// Before:
node.CreatedAt, _ = time.Parse(time.RFC3339, createdAt)
node.UpdatedAt, _ = time.Parse(time.RFC3339, updatedAt)

// After:
if t, err := time.Parse(time.RFC3339, createdAt); err == nil {
    node.CreatedAt = &t
}
if t, err := time.Parse(time.RFC3339, updatedAt); err == nil {
    node.UpdatedAt = &t
}
```

In `scanNodePartial` (around line 2778):
```go
// Before:
case "created_at":
    if str, ok := val.(string); ok {
        node.CreatedAt, _ = time.Parse(time.RFC3339, str)
    }
case "updated_at":
    if str, ok := val.(string); ok {
        node.UpdatedAt, _ = time.Parse(time.RFC3339, str)
    }

// After:
case "created_at":
    if str, ok := val.(string); ok {
        if t, err := time.Parse(time.RFC3339, str); err == nil {
            node.CreatedAt = &t
        }
    }
case "updated_at":
    if str, ok := val.(string); ok {
        if t, err := time.Parse(time.RFC3339, str); err == nil {
            node.UpdatedAt = &t
        }
    }
```

### Step 3 — Grep for all other callers

Run `grep -rn "\.CreatedAt\|\.UpdatedAt" --include="*.go" .` after the change to catch any remaining dereferences that need `*` or nil-guards. Expected callers: storage `PutNode`/scan paths.

**Tests**: `go test ./...` — the JSON serialization test for partial-column queries should show `created_at` is absent.

---

## Task 4 — Ordered `Counts` in `QueryResult` (F5 + F7 root fix)

This is the deep data-model fix. It changes `QueryResult.Counts` from `map[string]int` to `[]CountItem`, preserving SQLite's ORDER BY at every layer.

### Step 1 — Add `CountItem` to `graph` package

**New file**: `graph/count.go`
```go
package graph

// CountItem represents a single aggregated count result (key + count).
// Used in QueryResult.Counts for GROUP BY queries.
type CountItem struct {
    Name  string `json:"name"`
    Count int    `json:"count"`
}
```

### Step 2 — Update `graph/storage.go`

In `QueryResult`:
```go
// Before:
Counts map[string]int // For GROUP BY queries (key → count)

// After:
Counts []CountItem // For GROUP BY queries, in SQLite result order
```

Update `QueryResult.Count()` to work with a slice:
```go
func (qr *QueryResult) Count() int {
    if qr.Type != ResultTypeCounts {
        return 0
    }
    // Scalar COUNT(*): special sentinel name "_count"
    for _, item := range qr.Counts {
        if item.Name == "_count" {
            return item.Count
        }
    }
    // GROUP BY: sum all buckets
    total := 0
    for _, item := range qr.Counts {
        total += item.Count
    }
    return total
}
```

### Step 3 — Update `adapters/sqlite/aql.go`

Change `executeCountQuery` signature and body:
```go
func (s *Storage) executeCountQuery(ctx context.Context, query *aql.Query, sqlQuery string, args []any) ([]graph.CountItem, error) {
    rows, err := s.db.QueryContext(ctx, sqlQuery, args...)
    if err != nil {
        return nil, err
    }
    defer rows.Close()

    var counts []graph.CountItem
    hasGroupBy := len(query.Select.GroupBy) > 0

    for rows.Next() {
        if hasGroupBy {
            var key sql.NullString
            var count int
            if err := rows.Scan(&key, &count); err != nil {
                return nil, err
            }
            if key.Valid {
                counts = append(counts, graph.CountItem{Name: key.String, Count: count})
            }
        } else {
            var count int
            if err := rows.Scan(&count); err != nil {
                return nil, err
            }
            counts = append(counts, graph.CountItem{Name: "_count", Count: count})
        }
    }
    return counts, rows.Err()
}
```

In `executeQuery`: the `result.Counts = counts` assignment type-checks automatically.

### Step 4 — Update `cmd/axon/results.go`

Remove the local `CountItem` definition. Import `graph.CountItem` instead.  
Add `FromSlice` method alongside the existing `FromMap`:
```go
import "github.com/codewandler/axon/graph"

// CountItem is re-exported from the graph package for CLI use.
type CountItem = graph.CountItem

// FromSlice populates CountResult from an ordered slice (preserves order).
func (r *CountResult) FromSlice(items []graph.CountItem) {
    r.Items = items
}
```

Using a type alias (`=`) means all existing code using `CountItem` compiles without changes.

### Step 5 — Update consumers

**`cmd/axon/types.go`**: replace `result.FromMap(result.Counts)` with `result.FromSlice(result.Counts)` (note: the comment "Already sorted by COUNT DESC from query" becomes actually true now).

**`cmd/axon/labels.go`**: same replacement. Remove the `SortByCount()` call since order is preserved from SQLite; OR keep it as a safety net (harmless).

**`cmd/axon/edges.go`**: same replacement.

**`cmd/axon/query.go`** — `printCountsTable`:
```go
func printCountsTable(counts []graph.CountItem) error {
    if len(counts) == 0 {
        fmt.Println("No results")
        return nil
    }
    w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
    defer w.Flush()
    fmt.Fprintln(w, "Key\tCount")
    for _, item := range counts {
        fmt.Fprintf(w, "%s\t%d\n", item.Name, item.Count)
    }
    return nil
}
```

**`cmd/axon/query.go`** — `printQueryResultJSON` for counts:
```go
case graph.ResultTypeCounts:
    type countRow struct {
        Key   string `json:"key"`
        Count int    `json:"count"`
    }
    rows := make([]countRow, len(result.Counts))
    for i, item := range result.Counts {
        rows[i] = countRow{Key: item.Name, Count: item.Count}
    }
    data = rows
```

**`cmd/axon/query.go`** — `printQueryResultCount` for counts:
```go
case graph.ResultTypeCounts:
    for _, item := range result.Counts {
        count += item.Count
    }
```

**`cmd/axon/query.go`** — `printQueryResultTable` passes `result.Counts` (now `[]graph.CountItem`) directly to `printCountsTable`.

**Tests**: `go test ./adapters/sqlite/... -run TestQuery_GroupBy` and `go test ./...`.

---

## Task 5 — SELECT column projection (F6 + F8B)

Thread selected-column metadata from the AST through `QueryResult` to both the table renderer and the JSON renderer.

### Step 1 — Add `SelectedColumns` to `graph.QueryResult`

**File**: `graph/storage.go`, `QueryResult` struct:
```go
type QueryResult struct {
    Type            ResultType
    Nodes           []*Node
    Edges           []*Edge
    Counts          []CountItem    // from Task 4
    SelectedColumns []string       // SELECT column names in order; nil/empty means SELECT *
}
```

`SelectedColumns` is a `[]string` of column names as they appear in the SQL result set (e.g., `["name", "type"]` for `SELECT name, type FROM nodes`). For `SELECT *` it is nil/empty. For COUNT queries it is irrelevant.

### Step 2 — Populate `SelectedColumns` in `adapters/sqlite/aql.go`

In `executeQuery`, after building `result`:
```go
// Populate SelectedColumns for non-star node/edge queries
if resultType == graph.ResultTypeNodes || resultType == graph.ResultTypeEdges {
    cols := extractSelectedColumns(query)
    if cols != nil {
        result.SelectedColumns = cols
    }
}
```

New helper `extractSelectedColumns`:
```go
// extractSelectedColumns returns the column names from a non-star SELECT clause,
// or nil for SELECT *.
func extractSelectedColumns(query *aql.Query) []string {
    if query.Select == nil || len(query.Select.Columns) == 0 {
        return nil
    }
    // SELECT * → nil
    if len(query.Select.Columns) == 1 {
        if _, ok := query.Select.Columns[0].Expr.(*aql.Star); ok {
            return nil
        }
    }
    var cols []string
    for _, col := range query.Select.Columns {
        if col.Alias != "" {
            cols = append(cols, col.Alias)
        } else if sel, ok := col.Expr.(*aql.Selector); ok {
            cols = append(cols, sel.String()) // e.g., "name", "data.ext"
        }
        // CountCall columns are not tracked here (handled by ResultTypeCounts path)
    }
    return cols
}
```

This is called once per query — negligible overhead.

### Step 3 — Update `printQueryResultTable`

**File**: `cmd/axon/query.go`

Change `printQueryResultTable` to pass `SelectedColumns`:
```go
func printQueryResultTable(result *graph.QueryResult) error {
    switch result.Type {
    case graph.ResultTypeNodes:
        return printNodesTable(result.Nodes, result.SelectedColumns)
    case graph.ResultTypeEdges:
        return printEdgesTable(result.Edges)
    case graph.ResultTypeCounts:
        return printCountsTable(result.Counts)
    default:
        return fmt.Errorf("unknown result type: %v", result.Type)
    }
}
```

Update `printNodesTable` signature and logic:
```go
func printNodesTable(nodes []*graph.Node, selectedCols []string) error {
    if len(nodes) == 0 {
        fmt.Println("No results")
        return nil
    }

    w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
    defer w.Flush()

    // If SELECT * or no column info, fall back to current auto-detect behavior
    if len(selectedCols) == 0 {
        return printNodesTableAutoDetect(nodes, w)
    }

    // Render columns in SELECT order
    fmt.Fprintln(w, strings.Join(colHeaders(selectedCols), "\t"))
    for _, node := range nodes {
        var row []string
        for _, col := range selectedCols {
            row = append(row, nodeFieldValue(node, col))
        }
        fmt.Fprintln(w, strings.Join(row, "\t"))
    }
    return nil
}
```

Helpers:
```go
// colHeaders converts SQL column names to display headers (e.g., "data.ext" → "Data.Ext").
func colHeaders(cols []string) []string {
    headers := make([]string, len(cols))
    for i, c := range cols {
        headers[i] = strings.Title(strings.ReplaceAll(c, "_", " "))
    }
    return headers
}

// nodeFieldValue extracts a node field value by column name for display.
func nodeFieldValue(node *graph.Node, col string) string {
    switch col {
    case "id":
        return truncate(node.ID, 22)
    case "type":
        return node.Type
    case "uri":
        return truncate(node.URI, 60)
    case "key":
        return truncate(node.Key, 40)
    case "name":
        return node.Name
    case "labels":
        return strings.Join(node.Labels, ", ")
    case "data":
        b, _ := json.Marshal(node.Data)
        return truncate(string(b), 40)
    case "generation":
        return node.Generation
    case "created_at":
        if node.CreatedAt != nil {
            return node.CreatedAt.Format(time.RFC3339)
        }
        return ""
    case "updated_at":
        if node.UpdatedAt != nil {
            return node.UpdatedAt.Format(time.RFC3339)
        }
        return ""
    default:
        // data.field selector: look in Data map
        if strings.HasPrefix(col, "data.") {
            field := strings.TrimPrefix(col, "data.")
            if m, ok := node.Data.(map[string]any); ok {
                if v, ok := m[field]; ok {
                    b, _ := json.Marshal(v)
                    return strings.Trim(string(b), `"`)
                }
            }
        }
        return ""
    }
}
```

Extract the existing detection logic into `printNodesTableAutoDetect(nodes []*graph.Node, w *tabwriter.Writer) error` (the current body of `printNodesTable`, unchanged).

### Step 4 — Update `printQueryResultJSON` for projection (F8B)

**File**: `cmd/axon/query.go`

```go
case graph.ResultTypeNodes:
    if len(result.SelectedColumns) > 0 {
        // Partial SELECT: project only the selected columns to avoid
        // serializing zero-value timestamp fields
        projected := make([]map[string]any, len(result.Nodes))
        for i, node := range result.Nodes {
            row := make(map[string]any, len(result.SelectedColumns))
            for _, col := range result.SelectedColumns {
                row[col] = nodeFieldRaw(node, col)
            }
            projected[i] = row
        }
        data = projected
    } else {
        data = result.Nodes
    }
```

Add `nodeFieldRaw` (returns `any`, not string — for correct JSON types):
```go
func nodeFieldRaw(node *graph.Node, col string) any {
    switch col {
    case "id":         return node.ID
    case "type":       return node.Type
    case "uri":        return node.URI
    case "key":        return node.Key
    case "name":       return node.Name
    case "labels":     return node.Labels
    case "data":       return node.Data
    case "generation": return node.Generation
    case "created_at":
        if node.CreatedAt != nil { return node.CreatedAt.Format(time.RFC3339) }
        return nil
    case "updated_at":
        if node.UpdatedAt != nil { return node.UpdatedAt.Format(time.RFC3339) }
        return nil
    default:
        if strings.HasPrefix(col, "data.") {
            field := strings.TrimPrefix(col, "data.")
            if m, ok := node.Data.(map[string]any); ok {
                return m[field]
            }
        }
        return nil
    }
}
```

**Tests**: `go test ./adapters/sqlite/... -run TestQuery_SelectColumns` and add a new test verifying that `result.SelectedColumns` is populated correctly.

---

## Task 6 — AQL subquery support in `IN`/`NOT IN` (F4)

Three-layer change: AST → parser grammar → SQL compiler.

### Step 1 — Extend `aql/ast.go`

Modify `InExpr` to optionally hold a subquery:
```go
type InExpr struct {
    Position Position
    Left     *Selector
    Values   []Value  // set when using literal list: IN ('a', 'b')
    Subquery *Query   // set when using subquery: IN (SELECT ...)
    Not      bool
}
```

### Step 2 — Extend `aql/parser.go`

Add a grammar struct for the body of `IN (...)`:
```go
type inBodyGrammar struct {
    Pos      lexer.Position
    Subquery *selectGrammar   `parser:"  @@"`
    Values   []*valueGrammar  `parser:"| @@ (',' @@)*"`
}
```

With `UseLookahead(4)` already set, participle will try `selectGrammar` (which starts with `'SELECT'`) first, then fall back to the value list.

Update `comparisonExprGrammar`:
```go
// Replace:
In       bool            `parser:"  | @'IN'"`
InValues []*valueGrammar `parser:"    '(' @@ (',' @@)* ')'"`

NotIn       bool            `parser:"  | @('NOT' 'IN')"`
NotInValues []*valueGrammar `parser:"    '(' @@ (',' @@)* ')'"`

// With:
In       bool           `parser:"  | @'IN'"`
InBody   *inBodyGrammar `parser:"    '(' @@ ')'"`

NotIn       bool           `parser:"  | @('NOT' 'IN')"`
NotInBody   *inBodyGrammar `parser:"    '(' @@ ')'"`
```

Update `comparisonExprGrammar.toAST()` for the IN cases:
```go
if g.In {
    expr := &InExpr{Position: toPosition(g.Pos), Left: sel}
    if g.InBody.Subquery != nil {
        expr.Subquery = &Query{Select: g.InBody.Subquery.toAST()}
    } else {
        for _, v := range g.InBody.Values {
            expr.Values = append(expr.Values, v.toAST())
        }
    }
    return expr
}
// Same pattern for NotIn
```

### Step 3 — Update `adapters/sqlite/aql.go` compiler

In `compileWhere` (wherever `InExpr` is handled), add subquery compilation:
```go
case *aql.InExpr:
    leftSQL, err := s.compileSelector(expr.Left, table)
    if err != nil {
        return "", nil, err
    }
    op := "IN"
    if expr.Not {
        op = "NOT IN"
    }
    if expr.Subquery != nil {
        // Compile inner query recursively — it produces its own SQL + args
        innerSQL, innerArgs, _, err := s.compileQuery(expr.Subquery)
        if err != nil {
            return "", nil, fmt.Errorf("subquery in %s: %w", op, err)
        }
        args = append(args, innerArgs...)
        return fmt.Sprintf("%s %s (%s)", leftSQL, op, innerSQL), args, nil
    }
    // Existing literal-list path
    placeholders := make([]string, len(expr.Values))
    for i, v := range expr.Values {
        sql, vArgs, err := s.compileValue(v)
        ...
    }
```

Add a `compileQuery(q *aql.Query) (sql string, args []any, resultType graph.ResultType, err error)` helper that wraps the existing compilation path (currently the body of `Query()`). This allows recursive subquery compilation without duplication.

### Step 4 — Validation

Update `aql/validate.go` to accept `SubqueryExpr`-style `InExpr` (skip literal-value validation when `Subquery != nil`).

Update `aql/grammar.md` to document subquery syntax:
```
in_expr ::= selector ("NOT")? "IN" "(" (select_stmt | value ("," value)*) ")"
```

### Step 5 — Tests

**`aql/parser_test.go`**: add `TestParser_InSubquery` checking that  
`SELECT id FROM nodes WHERE id NOT IN (SELECT from_id FROM edges)` parses without error and produces `InExpr{Not: true, Subquery: non-nil}`.

**`adapters/sqlite/aql_test.go`**: add `TestQuery_WhereNotInSubquery` that:
1. Inserts several nodes and edges
2. Runs `SELECT id, name FROM nodes WHERE id NOT IN (SELECT from_id FROM edges) LIMIT 20`
3. Asserts the result contains nodes that have no outgoing edges

---

## Implementation Order

| # | Task | Priority | Risk | Tests |
|---|------|----------|------|-------|
| 1 | F1, F2: doc fixes | Low | None | None needed |
| 2 | F3: Go indexer orphaned edges | High | Low | `go test ./indexer/golang/...` |
| 3 | F8A: `*time.Time` | Medium | Low (grep after) | `go test ./...` |
| 4 | F5/F7: Ordered Counts (`graph.CountItem`) | High | Medium | `go test ./adapters/sqlite/... -run TestQuery_GroupBy` |
| 5 | F6/F8B: `SelectedColumns` + projection | Medium | Medium | `go test ./adapters/sqlite/... -run TestQuery_SelectColumns` |
| 6 | F4: AQL subqueries | Medium | Medium | new parser + integration tests |

Tasks 4 and 5 both touch `graph.QueryResult` — implement them in the same pass to avoid a second compile-cycle. Task 3 (`*time.Time`) affects `graph.Node` which is read in Task 5 (`nodeFieldRaw`), so do Task 3 before Task 5.

Final verification after all tasks:
```bash
go build ./...
go test ./...
axon init --local . && axon gc --dry-run   # should show 0 orphaned edges
axon query "SELECT name, type FROM nodes WHERE type = 'fs:file' LIMIT 2 --output json"
# → only name+type fields, no created_at/updated_at
axon query "SELECT type, COUNT(*) FROM nodes GROUP BY type ORDER BY COUNT(*) DESC LIMIT 5"
# → deterministic order, highest count first
axon query --output json "SELECT type, COUNT(*) FROM nodes GROUP BY type ORDER BY COUNT(*) DESC LIMIT 5"
# → [{key: ..., count: ...}] array, ordered
axon query "SELECT id FROM nodes WHERE id NOT IN (SELECT from_id FROM edges) LIMIT 5"
# → valid results, no parse error
```
