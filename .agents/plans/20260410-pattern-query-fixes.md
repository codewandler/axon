# Plan: Fix Pattern Query Gaps — 2026-04-10

**Reference**: `.agents/improve/20260410-022844-pattern-query-gaps.md`
**Total estimated time**: ~30 minutes
**Risk**: Low — all changes are additive or one-line; existing tests stay green

---

## Task Order and Dependencies

```
Task 1  Fix SELECT COUNT(*) FROM pattern        (aql.go — 1 line + 1 new test)
Task 2  Fix SELECT <var> returns null           (aql.go — extractSelectedColumns)
Task 3  Fix SELECT * FROM pattern compile error (aql.go — compilePatternSelect)
Task 4  Fix _count sentinel in COUNT output     (aql.go + query.go)
Task 5  Fix axon tree <node-id>                 (tree.go)
Task 6  Add scope labels to info/stats output   (info.go + stats.go)
```

Tasks 1–3 all modify `adapters/sqlite/aql.go` with no inter-dependency.
Tasks 5 and 6 are fully independent of each other and of Tasks 1–4.
Task 4 depends on nothing but touches two files (aql.go + query.go).

---

## Task 1: Fix `SELECT COUNT(*) FROM pattern` (Finding 5 — 🔴 High)

**Files modified**: `adapters/sqlite/aql.go`, `adapters/sqlite/aql_test.go`
**Estimated time**: 5 minutes

### What to change

In `adapters/sqlite/aql.go`, **line 1234**, change:

```go
// BEFORE
if hasCount && len(q.Select.GroupBy) > 0 {
    resultType = graph.ResultTypeCounts
} else {
```

to:

```go
// AFTER
if hasCount {
    resultType = graph.ResultTypeCounts
} else {
```

**Why**: `compileFlatQuery` already uses `ResultTypeCounts` for any COUNT regardless of GROUP BY. `compilePatternQuery` mistakenly required GROUP BY, causing scalar COUNT to fall through to `ResultTypeNodes` where `scanNodePartial` silently ignores the "COUNT(*)" column and returns an empty node.

### What to update in the test file

In `adapters/sqlite/aql_test.go`, the existing `TestQuery_Count` (line 364–377) has a comment documenting the broken behaviour:

```go
// COUNT without GROUP BY doesn't populate Counts map in current implementation
// This is a limitation - would need to handle this case specially
// For now, this test documents current behavior
_ = result
```

Replace the body of that test to assert the correct behaviour:

```go
func TestQuery_Count(t *testing.T) {
    s, ctx := setupAQLTest(t)

    q := aql.Nodes.Select(aql.Count()).Build()
    result, err := s.Query(ctx, q)
    if err != nil {
        t.Fatalf("Query failed: %v", err)
    }

    if result.Type != graph.ResultTypeCounts {
        t.Errorf("expected ResultTypeCounts, got %v", result.Type)
    }
    if len(result.Counts) != 1 {
        t.Fatalf("expected 1 count item, got %d", len(result.Counts))
    }
    // 4 files + 2 dirs + 1 repo + 2 branches = 9
    if result.Counts[0].Count != 9 {
        t.Errorf("expected count 9, got %d", result.Counts[0].Count)
    }
}
```

Add a new pattern-specific test after the existing pattern tests:

```go
// TestPattern_Count verifies SELECT COUNT(*) FROM pattern returns the correct integer.
func TestPattern_Count(t *testing.T) {
    s, ctx := setupAQLTest(t)

    pattern := aql.Pat(aql.N("dir").OfTypeStr("fs:dir").Build()).
        To(aql.Edge.Contains.ToEdgePattern(), aql.N("file").OfTypeStr("fs:file").Build()).
        Build()

    q := aql.Select(aql.Count()).FromPattern(pattern).Build()
    result, err := s.Query(ctx, q)
    if err != nil {
        t.Fatalf("Pattern COUNT query failed: %v", err)
    }

    if result.Type != graph.ResultTypeCounts {
        t.Errorf("expected ResultTypeCounts, got %v", result.Type)
    }
    if len(result.Counts) != 1 {
        t.Fatalf("expected 1 count item, got %d", len(result.Counts))
    }
    // src->test1.go, src->test2.py, cmd->main.go = 3
    if result.Counts[0].Count != 3 {
        t.Errorf("expected count 3, got %d", result.Counts[0].Count)
    }
}
```

