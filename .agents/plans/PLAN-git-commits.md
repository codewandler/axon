# Plan: Extend Git Indexer — Commits, Tags, Branches

**Design ref**: `DESIGN-git-commits.md`
**Date**: 2026-04-10
**Total estimated time**: ~70–80 minutes

---

## Prerequisites

- All tests pass: `go test ./...` ✅
- Build passes: `go build ./...` ✅
- Design document approved ✅

---

## Phase A — Types & Registration

### Task A1: Add `vcs:commit` node type to `types/vcs.go`

**Files modified**: `types/vcs.go`
**Estimated time**: 5 minutes

Add `"time"` to the imports. Add `TypeCommit` constant, `CommitData` struct, and `CommitToURI` helper. Register the type in `RegisterVCSTypes()`.

**Code to write**:

```go
// Add to the const block:
TypeCommit = "vcs:commit"

// Add struct after TagData:
// CommitData holds data for a commit node.
type CommitData struct {
    SHA            string    `json:"sha"`
    Message        string    `json:"message"`       // First line only (subject)
    Body           string    `json:"body,omitempty"` // Full body after subject
    AuthorName     string    `json:"author_name"`
    AuthorEmail    string    `json:"author_email"`
    AuthorDate     time.Time `json:"author_date"`
    CommitterName  string    `json:"committer_name,omitempty"`
    CommitterEmail string    `json:"committer_email,omitempty"`
    CommitDate     time.Time `json:"commit_date,omitempty"`
    Parents        []string  `json:"parents"`        // Parent commit SHAs
    FilesChanged   int       `json:"files_changed"`
    Insertions     int       `json:"insertions"`
    Deletions      int       `json:"deletions"`
}

// CommitToURI returns the URI for a commit node.
func CommitToURI(repoPath, sha string) string {
    return RepoPathToURI(repoPath) + "/commit/" + sha
}
```

Add to `RegisterVCSTypes()`, after the `TagData` registration:

```go
graph.RegisterNodeType[CommitData](r, graph.NodeSpec{
    Type:        TypeCommit,
    Description: "A git commit",
})
```

**Verification**:
```bash
go build ./...
```

---

### Task A2: Add `EdgeParentOf` and `EdgeModifies` to `types/edges.go`

**Files modified**: `types/edges.go`
**Estimated time**: 5 minutes

Add two new constants and register them in `RegisterCommonEdges()`.

**Code to write**:

Add to the const block (after `EdgeTests`):
```go
// Commit DAG: parent commit → child commit
EdgeParentOf = "parent_of"

// File modification: commit → file (the commit changed this file)
EdgeModifies = "modifies"
```

Add to `RegisterCommonEdges()`:
```go
r.RegisterEdgeType(graph.EdgeSpec{
    Type:        EdgeParentOf,
    Description: "Commit DAG parent-to-child relationship",
})

r.RegisterEdgeType(graph.EdgeSpec{
    Type:        EdgeModifies,
    Description: "Commit modified a file",
})
```

**Verification**:
```bash
go build ./...
```

---

### Task A3: Add AQL constants for `vcs:commit`, `parent_of`, `modifies`

**Files modified**: `aql/nodetypes.go`, `aql/edgetypes.go`
**Estimated time**: 5 minutes

**In `aql/nodetypes.go`**, add `Commit NodeTypeRef` to the `NodeType` struct definition and initializer:

```go
// In the struct definition, VCS section:
Commit NodeTypeRef

// In the struct initializer, VCS section:
Commit: NodeTypeRef{"vcs:commit"},
```

**In `aql/edgetypes.go`**, add `ParentOf EdgeTypeRef` and `Modifies EdgeTypeRef` to the `Edge` struct definition and initializer:

```go
// In the struct definition:
ParentOf EdgeTypeRef
Modifies EdgeTypeRef

// In the struct initializer:
ParentOf: EdgeTypeRef{"parent_of"},
Modifies: EdgeTypeRef{"modifies"},
```

**Verification**:
```bash
go build ./...
```

---

## Phase B — Indexer Config

### Task B1: Add `Config` struct and update `Indexer` in `indexer/git/indexer.go`

**Files modified**: `indexer/git/indexer.go`
**Estimated time**: 5 minutes

**Code to write**:

Before `type Indexer struct{}`, add:

```go
// Config holds configuration for the git indexer.
type Config struct {
    // MaxCommits limits the number of commits to index per repository.
    // Default: 500. Set to 0 for unlimited (not recommended for large repos).
    MaxCommits int
}

func defaultConfig() Config {
    return Config{MaxCommits: 500}
}
```

Change `Indexer` struct:
```go
type Indexer struct {
    cfg Config
}
```

Change `New()`:
```go
// New creates a new git indexer with optional configuration.
func New(cfg ...Config) *Indexer {
    c := defaultConfig()
    if len(cfg) > 0 {
        c = cfg[0]
        if c.MaxCommits == 0 {
            c.MaxCommits = defaultConfig().MaxCommits
        }
    }
    return &Indexer{cfg: c}
}
```

**Verification**:
```bash
go build ./...
```

---

## Phase C — Commit Indexing (TDD)

### Task C1: Write failing tests for commit indexing

**Files modified**: `indexer/git/indexer_test.go`
**Estimated time**: 15 minutes

Add `"fmt"` to imports (already has `os`, `path/filepath`, `testing`, `time`, `gogit`, `object`).

Add helper `setupTestRepoWithCommits` and three test functions:

```go
func setupTestRepoWithCommits(t *testing.T, n int) string {
    t.Helper()
    dir := t.TempDir()

    repo, err := gogit.PlainInit(dir, false)
    if err != nil {
        t.Fatalf("PlainInit: %v", err)
    }

    worktree, err := repo.Worktree()
    if err != nil {
        t.Fatalf("Worktree: %v", err)
    }

    for i := 0; i < n; i++ {
        fname := fmt.Sprintf("file%d.txt", i)
        if err := os.WriteFile(filepath.Join(dir, fname), []byte(fmt.Sprintf("content %d", i)), 0644); err != nil {
            t.Fatalf("WriteFile: %v", err)
        }
        if _, err := worktree.Add(fname); err != nil {
            t.Fatalf("Add: %v", err)
        }
        if _, err := worktree.Commit(fmt.Sprintf("commit %d", i), &gogit.CommitOptions{
            Author: &object.Signature{
                Name:  "Test",
                Email: "test@example.com",
                When:  time.Now(),
            },
        }); err != nil {
            t.Fatalf("Commit: %v", err)
        }
    }

    return dir
}

func TestIndexerCommits(t *testing.T) {
    ctx := context.Background()
    dir := setupTestRepoWithCommits(t, 3)
    g := setupGraph(t)

    idx := New()
    emitter := indexer.NewGraphEmitter(g, "gen-1")
    ictx := &indexer.Context{
        Root:       types.RepoPathToURI(dir),
        Generation: "gen-1",
        Graph:      g,
        Emitter:    emitter,
    }

    gitDir := filepath.Join(dir, ".git")
    event := indexer.Event{
        Type:     indexer.EventEntryVisited,
        URI:      types.PathToURI(gitDir),
        Path:     gitDir,
        Name:     ".git",
        NodeType: types.TypeDir,
        NodeID:   "test-git-dir-id",
    }

    if err := idx.HandleEvent(ctx, ictx, event); err != nil {
        t.Fatalf("HandleEvent: %v", err)
    }
    if err := g.Storage().Flush(ctx); err != nil {
        t.Fatalf("Flush: %v", err)
    }

    // 3 commit nodes
    commits, err := g.FindNodes(ctx, graph.NodeFilter{Type: types.TypeCommit}, graph.QueryOptions{})
    if err != nil {
        t.Fatalf("FindNodes: %v", err)
    }
    if len(commits) != 3 {
        t.Errorf("expected 3 commits, got %d", len(commits))
    }

    // Repo → commit via 'has' edges
    repos, _ := g.FindNodes(ctx, graph.NodeFilter{Type: types.TypeRepo}, graph.QueryOptions{})
    if len(repos) == 0 {
        t.Fatal("no repo found")
    }
    edges, _ := g.GetEdgesFrom(ctx, repos[0].ID)
    hasCommitEdges := 0
    for _, e := range edges {
        if e.Type == types.EdgeHas {
            target, err := g.GetNode(ctx, e.To)
            if err == nil && target.Type == types.TypeCommit {
                hasCommitEdges++
            }
        }
    }
    if hasCommitEdges != 3 {
        t.Errorf("expected 3 'has' edges to commits, got %d", hasCommitEdges)
    }

    // parent_of edges: chain of 3 → 2 parent edges
    parentEdges := 0
    for _, c := range commits {
        ces, _ := g.GetEdgesFrom(ctx, c.ID)
        for _, e := range ces {
            if e.Type == types.EdgeParentOf {
                parentEdges++
            }
        }
    }
    if parentEdges != 2 {
        t.Errorf("expected 2 parent_of edges, got %d", parentEdges)
    }
}

func TestIndexerCommitDepthLimit(t *testing.T) {
    ctx := context.Background()
    dir := setupTestRepoWithCommits(t, 10)
    g := setupGraph(t)

    idx := New(Config{MaxCommits: 3})
    emitter := indexer.NewGraphEmitter(g, "gen-1")
    ictx := &indexer.Context{
        Root:       types.RepoPathToURI(dir),
        Generation: "gen-1",
        Graph:      g,
        Emitter:    emitter,
    }

    gitDir := filepath.Join(dir, ".git")
    event := indexer.Event{
        Type:     indexer.EventEntryVisited,
        URI:      types.PathToURI(gitDir),
        Path:     gitDir,
        Name:     ".git",
        NodeType: types.TypeDir,
        NodeID:   "test-git-dir-id",
    }

    if err := idx.HandleEvent(ctx, ictx, event); err != nil {
        t.Fatalf("HandleEvent: %v", err)
    }
    if err := g.Storage().Flush(ctx); err != nil {
        t.Fatalf("Flush: %v", err)
    }

    commits, err := g.FindNodes(ctx, graph.NodeFilter{Type: types.TypeCommit}, graph.QueryOptions{})
    if err != nil {
        t.Fatalf("FindNodes: %v", err)
    }
    if len(commits) != 3 {
        t.Errorf("expected 3 commits (MaxCommits limit), got %d", len(commits))
    }
}

func TestIndexerModifiesEdges(t *testing.T) {
    ctx := context.Background()
    dir := setupTestRepoWithCommits(t, 2)
    g := setupGraph(t)

    idx := New()
    emitter := indexer.NewGraphEmitter(g, "gen-1")
    ictx := &indexer.Context{
        Root:       types.RepoPathToURI(dir),
        Generation: "gen-1",
        Graph:      g,
        Emitter:    emitter,
    }

    gitDir := filepath.Join(dir, ".git")
    event := indexer.Event{
        Type:     indexer.EventEntryVisited,
        URI:      types.PathToURI(gitDir),
        Path:     gitDir,
        Name:     ".git",
        NodeType: types.TypeDir,
        NodeID:   "test-git-dir-id",
    }

    if err := idx.HandleEvent(ctx, ictx, event); err != nil {
        t.Fatalf("HandleEvent: %v", err)
    }
    if err := g.Storage().Flush(ctx); err != nil {
        t.Fatalf("Flush: %v", err)
    }

    commits, _ := g.FindNodes(ctx, graph.NodeFilter{Type: types.TypeCommit}, graph.QueryOptions{})
    if len(commits) == 0 {
        t.Fatal("no commits found")
    }

    // At least one commit should have a modifies edge
    found := false
    for _, c := range commits {
        ces, _ := g.GetEdgesFrom(ctx, c.ID)
        for _, e := range ces {
            if e.Type == types.EdgeModifies {
                found = true
                break
            }
        }
        if found {
            break
        }
    }
    if !found {
        t.Error("expected at least one 'modifies' edge from a commit to a file")
    }
}
```

