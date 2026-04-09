# Changelog

All notable changes to this project will be documented in this file.

The format follows [Keep a Changelog](https://keepachangelog.com/en/1.1.0/).
This project uses [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

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
