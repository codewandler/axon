# DESIGN: Extend Git Indexer — Commits, Tags, Branches

**Date**: 2026-04-10
**Status**: DRAFT — awaiting approval

---

## Problem Statement

The git indexer currently creates nodes for `vcs:repo`, `vcs:branch`, `vcs:tag`,
and `vcs:remote`, but stores only their names and the hash they point to as flat
strings in the data payload. Commits themselves are not represented as nodes.

This means we cannot:
- Query commit history (author, date, message, changed files)
- Traverse the commit graph (parent → child relationships)
- Link branches/tags to their actual commit nodes
- Answer "who last touched this file?" or "what changed in the last week?"
- Correlate code changes with specific commits

## Scope

### In Scope

1. **`vcs:commit` nodes** — represent individual git commits with author, date,
   message, and stats
2. **Commit-to-commit edges** — `parent_of` edges forming the DAG
3. **Branch/tag → commit edges** — `references` edges linking refs to their
   tip commits
4. **Commit → file edges** — `modifies` edges linking commits to the files
   they changed (with change type: add/modify/delete)
5. **Configurable depth limit** — cap how many commits to ingest (default: 500)
6. **Enriched branch/tag data** — add `ahead`/`behind` counts where useful

### Out of Scope

- Blame/line-level attribution (too granular for a graph index)
- Diff content (too large to store, not useful for graph queries)
- Remote branch tracking (only local branches/tags)
- Merge conflict detection

---

## Architecture

### New Node Type

```
vcs:commit
```

**URI**: `git+file:///path/to/repo/commit/<sha>`

**CommitData**:
```go
type CommitData struct {
    SHA        string    `json:"sha"`
    Message    string    `json:"message"`      // First line only (subject)
    Body       string    `json:"body"`         // Full body after subject (may be empty)
    AuthorName  string   `json:"author_name"`
    AuthorEmail string   `json:"author_email"`
    AuthorDate  time.Time `json:"author_date"`
    CommitterName  string   `json:"committer_name,omitempty"`
    CommitterEmail string   `json:"committer_email,omitempty"`
    CommitDate     time.Time `json:"commit_date,omitempty"`
    Parents     []string  `json:"parents"`       // Parent commit SHAs
    FilesChanged int      `json:"files_changed"` // Number of files changed
    Insertions   int      `json:"insertions"`    // Lines added
    Deletions    int      `json:"deletions"`     // Lines deleted
}
```

### New Edge Types

| Edge | From | To | Semantics |
|------|------|----|-----------|
| `parent_of` | `vcs:commit` | `vcs:commit` | Commit DAG (parent → child) |
| `modifies` | `vcs:commit` | `fs:file` | Commit changed this file |

### Modified Edges (using existing types)

| Edge | From | To | Change |
|------|------|----|--------|
| `references` | `vcs:branch` | `vcs:commit` | Branch tip → commit (NEW) |
| `references` | `vcs:tag` | `vcs:commit` | Tag → commit (NEW) |

`references` is already registered as a common edge type. We use it here
because branches/tags are pointers (references) to commits — this is the
exact semantic of the existing edge type.

### Graph Shape

```
vcs:repo
  ├── has → vcs:branch (main)
  │           └── references → vcs:commit (abc123)
  │                               ├── parent_of → vcs:commit (def456)
  │                               │                  └── parent_of → ...
  │                               └── modifies → fs:file (src/main.go)
  ├── has → vcs:tag (v1.0.0)
  │           └── references → vcs:commit (abc123)
  └── has → vcs:commit (abc123)    ← repo owns all commits
              └── ...
```

### Configuration

```go
type Config struct {
    // MaxCommits limits how many commits to ingest per repository.
    // Default: 500. Set to 0 for unlimited (not recommended for large repos).
    MaxCommits int
}
```