### Verification

```bash
go test ./adapters/sqlite -run TestQuery_Count -v
go test ./adapters/sqlite -run TestPattern_Count -v
go test ./adapters/sqlite ./... # ensure nothing else broke
go build -o ./bin/axon ./cmd/axon
./bin/axon query "SELECT COUNT(*) FROM (dir:fs:dir)-[:contains]->(file:fs:file)"
# Expected: a table showing Count = 3 (or similar)
```

---

## Task 2: Fix `SELECT <var>` Returns Null (Finding 3 — 🔴 High)

**Files modified**: `adapters/sqlite/aql.go`, `adapters/sqlite/aql_test.go`
**Estimated time**: 5 minutes

### What to change

In `adapters/sqlite/aql.go`, in `extractSelectedColumns` (lines 2738–2762), the function captures whole-variable selectors like `"file"` as column names, but `nodeFieldRaw(node, "file")` has no mapping for variable names and returns nil.

The fix: when all selected columns are whole-variable references (single-part selectors that match a pattern variable), return `nil` — which signals the caller to use SELECT * (full-node) display mode.

But `extractSelectedColumns` doesn't receive the set of pattern variables. The simpler fix is: treat a single-part selector that produces no dot in the column name as a "whole-variable" reference that should NOT be projected.

Replace the `*aql.Selector` case in `extractSelectedColumns` (lines 2750–2756):

```go
// BEFORE
case *aql.Selector:
    if col.Alias != "" {
        cols = append(cols, col.Alias)
    } else {
        cols = append(cols, expr.String())
    }
```

```go
// AFTER
case *aql.Selector:
    if col.Alias != "" {
        cols = append(cols, col.Alias)
    } else if len(expr.Parts) > 1 {
        // var.field or var.data.field — keep as projection column
        cols = append(cols, expr.String())
    }
    // Single-part selectors (whole-variable references like "file") are
    // NOT added to SelectedColumns. This causes the caller to display the
    // full node (SELECT * behaviour), which is correct since the SQL
    // already emits alias.* for single-variable pattern queries.
```

This means:
- `SELECT file` → `SelectedColumns = nil` → full node displayed ✅
- `SELECT file.name, file.type` → `SelectedColumns = ["file.name", "file.type"]` → projection ✅
- `SELECT name, type` (flat query) → `SelectedColumns = ["name", "type"]` → projection ✅

### Add a test

After `TestPattern_AnonymousNode`, add:

```go
// TestPattern_SelectSingleVariable verifies SELECT <var> returns fully populated nodes,
// not null. Regression test for: single-part variable selector captured by
// extractSelectedColumns as a projection column, causing nodeFieldRaw to return nil.
func TestPattern_SelectSingleVariable(t *testing.T) {
    s, ctx := setupAQLTest(t)

    pattern := aql.Pat(aql.AnyNode()).
        To(aql.Edge.Contains.ToEdgePattern(), aql.N("file").OfTypeStr("fs:file").Build()).
        Build()

    q := aql.Select(aql.Var("file")).FromPattern(pattern).Build()
    result, err := s.Query(ctx, q)
    if err != nil {
        t.Fatalf("Pattern query failed: %v", err)
    }

    // Should return 3 file nodes (test1.go, test2.py, main.go)
    if len(result.Nodes) != 3 {
        t.Errorf("expected 3 nodes, got %d", len(result.Nodes))
    }

    // Nodes must be fully populated — not empty due to projection failure
    for _, n := range result.Nodes {
        if n.ID == "" {
            t.Error("node ID is empty — whole-variable SELECT returned unpopulated node")
        }
        if n.Type == "" {
            t.Error("node Type is empty — whole-variable SELECT returned unpopulated node")
        }
        if n.Name == "" {
            t.Error("node Name is empty — whole-variable SELECT returned unpopulated node")
        }
    }

    // SelectedColumns must be nil (full-node mode, not projection mode)
    if len(result.SelectedColumns) != 0 {
        t.Errorf("expected empty SelectedColumns for whole-variable SELECT, got %v", result.SelectedColumns)
    }
}
```

