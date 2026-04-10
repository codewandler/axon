# PLAN: AQL pattern query with COUNT(*) and 3+ SELECT columns (issue #24)

## Task 1 — Write the failing test

**File**: `adapters/sqlite/aql_test.go`

Add `TestPattern_GroupBy_MultiCol` after the existing `TestPattern_GroupBy` test.

The test selects 3 columns from a pattern query (2 node fields + COUNT(*)):

```go
func TestPattern_GroupBy_MultiCol(t *testing.T) {
    s, ctx := setupAQLTest(t)

    // SELECT dir.name, dir.type, COUNT(*)
    // FROM (dir:fs:dir)-[:contains]->(file:fs:file)
    // GROUP BY dir.id ORDER BY COUNT(*) DESC
    pattern := aql.Pat(aql.N("dir").OfTypeStr("fs:dir").Build()).
        To(aql.Edge.Contains.ToEdgePattern(), aql.N("file").OfTypeStr("fs:file").Build()).
        Build()

    q := aql.Select(aql.Col("dir", "name"), aql.Col("dir", "type"), aql.Count()).
        FromPattern(pattern).
        GroupByCol("dir.id").
        OrderByCount(false).
        Build()

    result, err := s.Query(ctx, q)
    if err != nil {
        t.Fatalf("Pattern GROUP BY multi-col query failed: %v", err)
    }

    if result.Type != graph.ResultTypeRows {
        t.Errorf("expected ResultTypeRows, got %v", result.Type)
    }

    if len(result.Rows) != 2 {
        t.Errorf("expected 2 rows (src, cmd), got %d", len(result.Rows))
    }

    // Find src row and verify fields
    var srcRow map[string]any
    for _, row := range result.Rows {
        if name, _ := row["dir.name"].(string); name == "src" {
            srcRow = row
        }
    }
    if srcRow == nil {
        t.Fatal("expected a row for 'src' directory")
    }
    if srcRow["dir.type"] != "fs:dir" {
        t.Errorf("expected dir.type = fs:dir, got %v", srcRow["dir.type"])
    }
    if count, ok := srcRow["COUNT(*)"].(int64); !ok || count != 2 {
        t.Errorf("expected COUNT(*) = 2 for src, got %v", srcRow["COUNT(*)"])
    }
}
```

**Verification**: `go test -v -run TestPattern_GroupBy_MultiCol ./adapters/sqlite` → must FAIL with the scan error.

---

## Task 2 — Implement the fix

**File**: `adapters/sqlite/aql.go`, function `compilePatternQuery`

Replace the unconditional `resultType = graph.ResultTypeCounts` block with a count of
non-COUNT columns. When 2+ non-COUNT columns are present, use `ResultTypeRows`.
Also force `multiVar=true` for all `ResultTypeRows` paths (not just multi-variable SELECT).

```go
// BEFORE (lines ~1227-1236):
if hasCount {
    resultType = graph.ResultTypeCounts
} else {
    // ... edge variable check
}

// AFTER:
if hasCount {
    nonCountCols := 0
    for _, col := range q.Select.Columns {
        if _, ok := col.Expr.(*aql.CountCall); !ok {
            nonCountCols++
        }
    }
    if nonCountCols > 1 {
        // 3+ columns (2+ fields + COUNT): executeCountQuery can only scan 2
        // columns (key + count). Use ResultTypeRows so executeRowsQuery
        // handles all columns dynamically.
        resultType = graph.ResultTypeRows
    } else {
        resultType = graph.ResultTypeCounts
    }
} else {
    // ... edge variable check (unchanged)
}
```

And update the `multiVar` block (~lines 1252-1255):

```go
// BEFORE:
multiVar := detectMultiVarSelect(q.Select, nodeAliases, edgeAliases)
if multiVar && resultType == graph.ResultTypeNodes {
    resultType = graph.ResultTypeRows
}

// AFTER:
multiVar := detectMultiVarSelect(q.Select, nodeAliases, edgeAliases)
if multiVar && resultType == graph.ResultTypeNodes {
    resultType = graph.ResultTypeRows
}
// Ensure qualified column aliases for all ResultTypeRows paths so that
// executeRowsQuery returns informative "var.field" keys.
if resultType == graph.ResultTypeRows {
    multiVar = true
}
```

**Verification**: `go test -v -run TestPattern_GroupBy_MultiCol ./adapters/sqlite` → must PASS.

---

## Task 3 — Run the full test suite

```bash
go test -race ./adapters/sqlite/...
go test -race ./...
```

All tests must pass.
