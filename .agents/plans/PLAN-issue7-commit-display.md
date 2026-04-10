# PLAN: Richer vcs:commit Node Descriptions (Issue #7)

**Design ref**: `DESIGN-issue7-commit-display.md`
**Date**: 2026-05-13
**Estimated total time**: ~35 minutes

---

## Prerequisites

- [ ] Build passes: `go build ./...`
- [ ] Tests pass: `go test ./...`
- [ ] Design document read and understood

---

## Phase A — Tests First (TDD)

### Task A1: Write failing test for commit node name format

**File modified**: `indexer/git/indexer_test.go`
**Estimated time**: 5 minutes

Add a new test after `TestIndexerCommits`:

```go
func TestIndexerCommitName(t *testing.T) {
	ctx := context.Background()
	dir := setupTestRepoWithCommits(t, 1)
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

	commits, err := g.FindNodes(ctx, graph.NodeFilter{Type: types.TypeCommit}, graph.QueryOptions{})
	if err != nil {
		t.Fatalf("FindNodes: %v", err)
	}
	if len(commits) == 0 {
		t.Fatal("no commits found")
	}

	c := commits[0]
	// Name must start with 8-char short SHA
	if len(c.Name) < 8 {
		t.Errorf("commit Name too short: %q", c.Name)
	}
	shortSHA := c.Name[:8]
	data, ok := c.Data.(types.CommitData)
	if !ok {
		t.Skip("Data not a CommitData (SQLite deserialization path; skip in-memory check)")
	}
	if data.Message != "" {
		// Name must include the subject
		want := shortSHA + " — " + data.Message
		if len(data.Message) > 72 {
			want = shortSHA + " — " + data.Message[:69] + "..."
		}
		if c.Name != want {
			t.Errorf("commit Name = %q, want %q", c.Name, want)
		}
	}
}
```