### Verification

```bash
go test ./adapters/sqlite -run TestPattern_SelectSingleVariable -v
go test ./adapters/sqlite -run TestPattern_AnonymousNode -v  # must still pass
go build -o ./bin/axon ./cmd/axon
./bin/axon query "SELECT file FROM (dir:fs:dir)-[:contains]->(file:fs:file) LIMIT 3" --output json
# Expected: 3 fully populated node objects, NOT [{"file":null}, ...]
```

---

## Task 3: Fix `SELECT * FROM pattern` Compile Error (Finding 4 — 🟠 Medium)

**Files modified**: `adapters/sqlite/aql.go`, `adapters/sqlite/aql_test.go`
**Estimated time**: 5 minutes

### What to change

In `adapters/sqlite/aql.go`, in `compilePatternSelect` (lines 1665–1740), before the final error return at line 1736, add a `*aql.Star` case.

Insert after the COUNT(*) block (after line 1733, before line 1736):

```go
// Handle SELECT *
if _, ok := col.Expr.(*aql.Star); ok {
    if multiVar {
        // Expand all node variables with aliased columns
        // to avoid duplicate column names
        first := true
        for varName, alias := range nodeAliases {
            if !first {
                sqlBuilder.WriteString(", ")
            }
            first = false
            parts := make([]string, len(nodeFields))
            for j, field := range nodeFields {
                parts[j] = fmt.Sprintf(`%s.%s AS "%s.%s"`, alias, field, varName, field)
            }
            sqlBuilder.WriteString(strings.Join(parts, ", "))
        }
    } else {
        // Single variable: expand to alias.*
        // Find the single node alias
        for _, alias := range nodeAliases {
            sqlBuilder.WriteString(alias)
            sqlBuilder.WriteString(".*")
            break
        }
    }
    continue
}
```

**Important**: For the single-variable `SELECT *` path, `isStar` in `executeNodeQuery` will still be `false` (the AQL query has `*aql.Star` but the check at line 2831–2835 checks the AQL column, not the SQL). The generated SQL `alias.*` expands to all columns, and `scanNodePartial` handles them correctly via the column-name switch. So this works without changes to the executor.

For the `multiVar` case, `resultType` should be `ResultTypeRows` (which is already set at line 1254–1255 when `multiVar && resultType == ResultTypeNodes`). This path works correctly.

### Add a test

```go
// TestPattern_SelectStar verifies SELECT * FROM pattern works without error.
func TestPattern_SelectStar(t *testing.T) {
    s, ctx := setupAQLTest(t)

    pattern := aql.Pat(aql.N("dir").OfTypeStr("fs:dir").Build()).
        To(aql.Edge.Contains.ToEdgePattern(), aql.N("file").OfTypeStr("fs:file").Build()).
        Build()

    q := aql.Select(aql.StarCol()).FromPattern(pattern).Build()
    result, err := s.Query(ctx, q)
    if err != nil {
        t.Fatalf("SELECT * FROM pattern failed: %v", err)
    }

    // Should return 3 pairs (src->test1.go, src->test2.py, cmd->main.go)
    // Since two vars but SELECT *, result is ResultTypeRows
    if result.Type != graph.ResultTypeRows && result.Type != graph.ResultTypeNodes {
        t.Errorf("unexpected result type %v", result.Type)
    }
    total := len(result.Nodes) + len(result.Rows)
    if total == 0 {
        t.Error("expected some results from SELECT * FROM pattern, got none")
    }
}
```

Note: Check what `aql.StarCol()` is called in the builder. Looking at the builder, `aql.Nodes.SelectStar()` or `aql.Select(aql.Star)` — check the actual API:

