# Axon Improvement Report — 2026-04-10

**Session focus**: Multi-variable pattern SELECT correctness, go:ref URI malformation, and search/context path display heuristics  
**DB used**: `.axon/graph.db` (local)  
**Phases completed**: Phase 1 ✅ | Phase 2 ✅

---

## Preamble: Relationship to Existing Reports

Before starting, the session reviewed the existing improve and plans folders:

- **`.agents/improve/20260410-004715-query-output-integrity.md`** — covers 8 findings (F1–F8) including orphaned edges, non-deterministic GROUP BY order, JSON aggregate structure, SELECT column projection, and `time.Time` zero-value serialization.
- **`.agents/plans/20260410-query-output-integrity.md`** — full implementation plan for all 8 of those findings.

The 4 findings below are **new and not covered** by either of those documents. Each is confirmed to be a distinct root cause with no overlap.

---

## Summary

CLI and graph exploration of the axon repo indexed against itself. Four new findings were surfaced, distinct from the prior session's report. The most critical is a data-loss bug in pattern query result handling: when `SELECT a, b` is used in a graph pattern query, only the last variable's data is returned. The remaining findings cover a malformed URI in the Go indexer and a path-display heuristic in the `search` and `context` commands that produces wrong file paths for many projects.

| # | Finding | Category | Severity |
|---|---------|----------|----------|
| 1 | `SELECT a, b FROM pattern` silently returns only the last variable | AQL Bug | 🔴 High |
| 2 | `go:ref` nodes have malformed URIs with a double-slash | Graph Integrity | 🟠 Medium |
| 3 | `search` and `context` display wrong file paths via heuristic | CLI Bug | 🟠 Medium |
| 4 | `axon context` returns empty for non-symbol task descriptions | UX Limitation | 🟡 Low |

---

## Finding 1: `SELECT a, b FROM pattern` silently returns only the last variable

**Category**: AQL Bug  
**Severity**: 🔴 High

### Evidence

```bash
axon query "SELECT a, b FROM (a:fs:dir)-[:contains]->(b:fs:file) WHERE b.name = 'README.md'"
```

Actual output:
```
ID                      Type     Name       URI                                        ...
JpiH9kTVh247QsDeWb5GfA  fs:file  README.md  file:///home/timo/projects/axon/README.md  ...
```

Expected: both `a` (the parent dir `axon`) and `b` (the file `README.md`) represented in the output.

Reversing the SELECT order confirms the pattern: the last listed variable always wins.

```bash
axon query "SELECT b, a FROM (a:fs:dir)-[:contains]->(b:fs:file) WHERE b.name = 'README.md'"
```

Actual output:
```
ID                      Type    Name  URI
nI3NDos2Cv-720e4Bwa8gw  fs:dir  axon  file:///home/timo/projects/axon
```

`b` was listed first in `SELECT b, a`, but `a` (the second node in the pattern — `n1` in SQL) is returned.

Field-level selection has the same problem:

```bash
axon query "SELECT a.name, b.name FROM (a:fs:dir)-[:contains]->(b:fs:file) WHERE b.name = 'README.md'"
```

Actual output:
```
Name
README.md
```

Expected: two `Name` columns (or two rows) for `axon` and `README.md`. Only the last one appears.

The `--explain` flag confirms the SQL is correct:

```
SQL: SELECT n0.*, n1.*
FROM nodes AS n0
JOIN edges AS e0 ON e0.from_id = n0.id
JOIN nodes AS n1 ON n1.id = e0.to_id
WHERE n0.type = ? AND e0.type = ? AND n1.type = ? AND (n1.name = ?)
```

SQLite returns all 22 columns correctly. The bug is in the Go result processor.

### Root Cause

**File**: `adapters/sqlite/aql.go`, function `scanNodePartial` (line 2717).

```go
func (s *Storage) scanNodePartial(rows *sql.Rows, cols []string) (*graph.Node, error) {
    scanDest := make([]any, len(cols))
    for i := range cols { scanDest[i] = new(any) }
    if err := rows.Scan(scanDest...); err != nil { return nil, err }

    node := &graph.Node{}
    for i, col := range cols {
        val := *(scanDest[i].(*any))
        switch col {         // ← column name used as key
        case "id":   node.ID   = ...
        case "type": node.Type = ...
        case "name": node.Name = ...
        ...
        }
    }
    return node, nil
}
```

