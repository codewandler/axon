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

This skill is divided into **two strictly ordered phases**.

```
Phase 1 — CLI-only exploration   (no source reading, no axon skill)
Phase 2 — Deep analysis and fix  (load axon skill, then read source)
```

Do not skip ahead. Phase 1 forces you to find problems the way a user would:
through the CLI alone. Phase 2 is where you confirm root causes and write code.

---

## PHASE ONE — Raw CLI Exploration

> **Hard constraints for this entire phase:**
> - ❌ Do NOT read any source file (`file_read`, `grep` on `.go` files, etc.)
> - ❌ Do NOT load the `axon` skill
> - ❌ Do NOT run `axon search` or `axon context`
> - ✅ Only: `axon` CLI commands, `--help` flags, and `bash` for running the binary

The goal is to surface findings **purely from what the CLI shows you**.
If something looks wrong, suspicious, or confusing from the CLI output alone,
that is a valid finding. Write it down. Confirm it with more CLI commands.
Do NOT reach for source code to explain it yet.

---

### P1.1 — Bootstrap: Build and Index

```bash
# Build a fresh binary
go build -o ./bin/axon ./cmd/axon

# Index the axon repo itself (local DB, doesn't pollute global)
axon init --local .

# Confirm it worked
axon info
axon stats -v
```

Expected: `fs:file`, `fs:dir`, `vcs:*`, `md:*` node types; edges like
`contains`, `has`, `located_at`.

---

### P1.2 — Structural Orientation

```bash
# High-level tree
axon tree --depth 2 --types

# Vocabulary
axon types
axon edges
axon labels
axon stats -v -g
```

Orientation queries:

```bash
# File count per directory
axon query "SELECT name, COUNT(*) FROM nodes WHERE type = 'fs:dir' GROUP BY name ORDER BY name"

# Go source files
axon find --ext go --global --output table

# Markdown docs
axon find --type md:document

# CLI command files
axon find --ext go --global --output path | grep cmd/
```

Refer to [./references/exploration-queries.md](./references/exploration-queries.md)
for the full query catalogue.

---

### P1.3 — CLI Behaviour Audit

Run every command's `--help` and exercise each example it gives.
Record every gap between what is claimed and what actually happens.

```bash
axon find --help
axon query --help
axon tree --help
axon stats --help
axon info --help
axon gc --help
axon show --help
```

For each command, try the first two examples literally as written.
If an example produces no output or an error, that is a finding.

```bash
# Exercise help examples, e.g.:
axon find --type "md:*"          # Should return markdown nodes
axon find --name README.md       # Should return README
axon stats -v                    # Should show type breakdown
axon stats -v -g                 # Should show global breakdown
axon gc --dry-run                # Should report orphaned count
```

---

### P1.4 — Graph Integrity Checks

```bash
# Orphaned edges (should be zero after gc)
axon gc
axon query "SELECT COUNT(*) FROM edges"

# Nodes without any edges (possible orphans)
axon query "SELECT id, type, name FROM nodes WHERE NOT EXISTS (SELECT 1 FROM edges WHERE from_id = nodes.id OR to_id = nodes.id) LIMIT 20"

# Duplicate node URIs (should be empty)
axon query "SELECT uri, COUNT(*) as c FROM nodes GROUP BY uri HAVING c > 1"

# Node/edge count consistency: info vs stats
axon info
axon stats -v
axon stats -v -g
# Do the counts agree? Document any discrepancy.
```

---

### P1.5 — Record Findings (CLI-only evidence)

For each problem found, write a brief entry:

```
## Finding: <short title>

**Category**: CLI Bug | Output | Documentation | Graph Integrity | Performance

**CLI Evidence**:
- Command(s) that reveal the problem
- Actual output (paste it)
- Expected output

**Hypothesis**: (a guess — NOT confirmed yet, that's Phase 2)
```

Do not start Phase 2 until you have at least one finding written down.

---

## PHASE TWO — Deep Analysis and Fix

> **Start this phase by loading the axon skill:**
>
> ```
> skill_load({"skill": "axon"})
> ```
>
> Now you may read source files, run `axon search`, `axon context`, and use
> all other tools. The axon skill gives you richer query patterns and
> context-building commands to confirm root causes efficiently.

---

### P2.1 — Confirm Root Cause with `axon search` / `axon context`

For each Phase 1 finding, confirm the root cause before touching any code.

```bash
# Understand the relevant subsystem
axon search "what is the Storage interface"
axon search "how does scoped querying work"
axon search "what does ScopedTo do"

# Pull the actual source into context
axon context --task "trace a scoped find query end to end" --tokens 10000
axon context --task "AQL compiler edge cases and error handling" --tokens 10000
axon context --task "how does axon find the database file" --tokens 6000
```

Only after you have read the relevant source and confirmed the root cause
should you write any code.

---

### P2.2 — Produce Improvements

Each finding must become a **concrete, verifiable change**.

#### Output Format

```
## Finding: <short title>

**Category**: CLI Bug | AQL Bug | Error Handling | Documentation | Performance | Test Gap

**Evidence**:
- CLI command that reveals the problem
- Actual output vs. expected output
- Source file(s) confirmed with `axon context`

**Root Cause**: (confirmed, not guessed)

**Proposed Fix**:
- File(s) to change
- What to change (with code snippet)
- Test to add

**Verification**:
- How to confirm the fix works
```

#### Fix Workflow

1. **Write failing test first** — `go test -v -run TestXxx ./...` must fail
2. **Implement fix** — minimal change, no unrelated cleanup
3. **Run full test suite** — `go test ./...` must pass
4. **Re-run the CLI command that exposed the bug** — confirm it behaves correctly
5. **Update docs if needed** — README, grammar.md, AGENTS.md

---

### P2.3 — Validate with Re-Index

```bash
axon init --local .
axon gc
axon stats -v

# Repeat the integrity checks from P1.4
axon query "SELECT uri, COUNT(*) as c FROM nodes GROUP BY uri HAVING c > 1"
axon query "SELECT COUNT(*) FROM edges"
```

---

## Anti-Patterns to Avoid

| Anti-Pattern | Correct Approach |
|---|---|
| Reading source before finishing Phase 1 | Complete all P1 steps first |
| Loading the `axon` skill during Phase 1 | It is not permitted until Phase 2 |
| "I noticed this bug" without a CLI command | Show the command and its output |
| Changing code without a failing test | Write test → confirm fail → fix |
| Editing multiple unrelated things at once | One finding = one fix = one test |
| Assuming graph is up-to-date | Always `axon init --local .` at session start |
| Fixing symptoms | Confirm root cause with `axon context` first |
