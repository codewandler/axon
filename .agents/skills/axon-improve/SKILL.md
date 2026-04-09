---
name: axon-improve
description: >
  Self-improvement skill: use the axon CLI to explore the axon codebase itself,
  surface confusion, bugs, and gaps in CLI usability, then produce concrete code
  improvements to this repository.
license: MIT
compatibility: opencode
trigger: self-improve, axon improve, improve axon, explore codebase, find bugs in axon, axon self-analysis
---

# Axon Self-Improvement Workflow

This skill guides an agent through **using axon to analyse axon** — eating its own
dog food — so it can find real problems and fix them.

## Guiding Principle

> "If you can't query it, it doesn't exist."

Every finding must be grounded in something axon can actually show you.
Don't speculate; query first, then read source, then propose a change.

---

## Phase 1 — Bootstrap: Index the Codebase

Before anything else, make sure the axon graph is up-to-date for this repo.

```bash
# Index the axon repo itself (local DB so it doesn't pollute the global one)
axon init --local .

# Confirm it worked
axon info
axon stats -v
```

Expected: you should see `fs:file`, `fs:dir`, `vcs:*`, `md:*` node types and
edges like `contains`, `has`, `located_at`.

---

## Phase 2 — Structural Orientation

Get the big picture before diving in.

```bash
# High-level tree (2 levels deep)
axon tree --depth 2 --types

# What types of nodes exist?
axon types

# What edge types exist?
axon edges

# Label vocabulary
axon labels

# How many of each?
axon stats -v -g
```

Exploration queries to run:

```bash
# Package breakdown – count files per directory
axon query "SELECT name, type, COUNT(*) FROM nodes WHERE type = 'fs:dir' GROUP BY name ORDER BY name"

# Go source files only
axon find --ext go --global --output table

# Markdown docs
axon find --type md:document --global

# All CLI command files
axon find --ext go --name "*.go" --global | grep cmd/
```

Refer to [./references/exploration-queries.md](./references/exploration-queries.md) for
a fuller catalogue of useful queries.

---

## Phase 3 — Deep-Dive with `ask` and `context`

Use natural language interrogation to surface intent vs. implementation gaps.

```bash
# Understand core interfaces
axon search "what is the Storage interface"
axon search "what implements Indexer"
axon search "what methods does Storage have"

# Understand the AQL pipeline
axon search "describe the AQL compiler"
axon search "how does the aql builder work"
axon search "explain the query validation"

# Understand CLI plumbing
axon search "how does db resolution work"
axon search "explain the output formatting system"

# Understand event routing
axon search "how does event routing work"
axon search "explain the indexer subscription system"
```

Pump interesting answers through `context` to get the actual source:

```bash
axon context --task "understand Storage interface and implementations" --tokens 8000
axon context --task "trace an AQL query from CLI to SQLite" --tokens 10000
axon context --task "how does axon find the database file" --tokens 6000
```

---

## Phase 4 — Problem Detection

Look for concrete categories of problems. For each, there are queries that
reveal them.  See [./references/improvement-patterns.md](./references/improvement-patterns.md)
for the full pattern library.

### 4.1 CLI Behaviour vs. Help Text Gaps

Run every command's `--help` and compare what it claims against what you can
verify in the source.

```bash
# Get context for a command, then read its help text
axon context --task "axon find command implementation" --tokens 6000
axon search "what flags does the find command accept"
bin/axon find --help
```

Questions to ask for each command:
- Does the help text list ALL accepted flags?
- Do examples in help text actually work?
- Are error messages meaningful when bad input is given?

### 4.2 AQL Edge Cases

```bash
# Find the AQL test file
axon find --name "aql_test.go" --global

# How many AQL tests are there?
axon query "SELECT COUNT(*) FROM nodes WHERE type = 'fs:file' AND name GLOB '*aql*test*'"

# Get context for the AQL compiler
axon context --task "AQL compiler edge cases and error handling" --tokens 10000

# Look for TODOs/FIXMEs in AQL code
axon find --ext go --global | xargs grep -l "TODO\|FIXME\|HACK\|XXX" 2>/dev/null
```