**Verification** (expect FAIL — `commitName` helper doesn't exist yet):
```bash
go test -v -run TestIndexerCommitName ./indexer/git
```

---

### Task A2: Write failing unit tests for `commitDisplay` and `formatCommitLine`

**File created**: `cmd/axon/find_test.go`
**Estimated time**: 8 minutes

```go
package main

import (
	"testing"
	"time"

	"github.com/codewandler/axon/graph"
	"github.com/codewandler/axon/types"
)

func TestFormatCommitLine(t *testing.T) {
	date := time.Date(2024, 12, 15, 0, 0, 0, 0, time.UTC)

	tests := []struct {
		name         string
		sha          string
		subject      string
		author       string
		date         time.Time
		filesChanged int
		want         string
	}{
		{
			name:         "full commit",
			sha:          "ee48448eabcdef01",
			subject:      "fix: prevent agent loss on validation failure",
			author:       "timo",
			date:         date,
			filesChanged: 3,
			want:         "ee48448e — fix: prevent agent loss on validation failure  by timo (2024-12-15, 3 files)",
		},
		{
			name:         "no subject",
			sha:          "ee48448eabcdef01",
			subject:      "",
			author:       "timo",
			date:         date,
			filesChanged: 1,
			want:         "ee48448e  by timo (2024-12-15, 1 files)",
		},
		{
			name:         "no author",
			sha:          "ee48448eabcdef01",
			subject:      "fix something",
			author:       "",
			date:         date,
			filesChanged: 2,
			want:         "ee48448e — fix something (2024-12-15, 2 files)",
		},
		{
			name:         "no date no files",
			sha:          "ee48448eabcdef01",
			subject:      "fix something",
			author:       "timo",
			date:         time.Time{},
			filesChanged: 0,
			want:         "ee48448e — fix something  by timo",
		},
		{
			name:         "long subject truncated",
			sha:          "abcdef0123456789",
			subject:      "this is a very long commit message that exceeds seventy-two characters easily yes",
			author:       "",
			date:         time.Time{},
			filesChanged: 0,
			want:         "abcdef01 — this is a very long commit message that exceeds seventy-two ...",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := formatCommitLine(tt.sha, tt.subject, tt.author, tt.date, tt.filesChanged)
			if got != tt.want {
				t.Errorf("formatCommitLine() =\n  %q\nwant:\n  %q", got, tt.want)
			}
		})
	}
}

func TestCommitDisplay_CommitData(t *testing.T) {
	date := time.Date(2024, 12, 15, 0, 0, 0, 0, time.UTC)
	node := &graph.Node{
		Type: types.TypeCommit,
		Name: "ee48448e — fix something",
		Key:  "ee48448eabcdef01",
		Data: types.CommitData{
			SHA:          "ee48448eabcdef01",
			Message:      "fix: prevent agent loss",
			AuthorName:   "timo",
			AuthorDate:   date,
			FilesChanged: 3,
		},
	}

	got := commitDisplay(node)
	want := "ee48448e — fix: prevent agent loss  by timo (2024-12-15, 3 files)"
	if got != want {
		t.Errorf("commitDisplay(CommitData) = %q, want %q", got, want)
	}
}

func TestCommitDisplay_Map(t *testing.T) {
	node := &graph.Node{
		Type: types.TypeCommit,
		Name: "ee48448e — fix something",
		Key:  "ee48448eabcdef01",
		Data: map[string]any{
			"sha":           "ee48448eabcdef01",
			"message":       "fix: prevent agent loss",
			"author_name":   "timo",
			"author_date":   "2024-12-15T00:00:00Z",
			"files_changed": float64(3),
		},
	}

	got := commitDisplay(node)
	want := "ee48448e — fix: prevent agent loss  by timo (2024-12-15, 3 files)"
	if got != want {
		t.Errorf("commitDisplay(map) = %q, want %q", got, want)
	}
}

func TestNodeDisplay_NonCommit(t *testing.T) {
	node := &graph.Node{
		Type: types.TypeFile,
		URI:  "file:///home/user/project/main.go",
		Key:  "/home/user/project/main.go",
		Name: "main.go",
	}
	got := nodeDisplay(node)
	want := "/home/user/project/main.go"
	if got != want {
		t.Errorf("nodeDisplay(file) = %q, want %q", got, want)
	}
}
```

**Verification** (expect compilation failure — functions don't exist yet):
```bash
go build ./cmd/axon
```

---

## Phase B — Implementation

### Task B1: Add `commitName()` helper and update `WithName()` in indexer

**File modified**: `indexer/git/indexer.go`
**Estimated time**: 5 minutes

Add after the `import` block (before `Config`):

```go
// commitName builds the human-readable Name for a vcs:commit node.
// Format: "sha8" if no subject, "sha8 — subject" otherwise.
// Subject is truncated to 72 characters with "..." suffix if longer.
func commitName(sha, subject string) string {
	short := sha[:8]
	if subject == "" {
		return short
	}
	if len(subject) > 72 {
		subject = subject[:69] + "..."
	}
	return short + " — " + subject
}
```

In `indexCommits()`, find the line:
```go
WithName(sha[:8]).
```
Replace with:
```go
WithName(commitName(sha, subject)).
```

**Verification**:
```bash
go build ./indexer/git
go test -v -run TestIndexerCommitName ./indexer/git
# Must PASS now
```

---

### Task B2: Add display helpers in `cmd/axon/find.go`

**File modified**: `cmd/axon/find.go`
**Estimated time**: 8 minutes

Add the following imports to the existing import block (add `"time"` and `"strings"` if not already present — check first):

Currently imports include: `"context"`, `"encoding/json"`, `"fmt"`, `"os"`, `"path/filepath"`, `"strings"`, `"text/tabwriter"`. Add `"time"`.

Add these functions at the bottom of `find.go`, before the closing of the file:

```go
// nodeDisplay returns the display string for a node in find output.
// For vcs:commit it shows enriched metadata. For filesystem nodes it returns
// the path. For other types it falls back to Key then Name.
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
// Handles both in-memory types.CommitData and map[string]any (from SQLite).
func commitDisplay(node *graph.Node) string {
	switch d := node.Data.(type) {
	case types.CommitData:
		return formatCommitLine(d.SHA, d.Message, d.AuthorName, d.AuthorDate, d.FilesChanged)
	case map[string]any:
		sha := getMapString(d, "sha")
		msg := getMapString(d, "message")
		author := getMapString(d, "author_name")
		files := int(getMapFloat(d, "files_changed"))
		t, _ := time.Parse(time.RFC3339, getMapString(d, "author_date"))
		return formatCommitLine(sha, msg, author, t, files)
	}
	if node.Name != "" {
		return node.Name
	}
	return node.Key
}

// formatCommitLine formats a single line of commit output.
// Format: sha8 — subject  by author (YYYY-MM-DD, N files)
func formatCommitLine(sha, subject, author string, date time.Time, filesChanged int) string {
	short := sha
	if len(sha) >= 8 {
		short = sha[:8]
	}
	var meta []string
	if !date.IsZero() {
		meta = append(meta, date.Format("2006-01-02"))
	}
	if filesChanged > 0 {
		meta = append(meta, fmt.Sprintf("%d files", filesChanged))
	}
	suffix := ""
	if len(meta) > 0 {
		suffix = " (" + strings.Join(meta, ", ") + ")"
	}
	by := ""
	if author != "" {
		by = "  by " + author
	}
	if subject == "" {
		return short + by + suffix
	}
	return short + " — " + subject + by + suffix
}
```

Update `outputPath` to use `nodeDisplay` (find the existing loop):

```go
// OLD:
for _, node := range nodes {
    path := types.URIToPath(node.URI)
    if path == "" {
        path = node.Key
    }
    fmt.Printf("[%s] %s (%s)\n", shortID(node.ID), path, node.Type)
}

// NEW:
for _, node := range nodes {
    fmt.Printf("[%s] %s (%s)\n", shortID(node.ID), nodeDisplay(node), node.Type)
}
```

Update `outputSemanticResults` default case (find the existing loop):

```go
// OLD:
default: // "path"
    for _, r := range results {
        path := types.URIToPath(r.URI)
        if path == "" {
            path = r.Key
        }
        if path == "" {
            path = r.Name
        }
        fmt.Printf("%.3f  [%s] %s (%s)\n", r.Score, shortID(r.ID), path, r.Type)
    }

// NEW:
default: // "path"
    for _, r := range results {
        fmt.Printf("%.3f  [%s] %s (%s)\n", r.Score, shortID(r.ID), nodeDisplay(r.Node), r.Type)
    }
```

**Verification**:
```bash
go build ./cmd/axon
go test -v -run "TestFormatCommitLine|TestCommitDisplay|TestNodeDisplay" ./cmd/axon
# Must PASS
```

---

### Task B3: Add `getMapFloat` to `cmd/axon/show.go`

**File modified**: `cmd/axon/show.go`
**Estimated time**: 2 minutes

Find the existing `getMapInt` helper at the bottom of `show.go`. Add `getMapFloat` directly below it:

```go
// getMapFloat extracts a float64 from a map (JSON numbers come as float64)
func getMapFloat(data map[string]any, key string) float64 {
	if v, ok := data[key].(float64); ok {
		return v
	}
	return 0
}
```

**Note**: `getMapString` is already defined in `show.go` (same `main` package),
so it's accessible in `find.go` without duplication.

**Verification**:
```bash
go build ./cmd/axon
```

---

### Task B4: Add `CommitData` case in `getNodeSummary` in `show.go`

**File modified**: `cmd/axon/show.go`
**Estimated time**: 3 minutes

In `getNodeSummary`, find the `case types.TagData:` block. Insert AFTER it:

```go
case types.CommitData:
    if data.Message != "" {
        name = data.SHA[:8] + " — " + data.Message
    } else {
        name = data.SHA[:8]
    }
```

**Verification**:
```bash
go build ./cmd/axon
```

---

### Task B5: Add `TypeCommit` case in `printMapData` in `show.go`

**File modified**: `cmd/axon/show.go`
**Estimated time**: 5 minutes

In `printMapData`, find the `case types.TypeTag:` block. Insert AFTER it, before
the `// Markdown types from JSON` comment:

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
    email := getMapString(data, "author_email")
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

**Note**: `strings` is already imported in `show.go`. Confirm before running.

**Verification**:
```bash
go build ./cmd/axon
```

---

## Phase C — Final Verification

### Task C1: Run full test suite

```bash
go test ./...
```

All packages must pass. Pay particular attention to:
- `./indexer/git` — `TestIndexerCommitName` must pass
- `./cmd/axon` — all new tests must pass

---

### Task C2: Manual smoke test

```bash
# Build the binary
go build -o ./bin/axon ./cmd/axon

# Run against the axon repo itself
./bin/axon find --type vcs:commit --global --limit 5
```

Expected: output includes subject lines, author names, dates, and file counts.
NOT: raw 40-char SHAs.

```bash
# Show a specific commit
./bin/axon find --type vcs:commit --global --limit 1 -o table
# Note a short ID from the output above, then:
./bin/axon show <short-id>
```

Expected: `show` output includes `Subject`, `Author`, `Date`, `Files` fields.

---

### Task C3: Static analysis

```bash
go vet ./...
```

Must produce no warnings.

---

## Summary

| Task | File | Change |
|------|------|--------|
| A1 | `indexer/git/indexer_test.go` | Add `TestIndexerCommitName` |
| A2 | `cmd/axon/find_test.go` (new) | Add unit tests for display helpers |
| B1 | `indexer/git/indexer.go` | Add `commitName()`, update `WithName()` |
| B2 | `cmd/axon/find.go` | Add `nodeDisplay()`, `commitDisplay()`, `formatCommitLine()`; update 2 output loops |
| B3 | `cmd/axon/show.go` | Add `getMapFloat()` |
| B4 | `cmd/axon/show.go` | Add `CommitData` case in `getNodeSummary` |
| B5 | `cmd/axon/show.go` | Add `TypeCommit` case in `printMapData` |
| C1 | — | `go test ./...` |
| C2 | — | Manual smoke test |
| C3 | — | `go vet ./...` |