When `SELECT n0.*, n1.*` is executed, SQLite returns 22 columns — 11 from `n0` and 11 from `n1` — all with the **same names** (`id`, `type`, `uri`, `name`, …, `id`, `type`, `uri`, `name`, …). The `switch col` statement processes them sequentially. When the second node's `id` column is reached, it overwrites `node.ID` with `n1.id`. By the end, the single `*graph.Node` returned contains entirely the data from the last node in the SELECT clause.

`executeNodeQuery` is designed on the assumption of one node per result row (`[]*graph.Node`). It cannot represent multiple nodes per row. The `resultType` for pattern queries is always `ResultTypeNodes`, which routes through this function regardless of how many node variables are selected.

### Suggested Fix

**Short-term** (prevent silent wrong results): Detect multi-variable node SELECT in `compilePatternSelect` and return an error when more than one node variable is selected, directing users to select one variable at a time or use field selectors (`a.name, b.name`).

**Medium-term** (correct output): Generate column aliases in the SQL to avoid name collisions, and introduce a `ProjectedRow` result type (already proposed in the earlier plan for Finding F6/F8B). For `SELECT a, b`:

```sql
SELECT n0.id AS a_id, n0.type AS a_type, n0.name AS a_name, ...,
       n1.id AS b_id, n1.type AS b_type, n1.name AS b_name, ...
```

The result would then be a `[]map[string]any` with prefixed keys, presented as a table with compound headers. This requires the `SelectedColumns` / `ProjectedRow` infrastructure from Task 5 of the existing plan, extended to handle multiple node variable prefixes.

**For field-level selectors** (`SELECT a.name, b.name`): the generated SQL is already `SELECT n0.name, n1.name` — both columns are named `name`. Apply the same aliasing: `SELECT n0.name AS a_name, n1.name AS b_name`. This is a self-contained fix inside `compilePatternSelect` → `resolvePatternSelector`.

### Related Patterns

- `SELECT a.name, b.name` (field-level multi-variable SELECT) has the exact same root cause.
- Any query selecting two edge variables (`SELECT e1, e2`) would have the same issue via `executeEdgeQuery`'s equivalent column-name mapping.
- The existing plan's Task 5 (`SelectedColumns` + projection) lays the groundwork for the correct fix here.

---

## Finding 2: `go:ref` nodes have malformed URIs with a double-slash

**Category**: Graph Integrity  
**Severity**: 🟠 Medium

### Evidence

```bash
axon query "SELECT name, uri FROM nodes WHERE type = 'go:ref' LIMIT 3" --output json
```

Actual output (URI field):
```
"uri": "go+file:///home/timo/projects/axon/ref//home/timo/projects/axon/aql/builder.go:117:72"
```

The URI contains a double-slash `ref//home/…` — i.e. the absolute file path is appended directly to a path segment that already ends with `/`.

Compare with correctly-formed URIs for other Go node types:
```
go:func:   go+file:///home/timo/projects/axon/pkg/github.com/codewandler/axon/aql/func/Pat
go:struct: go+file:///home/timo/projects/axon/pkg/github.com/codewandler/axon/aql/struct/Query
go:field:  go+file:///home/timo/projects/axon/pkg/github.com/codewandler/axon/aql/struct/Position/field/Line
```

These use a clean relative path. Only `go:ref` nodes have the double-slash.

There are **21,566** `go:ref` nodes — 92% of all nodes in the graph — all with this malformed URI pattern.

```bash
axon show -- --MnRhP
# Key: go+file:///home/timo/projects/axon/ref//home/timo/projects/axon/adapters/sqlite/aql.go:45:23
# Data.position.file: /home/timo/projects/axon/adapters/sqlite/aql.go  ← correct absolute path
```

The `key` field stores the same malformed URI. The `data.position.file` stores the correct absolute path separately.

### Root Cause

**File**: `indexer/golang/indexer.go`, `indexReferences()`, line 968:

```go
refURI := fmt.Sprintf("%s/ref/%s:%d:%d", moduleURI, pos.Filename, pos.Line, pos.Column)
```

Where:
- `moduleURI` = `"go+file:///home/timo/projects/axon"` (from `types.GoModulePathToURI(modDir)` — see `types/golang.go:232`)
- `pos.Filename` = `"/home/timo/projects/axon/aql/builder.go"` (absolute path from `go/token.FileSet`)

