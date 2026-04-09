# Improvement Patterns Reference

Concrete patterns for detecting and fixing problems in the axon codebase,
grounded in queries you can actually run.

---

## Pattern 1 — CLI Help Text Drift

**What it is**: A flag exists in the source but is missing from `--help`, or the
help text describes behaviour that no longer matches the implementation.

**Detection**:

```bash
# Read the help text for every command
bin/axon find --help
bin/axon query --help
bin/axon tree --help
bin/axon stats --help
bin/axon show --help
bin/axon init --help
bin/axon search --help
bin/axon context --help
bin/axon info --help

# Get the source for each command
axon context --task "axon find command complete implementation" --tokens 6000
axon context --task "axon query command complete implementation" --tokens 6000
```

**Things to cross-check**:
- Every `cmd.Flags().XXX(...)` call has a corresponding line in the help text
- Every help-text example is syntactically valid and actually runs
- Default values shown in help match the code

**Fix template**:
```go
// In cmd/axon/<command>.go — add/fix the flag description
cmd.Flags().StringVarP(&output, "output", "o", "table",
    "Output format: table, json, count")  // ← update this string
```

---

## Pattern 2 — AQL Compiler Gaps

**What it is**: A syntactically valid AQL query that the compiler silently
miscompiles, panics on, or rejects with a confusing error.

**Detection**:

```bash
# Test edge cases manually
bin/axon query "SELECT * FROM nodes WHERE 1=1"                  # trivially true
bin/axon query "SELECT * FROM nodes WHERE name = ''"            # empty string
bin/axon query "SELECT COUNT(*) FROM nodes HAVING COUNT(*) > 0" # HAVING without GROUP BY
bin/axon query "SELECT * FROM nodes ORDER BY data.nonexistent"  # missing JSON field
bin/axon query "SELECT * FROM nodes LIMIT 0"                    # zero limit
bin/axon query "SELECT * FROM nodes LIMIT -1"                   # negative limit
bin/axon query "SELECT a FROM (x)-[:e]->(y) WHERE z.name=''"   # undefined variable z

# Use --explain to see the generated SQL
bin/axon query --explain "SELECT file FROM (dir)-[:contains]->(file)"
bin/axon query --explain "SELECT * FROM nodes, json_each(labels)"
bin/axon query --explain "SELECT * FROM nodes WHERE type GLOB 'fs:*'"

# Run parse to see AST
bin/axon parse "SELECT * FROM nodes WHERE type = 'fs:file'"
bin/axon parse "SELECT file FROM (dir)-[:contains]->(file)"
```

**Get compiler source**:
```bash
axon context --task "AQL compiler edge cases validate and compile" --tokens 12000
```

**Fix template**:
```go
// In aql/validate.go — add missing validation
func validatePatternVariables(q *Query) error {
    // collect all variables defined in FROM patterns
    // check all WHERE references are defined
}
```

**Test template**:
```go
// In adapters/sqlite/aql_test.go
func TestQueryEdgeCase_UndefinedVariable(t *testing.T) {
    // setup test DB
    // execute the bad query
    // assert: returns error, not panic
}
```

---

## Pattern 3 — Silent Error Swallowing

**What it is**: An error is returned from a called function but the caller
ignores it, leading to confusing silent failures.

**Detection**:

```bash
# Find unchecked errors in non-test Go files
axon find --ext go --global | xargs grep -n "^\s*[a-zA-Z].*()\s*$" 2>/dev/null | grep -v "_test.go"

# More targeted: look for common patterns
grep -rn "\.Flush()" --include="*.go" . | grep -v "_test.go" | grep -v "err ="
grep -rn "\.Close()" --include="*.go" . | grep -v "_test.go" | grep -v "err ="
grep -rn "json\.Marshal" --include="*.go" . | grep -v "_test.go" | grep -v "err ="

# axon context to read each suspicious file
axon context --task "error handling in storage flush and close" --tokens 6000
```

**Fix template**:
```go
// Before (bad):
s.storage.Flush()

// After (good):
if err := s.storage.Flush(); err != nil {
    return fmt.Errorf("flush: %w", err)
}
```

---

## Pattern 4 — Context Cancellation Not Checked

**What it is**: A long-running loop (indexing, traversal) doesn't check
`ctx.Done()`, making it unresponsive to cancellation.

**Detection**:

```bash
axon context --task "indexer loops that iterate over filesystem" --tokens 8000

# Look for Walk/WalkDir without ctx check
grep -rn "filepath.Walk\|filepath.WalkDir\|fs.WalkDir" --include="*.go" . | grep -v "_test.go"

# Check those files for ctx.Done() usage
axon search "which indexers check context cancellation"
```

**Fix template**:
```go
// In a WalkDir callback:
return filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
    select {
    case <-ctx.Done():
        return ctx.Err()  // ← add this
    default:
    }
    // ... existing logic
})
```

---

## Pattern 5 — Documentation Out of Sync

**What it is**: The README, AGENTS.md, or grammar.md describes a feature that
has since changed, or omits a newly added feature.

**Detection**:

```bash
# Compare CLI commands in README vs actual binary
bin/axon --help | grep "^  " | awk '{print $1}' | sort > /tmp/actual_commands.txt
axon query "SELECT name FROM nodes WHERE type = 'md:heading' AND uri GLOB '*README*'" \
  | grep -i "axon " | sort > /tmp/doc_commands.txt
diff /tmp/actual_commands.txt /tmp/doc_commands.txt

# Check grammar.md covers all 4 AQL phases
axon query "SELECT name FROM nodes WHERE type = 'md:heading' AND uri GLOB '*grammar*'"

# Does README mention context, ask, info commands?
grep -i "axon search\|axon context\|axon info" README.md

# Check AGENTS.md fluent API section matches current builder.go
axon context --task "current AQL builder API public methods" --tokens 6000
```

