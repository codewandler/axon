# Improvement Patterns Reference

Concrete patterns for detecting problems in the axon codebase and reasoning
about their root causes. All detection is via CLI or source reading only.
**No code is written.** These patterns produce suggestions for the report.

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

**Suggested fix direction**:
> In `cmd/axon/<command>.go`, find the `Flags().StringVarP(...)` (or similar) call
> for the drifting flag. Update either the flag description string or the default
> value so they match actual runtime behaviour.

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

**Suggested fix direction**:
> In `aql/validate.go`, describe the missing validation rule that should catch
> the problematic query (e.g. "all WHERE variable references must be bound in
> the FROM pattern"). Name the function that should be added or extended.
> Describe what error message the user should see.

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

**Suggested fix direction**:
> For each unchecked call, describe: the file, the function, the line behaviour,
> and what the corrected pattern should look like. Example:
> "In `x.go`, `s.storage.Flush()` on line N discards the error. It should be
> wrapped and returned: `if err := s.storage.Flush(); err != nil { return … }`."

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

**Suggested fix direction**:
> In the WalkDir callback in `indexer/<name>/indexer.go`, describe where a
> `select { case <-ctx.Done(): return ctx.Err(); default: }` block should be
> inserted and why. Note whether this affects UX (e.g. Ctrl-C responsiveness).

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

**Suggested fix direction**:
> List which specific sections of which doc files need updating. For each,
> describe the current (wrong) text and what it should say. Do not edit the
> files — describe the change precisely enough that a developer can apply it.

---

## Pattern 6 — Test Coverage Gaps

**What it is**: A code path exists that has no corresponding test.

**Detection**:

```bash
# Find files with no corresponding test file
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

**Suggested fix direction**:
> For each untested path, describe: what scenario is not covered, what a test
> would need to arrange/act/assert, and which package the test should live in.
> Do not write the test — describe its structure and intent.

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

**Suggested fix direction**:
> Describe which commands produce inconsistent shapes and what the unified
> shape should be. Note whether a shared helper already exists that is being
> bypassed, or whether one needs to be introduced.

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

**Suggested fix direction**:
> In `cmd/axon/db.go` (or wherever the lookup lives), describe what the current
> error message says and what it should say. An actionable error includes the
> exact command a user should run next (e.g. `axon init .`).

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

**Suggested fix direction**:
> For each indexer that does not call `DeleteStaleByURIPrefix`, describe where
> in its `Index` method the call should be added, and what URI prefix it should
> use. Confirm the generation value flows through correctly.

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

**Suggested fix direction**:
> Describe which command or sub-operation ignores `--local`, trace where the
> flag is parsed and where it is lost, and explain what the correct resolution
> order should be (e.g. explicit `--db-dir` → `--local` → walk-up discovery →
> global fallback).

---

## Triage Priority Matrix

Use this matrix to decide the severity rating in the report:

| Severity | Category | Example |
|---|---|---|
| 🔴 High | Panic / data loss | nil deref crash, silent data corruption |
| 🔴 High | Silent wrong result | AQL query returns wrong rows without error |
| 🟠 Medium | Confusing error messages | "error: EOF" with no context |
| 🟠 Medium | Help text drift | `--output` default says "json" but is "table" |
| 🟡 Low | Missing test coverage | no test for a non-trivial path |
| 🟡 Low | Documentation sync | README describes removed flag |
| 🟢 Nice | Output formatting | spacing inconsistency in table output |