**Verification** (tests must FAIL at this point — expected):
```bash
go test -v ./indexer/git -run "TestIndexerCommits|TestIndexerCommitDepthLimit|TestIndexerModifiesEdges"
# Expected: FAIL — TypeCommit not yet in setupGraph's registry; indexCommits not yet called
```

---

### Task C2: Update `setupGraph` in the test to register the new commit type

**Files modified**: `indexer/git/indexer_test.go`
**Estimated time**: 2 minutes

The `setupGraph` helper calls `types.RegisterVCSTypes(r)` which will now include `vcs:commit` after Task A1. No change needed — as long as Task A1 is done first.

If tests still fail to compile, ensure `types.TypeCommit` and `types.EdgeParentOf`/`types.EdgeModifies` are referenced but both constants are now defined.

**Verification**:
```bash
go build ./indexer/git
# Must compile cleanly before proceeding
```

---

### Task C3: Implement `indexCommits()` in `indexer/git/indexer.go`

**Files modified**: `indexer/git/indexer.go`
**Estimated time**: 20 minutes

Add `"fmt"`, `"io"`, and `"time"` to imports (drop any that were already present; `time` may be new).

Add the `indexCommits` method:

```go
func (i *Indexer) indexCommits(ctx context.Context, ictx *indexer.Context, repo *git.Repository, repoNode *graph.Node, repoPath string) error {
    iter, err := repo.Log(&git.LogOptions{})
    if err != nil {
        return fmt.Errorf("git log: %w", err)
    }
    defer iter.Close()

    count := 0
    for {
        if i.cfg.MaxCommits > 0 && count >= i.cfg.MaxCommits {
            break
        }
        commit, err := iter.Next()
        if err == io.EOF {
            break
        }
        if err != nil {
            return fmt.Errorf("iterating commits: %w", err)
        }

        sha := commit.Hash.String()
        commitURI := types.CommitToURI(repoPath, sha)

        // Split message into subject + body
        msg := strings.TrimSpace(commit.Message)
        subject, body := msg, ""
        if idx := strings.Index(msg, "\n"); idx != -1 {
            subject = msg[:idx]
            body = strings.TrimSpace(msg[idx+1:])
        }

        // Parent SHAs
        parents := make([]string, len(commit.ParentHashes))
        for idx, h := range commit.ParentHashes {
            parents[idx] = h.String()
        }

        // File stats for single-parent commits only (merge commits skipped)
        type fileChange struct{ name string }
        var fileChanges []fileChange
        var filesChanged, insertions, deletions int
        if commit.NumParents() == 1 {
            stats, err := commit.Stats()
            if err == nil {
                for _, fs := range stats {
                    filesChanged++
                    insertions += fs.Addition
                    deletions += fs.Deletion
                    fileChanges = append(fileChanges, fileChange{name: fs.Name})
                }
            }
        }

        commitNode := graph.NewNode(types.TypeCommit).
            WithURI(commitURI).
            WithKey(sha).
            WithName(sha[:8]).
            WithData(types.CommitData{
                SHA:            sha,
                Message:        subject,
                Body:           body,
                AuthorName:     commit.Author.Name,
                AuthorEmail:    commit.Author.Email,
                AuthorDate:     commit.Author.When,
                CommitterName:  commit.Committer.Name,
                CommitterEmail: commit.Committer.Email,
                CommitDate:     commit.Committer.When,
                Parents:        parents,
                FilesChanged:   filesChanged,
                Insertions:     insertions,
                Deletions:      deletions,
            })

        if err := ictx.Emitter.EmitNode(ctx, commitNode); err != nil {
            return err
        }

        // Repo owns commit
        if err := indexer.EmitOwnership(ctx, ictx.Emitter, repoNode.ID, commitNode.ID); err != nil {
            return err
        }

        // parent_of edges (commit → parent)
        for _, parentSHA := range parents {
            parentID := graph.IDFromURI(types.CommitToURI(repoPath, parentSHA))
            edge := graph.NewEdge(types.EdgeParentOf, commitNode.ID, parentID)
            if err := ictx.Emitter.EmitEdge(ctx, edge); err != nil {
                return err
            }
        }

        // modifies edges (commit → fs:file)
        for _, fc := range fileChanges {
            fileID := graph.IDFromURI(types.PathToURI(filepath.Join(repoPath, fc.name)))
            edge := graph.NewEdge(types.EdgeModifies, commitNode.ID, fileID)
            if err := ictx.Emitter.EmitEdge(ctx, edge); err != nil {
                return err
            }
        }

        count++
        _ = time.Time{} // suppress unused import if time only used in CommitData
    }

    return nil
}
```