```bash
grep -n "StarCol\|SelectStar\|Star\b" /home/timo/projects/axon/aql/builder.go | head -20
```

Use the correct constructor in the test based on what you find.

### Verification

```bash
go test ./adapters/sqlite -run TestPattern_SelectStar -v
go build -o ./bin/axon ./cmd/axon
./bin/axon query "SELECT * FROM (dir:fs:dir)-[:contains]->(file:fs:file) LIMIT 3"
# Expected: table output with columns (no compile error)
```

---

## Task 4: Fix `_count` Sentinel in Scalar COUNT Display (Finding 6 — 🟡 Low)

**Files modified**: `adapters/sqlite/aql.go`, `cmd/axon/query.go`
**Estimated time**: 5 minutes

### Change 1 — `adapters/sqlite/aql.go` line 3145

```go
// BEFORE
counts = append(counts, graph.CountItem{Name: "_count", Count: count})

// AFTER
counts = append(counts, graph.CountItem{Name: "", Count: count})
```

This stops the internal sentinel from leaking out.

### Change 2 — `cmd/axon/query.go`, `printCountsTable` (lines 518–542)

The `header` logic at lines 526–531 already defaults to `"Key"` when `groupingCol == ""`. For scalar COUNT (one item, empty name, no groupingCol), we want:

```
Count
9
```

Replace the entire `printCountsTable` function:

```go
func printCountsTable(counts []graph.CountItem, groupingCol string) error {
    if len(counts) == 0 {
        fmt.Println("No results")
        return nil
    }

    w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
    defer w.Flush()

    // Scalar COUNT(*) — single item with no name and no grouping column
    isScalar := groupingCol == "" && len(counts) == 1 && counts[0].Name == ""
    if isScalar {
        fmt.Fprintf(w, "Count\n")
        fmt.Fprintf(w, "%d\n", counts[0].Count)
        return nil
    }

    header := groupingCol
    if header == "" {
        header = "Key"
    } else {
        header = strings.Title(strings.ReplaceAll(strings.ReplaceAll(header, "_", " "), ".", " "))
    }

    fmt.Fprintf(w, "%s\tCount\n", header)
    for _, item := range counts {
        fmt.Fprintf(w, "%s\t%d\n", item.Name, item.Count)
    }

    return nil
}
```

### Change 3 — `cmd/axon/query.go`, `printQueryResultJSON` (lines 161–174)

In the `ResultTypeCounts` case, detect scalar:

```go
case graph.ResultTypeCounts:
    groupingKey := result.GroupingColumn
    isScalar := groupingKey == "" && len(result.Counts) == 1 && result.Counts[0].Name == ""
    if isScalar {
        data = []map[string]any{{"count": result.Counts[0].Count}}
    } else {
        if groupingKey == "" {
            groupingKey = "key"
        }
        countRows := make([]map[string]any, len(result.Counts))
        for i, item := range result.Counts {
            countRows[i] = map[string]any{
                groupingKey: item.Name,
                "count":     item.Count,
            }
        }
        data = countRows
    }
```

### Verification

```bash
go build -o ./bin/axon ./cmd/axon
./bin/axon query "SELECT COUNT(*) FROM nodes"
# Expected:
# Count
# 5874

./bin/axon query "SELECT COUNT(*) FROM nodes" --output json
# Expected: [{"count":5874}]

./bin/axon query "SELECT type, COUNT(*) FROM nodes GROUP BY type"
# Expected: Type  Count table (unchanged behaviour)
```

---

## Task 5: Fix `axon tree <node-id>` (Finding 2 — 🟠 Medium)

**Files modified**: `cmd/axon/tree.go`
**Estimated time**: 8 minutes

### What to change

`runTree` in `tree.go` currently always treats the argument as a filesystem path. Add a node-ID lookup path before the `filepath.Abs` resolution.

The `findNodesByPrefix` function already exists in `show.go` and works for 4+ char prefixes. We need to open the DB first, then try node lookup, and fall through to path resolution if no node is found.

