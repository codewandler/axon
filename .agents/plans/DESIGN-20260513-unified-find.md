# Design: Unified `find` with Semantic Search

**Date:** 2026-05-13  
**Status:** Proposed  
**Scope:** `cmd/axon/find.go`, `cmd/axon/search.go`, `cmd/axon/main.go`

---

## Problem

Axon currently exposes three overlapping ways to find things:

| Command | Mechanism | Interface |
|---|---|---|
| `axon find` | AQL via flags | Structured (`--type`, `--name`, `--ext`, …) |
| `axon search <text>` | Regex NL → AQL | Natural language phrases |
| `axon search --semantic <text>` | Vector cosine similarity | Free-form text |

The `search` command has two unrelated modes behind one name, the regex NL parser
is fragile (fixed patterns, silently wrong on anything outside them), and users
have no intuitive way to combine semantic search with attribute filters.

---

## Goal

One `find` command that handles every discovery use-case:

```
axon find                                  # flag-only (unchanged today)
axon find "embedding and concurrency"      # semantic search (new)
axon find "error handling" --type go:func  # semantic + type filter (new)
axon find "commit message" --type vcs:commit --limit 5
axon find --type go:struct --label public  # pure flag (unchanged)
```

- A **positional string argument** triggers semantic/vector search.
- All existing flags (`--type`, `--ext`, `--label`, `--limit`, `--global`, …) apply
  as post-filters on top of the semantic results.
- No positional arg → pure flag-based AQL query (current behaviour, unchanged).
- `axon search` is removed (or kept as a deprecated alias that prints a warning).

---

## Non-goals

- Do **not** change `axon query` — raw AQL stays as-is.
- Do **not** add LLM/AI answer generation — this is retrieval only.
- Do **not** change the embedding pipeline (`axon index --embed`) or providers.
- Do **not** break existing `axon find --type … --name …` invocations.

---

## Design

### 1. Argument parsing

`find` today has no positional args (`Use: "find [flags]"`). We change it to:

```
Use: "find [query] [flags]"
```

If `len(args) == 1`, that string is the semantic query. If `len(args) == 0`,
pure flag mode as today.

### 2. Semantic path (`args[0]` present)

```
query text
    │
    ▼
resolveEmbeddingProvider()   ← same helper used by search today
    │
    ▼
provider.Embed(ctx, query)   ← single embedding call
    │
    ▼
storage.FindSimilar(ctx, embedding, limit, typeFilter)
    │                                       ▲
    │                              --type flag (optional)
    ▼
[]*graph.SimilarNode  (each has Score + *graph.Node)
    │
    ▼
apply remaining flag filters in-process:
  --ext     → node.Data["ext"]
  --label   → node.Labels
  --name    → node.Name (GLOB)
  --data    → node.Data fields
  --global  → already handled: FindSimilar searches full graph;
              local scoping uses the cwd node as a pre-filter
    │
    ▼
render with score prefix:  0.642  buildNodeText(node) …
```

`FindSimilar` already accepts a `*graph.NodeFilter` with a `Type` field — we pass
`--type` straight through so the vector scan is type-scoped at the DB level
(efficient). The remaining filters (ext, label, data) are applied in-process on
the returned slice since `FindSimilar` doesn't support them natively; the result
set from vector search is small (default limit 20) so this is fine.

#### Scoped semantic search (`--global` absent)

When searching locally, resolve the cwd node and pass its ID into a pre-filter.
`FindSimilar` will need a `ScopeNodeID` field added to `graph.NodeFilter` (see
§4) so the SQL can add an EXISTS clause, or we post-filter the results by URI
prefix. Post-filtering on URI prefix is simpler and safe given small result sets.

### 3. Flag-only path (no positional arg)

Zero changes. All existing behaviour is preserved. Existing scripts and docs
continue to work.

### 4. `graph.NodeFilter` extension

Add one optional field:

```go
type NodeFilter struct {
    // … existing fields …
    Type       string
    Generation string
    // new:
    ScopeURI   string // URI prefix; when set, only nodes whose URI has this prefix
}
```

`FindSimilar` in the SQLite adapter joins on this prefix when set:

```sql
WHERE n.uri LIKE ? || '%'
```

This replaces the in-process URI-prefix post-filter and keeps scoping consistent
with flag-only mode.

### 5. Output format

Semantic results get a score prefix; flag-only results stay the same.

```
# semantic mode
0.642  [a3f1] commit: fix logo background clipping…    (vcs:commit)
0.598  [b22c] func: buildNodeText                      (go:func)

# flag-only mode (unchanged)
[a3f1] path/to/file.go  (fs:file)
```

The `--output` flag (`path`, `uri`, `json`, `table`) still applies:
- `table` adds a `Score` column in semantic mode.
- `json` adds a `"score"` field per result in semantic mode.

### 6. Error handling when embeddings are missing

If no embedding provider is resolvable:

```
Error: no embedding provider available.
Run 'axon index --embed .' to generate embeddings, then retry.
```

Do not silently fall back to keyword search — that would make the behaviour
surprising and hard to reason about.

### 7. Retire `axon search`

- `search.go` is deleted.
- `axon search` is registered as a hidden cobra alias of `find` that prints a
  one-line deprecation notice before delegating:

```
Note: 'axon search' is deprecated, use 'axon find' instead.
```

  The alias accepts the same positional arg so existing invocations keep working
  for one release cycle.

---

## Affected files

| File | Change |
|---|---|
| `cmd/axon/find.go` | Add positional arg; add semantic path; update help text |
| `cmd/axon/search.go` | Delete; register deprecated alias in `main.go` |
| `cmd/axon/main.go` | Wire deprecated alias |
| `graph/graph.go` | Add `ScopeURI` to `NodeFilter` |
| `adapters/sqlite/sqlite.go` | Honour `ScopeURI` in `FindSimilar` |
| `cmd/axon/results.go` | Add score-annotated output helpers |
| `README.md` | Update `find` docs, remove `search` docs |
| `AGENTS.md` | Update CLI command table |

---

## Acceptance criteria

1. `axon find "logo and visual assets"` returns top semantic matches with scores.
2. `axon find "error handling" --type go:func` returns only `go:func` matches.
3. `axon find "commit" --type vcs:commit --limit 5` returns top 5 commits.
4. `axon find --type go:struct` (no text arg) still works exactly as today.
5. `axon search "anything"` prints deprecation notice then behaves like `axon find "anything"`.
6. Missing embedding provider prints a clear error, no silent fallback.
7. `--output json` includes `"score"` field when semantic mode is active.
8. All existing `find` flag combinations pass their tests unchanged.