> **Note**: The `_ = time.Time{}` line can be removed if `time` is actually used elsewhere in the file. Check after writing.

Call `indexCommits` from `HandleEvent`, after `indexTags` and before the progress completion block:

```go
// Index commits (after tags)
if err := i.indexCommits(ctx, ictx, repo, repoNode, repoPath); err != nil {
    return err
}
```

**Verification**:
```bash
go test -v ./indexer/git -run "TestIndexerCommits|TestIndexerCommitDepthLimit|TestIndexerModifiesEdges"
# Expected: all 3 PASS
go test ./indexer/git
# Expected: all tests pass
```

---

## Phase D — Branch/Tag References (TDD)

### Task D1: Write failing tests for branch/tag → commit `references` edges

**Files modified**: `indexer/git/indexer_test.go`
**Estimated time**: 10 minutes

Add two test functions:

```go
func TestIndexerBranchCommitReference(t *testing.T) {
    ctx := context.Background()
    dir := setupTestRepoWithCommits(t, 2)
    g := setupGraph(t)

    idx := New()
    emitter := indexer.NewGraphEmitter(g, "gen-1")
    ictx := &indexer.Context{
        Root:       types.RepoPathToURI(dir),
        Generation: "gen-1",
        Graph:      g,
        Emitter:    emitter,
    }

    gitDir := filepath.Join(dir, ".git")
    event := indexer.Event{
        Type:     indexer.EventEntryVisited,
        URI:      types.PathToURI(gitDir),
        Path:     gitDir,
        Name:     ".git",
        NodeType: types.TypeDir,
        NodeID:   "test-git-dir-id",
    }

    if err := idx.HandleEvent(ctx, ictx, event); err != nil {
        t.Fatalf("HandleEvent: %v", err)
    }
    if err := g.Storage().Flush(ctx); err != nil {
        t.Fatalf("Flush: %v", err)
    }

    // Find branch node and check for references edge to a commit
    branches, err := g.FindNodes(ctx, graph.NodeFilter{Type: types.TypeBranch}, graph.QueryOptions{})
    if err != nil {
        t.Fatalf("FindNodes: %v", err)
    }
    if len(branches) == 0 {
        t.Fatal("no branches found")
    }

    found := false
    for _, branch := range branches {
        edges, _ := g.GetEdgesFrom(ctx, branch.ID)
        for _, e := range edges {
            if e.Type == types.EdgeReferences {
                target, err := g.GetNode(ctx, e.To)
                if err == nil && target.Type == types.TypeCommit {
                    found = true
                    break
                }
            }
        }
        if found {
            break
        }
    }
    if !found {
        t.Error("expected 'references' edge from branch to its tip commit")
    }
}

func TestIndexerTagCommitReference(t *testing.T) {
    ctx := context.Background()
    dir := setupTestRepo(t) // reuses existing helper which creates 1 commit + 1 tag
    g := setupGraph(t)

    idx := New()
    emitter := indexer.NewGraphEmitter(g, "gen-1")
    ictx := &indexer.Context{
        Root:       types.RepoPathToURI(dir),
        Generation: "gen-1",
        Graph:      g,
        Emitter:    emitter,
    }

    gitDir := filepath.Join(dir, ".git")
    event := indexer.Event{
        Type:     indexer.EventEntryVisited,
        URI:      types.PathToURI(gitDir),
        Path:     gitDir,
        Name:     ".git",
        NodeType: types.TypeDir,
        NodeID:   "test-git-dir-id",
    }

    if err := idx.HandleEvent(ctx, ictx, event); err != nil {
        t.Fatalf("HandleEvent: %v", err)
    }
    if err := g.Storage().Flush(ctx); err != nil {
        t.Fatalf("Flush: %v", err)
    }

    tags, err := g.FindNodes(ctx, graph.NodeFilter{Type: types.TypeTag}, graph.QueryOptions{})
    if err != nil {
        t.Fatalf("FindNodes: %v", err)
    }
    if len(tags) == 0 {
        t.Fatal("no tags found")
    }

    found := false
    for _, tag := range tags {
        edges, _ := g.GetEdgesFrom(ctx, tag.ID)
        for _, e := range edges {
            if e.Type == types.EdgeReferences {
                target, err := g.GetNode(ctx, e.To)
                if err == nil && target.Type == types.TypeCommit {
                    found = true
                    break
                }
            }
        }
        if found {
            break
        }
    }
    if !found {
        t.Error("expected 'references' edge from tag to its commit")
    }
}
```