Combining: `"go+file:///home/timo/projects/axon"` + `"/ref/"` + `"/home/timo/projects/axon/aql/builder.go"` → `go+file:///home/timo/projects/axon/ref//home/timo/projects/axon/aql/builder.go:117:72`

The leading `/` of the absolute `pos.Filename` creates the double-slash after `ref/`. All other node types use module-relative paths (e.g. `pkg/github.com/codewandler/axon/...`) so they don't have this issue.

### Suggested Fix

In `indexer/golang/indexer.go`, `indexReferences()`, change the URI construction to use a module-relative path:

```go
// Before:
refURI := fmt.Sprintf("%s/ref/%s:%d:%d", moduleURI, pos.Filename, pos.Line, pos.Column)

// After:
relFilename := strings.TrimPrefix(pos.Filename, modDir+string(filepath.Separator))
refURI := fmt.Sprintf("%s/ref/%s:%d:%d", moduleURI, relFilename, pos.Line, pos.Column)
```

Where `modDir` is the module root directory (already available in scope — used to build `moduleURI` via `types.GoModulePathToURI(modDir)` a few lines earlier).

For the example above:
- `modDir` = `/home/timo/projects/axon`
- `pos.Filename` = `/home/timo/projects/axon/aql/builder.go`
- `relFilename` = `aql/builder.go`
- `refURI` = `go+file:///home/timo/projects/axon/ref/aql/builder.go:117:72` ✓

This also brings the `go:ref` URI structure in line with other go node types (module-relative, no absolute paths).

**Migration note**: Existing databases will have 21,566 nodes with old malformed URIs. On the next `axon init`, the Go indexer will regenerate them with correct URIs. The old nodes will be cleaned up by generation-based stale node deletion (`DeleteStaleByURIPrefix`) because the old URIs won't match the new generation. No manual migration needed — a fresh `axon init --local .` is sufficient.

### Related Patterns

- The `key` field is set to `refURI` (`WithKey(refURI)` on the same line), so the key is also malformed. The fix above corrects both.
- Any tooling that parses `go:ref` URIs by splitting on `/ref/` to extract the file path will currently receive an absolute path with a leading `/`; after the fix, it will receive a clean relative path. Update `URIToGoModulePath` in `types/golang.go` if it needs to reverse-parse `ref` URIs.

---

## Finding 3: `search` and `context` display wrong file paths via heuristic

**Category**: CLI Bug  
**Severity**: 🟠 Medium

### Evidence

```bash
axon search "where is NewNode defined"
```

Actual output:
```
axon/graph/node.go:55  (go:func)
```

Expected: `graph/node.go:55` (relative to the project root / CWD).

```bash
ls graph/node.go    # exists
ls axon/graph/node.go  # does not exist
```

```bash
axon search "what is the parser"
```

Actual output:
```
Defined in `axon/aql/parser.go:13`
```

Expected: `aql/parser.go:13`

```bash
axon context --task "Storage interface" --tokens 5000 --no-source
```

Manifest shows paths such as:
```
| axon/graph/graph.go   | 46-50 | 23  | definition of Storage |
| axon/graph/storage.go | ...   | ... | ...                   |
| projects/axon/axon.go | 57-59 | 19  | definition of Storage |
| cmd/axon/db.go        | ...   | ... | ...                   |  ← correct
| adapters/sqlite/aql.go| ...   | ... | ...                   |  ← correct
```

Some paths are correct (`cmd/axon/db.go`, `adapters/sqlite/aql.go`) while others are wrong (`axon/graph/graph.go`, `projects/axon/axon.go`). The inconsistency depends on how deep the file is in the directory tree.

### Root Cause

**Two separate `shortenPath` implementations, both heuristic-based:**

**File 1**: `cmd/axon/search.go`, line 1153:
```go
func shortenPath(path string) string {
    // Find "axon" in path and return from there
    if idx := strings.Index(path, "/axon/"); idx != -1 {
        return path[idx+1:]  // returns "axon/graph/node.go" — includes project name
    }
    // Otherwise just return last 3 parts
    ...
}
```

For `/home/timo/projects/axon/graph/node.go`:
- `strings.Index(path, "/axon/")` finds `/axon/` at the projects segment
- `path[idx+1:]` returns `axon/graph/node.go` — the `/axon/` prefix is retained

