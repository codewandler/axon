# DESIGN: Semantic Commit Parser (#8)

## Problem

The git indexer stores raw commit messages as opaque strings:
`CommitData.Message` holds the first line and `CommitData.Body` holds the rest.
This makes structured AQL queries impossible — there is no way to ask "all
breaking changes" or "fix commits in the `aql` scope" because the type,
scope, refs, and other semantic fields are embedded in free-form text.

## Proposed Solution

1. Add a small `commitparser` sub-package at
   `indexer/git/commitparser/` that parses a raw commit message string
   into structured fields following the
   [Conventional Commits v1.0](https://www.conventionalcommits.org/)
   specification with lenient fallback for non-conforming messages.

2. Extend `CommitData` in `types/vcs.go` with six new JSON fields:

   | Field        | JSON tag       | Type                | Notes                                 |
   |--------------|----------------|---------------------|---------------------------------------|
   | `CommitType` | `commit_type`  | `string`            | e.g. `feat`, `fix`, `""` if not CC   |
   | `Scope`      | `scope`        | `string`            | e.g. `aql`, `cli`                     |
   | `Breaking`   | `breaking`     | `bool`              | `!` suffix or `BREAKING CHANGE` footer|
   | `Subject`    | `subject`      | `string`            | description after type/scope prefix   |
   | `Footers`    | `footers`      | `map[string][]string` | all git trailer key→values          |
   | `Refs`       | `refs`         | `[]string`          | deduplicated values from `Refs:` key  |

   The existing `Message` field (raw first line) is **retained** for
   backward compatibility with already-indexed nodes and JSON-deserialized
   data from SQLite.

3. Wire the parser into `indexer/git/indexer.go` in `indexCommits`.

## Architecture

```
indexer/git/
├── commitparser/
│   ├── parser.go        ← new: pure parse logic, zero axon imports
│   └── parser_test.go   ← new: 20+ table-driven test cases
├── indexer.go           ← updated: call ParseCommitMessage in indexCommits
└── indexer_test.go      ← updated: add test with conventional commit message

types/
└── vcs.go               ← updated: 6 new fields on CommitData
```

The `commitparser` package has **no imports from axon internals**, making
it trivially unit-testable and extractable in the future.

## Parser Specification

### First-line parsing

The header regex: `^(\w+)(\(([^)]+)\))?(!)?: (.+)$`

- Group 1 → `CommitType`
- Group 3 → `Scope`
- Group 4 → `Breaking = true` if `!` present
- Group 5 → `Subject`

**Lenient fallback**: if the first line does not match, `CommitType = ""`,
`Scope = ""`, `Breaking = false`, `Subject = full first line`.

### Body and footer parsing

After the blank line that separates the first line from the rest, the body
is split from the footer block. A footer block is one or more consecutive
lines matching the git trailer pattern:

```
Token: value
Token #value
```

The parser works backward from the end of the body, collecting contiguous
trailer lines. Everything before that block is the free-form `Body`.

**`BREAKING CHANGE`** in a footer sets `Breaking = true`.

### Refs extraction

- Source: `Footers["Refs"]`
- Split each value on `,`, whitespace runs, and `#`
- Trim leading `#` from each token
- Deduplicate, preserving insertion order

## Key Decisions

| Decision | Rationale |
|----------|-----------|
| Lenient parser (no error on non-CC messages) | The issue explicitly requires it; most repos have mixed commit styles |
| `CommitType` not `Type` | Avoids shadowing the ubiquitous `graph.Node.Type` concept in calling code |
| Keep `Message` field | Backward compat; already-indexed nodes in SQLite have this field serialised |
| `Subject` is separate from `Message` | `Message` = raw first line; `Subject` = cleaned description (no type/scope prefix). For non-CC commits they are identical |
| Footer map values are `[]string` | The same trailer key can appear multiple times (e.g. multiple `Co-authored-by`) |
| `commitparser` has zero axon imports | Keeps the package independently testable and future-extractable |

## Out of Scope

- Re-indexing existing commits (new fields will be empty on already-indexed nodes; a fresh `axon index` will populate them)
- Multi-paragraph body handling beyond the simple footer/body split
- CC validation — we are a lenient consumer, not a strict linter

## Example AQL Use Cases (enabled by this feature)

```sql
-- All breaking changes
SELECT * FROM nodes WHERE type = 'vcs:commit' AND data.breaking = true

-- Commits referencing a ticket
SELECT * FROM nodes WHERE type = 'vcs:commit' AND data.refs CONTAINS 'DEV-399'

-- Count commits by type
SELECT data.commit_type, COUNT(*) FROM nodes WHERE type = 'vcs:commit' GROUP BY data.commit_type

-- All fix commits in the aql scope
SELECT * FROM nodes WHERE type = 'vcs:commit' AND data.commit_type = 'fix' AND data.scope = 'aql'
```

## Files Changed

| File | Type | What changes |
|------|------|--------------|
| `indexer/git/commitparser/parser.go` | New | Parser package |
| `indexer/git/commitparser/parser_test.go` | New | 20+ unit tests |
| `types/vcs.go` | Modified | 6 new fields on `CommitData` |
| `indexer/git/indexer.go` | Modified | Call parser, populate new fields |
| `indexer/git/indexer_test.go` | Modified | Integration test with CC message |
