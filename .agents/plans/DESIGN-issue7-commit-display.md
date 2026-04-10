# DESIGN: Richer vcs:commit Node Descriptions (Issue #7)

**Date**: 2026-05-13
**Status**: APPROVED
**Refs**: https://github.com/codewandler/axon/issues/7

---

## Problem Statement

`vcs:commit` nodes show only a SHA hash in all display contexts, even though the
underlying `CommitData` struct already contains the commit message, author name,
date, and file change counts. The data is indexed but never surfaced.

**Current output (`axon find "prevent agent loss" --type vcs:commit`)**:
```
0.625  [abc123d] a1b2c3d4e5f6... (vcs:commit)
0.616  [def456a] 9f8e7d6c5b4a... (vcs:commit)
```

**Desired output**:
```
0.625  [abc123d] ee48448e -- fix: prevent agent loss on validation failure  by timo (2024-12-15, 3 files)
0.616  [def456a] 03ae12c2 -- feat: add watch mode                           by timo (2024-12-10, 5 files)
```

---

## Root Cause

Three independent gaps cause the poor display:

### Gap 1 -- Node `Name` contains no subject line (indexer)

In `indexer/git/indexer.go`, commit nodes are created with:
```go
WithName(sha[:8])
```

The `Name` field is the primary human-readable label, but it only holds the
8-character short hash. The commit subject (`CommitData.Message`) is present in
the data payload but is not promoted to the name.

### Gap 2 -- `outputSemanticResults` / `outputPath` prefer `Key` over `Name` (find.go)

For non-filesystem nodes, the display logic falls through:
```go
path := types.URIToPath(r.URI)  // "" -- git+file:// is not a file:// URI
if path == "" { path = r.Key }   // 40-char full SHA -- not readable
if path == "" { path = r.Name }  // sha[:8] -- better, but no context
```

The full 40-char SHA (`Key`) crowds out the shorter, more relevant `Name`.

### Gap 3 -- `show.go` has no CommitData handling

`getNodeSummary` and `printMapData` lack `types.CommitData` / `types.TypeCommit`
cases. Commits fall through to the generic `map[string]any` handler that dumps all
fields as raw key=value pairs without formatting.

---

## Scope

### In Scope

1. **`CommitData.Description()` on the library type** -- single source of truth for
   commit display formatting, accessible to any library consumer (not just CLI)
2. **Enrich commit node `Name`** -- include subject line (truncated to 72 chars)
3. **Commit-aware display in `find.go`** -- delegates to `CommitData.Description()`
   for both `outputSemanticResults` and `outputPath`
4. **Commit-aware display in `show.go`** -- proper handling for `vcs:commit`

### Out of Scope

- Changing the commit URI format
- Changing the `Key` field (stays as the full SHA)
- Changing `CommitData` fields (already complete)
- Adding commit display to the `tree` command

---

## Design

### Change 1: Add `CommitData.Description()` to the library (`types/vcs.go`)

This is the key addition for library users. Any code importing
`github.com/codewandler/axon/types` can call `.Description()` on a `CommitData`
value to get a consistent, formatted one-liner.

**File**: `types/vcs.go`

```go
// Description returns a human-readable one-liner for display in search results,
// CLIs, and any other consumer of the library.
// Format: sha8 -- subject  by author (YYYY-MM-DD, N files)
// All parts are omitted gracefully when the underlying field is empty/zero.
func (d CommitData) Description() string {
    short := d.SHA
    if len(d.SHA) >= 8 {
        short = d.SHA[:8]
    }
    var meta []string
    if !d.AuthorDate.IsZero() {
        meta = append(meta, d.AuthorDate.Format("2006-01-02"))
    }
    if d.FilesChanged > 0 {
        meta = append(meta, fmt.Sprintf("%d files", d.FilesChanged))
    }
    suffix := ""
    if len(meta) > 0 {
        suffix = " (" + strings.Join(meta, ", ") + ")"
    }
    by := ""
    if d.AuthorName != "" {
        by = "  by " + d.AuthorName
    }
    if d.Message == "" {
        return short + by + suffix
    }
    return short + " -- " + d.Message + by + suffix
}
```

### Change 2: Enrich commit `Name` in the indexer (`indexer/git/indexer.go`)

**File**: `indexer/git/indexer.go`

Add a package-level helper and use it:
```go
// commitName builds the human-readable Name for a vcs:commit node.
// Format: "sha8" if no subject, "sha8 -- subject" otherwise.
// Subject is truncated to 72 characters with "..." suffix if longer.
func commitName(sha, subject string) string {
    short := sha[:8]
    if subject == "" {
        return short
    }
    if len(subject) > 72 {
        subject = subject[:69] + "..."
    }
    return short + " -- " + subject
}
```

Replace `WithName(sha[:8])` with `WithName(commitName(sha, subject))`.

**Rationale**: `Name` is the canonical human-readable label. Enriching it here
also benefits embedding quality and the `table` output format automatically.

