# DESIGN: ExcludeTypes filter for NodeFilter (Issue #4)

**Date**: 2026-04-10
**Status**: APPROVED
**Refs**: https://github.com/codewandler/axon/issues/4

---

## Problem

Semantic search results are crowded by `vcs:commit` nodes (and potentially
other low-signal types) because there is no way to exclude a type from results.
With 10–20 result slots, every slot matters. The caller has no opt-out.

Note: the companion issue (richer commit display) was shipped in v0.10.1, so
commits now show subject/author/date. The exclusion filter is still valuable
as an independent control — callers may want to exclude commits, or dirs, or
any other type regardless of how rich their display is.

---

## Proposed Solution

Add `ExcludeTypes []string` to `graph.NodeFilter`. When non-empty, any node
whose `Type` matches any element of the slice is excluded from results.
Semantics: OR logic (exclude if type == any of the listed values).

This is the minimal, composable solution. It slots into the existing filter
plumbing at every layer rather than requiring a new parameter at each
call-site.

---

## Architecture

### Layers changed

| Layer | Change | Why |
|---|---|---|
| `graph.NodeFilter` | New field `ExcludeTypes []string` | Single source of truth for all filter criteria |
| `adapters/sqlite` `buildNodeFilterArgs` | `AND type NOT IN (?,?,...)` | Efficient SQL-level exclusion for `FindNodes`, `CountNodes` |
| `adapters/sqlite` `nodeMatchesFilter` | Early-return loop over `ExcludeTypes` | In-memory exclusion for `FindSimilar` post-filter |
| `cmd/axon/find.go` | `--exclude-type` flag (repeatable) | CLI access for both AQL and semantic paths |

### Not changed

- `axon.SemanticSearch` / `axon.Search` — already accept `*graph.NodeFilter`;
  callers set `Filter.ExcludeTypes` directly. No new parameter needed.
- `SearchOptions` — already has `Filter *graph.NodeFilter`; no new field needed.
- AQL compiler — not touched; exclusion is expressed as `NOT(OR(...))` using
  existing `aql.Not` / `aql.Or` / `aql.Type.Eq` combinators.

### Key decisions

**`ExcludeTypes` on `NodeFilter`, not a separate parameter**
Adding a separate `excludeTypes []string` argument to `FindSimilar`,
`SemanticSearch`, etc. would balloon every call-site. `NodeFilter` is already
the unified filter struct — it's the right home.

**SQL `NOT IN` over multiple `type != ?` ANDs**
`NOT IN (...)` is more readable, equivalent in correctness, and leaves the
query planner free to optimise. The existing `Labels`, `Extensions`, and
`NodeIDs` fields already use the same multi-placeholder pattern.

**`ExcludeTypes` wins over `Type` when they conflict**
If a caller sets both `Type: "fs:file"` and `ExcludeTypes: ["fs:file"]`, zero
results is the correct outcome. The SQL `AND type = ? AND type NOT IN (?)`
produces this naturally; no special-casing needed.

**No default exclusions**
The issue floated excluding `vcs:commit` by default. Rejected: defaults that
hide data are surprising. Callers opt in explicitly.

---

## Files Changed

| File | Change |
|---|---|
| `graph/storage.go` | Add `ExcludeTypes []string` to `NodeFilter` with doc comment |
| `adapters/sqlite/sqlite.go` | `buildNodeFilterArgs`: add `NOT IN` clause; `nodeMatchesFilter`: add exclusion loop |
| `adapters/sqlite/sqlite_test.go` | `TestFindNodes_ExcludeTypes`, `TestFindSimilar_ExcludeTypes` |
| `cmd/axon/find.go` | `--exclude-type` flag; wire into AQL conditions and semantic `NodeFilter` |

---

## Acceptance Criteria

- [ ] `FindNodes` with `ExcludeTypes: ["vcs:commit"]` returns no commit nodes
- [ ] `FindNodes` with multiple `ExcludeTypes` excludes all listed types
- [ ] `FindSimilar` with `ExcludeTypes` excludes the type from similarity results
- [ ] `ExcludeTypes` + `Type` conflict → zero results (no panic, no silent pass)
- [ ] `axon find --exclude-type vcs:commit` suppresses commits in regular output
- [ ] `axon find "query" --exclude-type vcs:commit` suppresses commits in semantic output
- [ ] `--exclude-type` is repeatable (`--exclude-type vcs:commit --exclude-type fs:dir`)
- [ ] All existing tests pass; `go test -race ./...` clean