**Verification** (must FAIL — expected):
```bash
go test -v ./indexer/git -run "TestIndexerBranchCommitReference|TestIndexerTagCommitReference"
# Expected: FAIL — references edges not yet emitted
```

---

### Task D2: Implement branch/tag `references` edges

**Files modified**: `indexer/git/indexer.go`
**Estimated time**: 5 minutes

In `indexBranches()`, inside the `ForEach` lambda, after `indexer.EmitOwnership`, add:

```go
// references edge: branch → tip commit
commitURI := types.CommitToURI(repoPath, ref.Hash().String())
commitID := graph.IDFromURI(commitURI)
refEdge := graph.NewEdge(types.EdgeReferences, branchNode.ID, commitID)
if err := ictx.Emitter.EmitEdge(ctx, refEdge); err != nil {
    return err
}
```

The `indexBranches` signature must accept `repoPath string` as an additional parameter since it currently doesn't have it. Update the call site in `HandleEvent` accordingly:

Current call:
```go
if err := i.indexBranches(ctx, ictx, repo, repoNode.ID, repoURI, head); err != nil {
```

New signature:
```go
func (i *Indexer) indexBranches(ctx context.Context, ictx *indexer.Context, repo *git.Repository, repoID string, repoURI string, repoPath string, head *plumbing.Reference) error {
```

