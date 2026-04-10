# Changelog

All notable changes to this project will be documented in this file.

The format follows [Keep a Changelog](https://keepachangelog.com/en/1.1.0/).
This project uses [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

---

## [Unreleased]

### Added

- **`axon index` command** — replaces the separate `init` and `watch` commands
  with a single `index [path]` command. `--watch`, `--watch-debounce`, and
  `--watch-quiet` flags added. `init` is kept as an alias for backward
  compatibility. (`cmd/axon/index.go`)

- **`Axon.DeleteByPath()`** — removes a filesystem path (or entire subtree for
  directories) from the graph and cleans up orphaned edges. Used internally by
  the watch loop on `Remove`/`Rename` events. (`axon.go`)

- **`FSInclude` config field** — allow-list of glob patterns; when non-empty
  only matching files are indexed (directories are always traversed). (`axon.go`)

- **`Registry.ByName()`** — looks up a registered indexer by its `Name()`
  string. (`indexer/registry.go`)

### Changed

- **Per-file targeted watch re-indexing** — the watch loop now re-indexes or
  deletes each changed path individually instead of computing the deepest
  common ancestor of all pending paths. Deletions are handled without calling
  `OnReindex`; the watcher also consults the FS indexer's exclusion rules so
  ignored paths never trigger re-indexes. (`axon.go`)

- **Dotfiles no longer blanket-excluded** — the FS indexer no longer skips all
  hidden files/directories. `DefaultFSIgnore` is updated with specific entries
  (`.git`, `.devspace`, `.DS_Store`, `*.log`) so that useful dotfiles such as
  `.agents/` and `.claude/` remain visible in the graph. (`axon.go`,
  `indexer/fs/indexer.go`)

- **`FSExclude` replaces `FSIgnore`** in `axon.Config`; `FSIgnore` kept as a
  deprecated alias that is merged into `FSExclude` at construction time.
  `fs.Config` gains matching `Include`/`Exclude` fields and exports
  `ShouldIgnore()`. (`axon.go`, `indexer/fs/indexer.go`)

### Fixed

- **`context.Canceled` on Ctrl+C in `--watch` mode** no longer causes cobra to
  print an error message and the full usage block. (`cmd/axon/index.go`)

---

### Added

- **Git commit indexing** — the git indexer now walks the commit log (default
  cap: 500 per repo) and creates `vcs:commit` nodes with full metadata: SHA,
  subject, body, author, committer, date, and change stats (files changed,
  insertions, deletions). Enables queries like "recent commits by author" and
  "largest commits by diff size".
  (`indexer/git/indexer.go`, `types/vcs.go`)

- **`parent_of` edges** — each `vcs:commit` node emits a `parent_of` edge to
  each of its parent commits, forming the commit DAG in the graph. Variable-
  length path queries (`[:parent_of*1..N]`) traverse the ancestry chain.
  (`types/edges.go`, `indexer/git/indexer.go`)

- **`modifies` edges** — for single-parent commits, a `modifies` edge is
  emitted from the commit to each `fs:file` node it touched, enabling
  "hotspot" queries (most frequently modified files).
  (`types/edges.go`, `indexer/git/indexer.go`)

- **Branch/tag → commit `references` edges** — `vcs:branch` and `vcs:tag`
  nodes now emit a `references` edge to their tip `vcs:commit` node, making
  it possible to query "what commit does this branch/tag point to?".
  (`indexer/git/indexer.go`)

- **`git.Config` with `MaxCommits`** — the git indexer accepts a `Config`
  struct with a `MaxCommits` field (default 500) to cap commit ingestion per
  repo. Exposed via `axon.Config.GitConfig`.
  (`indexer/git/indexer.go`, `axon.go`)

- **AQL constants** — `aql.NodeType.Commit`, `aql.Edge.ParentOf`, and
  `aql.Edge.Modifies` added to the type-safe fluent builder.
  (`aql/nodetypes.go`, `aql/edgetypes.go`)

### Fixed

- **SQLite SQLITE_BUSY errors eliminated** — PRAGMAs (including `busy_timeout=30s`)
  are now set per-connection via DSN `_pragma` parameters instead of a one-shot
  `ExecContext`. The `database/sql` connection pool could previously hand out
  connections with default `busy_timeout=0`, causing immediate SQLITE_BUSY on any
  lock contention. Retry backoff improved to 5 attempts with exponential delays.

### Added

- **All hidden files/dirs excluded from indexing** — entries starting with `.`
  are now unconditionally skipped by the FS indexer (hidden directories still get
  a minimal node for deletion detection). The watcher also skips hidden dirs and
  filters hidden-path events, saving inotify watches and avoiding wasted re-indexes.
- **`axon watch --embed`** — generates embeddings on each re-index, scoped to
  only the nodes written in that run (via a new `Generation` filter on
  `NodeFilter`), so incremental re-indexes don't re-embed the entire corpus.

### Changed

- `DefaultFSIgnore` cleaned up — dot-prefixed entries (`.git`, `.axon`, `.idea`,
  `.vscode`, `.DS_Store`, `.venv`, `.virtualenv`, `.tox`, `.pytest_cache`,
  `.mypy_cache`) removed since the blanket hidden-file rule now covers them.

## [0.5.0] — 2026-04-10

### Added

- **Import graph edges** — Go packages now emit `imports` edges between
  `go:package` nodes for every intra-module import. `PackageData` gains an
  `import_paths` field listing direct intra-module import paths.
  (`indexer/golang/indexer.go`, `types/golang.go`)

- **Interface implementation edges** — the Go indexer now uses `go/types` to
  detect which structs satisfy which interfaces (value or pointer receiver) and
  emits `implements` edges (`go:struct → go:interface`). New `EdgeImplements`
  constant added to `types/edges.go`.
  (`indexer/golang/indexer.go`, `types/edges.go`, `types/golang.go`)

- **Test-to-source linkage** — test packages (`_test` suffix) are now indexed
  with `is_test: true` and a `tests` edge pointing to their source package.
  `PackageData` gains `is_test` and `test_for` fields.
  (`indexer/golang/indexer.go`, `types/edges.go`, `types/golang.go`)

- **`axon watch` command** — watches a directory with `fsnotify`, debounces
  file events, and re-indexes the affected subtree on each batch of changes.
  Flags: `--debounce` (default 150 ms), `--quiet`. `Axon.Watch()` and
  `WatchOptions` added to the library API.
  (`axon.go`, `cmd/axon/watch.go`)

- **Embedding storage** — `graph.Storage` now includes an `EmbeddingStore`
  interface (`PutEmbedding`, `GetEmbedding`, `FindSimilar`). The SQLite adapter
  implements it via an `embeddings` table with little-endian float32 blobs and
  in-process cosine similarity. `NodeWithScore` type added to `graph/`.
  (`graph/storage.go`, `adapters/sqlite/sqlite.go`)

- **`indexer/embeddings` package** — `Provider` interface with `NullProvider`
  (testing) and `OllamaProvider` (`nomic-embed-text`) implementations. A
  `PostIndexer` generates embeddings for `go:func`, `go:struct`,
  `go:interface`, and `md:section` nodes after each index run.
  (`indexer/embeddings/`)

- **`axon init --embed`** — new flag to activate the embedding PostIndexer
  during indexing. Provider is configured via `AXON_EMBED_PROVIDER`,
  `AXON_OLLAMA_URL`, and `AXON_OLLAMA_MODEL` env vars.
  (`cmd/axon/init.go`, `axon.go`)

- **`axon search --semantic`** — new flag to run vector similarity search
  against stored embeddings. Supports `--type` filter and `--limit`.
  (`cmd/axon/search.go`)

- **`axon impact` command** — blast-radius analysis: finds direct `go:ref`
  references to a named symbol, groups them by package, and reports which
  packages import those packages via the import graph.
  (`cmd/axon/impact.go`)

- **`axon tree` accepts a node ID prefix** — in addition to filesystem
  paths, `axon tree` now resolves a short node ID prefix (≥ 4 chars,
  no path separators) against the graph and roots the tree at the
  matching node. (`cmd/axon/tree.go`)

### Changed

- **`data.ext` stored without leading dot** — `data.ext` was stored as
  `".go"` (matching `filepath.Ext()` output) but every query example and
  user expectation used `'go'` without the dot. The fs indexer now strips
  the leading dot before storing, so `data.ext = 'go'` and
  `axon find --ext go` work consistently. Existing databases need
  re-indexing. (`indexer/fs/indexer.go`, `types/fs.go`, `cmd/axon/find.go`,
  `cmd/axon/query.go`, `cmd/axontui/preview.go`, `cmd/axontui/querybar.go`)

- **Removed dead code identified by staticcheck** — unused functions
  (`formatTimestamp`, `fileExtension`, `edgeArrow`, `padRight`,
  `compileCTEVariableLength`), unused struct fields (`skipIndex`,
  `contentStart`, `contentEnd`), unused variables (`pomNameRegex`,
  `pomVersionRegex`), and unused style declarations removed across
  multiple packages.

- **`strings.Title` replaced with an ASCII-safe `titleCase` helper** —
  the deprecated `strings.Title` call used for display-header formatting
  has been replaced with a local helper that capitalises ASCII words
  without importing `golang.org/x/text`. (`cmd/axon/query.go`)

### Fixed

- **`SELECT COUNT(*)` in pattern queries no longer requires `GROUP BY`** —
  the guard that blocked scalar counts in pattern queries (`hasCount &&
  len(GroupBy) > 0`) has been removed. Pattern queries such as
  `SELECT COUNT(*) FROM (a)-[:contains]->(b)` now return a single scalar
  count. (`adapters/sqlite/aql.go`)

- **Star projection in multi-variable pattern queries** — `SELECT *` in a
  pattern with more than one variable now correctly namespaces each column
  as `varName.field` (e.g. `repo.id`, `branch.name`) instead of emitting
  duplicate bare column names. (`adapters/sqlite/aql.go`)

- **Single-variable `SELECT var` pattern queries** — a whole-variable
  selector (e.g. `SELECT file`) no longer triggers projection mode; nodes
  are now returned as full objects. (`adapters/sqlite/aql.go`)

- **Scalar count display** — `SELECT COUNT(*)` without `GROUP BY` now
  renders as a plain number in both table and JSON output instead of being
  attributed to a spurious `_count` key. (`cmd/axon/query.go`,
  `adapters/sqlite/aql.go`)

- **Stats output labels node/edge counts as scoped or global** — the
  `axon stats` display now annotates node and edge counts with
  `(scoped to CWD)` or `(global)` depending on the `--global` flag.
  (`cmd/axon/stats.go`)

---

## [0.4.0] — 2026-04-10

### Changed

- **Default DB lookup is now CWD-local; `--global` replaces `--local`** — the
  old default walked up the directory tree and fell back to `~/.axon`;
  this was surprising in nested projects. The new default resolves the
  database strictly from `<CWD>/.axon`, returning an error if it is absent.
  Pass `--global` to restore the walk-up behaviour (walk up, then fall back
  to `~/.axon`). The old `--local` flag is removed. `axon init` and
  `axon tree` now base resolution on the current working directory rather
  than the path argument. Error messages updated to reflect the new
  behaviour. (`cmd/axon/main.go`, `cmd/axon/db.go`, `cmd/axon/init.go`,
  `cmd/axon/tree.go`)

### Added

- Unit tests for all `resolveDB` branches (default local, `--global`
  walk-up, explicit `--db-dir`, and `forWrite` creation).
  (`cmd/axon/db_test.go`)
- Release & Tagging Workflow documented in `AGENTS.md` — covers version
  selection, CHANGELOG hygiene, commit conventions, and the rule that a
  tag must never land on an `[Unreleased]` entry. (`AGENTS.md`)

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

- **`data.ext` field now stored without leading dot** —
  `data.ext` was stored as `".go"` (with dot, matching `filepath.Ext()`
  output) but every query example and user expectation used `'go'`
  (without dot). The fs indexer now strips the leading dot before storing,
  so `data.ext = 'go'` and `axon find --ext go` work consistently. The
  `codeExtensions` map and TUI querybar filter updated accordingly.
  The previous CHANGELOG entry recording `data.ext = '.go'` as correct
  is superseded by this fix.
  (`indexer/fs/indexer.go`, `types/fs.go`, `cmd/axon/find.go`,
  `cmd/axon/query.go`, `cmd/axontui/preview.go`, `cmd/axontui/querybar.go`)

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
