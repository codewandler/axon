# Changelog

All notable changes to this project will be documented in this file.

The format follows [Keep a Changelog](https://keepachangelog.com/en/1.1.0/).
This project uses [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

---

## [0.3.2] — 2026-04-10

### Fixed

- **`SELECT data.*` returned `null` for integer and boolean fields** — the
  `scanNodePartial` scanner only accepted `string`-typed values from
  `json_extract`, silently discarding `int64` and `bool` results (e.g.
  `data.size`, `data.mode`). The type assertion is removed; native SQLite
  types now pass through unchanged. (`adapters/sqlite/aql.go`)

- **`SELECT file.name` in single-variable pattern queries rendered as `null`**
  — `nodeFieldRaw` and `nodeFieldValue` did not recognise `"var.field"`
  selectors. Both now strip the leading variable prefix (`"file.name"` →
  `"name"`, `"file.data.ext"` → `"data.ext"`) before looking up the value.
  Regression tests added for both functions. (`cmd/axon/query.go`,
  `cmd/axon/query_test.go`)

- **`axon find --output json` produced 0 bytes on empty results** — the
  early-return when no nodes matched fired before the output-format switch.
  `--output json` now emits `[]`; `--output table` calls `outputTable`;
  path and uri outputs remain silently empty. (`cmd/axon/find.go`)

- **`axon query --output json` emitted `null` instead of `[]` on empty
  results** — `json.Encode(nil)` produces `"null"`. All result-type branches
  in `printQueryResultJSON` now fall back to typed empty slices when the
  result slice is nil. (`cmd/axon/query.go`)

- **`axon query --help` example `data.ext = 'go'` returned 0 rows** —
  extensions are stored with a leading dot (`.go`), so the correct literal
  is `data.ext = '.go'`. (`cmd/axon/query.go`)

- **`GROUP BY` queries used hardcoded `"key"` as the JSON field name** —
  `SELECT type, COUNT(*) … GROUP BY type` produced `{"key": "fs:file", …}`
  instead of `{"type": "fs:file", …}`. A new `GroupingColumn` field on
  `QueryResult` is populated from the `GROUP BY` selector; both JSON and
  table renderers use it. (`graph/storage.go`, `adapters/sqlite/aql.go`,
  `cmd/axon/query.go`)

- **`flushBatch` swallowed `SQLITE_BUSY` errors silently and always returned
  success** — the batch writer now retries up to 3 times with 50 ms / 100 ms
  exponential backoff on `SQLITE_BUSY`. Permanent failures are propagated to
  `Flush()` callers via a mutex-protected `lastFlushErr` field, allowing
  commands like `axon init` to exit non-zero when writes fail.
  (`adapters/sqlite/sqlite.go`)

---

## [0.3.1] — 2026-04-10

### Added

- **`Makefile`** with `build`, `install`, `test`, `lint`, and `clean` targets.
  `make install` and `make build` stamp the version via
  `-ldflags "-X main.version=$(git describe --tags --always --dirty)"`
  so `axon --version` always reports the correct release tag.

---

## [0.3.0] — 2026-04-10

### Added

- **Multi-variable pattern SELECT** (`SELECT a, b FROM pattern`) now returns
  both nodes per match instead of silently discarding all but the last variable.
  A new `ResultTypeRows` result type is introduced; the compiler generates
  aliased SQL columns (`n0.id AS "a.id"`, `n1.name AS "b.name"`) to avoid
  name collisions, and a new `executeRowsQuery` scanner populates
  `QueryResult.Rows []map[string]any`. Field-level cross-variable selectors
  (`SELECT a.name, b.name`) are also fixed by the same aliasing. CLI table
  and JSON output both handle the new result type. (`adapters/sqlite/aql.go`,
  `graph/storage.go`, `cmd/axon/query.go`)

- **`ResultTypeRows` added to `graph.QueryResult`** — `Rows []map[string]any`
  field for multi-variable pattern query results. (`graph/storage.go`)

- Regression tests: `TestQuery_MultiVariablePatternSelect` (3 sub-cases:
  whole-node SELECT, reversed SELECT, and field-selector SELECT). (`adapters/sqlite/aql_test.go`)

### Fixed

- **`go:ref` nodes had malformed URIs containing a double-slash** —
  `pos.Filename` is an absolute path; appending it to `moduleURI + "/ref/"`
  produced `go+file:///…/ref//abs/path`. The fix uses
  `pkg.Module.Dir` (already in scope) to make the filename module-relative
  before building the URI. Affects all 21 k+ `go:ref` nodes; correct URIs
  are regenerated on the next `axon init`. (`indexer/golang/indexer.go`)

- **`axon search` and `axon context` displayed wrong file paths** — both
  `shortenPath` implementations used hardcoded heuristics (`"/axon/"` string
  search; `/src/`, `/pkg/`, `/cmd/`, `/internal/` markers) that produced
  incorrect paths for any project not named `axon` or with files outside
  the marker directories. Both are now replaced with `filepath.Rel(cwd, path)`,
  falling back to last-3-components only when the path is outside CWD.
  (`cmd/axon/search.go`, `context/format.go`)

- **`axon context` showed "0 files, 0 tokens" with no explanation when
  symbols were not found** — e.g. `--task "how does AQL parsing work"` extracts
  `"AQL"` as a symbol but no Go node is named `"AQL"`. A hint is now prepended
  to the output when `Walk()` returns zero items but symbols were extracted,
  listing the searched symbols and suggesting alternatives.
  (`context/context.go`)

---

## [0.2.0] — 2026-04-10

### Added

- **AQL `IN (SELECT ...)` subquery support** — `IN` and `NOT IN` now accept a
  full `SELECT` statement in place of a literal value list, enabling queries like
  `SELECT id FROM nodes WHERE id NOT IN (SELECT from_id FROM edges)`. The parser,
  AST, and SQL compiler all support this; the grammar document is updated
  accordingly. (`aql/parser.go`, `aql/ast.go`, `adapters/sqlite/aql.go`,
  `aql/grammar.md`)

- **`SelectedColumns` on `QueryResult`** — partial `SELECT` queries (e.g.
  `SELECT name, type FROM nodes`) now populate `QueryResult.SelectedColumns` with
  the column names in their original order. CLI rendering uses this to display
  table columns in the exact order written in the query and to project only the
  selected fields in JSON output. (`graph/storage.go`, `adapters/sqlite/aql.go`,
  `cmd/axon/query.go`)

- **`graph.CountItem` type and `QueryResult.Counts []CountItem`** — aggregated
  count results are now an ordered slice instead of a `map[string]int`, preserving
  the `ORDER BY` that SQLite already applies. `graph.CountItem` is the canonical
  type; `cmd/axon` exposes it as a type alias. A `FromSlice` method is added to
  `CountResult`. (`graph/count.go`, `graph/storage.go`, `cmd/axon/results.go`)

- Regression tests: `TestParser_InSubquery` (AQL parser) and
  `TestQuery_WhereNotInSubquery` (SQLite integration). (`aql/parser_test.go`,
  `adapters/sqlite/aql_test.go`)

### Fixed

- **`axon query` GROUP BY output had non-deterministic row order** — counts were
  collected into a Go `map`, discarding the `ORDER BY COUNT(*) DESC` order that
  SQLite returned. With `Counts` now a slice, sort order is preserved end-to-end
  in both table and JSON output. (`adapters/sqlite/aql.go`)

- **`axon query --output json` serialised aggregate results as a JSON object**
  (key→value map) instead of an ordered array. Output is now
  `[{"key": "...", "count": N}, ...]`, preserving sort order and matching the
  table output. (`cmd/axon/query.go`)

- **`axon query --output json` included zero-value timestamp fields for partial
  `SELECT` queries** — selecting `name, type` would still emit
  `"created_at": "0001-01-01T00:00:00Z"` in the JSON. JSON output for node
  queries now projects only the selected columns. (`cmd/axon/query.go`)

- **`axon query` table output ignored `SELECT` column order** — the table
  renderer auto-detected populated fields rather than respecting the order
  written in the query. Columns are now rendered in `SELECT` order when
  `SelectedColumns` is set. (`cmd/axon/query.go`)

- **Go indexer created ~14,500 orphaned reference edges per run when
  `ExportedOnly` was set** — `indexReferences` emitted edges to unexported
  symbols even though no node was created for them. The fix skips any
  reference whose target symbol is unexported. (`indexer/golang/indexer.go`)

- **Orphaned edges were only GC'd when nodes were deleted** — the
  `NodesDeleted() > 0` guard meant orphaned reference edges accumulated silently
  across re-index runs. GC now always runs unless `SkipGC` is set; the SQL
  `DELETE` is fast enough that the optimisation was not worth its cost. (`axon.go`)

- **`time.Time` zero values serialised as `"0001-01-01T00:00:00Z"` in JSON** —
  `Node.CreatedAt`, `Node.UpdatedAt`, and `Edge.CreatedAt` are now pointer fields
  (`*time.Time`) so they marshal as `null`/omitted when unpopulated. All storage
  scan and write paths updated. (`graph/node.go`, `graph/edge.go`,
  `adapters/sqlite/sqlite.go`, `adapters/sqlite/aql.go`)

- **`axon info` printed `"Orphaned:"` instead of `"Orphaned edges:"`** —
  cosmetic label fix. (`cmd/axon/info.go`)

- **`axon find` help examples for `vcs:*` types were missing `--global`** —
  VCS nodes use `git+file://` URIs and are not reachable in scoped mode;
  the examples now include `--global` with an explanatory comment.
  (`cmd/axon/find.go`)

---

## [0.1.0] — 2025-07-17

First tagged release. Captures the state of the project after 54 commits
building out the core graph engine, AQL query language, CLI, and multiple
indexers (filesystem, git, Go, markdown, project).

### Fixed

- **`axon find --type "md:*"` returned empty without `--global`** — the first
  example in `axon find --help` was silently broken. Scoped queries (the
  default, without `--global`) now traverse both `contains` *and* `has` edges
  when determining which nodes fall within the current directory's scope.
  Previously only `contains` edges were followed, which excluded every
  non-filesystem node type (`md:document`, `md:section`, `md:codeblock`,
  `go:struct`, etc.) even when those nodes belonged to files inside the
  directory. The generated AQL pattern changes from `[:contains*0..]` to
  `[:contains|has*0..]`. (`aql/scope.go`)

- **`axon stats -v` (scoped) omitted markdown and Go node types** — a
  consequence of the same root cause above. After the fix, a scoped
  `axon stats -v` correctly reports `md:document`, `md:section`,
  `md:codeblock`, and any other `has`-owned node types within the directory.

### Added

- Regression test `TestQuery_ScopedTo_FollowsHasEdges` covering the scoped
  traversal fix. (`adapters/sqlite/aql_test.go`)

---

*Earlier history is available via `git log`.*