**Fix template**:
- Add missing commands to README.md command reference table
- Update grammar.md with missing clauses
- Sync AGENTS.md builder examples with `aql/builder.go` actual API

---

## Pattern 6 — Test Coverage Gaps

**What it is**: A code path exists that has no corresponding test.

**Detection**:

```bash
# Find files with no corresponding test file
axon query "
  SELECT f.name, f.uri
  FROM nodes f
  WHERE f.type = 'fs:file'
    AND f.data.ext = 'go'
    AND f.name NOT GLOB '*_test.go'
    AND f.name NOT GLOB 'main.go'
    AND NOT EXISTS (
      SELECT 1
      FROM nodes t
      WHERE t.type = 'fs:file'
        AND t.name = REPLACE(f.name, '.go', '_test.go')
    )
" --explain   # note: JOIN-based query, use --explain to check

# Simpler: list all non-test Go files and check for test siblings
axon find --ext go --global | grep -v "_test.go" | while read f; do
  base=$(basename "$f" .go)
  dir=$(dirname "$f")
  test_file="${dir}/${base}_test.go"
  [ ! -f "$test_file" ] && echo "NO TEST: $f"
done

# Which packages have zero test files?
axon query "
  SELECT parent.uri
  FROM nodes parent
  JOIN edges e ON e.from_id = parent.id
  JOIN nodes child ON child.id = e.to_id
  WHERE parent.type = 'fs:dir'
    AND child.type = 'fs:file'
    AND child.data.ext = 'go'
    AND e.type = 'contains'
  GROUP BY parent.id
  HAVING SUM(CASE WHEN child.name GLOB '*_test.go' THEN 1 ELSE 0 END) = 0
"
```

**Fix template**:
```go
// In <package>/<file>_test.go
func TestXxx_EdgeCase(t *testing.T) {
    // arrange
    // act
    // assert
}
```

---

## Pattern 7 — Inconsistent Output Formatting

**What it is**: Some commands support `--output json` but produce different
JSON shapes, or a command claims `--output table` but formats it differently.

**Detection**:

```bash
# Compare JSON output shapes
bin/axon stats -o json | jq 'keys'
bin/axon find --type fs:file --output json | jq '.[0] | keys'
bin/axon query --output json "SELECT * FROM nodes LIMIT 1" | jq '.[0] | keys'

# Read the output package
axon context --task "output formatting and json serialisation" --tokens 6000
axon search "explain the output formatting system"
axon search "what is the results package"
```

**Fix template**:
- Standardise all JSON output to use a consistent envelope (or consistently bare arrays)
- Add a shared `OutputResult(format, data)` helper if one doesn't exist

---

## Pattern 8 — DB Lookup Confusion

**What it is**: The auto-lookup logic for finding the database silently picks
the wrong file, or fails with a confusing message when no database exists yet.

**Detection**:

```bash
# Test from different directories
mkdir /tmp/axon-test && cd /tmp/axon-test
axon stats          # should say "no database found" clearly
axon find --type fs:file  # should give actionable error

# Check the lookup source
axon context --task "database file auto-lookup logic" --tokens 6000
axon search "how does axon find the database file"
```

**Fix template**:
```go
// In cmd/axon/db.go
if db == nil {
    return fmt.Errorf("no axon database found. Run 'axon init .' to create one")
    //                 ↑ actionable message with the exact command to run
}
```

---

## Pattern 9 — Stale Generation Leak

**What it is**: Nodes from a previous index run are not cleaned up because the
generation-based GC has a gap (e.g., an indexer doesn't call
`DeleteStaleByURIPrefix`).

**Detection**:

```bash
# Index twice, check counts are stable
axon init --local .
COUNT1=$(bin/axon query --output count "SELECT * FROM nodes")
axon init --local .
COUNT2=$(bin/axon query --output count "SELECT * FROM nodes")
echo "Before: $COUNT1  After: $COUNT2"   # should be equal or lower

# Run GC and check again
bin/axon gc
COUNT3=$(bin/axon query --output count "SELECT * FROM nodes")
echo "After GC: $COUNT3"  # if COUNT3 < COUNT2 there was a leak

# Read the indexer cleanup code
axon context --task "generation-based cleanup in indexers" --tokens 8000
axon search "how does stale node cleanup work"
```

**Fix template**:
```go
// In indexer/<name>/indexer.go — ensure cleanup is called
func (idx *Indexer) Index(ctx context.Context, ictx *indexer.Context) error {
    // ... index all nodes ...
    return ictx.Storage.DeleteStaleByURIPrefix(ctx, uriPrefix, ictx.Generation)
}
```

---

## Pattern 10 — Missing `--local` Propagation

**What it is**: A command supports `--local` but some sub-operation inside it
ignores the flag and always uses the global DB.

**Detection**:

```bash
# Read db.go to see how --local is resolved
axon context --task "local flag and db-dir flag handling in CLI" --tokens 6000

# Test: init with --local, then run other commands without --local
axon init --local .
bin/axon stats        # should use local DB because it auto-discovers .axon/
bin/axon tree         # same
bin/axon query "SELECT COUNT(*) FROM nodes"  # same
```

---

## Triage Priority Matrix

Use this matrix to decide which findings to fix first:

| Severity | Category | Fix First? |
|---|---|---|
| 🔴 High | Panic / data loss | Always |
| 🔴 High | Silent wrong result (AQL bug) | Always |
| 🟠 Medium | Confusing error messages | Yes |
| 🟠 Medium | Help text drift | Yes |
| 🟡 Low | Missing test coverage | When convenient |
| 🟡 Low | Documentation sync | When convenient |
| 🟢 Nice | Output formatting consistency | Batch together |