### Change 3: Commit-aware display in `find.go` (`cmd/axon/find.go`)

**File**: `cmd/axon/find.go`

```go
// nodeDisplay returns the display string for a node in find output.
func nodeDisplay(node *graph.Node) string {
    if node.Type == types.TypeCommit {
        return commitDisplay(node)
    }
    path := types.URIToPath(node.URI)
    if path == "" {
        path = node.Key
    }
    if path == "" {
        path = node.Name
    }
    return path
}

// commitDisplay formats a vcs:commit node for one-line display.
// Delegates to CommitData.Description() -- the single source of truth --
// for both the typed (in-memory) and map (SQLite-loaded) paths.
func commitDisplay(node *graph.Node) string {
    switch d := node.Data.(type) {
    case types.CommitData:
        return d.Description()
    case map[string]any:
        t, _ := time.Parse(time.RFC3339, getMapString(d, "author_date"))
        cd := types.CommitData{
            SHA:          getMapString(d, "sha"),
            Message:      getMapString(d, "message"),
            AuthorName:   getMapString(d, "author_name"),
            AuthorDate:   t,
            FilesChanged: int(getMapFloat(d, "files_changed")),
        }
        return cd.Description()
    }
    if node.Name != "" {
        return node.Name
    }
    return node.Key
}
```

Also add `getMapFloat` to `show.go` (same package, used by `commitDisplay`):
```go
func getMapFloat(data map[string]any, key string) float64 {
    if v, ok := data[key].(float64); ok {
        return v
    }
    return 0
}
```

Update both output functions to use `nodeDisplay(node)`.

### Change 4: Commit handling in `show.go` (`cmd/axon/show.go`)

Add `types.CommitData` case in `getNodeSummary`:
```go
case types.CommitData:
    if data.Message != "" {
        name = data.SHA[:8] + " -- " + data.Message
    } else {
        name = data.SHA[:8]
    }
```

Add `types.TypeCommit` case in `printMapData`:
```go
case types.TypeCommit:
    fmt.Println("\nData:")
    sha := getMapString(data, "sha")
    if len(sha) >= 8 {
        fmt.Printf("  SHA:     %s\n", sha[:8])
    }
    if msg := getMapString(data, "message"); msg != "" {
        fmt.Printf("  Subject: %s\n", msg)
    }
    if body := getMapString(data, "body"); body != "" {
        fmt.Println("  Body:")
        for _, line := range strings.Split(body, "\n") {
            fmt.Printf("    %s\n", line)
        }
    }
    author := getMapString(data, "author_name")
    email  := getMapString(data, "author_email")
    if author != "" {
        if email != "" {
            fmt.Printf("  Author:  %s <%s>\n", author, email)
        } else {
            fmt.Printf("  Author:  %s\n", author)
        }
    }
    if dateStr := getMapString(data, "author_date"); dateStr != "" {
        fmt.Printf("  Date:    %s\n", dateStr)
    }
    fc := int(getMapFloat(data, "files_changed"))
    if fc > 0 {
        ins := int(getMapFloat(data, "insertions"))
        del := int(getMapFloat(data, "deletions"))
        fmt.Printf("  Files:   %d changed, +%d -%d lines\n", fc, ins, del)
    }
    if parents, ok := data["parents"].([]any); ok && len(parents) > 0 {
        fmt.Printf("  Parents: %d\n", len(parents))
    }
```

---

## Affected Files

| File | Change |
|------|--------|
| `types/vcs.go` | Add `CommitData.Description()` -- shared formatting, library-level |
| `types/vcs_test.go` (new) | Unit tests for `CommitData.Description()` |
| `indexer/git/indexer.go` | Add `commitName()` helper; use it in `WithName(...)` |
| `indexer/git/indexer_test.go` | Add `TestIndexerCommitName` |
| `cmd/axon/find.go` | Add `nodeDisplay()`, `commitDisplay()` (delegates to `Description()`); update two output loops |
| `cmd/axon/find_test.go` (new) | Unit tests for `commitDisplay()` and `nodeDisplay()` |
| `cmd/axon/show.go` | Add `CommitData` case in `getNodeSummary`; `TypeCommit` case in `printMapData`; add `getMapFloat()` |

---

## Acceptance Criteria

- [ ] `CommitData.Description()` returns the correct one-liner (unit tested in `types/vcs_test.go`)
- [ ] `vcs:commit` node `Name` field = `sha8 -- subject` (verified in `indexer/git/indexer_test.go`)
- [ ] `axon find "query" --type vcs:commit` shows subject, author, date, file count
- [ ] `axon find --type vcs:commit` (non-semantic) shows the same rich format
- [ ] `axon show <commit-id>` shows subject, body (if any), author, date, file stats
- [ ] Library users can call `commitData.Description()` directly
- [ ] Long subjects (>72 chars) are truncated with "..." in the `Name` field
- [ ] Both `types.CommitData` and `map[string]any` paths produce identical output
- [ ] All existing tests still pass (`go test ./...`)