The current code opens the DB *after* resolving the path (because it uses `absPath` to find the DB). We need to restructure slightly: open DB first using CWD, then detect whether the argument is a node ID or path.

Replace `runTree` with:

```go
func runTree(cmd *cobra.Command, args []string) error {
    ctx := context.Background()

    cwd, err := os.Getwd()
    if err != nil {
        return fmt.Errorf("failed to get current directory: %w", err)
    }

    // Resolve database location using CWD (read-only)
    dbLoc, err := resolveDB(flagDBDir, flagGlobal, cwd, false)
    if err != nil {
        return err
    }

    fmt.Printf("Using database: %s\n", dbLoc.Path)

    storage, err := sqlite.New(dbLoc.Path)
    if err != nil {
        return fmt.Errorf("failed to open database: %w", err)
    }
    defer storage.Close()

    ax, err := axon.New(axon.Config{
        Dir:     cwd,
        Storage: storage,
    })
    if err != nil {
        return fmt.Errorf("failed to create axon: %w", err)
    }

    // Determine root node: try node-ID first, then path
    var rootNodeID string

    if len(args) > 0 && !strings.ContainsAny(args[0], "/\\") && !strings.HasPrefix(args[0], ".") {
        // Looks like a node ID (no path separators, not a relative path)
        // Minimum 4 chars required for a meaningful prefix search
        if len(args[0]) >= 4 {
            nodes, err := findNodesByPrefix(ctx, ax.Graph(), args[0])
            if err == nil && len(nodes) == 1 {
                rootNodeID = nodes[0].ID
            } else if err == nil && len(nodes) > 1 {
                fmt.Printf("Multiple nodes match '%s':\n", args[0])
                for _, n := range nodes {
                    fmt.Printf("  %s  %s (%s)\n", n.ID[:7], n.Name, n.Type)
                }
                return nil
            }
        }
    }

    // Fall back to path resolution
    if rootNodeID == "" {
        startPath := "."
        if len(args) > 0 {
            startPath = args[0]
        }
        absPath, err := filepath.Abs(startPath)
        if err != nil {
            return fmt.Errorf("failed to resolve path: %w", err)
        }
        info, err := os.Stat(absPath)
        if err != nil {
            return fmt.Errorf("path does not exist: %w", err)
        }
        if !info.IsDir() {
            return fmt.Errorf("path is not a directory: %s", absPath)
        }
        uri := types.PathToURI(absPath)
        rootNode, err := ax.Graph().GetNodeByURI(ctx, uri)
        if err != nil {
            return fmt.Errorf("failed to find root node for path %s: %w", absPath, err)
        }
        rootNodeID = rootNode.ID
    }

    isTTY := isatty.IsTerminal(os.Stdout.Fd()) || isatty.IsCygwinTerminal(os.Stdout.Fd())
    useColor := (isTTY || treeColor) && !treeNoColor
    useEmoji := (isTTY || treeEmoji) && !treeNoEmoji

    opts := render.Options{
        MaxDepth:   treeDepth,
        ShowIDs:    treeShowIDs,
        ShowTypes:  treeShowTypes,
        UseColor:   useColor,
        UseEmoji:   useEmoji,
        TypeFilter: treeTypeFilter,
    }

    output, err := render.Tree(ctx, ax.Graph(), rootNodeID, opts)
    if err != nil {
        return fmt.Errorf("failed to render tree: %w", err)
    }

    fmt.Print(output)
    return nil
}
```

Also update the imports: add `"strings"` to the imports in `tree.go` if it isn't already there.

Also update the `Long:` description to add usage examples:

```go
Long: `Display the indexed graph as a tree structure.

If no argument is provided, shows the tree from the current directory.
You can specify a filesystem path or a node ID to start from.

Node IDs are shown in brackets by axon tree --ids and axon find output.
A prefix of at least 4 characters is sufficient.

Examples:
  axon tree                    # Current directory subtree
  axon tree /path/to/dir       # Subtree rooted at directory
  axon tree nI3NDos            # Subtree rooted at node by ID prefix
  axon tree --depth 2          # Limit depth
  axon tree --type fs:file     # Show only matching node types`,