We use a limit because:
- A repo with 50k commits would create 50k nodes + edges to parents + edges to files
- The common queries ("recent changes", "who touched X") only need recent history
- Users can increase the limit if they need deeper history

### Performance Considerations

**go-git commit walking**: `repo.Log()` returns an iterator. We walk up to
`MaxCommits` commits from HEAD. For each commit:
- Create the `vcs:commit` node: O(1)
- Create `parent_of` edges: O(num_parents), typically 1-2
- Get file stats: `commit.Stats()` — this is the expensive call (diffs each
  file). We call it only when the commit has a single parent (skip merge commits
  for stats, as merge diffs are ambiguous and slow)

**File linkage**: `modifies` edges point to `fs:file` nodes using
`IDFromURI(types.PathToURI(repoPath + "/" + filePath))`. This is a
deterministic ID computed from the file's URI — no storage read needed.
If the file doesn't exist (was deleted), the edge becomes an orphan and gets
cleaned up by GC. This is fine — the edge still existed temporarily and the
commit node's data records the change.

**Batch size**: With 500 commits averaging 3 files each, that's ~500 nodes +
~500 parent edges + ~1500 modifies edges = ~2500 write ops. Well within the
SQLite buffered writer's capacity.

### Indexing Flow

```
HandleEvent(.git visited)
  → Open repo
  → Index repo node (existing)
  → Index remotes (existing)
  → Index branches (existing, + add references edge to tip commit)
  → Index tags (existing, + add references edge to commit)
  → Index commits (NEW)
      → Walk from HEAD, up to MaxCommits
      → For each commit:
          - Create vcs:commit node
          - Create parent_of edges
          - Create modifies edges (single-parent commits only)
          - Emit has edge from repo to commit
  → Cleanup stale (existing)
```

---

## Queries This Enables

```sql
-- Recent commits by author
SELECT * FROM nodes
WHERE type = 'vcs:commit' AND data.author_name = 'Timo'
ORDER BY data.author_date DESC LIMIT 10

-- Files changed in last commit
(commit:vcs:commit)-[:modifies]->(file:fs:file)
WHERE commit.data.sha = 'abc123'
SELECT file

-- Commit history (parent chain)
(c1:vcs:commit)-[:parent_of*1..10]->(c2:vcs:commit)
WHERE c1.data.sha = 'abc123'
SELECT c2

-- What branch points to what commit
(branch:vcs:branch)-[:references]->(commit:vcs:commit)
SELECT branch.name, commit.data.sha, commit.data.message

-- Hotspot files (most frequently modified)
(commit:vcs:commit)-[:modifies]->(file:fs:file)
SELECT file.name, COUNT(*) as changes
GROUP BY file.name
ORDER BY changes DESC LIMIT 20
```

---

## Task Breakdown

1. **Types** — Add `TypeCommit`, `CommitData`, `EdgeParentOf`, `EdgeModifies`
   to `types/vcs.go` and `types/edges.go`; register them
2. **Indexer** — Add `Config` struct with `MaxCommits`, implement `indexCommits()`
   that walks the commit log and creates nodes + edges
3. **Branch/tag refs** — Modify `indexBranches()` and `indexTags()` to emit
   `references` edges pointing to the commit node
4. **Tests** — Test commit indexing, parent edges, file modification edges,
   depth limit, and branch/tag reference edges
5. **Wire up** — Update `axon.go` to pass config (or use sensible defaults)

---

## Acceptance Criteria

- [ ] `vcs:commit` nodes appear in the graph with correct data fields
- [ ] `parent_of` edges form the correct DAG
- [ ] `modifies` edges link commits to changed `fs:file` nodes
- [ ] `vcs:branch` and `vcs:tag` have `references` edges to their tip commit
- [ ] Commit ingestion is capped at `MaxCommits` (default 500)
- [ ] Merge commits skip file stats (no `modifies` edges, only `parent_of`)
- [ ] All existing tests still pass
- [ ] Full test suite passes with `go test ./...`
