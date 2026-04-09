---
name: axon-improve
description: >
  Self-improvement skill: use the axon CLI to explore the axon codebase itself,
  surface confusion, bugs, and gaps in CLI usability, then reason about their
  root causes and produce a structured improvement report. Read-only — no code
  is written or changed.
license: MIT
compatibility: opencode
trigger: self-improve, axon improve, improve axon, explore codebase, find bugs in axon, axon self-analysis
---

# Axon Self-Improvement Workflow

> ⚠️ **This workflow is strictly read-only.**
> No source files are modified. No tests are written. No code is changed.
> The sole output is a structured markdown report.

This skill is divided into **three strictly ordered phases**.

```
Phase 1 — CLI-only exploration     (no source reading, no axon skill)
Phase 2 — Source analysis          (load axon skill, read code, confirm causes)
Phase 3 — Report writing           (produce .agents/improve/<timestamp>-slug.md)
```

Do not skip ahead. Phase 1 forces you to find problems the way a user would.
Phase 2 confirms root causes by reading source. Phase 3 turns everything into
a durable, actionable report with reasoned fix suggestions.

---

## Output File

At the end of the session, write a single file:

```
.agents/improve/<timestamp>-<slug>.md
```

- **timestamp**: `YYYYMMDD-HHMMSS` (e.g. `20250115-143022`)
- **slug**: 2–4 word kebab-case summary of the session theme
  (e.g. `cli-help-drift`, `aql-edge-cases`, `graph-integrity`)

**Example path**: `.agents/improve/20250115-143022-cli-help-drift.md`

The file is the primary deliverable. If the session ends without writing it,
the work is lost. Write it at the end of Phase 3, not before.

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

### P1.5 — Record Raw Findings

For each problem found, write a brief entry in your working notes:

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

## PHASE TWO — Source Analysis

> **Start this phase by loading the axon skill:**
>
> ```
> skill_load({"skill": "axon"})
> ```
>
> Now you may read source files, run `axon search`, `axon context`, and use
> all other tools. The axon skill gives you richer query patterns and
> context-building commands to confirm root causes efficiently.
>
> ⚠️ You are still **read-only**. Do not edit, create, or delete any files.

---

### P2.1 — Confirm Root Cause

For each Phase 1 finding, confirm the root cause before writing the report.

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

For each finding, answer:
- Which source file(s) are involved?
- What is the exact code path that produces the problem?
- Is the hypothesis from Phase 1 correct? If not, what is the real cause?

Read the source. Reason carefully. Do not guess.

---

### P2.2 — Reason About Why

For each confirmed finding, reason about:

1. **Why does this happen?** — design decision, oversight, or refactor artifact?
2. **What is the minimal change that would fix it?** — sketch the fix in prose or pseudocode
3. **Are there related problems?** — does the same pattern appear elsewhere?
4. **What is the risk of fixing it?** — could it break other things?

This reasoning becomes the content of the report. Capture it as you go.

---

### P2.3 — Validate with Re-Index

After analysis, re-index to confirm the graph state used for findings is fresh:

```bash
axon init --local .
axon gc
axon stats -v

# Repeat the integrity checks from P1.4
axon query "SELECT uri, COUNT(*) as c FROM nodes GROUP BY uri HAVING c > 1"
axon query "SELECT COUNT(*) FROM edges"
```

---

## PHASE THREE — Report Writing

Write the output file at `.agents/improve/<timestamp>-<slug>.md`.

The file must be created by the agent using `file_write`. Do not print it to
the terminal and ask the user to save it. Write it directly.

---

### Report Structure

```markdown
# Axon Improvement Report — <YYYY-MM-DD>

**Session focus**: <one sentence describing the theme of this run>
**DB used**: `.axon/graph.db` (local) | `~/.axon/graph.db` (global)
**Phases completed**: Phase 1 ✅ | Phase 2 ✅

---

## Summary

<2–4 sentences: what was explored, how many findings, overall health>

| # | Finding | Category | Severity |
|---|---------|----------|----------|
| 1 | <title> | CLI Bug  | 🔴 High  |
| 2 | <title> | Docs     | 🟡 Low   |

---

## Finding 1: <title>

**Category**: CLI Bug | AQL Bug | Error Handling | Documentation | Performance | Graph Integrity

**Severity**: 🔴 High | 🟠 Medium | 🟡 Low | 🟢 Nice-to-have

### Evidence

- CLI command that reveals the problem:
  ```bash
  axon <command>
  ```
- Actual output:
  ```
  <paste output>
  ```
- Expected output: <describe>

### Root Cause

<Confirmed root cause from Phase 2 source reading. Name the file and
function. Quote relevant lines if helpful. Explain WHY the bug exists —
design decision, oversight, refactor artifact?>

### Suggested Fix

<Describe what should change, in prose or pseudocode. Be specific:
name the file, function, and the nature of the change. Do not write
actual code changes — describe them.>

Example:
> In `cmd/axon/find.go`, the `--output` flag default is `"table"` but
> the help text says `"json"`. Change the help text string in the
> `Flags().StringVarP(...)` call to match `"table"`.

### Related Patterns

<Does the same problem exist elsewhere? List files or commands
where a similar issue might occur.>

---

## Finding 2: <title>

... (repeat structure)

---

## Observations Without Findings

<List things that looked suspicious but turned out fine, or things
that are worth monitoring but don't rise to the level of a finding.>

---

## Recommended Next Steps

<Ordered list of the top 3–5 actions a developer should take, based
on severity and effort. Do not write "fix the code" — write what to
investigate or change and why.>
```

---

### Severity Guide

| Severity | When to use |
|---|---|
| 🔴 High | Panic, data loss, silent wrong result, crash |
| 🟠 Medium | Confusing error, misleading help text, broken example |
| 🟡 Low | Missing test coverage, doc drift, minor inconsistency |
| 🟢 Nice | Output polish, naming, minor UX improvement |

---

## Anti-Patterns to Avoid

| Anti-Pattern | Correct Approach |
|---|---|
| Reading source before finishing Phase 1 | Complete all P1 steps first |
| Loading the `axon` skill during Phase 1 | Not permitted until Phase 2 |
| "I noticed this bug" without a CLI command | Show the command and its actual output |
| Writing or editing any source file | This workflow is read-only — document the suggestion |
| Writing tests | Read-only — describe the test that should exist, do not write it |
| Printing the report to chat and not saving it | Always write `.agents/improve/<timestamp>-slug.md` with `file_write` |
| Assuming graph is up-to-date | Always `axon init --local .` at session start |
| Stating a root cause without reading the source | Confirm with `axon context` before writing the report |
| One big "everything is fine" observation | Be specific — name files, functions, line behaviours |