```

### Verification

```bash
go build -o ./bin/axon ./cmd/axon

# Get a real node ID from the indexed graph
./bin/axon find --type fs:dir --output table | head -5
# Copy a 7-char prefix from the ID column, e.g. "gaqGIb8"

./bin/axon tree gaqGIb8
# Expected: tree rooted at the .agents/ directory (or whatever node matches)

./bin/axon tree .
# Expected: full tree from CWD (unchanged behaviour)

./bin/axon tree nonexistentpath
# Expected: "path does not exist" error (unchanged error for bad paths)

./bin/axon tree --help
# Expected: examples section visible
```

---

## Task 6: Add Scope Labels to `axon info` and `axon stats` (Finding 1 — 🟠 Medium)

**Files modified**: `cmd/axon/info.go`, `cmd/axon/stats.go`
**Estimated time**: 4 minutes

### Change 1 — `cmd/axon/info.go`, `renderInfoText` (lines 193–200)

`axon info` always shows global counts. Make this explicit:

```go
// BEFORE
p.Printf("Nodes:         %d\n", data.Nodes)
p.Printf("Edges:         %d\n", data.Edges)

// AFTER
p.Printf("Nodes:         %d  (global)\n", data.Nodes)
p.Printf("Edges:         %d  (global)\n", data.Edges)
```

Also update `renderInfoJSON` — the `infoData` struct already has `IsGlobal bool`. The JSON is correct as-is (is_global: false for local dbs). No JSON change needed.

### Change 2 — `cmd/axon/stats.go`, `renderStatsText` (lines 318–366)

In `renderStatsText`, add the scope label to the Nodes/Edges lines. The function currently doesn't receive `statsGlobal` directly — pass it in, or check `data.NodeTypes` length to infer scope. The cleaner approach is to pass `statsGlobal` as a parameter (or use the package-level `statsGlobal` var, which is accessible since it's the same package):

```go
// In renderStatsText, replace the Nodes/Edges printf block:

// BEFORE
p.Printf("Nodes:        %d\n", data.Nodes)
p.Printf("Edges:        %d\n", data.Edges)

// AFTER
if statsGlobal {
    p.Printf("Nodes:        %d  (global)\n", data.Nodes)
    p.Printf("Edges:        %d  (global)\n", data.Edges)
} else {
    p.Printf("Nodes:        %d  (scoped to CWD)\n", data.Nodes)
    p.Printf("Edges:        %d  (scoped to CWD)\n", data.Edges)
}
```

### Verification

```bash
go build -o ./bin/axon ./cmd/axon
./bin/axon info
# Expected: "Nodes:  5,874  (global)"

./bin/axon stats
# Expected: "Nodes:  720  (scoped to CWD)"

./bin/axon stats --global
# Expected: "Nodes:  5,874  (global)"

./bin/axon stats -o json
# Verify JSON output is not broken (no new fields needed)
```

---

## Final Verification (All Tasks)

After all tasks are complete:

```bash
# Full test suite must pass
go test ./...

# Build must succeed
go build -o ./bin/axon ./cmd/axon

# Re-index to get a fresh local DB
./bin/axon init --local .

# Smoke test each fixed behaviour:
./bin/axon query "SELECT COUNT(*) FROM (dir:fs:dir)-[:contains]->(file:fs:file)"
./bin/axon query "SELECT file FROM (dir:fs:dir)-[:contains]->(file:fs:file) LIMIT 3" --output json
./bin/axon query "SELECT * FROM (dir:fs:dir)-[:contains]->(file:fs:file) LIMIT 3"
./bin/axon query "SELECT COUNT(*) FROM nodes"
./bin/axon query "SELECT COUNT(*) FROM nodes" --output json
./bin/axon tree nI3NDos   # replace with actual short ID from: axon tree --depth 1
./bin/axon info
./bin/axon stats
./bin/axon stats --global
```

All six commands above should produce meaningful, correct output.