This is hardcoded to the string `"axon"` — it will produce wrong output for any project not named `axon`.

**File 2**: `context/format.go`, line 180:
```go
func shortenPath(path string) string {
    markers := []string{"/src/", "/pkg/", "/cmd/", "/internal/"}
    for _, marker := range markers {
        if idx := strings.LastIndex(path, marker); idx != -1 {
            return path[idx+1:]  // e.g. "cmd/axon/db.go" ← correct
        }
    }
    // Fall back to last 3 path components
    parts := strings.Split(path, string(filepath.Separator))
    if len(parts) > 3 {
        return strings.Join(parts[len(parts)-3:], string(filepath.Separator))
    }
    return path
}
```

For `/home/timo/projects/axon/graph/graph.go`:
- No `/src/`, `/pkg/`, `/cmd/`, `/internal/` markers found
- Falls back to last 3 components: `["axon", "graph", "graph.go"]` → `axon/graph/graph.go` ✗

For `/home/timo/projects/axon/axon.go`:
- No markers
- Last 3 components: `["projects", "axon", "axon.go"]` → `projects/axon/axon.go` ✗

For `/home/timo/projects/axon/adapters/sqlite/aql.go`:
- No markers
- Last 3 components: `["adapters", "sqlite", "aql.go"]` → `adapters/sqlite/aql.go` ✓ (happens to work)

The "last 3 components" fallback is coincidentally correct for files 3+ directories deep, but fails for files at the project root or one level deep.

### Suggested Fix

Both `shortenPath` functions should use `filepath.Rel(cwd, path)` rather than heuristics. The CWD is already available or easily obtained in both contexts.

**`cmd/axon/search.go`**: The search command already has access to the DB path and CWD. Pass CWD to `shortenPath` or make it a closure:

```go
func makePathShortener(cwd string) func(string) string {
    return func(path string) string {
        rel, err := filepath.Rel(cwd, path)
        if err != nil || strings.HasPrefix(rel, "..") {
            return path // fallback for paths outside cwd
        }
        return rel
    }
}
```

**`context/format.go`**: The `Format` and `FormatManifest` functions receive a `*FitResult` which knows the task but not the CWD. Pass CWD through `FitResult` or `Options`:

```go
// In Options:
type Options struct {
    ...
    CWD string // for relative path display; defaults to os.Getwd()
}
```

Then in `shortenPath(path, cwd string)`:
```go
func shortenPath(path, cwd string) string {
    rel, err := filepath.Rel(cwd, path)
    if err != nil || strings.HasPrefix(rel, "../../../") {
        return path
    }
    return rel
}
```

### Related Patterns

- Both `search.go` and `context/format.go` have independent implementations of the same faulty heuristic. Consolidating into a shared utility in `cmd/axon/output.go` (or a new `cmd/axon/paths.go`) would prevent the divergence from recurring.
- The `axon show` command correctly displays full URIs and doesn't use `shortenPath` — no issue there.
- README and AGENTS.md document CLI output that would change aesthetically once paths are corrected. No user-visible contract is violated; this is a pure bug fix.

---

## Finding 4: `axon context` returns empty for non-symbol task descriptions

**Category**: UX Limitation  
**Severity**: 🟡 Low

### Evidence

```bash
axon context --task "how does AQL parsing work" --tokens 3000
```

Actual output:
```
## Context for: "how does AQL parsing work"
### Summary
- **0 files**, **0 tokens** (budget: 3000)
```

```bash
axon search "how does AQL parsing work"
```

Actual output:
```
## Explaining: AQL parsing
No symbols found related to 'aql parsing'
```

But with exact symbol names:
```bash
axon context --task "Parser" --tokens 2000  # → 265 tokens, 1 file, correct content
axon search "what is the parser"             # → finds Parser struct correctly
```

### Root Cause

**File**: `context/task.go`, `ParseTask()` (line 103) + `context/walker.go`, `Walk()` (line 68–72).

`ParseTask` extracts symbols using:
1. Backtick-quoted identifiers
2. Dotted paths (e.g. `graph.Storage`)
3. PascalCase/camelCase words matching `\b([A-Z][a-zA-Z0-9]*|[a-z]+[A-Z][a-zA-Z0-9]*)\b`

For `"how does AQL parsing work"`:
- "AQL" matches the PascalCase pattern (starts with uppercase A, followed by letters)
- `task.Symbols = ["AQL"]`