Known areas to probe:
- What happens with an empty `FROM` clause?
- What happens when a pattern variable is used in `WHERE` but not in `FROM`?
- Does `GROUP BY` work with pattern queries? (check the grammar)
- Are integer literals in AQL treated as strings in some contexts?

### 4.3 Error Handling Gaps

```bash
axon context --task "error handling in sqlite adapter" --tokens 8000
axon search "how does axon handle database not found errors"
axon search "what happens when init fails midway"
```

Look for:
- Unchecked errors from `Flush()`
- Silent failures in edge deletion
- Missing `context.Context` cancellation checks in long operations

### 4.4 Documentation vs. Reality

```bash
# Find all markdown files
axon find --type md:document --global

# Get every heading in docs
axon query "SELECT name, uri FROM nodes WHERE type = 'md:heading' ORDER BY uri, name"
```

Questions:
- Does `README.md` mention all CLI commands (`ask`, `context`, `info`)?
- Does `aql/grammar.md` cover Phase 4 table functions?
- Is the AGENTS.md `New: Type-Safe Fluent AQL API` section accurate?

### 4.5 Graph Integrity

```bash
# Orphaned edges (should be zero after gc)
axon gc
axon query "SELECT COUNT(*) FROM edges"

# Nodes without any edges (possible orphans)
axon query "SELECT id, type, name FROM nodes WHERE NOT EXISTS (SELECT 1 FROM edges WHERE from_id = nodes.id OR to_id = nodes.id) LIMIT 20"

# Duplicate node URIs (should be empty)
axon query "SELECT uri, COUNT(*) as c FROM nodes GROUP BY uri HAVING c > 1"
```

---

## Phase 5 — Produce Improvements

Each finding must become a **concrete, verifiable change**.

### Output Format for Each Finding

```
## Finding: <short title>

**Category**: CLI Bug | AQL Bug | Error Handling | Documentation | Performance | Test Gap

**Evidence**:
- Command/query that reveals the problem
- Actual output vs. expected output
- Relevant source file(s) from `axon context`

**Root Cause**: (confirmed, not guessed)

**Proposed Fix**:
- File(s) to change
- What to change (with code snippet)
- Test to add

**Verification**:
- How to confirm the fix works
```

### Workflow for Each Fix

1. **Write failing test first** — confirm it fails with `go test -v -run TestXxx ./...`
2. **Implement fix** — minimal change, no unrelated cleanup
3. **Run full test suite** — `go test ./...`
4. **Re-run the axon query that exposed the bug** — confirm it no longer triggers it
5. **Update docs if needed** — README, grammar.md, AGENTS.md

---

## Phase 6 — Validate with Re-Index

After applying fixes, re-index and verify the graph is consistent:

```bash
axon init --local .
axon gc
axon stats -v

# Re-run the integrity checks from Phase 4.5
axon query "SELECT uri, COUNT(*) as c FROM nodes GROUP BY uri HAVING c > 1"
axon query "SELECT COUNT(*) FROM edges"
```

---

## Quick-Start Checklist

Run this sequence at the start of any self-improvement session:

```bash
# 1. Rebuild the binary
go build -o ./bin/axon ./cmd/axon

# 2. Re-index
axon init --local .

# 3. Orientation
axon info
axon stats -v
axon types
axon edges

# 4. Start exploring
axon search "explain the indexer system"
axon context --task "overview of all CLI commands" --tokens 8000

# 5. Look for known weak spots
axon context --task "AQL error handling and edge cases" --tokens 10000
axon context --task "CLI output formatting consistency" --tokens 8000
```

---

## Anti-Patterns to Avoid

| Anti-Pattern | Correct Approach |
|---|---|
| "I noticed this bug" without a query | Run the query, show the evidence |
| Changing code without a failing test | Write test → confirm fail → fix |
| Editing multiple unrelated things in one pass | One finding = one fix = one test |
| Relying on `--help` text as ground truth | Verify help text against source with `axon context` |
| Assuming graph is up-to-date | Always `axon init --local .` at session start |
| Fixing symptoms | Use `axon search "root cause of …"` + read source |