New call:
```go
if err := i.indexBranches(ctx, ictx, repo, repoNode.ID, repoURI, repoPath, head); err != nil {
```

In `indexTags()`, similarly add `repoPath string` to the signature and after `indexer.EmitOwnership` add:

```go
// references edge: tag → commit
commitURI := types.CommitToURI(repoPath, ref.Hash().String())
commitID := graph.IDFromURI(commitURI)
refEdge := graph.NewEdge(types.EdgeReferences, tagNode.ID, commitID)
if err := ictx.Emitter.EmitEdge(ctx, refEdge); err != nil {
    return err
}
```

Updated signature for `indexTags`:
```go
func (i *Indexer) indexTags(ctx context.Context, ictx *indexer.Context, repo *git.Repository, repoID string, repoURI string, repoPath string) error {
```

Updated call in `HandleEvent`:
```go
if err := i.indexTags(ctx, ictx, repo, repoNode.ID, repoURI, repoPath); err != nil {
```

**Verification**:
```bash
go test -v ./indexer/git -run "TestIndexerBranchCommitReference|TestIndexerTagCommitReference"
# Expected: PASS
go test ./indexer/git
# Expected: all pass
```

---

## Phase E — Wire Up

### Task E1: Add `GitConfig` to `axon.Config` and pass it to `git.New()`

**Files modified**: `axon.go`
**Estimated time**: 5 minutes

Add `GitConfig` field to `Config` struct:

```go
// GitConfig holds configuration for the git indexer.
// Controls how many commits are indexed per repository (default: 500).
GitConfig git.Config
```

Change `git.New()` call in `New()`:

```go
// Before:
idxRegistry.Register(git.New())

// After:
idxRegistry.Register(git.New(cfg.GitConfig))
```

**Verification**:
```bash
go build ./...
```

---

## Phase F — Final Verification

### Task F1: Run the full test suite

**Estimated time**: 2 minutes

```bash
go test ./...
# Expected: all packages pass, no regressions
```

```bash
go build ./...
# Expected: clean build
```

Optionally run against a real repo to confirm commit nodes appear:
```bash
go install ./cmd/axon
axon init --local .
axon query "SELECT data.sha, data.message FROM nodes WHERE type = 'vcs:commit' LIMIT 5"
axon query "SELECT COUNT(*) FROM nodes WHERE type = 'vcs:commit'"
```

---

## Acceptance Criteria Checklist

- [ ] `vcs:commit` nodes appear with correct fields (SHA, message, author, date, stats)
- [ ] `parent_of` edges form the correct chain (N commits → N-1 edges)
- [ ] `modifies` edges link single-parent commits to the files they changed
- [ ] `vcs:branch` nodes have `references` edges to their tip commit nodes
- [ ] `vcs:tag` nodes have `references` edges to their commit nodes
- [ ] Commit ingestion capped at `MaxCommits` (default 500)
- [ ] Merge commits (>1 parent) have no `modifies` edges, only `parent_of`
- [ ] `aql.NodeType.Commit` and `aql.Edge.ParentOf` / `aql.Edge.Modifies` available
- [ ] `axon.Config.GitConfig` allows callers to override `MaxCommits`
- [ ] All existing tests still pass: `go test ./...`