`Walk()` (line 70–72) immediately returns `nil, nil` if `len(task.Symbols) == 0`. Here symbols are non-empty, but `findDefinitions` (line 139) queries:

```sql
SELECT * FROM nodes WHERE name = 'AQL' AND type LIKE 'go:%' AND type != 'go:ref'
```

No node named `"AQL"` exists — the Go package is `aql` (lowercase), and its package node is named `aql`. "AQL" is a doc-level abbreviation, not a Go identifier. Zero results → zero context.

`axon search` routes through the same path and similarly finds nothing.

The design is intentional: the context engine is symbol-oriented, not keyword-search-oriented. But the failure is silent — the user sees "0 files" with no hint about why.

### Suggested Fix

**Minimal (quick win)**: When `Walk()` returns zero items (or `findDefinitions` finds no definitions for any extracted symbol), print a diagnostic message:

```
Note: no Go symbols found matching ["AQL"]. Try using exact symbol names
like `Parser`, `Query`, or `aql.Parse`. Use `axon search "list structs"` to browse.
```

**Longer-term**: Add keyword-based fallback in `Walk()` when Ring 0 returns empty — search for nodes whose `data.doc` or `name` contains the task keywords. For example, `keywords = ["aql", "parsing"]` could match `go:package` nodes with name `aql` or `go:func` nodes with doc containing "parse".

### Related Patterns

The same issue applies to any all-caps acronym (e.g. "JSON", "CLI", "HTTP"), snake_case identifiers, or lowercase Go names (unexported functions). Users coming from natural language (as the `search` help text implies with examples like `"explain the indexer system"`) will regularly hit this without understanding why.

---

## Observations Without Findings

- **`axon info` vs `axon stats` node count**: `info` shows all 23,451 nodes in the database; `stats -v` without `-g` shows 658 (scoped to CWD). Both behaviors are documented and correct. The existing report already notes this as an observation. The word "local" in `info`'s output refers to the database *location* not a *scope* — this distinction could be clearer but is not a new finding.

- **AQL column-to-column comparison**: `WHERE from_id = to_id` fails (`unexpected token "to_id"`). This is a grammar limitation — the right-hand side of comparisons must be a literal value, not another column. Already covered as part of the AQL grammar design in Finding 4 of the prior report. No new code to write; the grammar.md should document this limitation explicitly.

- **`go:ref` nodes without `references` edges**: 14,441 of 21,566 `go:ref` nodes (67%) have no outgoing `references` edge. These are refs to local variables and unexported symbols. This is the same phenomenon documented in Finding 3 of the prior report (ExportedOnly inconsistency). The URI fix in Finding 2 above does not change this count — those nodes will still exist but will have well-formed URIs.

- **`TODO.md` has title "foo"**: The file literally starts with `# foo` — a placeholder heading. Not an axon bug; cosmetic content issue in the repo.

---

## Recommended Next Steps

These complement (and do not replace) the 6-task plan in `.agents/plans/20260410-query-output-integrity.md`:

1. **Fix multi-variable pattern SELECT** (`adapters/sqlite/aql.go` → `compilePatternSelect`): As a short-term measure, detect when more than one node variable is selected and return a clear error. As a medium-term fix, generate aliased columns (`n0.name AS a_name`) and extend the `ProjectedRow` infrastructure from Task 5 of the existing plan. This should be folded into Task 5 as a sub-task.

2. **Fix `go:ref` URI double-slash** (`indexer/golang/indexer.go`, line 968): One-line fix — `strings.TrimPrefix(pos.Filename, modDir+"/")` before building `refURI`. Run `go test ./indexer/golang/...` afterward and verify with `axon init --local . && axon query "SELECT uri FROM nodes WHERE type = 'go:ref' LIMIT 3"`. Low risk, high cosmetic value (cleans up 21K malformed URIs).

3. **Fix `shortenPath` in both `search.go` and `context/format.go`**: Replace both heuristic implementations with `filepath.Rel(cwd, path)`. The CWD is available in the CLI layer. This is a 2-file change with broad user-visible impact — displayed paths will become correct and consistent for any project name or depth.

4. **Add diagnostic on empty context results** (`context/walker.go`): When `Walk()` returns no items, emit a hint message listing the extracted symbols that were searched and suggest alternatives. Zero-line change to behavior, significant improvement to discoverability.
