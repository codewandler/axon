# Plan: Unified `find` with Semantic Search

**Design ref:** `DESIGN-20260513-unified-find.md`  
**Estimated total:** ~45 minutes  

---

## Task 1 — Fix `FindSimilar`: apply filter before truncating

**File:** `adapters/sqlite/sqlite.go`  
**Estimated time:** 3 minutes

Currently the function slices `candidates` to `limit` **before** fetching nodes
and applying the filter. That means a `--type go:func --limit 10` query might
return fewer than 10 results even when more matches exist further down the
similarity list.

**Change:** Remove the early truncation. Iterate the full sorted slice, skip
filtered nodes, and stop once `len(results) == limit`.

```go
// REMOVE this block:
if limit > 0 && len(candidates) > limit {
    candidates = candidates[:limit]
}

// REPLACE the fetch+filter loop with:
var results []*graph.NodeWithScore
for _, c := range candidates {
    if limit > 0 && len(results) >= limit {
        break
    }
    node, err := s.GetNode(ctx, c.nodeID)
    if err != nil || node == nil {
        continue
    }
    if filter != nil && !nodeMatchesFilter(node, filter) {
        continue
    }
    results = append(results, &graph.NodeWithScore{Node: node, Score: c.score})
}
return results, nil
```

**Verification:**
```bash
go build ./...
go test ./adapters/sqlite/...
```

---

## Task 2 — Extend `nodeMatchesFilter` to cover all `NodeFilter` fields

**File:** `adapters/sqlite/sqlite.go`  
**Estimated time:** 5 minutes

Currently `nodeMatchesFilter` only checks `filter.Type`. The semantic path in
`find` needs it to also cover `URIPrefix` (local scope), `Labels` (OR), and
`Extensions` (OR) — all the fields set by find's existing flags.

```go
func nodeMatchesFilter(node *graph.Node, filter *graph.NodeFilter) bool {
    if filter.Type != "" && node.Type != filter.Type {
        return false
    }
    if filter.URIPrefix != "" && !strings.HasPrefix(node.URI, filter.URIPrefix) {
        return false
    }
    if len(filter.Labels) > 0 {
        found := false
        for _, want := range filter.Labels {
            for _, have := range node.Labels {
                if have == want {
                    found = true
                    break
                }
            }
            if found {
                break
            }
        }
        if !found {
            return false
        }
    }
    if len(filter.Extensions) > 0 {
        ext := ""
        if m, ok := node.Data.(map[string]any); ok {
            ext, _ = m["ext"].(string)
        }
        found := false
        for _, want := range filter.Extensions {
            if ext == want {
                found = true
                break
            }
        }
        if !found {
            return false
        }
    }
    return true
}
```

Also add `"strings"` to the import block if not already present.

**Verification:**
```bash
go build ./...
go test ./adapters/sqlite/...
```

---

## Task 3 — Add semantic path to `find.go`

**File:** `cmd/axon/find.go`  
**Estimated time:** 15 minutes

### 3a. Update cobra metadata

```go
var findCmd = &cobra.Command{
    Use:   "find [query] [flags]",
    Short: "Search for nodes in the graph",
    Long: `Search for nodes in the graph.

With no arguments, filters by flags (--type, --name, --ext, etc.).
With a text argument, performs semantic vector similarity search and
applies any flags as post-filters.

Examples:
  # Semantic search (requires embeddings — run 'axon index --embed' first)
  axon find "error handling"
  axon find "logo and visual assets"
  axon find "concurrency and goroutines" --type go:func
  axon find "recent logo commits" --type vcs:commit --limit 5

  # Flag-only (unchanged)
  axon find --type fs:file --ext go
  axon find --name main.go
  axon find --label ci:config --global`,
    Args: cobra.MaximumNArgs(1),
    RunE: runFind,
}
```

### 3b. Add semantic-specific flag

Add a `--limit` default for semantic mode. The existing `findLimit` flag
defaults to `0` (unlimited) for flag-only mode; in semantic mode default to 20.
No new flag needed — just check in the semantic path:

```go
semanticLimit := findLimit
if semanticLimit <= 0 {
    semanticLimit = 20
}
```

### 3c. Route at the top of `runFind`

```go
func runFind(cmd *cobra.Command, args []string) error {
    if len(args) == 1 {
        return runSemanticFind(cmd, args[0])
    }
    // ... existing flag-only path unchanged ...
}
```

### 3d. Implement `runSemanticFind`

```go
func runSemanticFind(cmd *cobra.Command, query string) error {
    cmdCtx, err := openDB(false)
    if err != nil {
        return err
    }
    defer cmdCtx.Close()

    ctx := cmdCtx.Ctx

    provider, err := resolveEmbeddingProvider("", "")
    if err != nil {
        return fmt.Errorf(
            "no embedding provider available: %w\n\nRun 'axon index --embed .' to generate embeddings first.",
            err,
        )
    }
    fmt.Fprintf(os.Stderr, "Using embedding provider: %s\n", provider.Name())

    embedding, err := provider.Embed(ctx, query)
    if err != nil {
        return fmt.Errorf("embedding query: %w", err)
    }

    // Build NodeFilter from flags
    filter := &graph.NodeFilter{}
    if len(findType) == 1 {
        filter.Type = findType[0]
        // Note: multiple --type values are not supported in semantic mode;
        // only the first is used. Print a warning if more than one is set.
    }
    if len(findLabels) > 0 {
        filter.Labels = findLabels
    }
    if len(findExt) > 0 {
        filter.Extensions = findExt
    }

    // Local scope: restrict to CWD URI prefix
    if !findGlobal {
        absPath, err := filepath.Abs(cmdCtx.Cwd)
        if err != nil {
            return err
        }
        filter.URIPrefix = types.PathToURI(absPath)
    }

    semanticLimit := findLimit
    if semanticLimit <= 0 {
        semanticLimit = 20
    }

    results, err := cmdCtx.Storage.FindSimilar(ctx, embedding, semanticLimit, filter)
    if err != nil {
        return fmt.Errorf("similarity search: %w", err)
    }

    if len(results) == 0 {
        fmt.Println("No results found.")
        fmt.Println("Tip: Run 'axon index --embed .' to generate embeddings first.")
        return nil
    }

    return outputSemanticResults(results, findOutput)
}
```

### 3e. Implement `outputSemanticResults`

Add to `find.go` (or a new helper block at the bottom):

```go
func outputSemanticResults(results []*graph.NodeWithScore, format string) error {
    switch format {
    case "json":
        type jsonResult struct {
            Score float32    `json:"score"`
            *graph.Node
        }
        out := make([]jsonResult, len(results))
        for i, r := range results {
            out[i] = jsonResult{Score: r.Score, Node: r.Node}
        }
        enc := json.NewEncoder(os.Stdout)
        enc.SetIndent("", "  ")
        return enc.Encode(out)

    case "table":
        w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
        fmt.Fprintln(w, "Score\tID\tType\tName\tURI")
        for _, r := range results {
            fmt.Fprintf(w, "%.3f\t%s\t%s\t%s\t%s\n",
                r.Score, shortID(r.ID), r.Type, r.Name, truncate(r.URI, 60))
        }
        return w.Flush()

    case "uri":
        for _, r := range results {
            fmt.Printf("%.3f  [%s] %s (%s)\n", r.Score, shortID(r.ID), r.URI, r.Type)
        }
        return nil

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
        return nil
    }
}
```

**Verification:**
```bash
go build ./...
./bin/axon find "error handling"
./bin/axon find "error handling" --type go:func
./bin/axon find "logo" --type vcs:commit --limit 5
./bin/axon find --type go:struct          # flag-only still works
```

---

## Task 4 — Deprecate `search.go`, wire alias

**Files:** `cmd/axon/search.go`, `cmd/axon/main.go`  
**Estimated time:** 5 minutes

Replace the entire content of `search.go` with a minimal deprecated shim:

```go
package main

import (
    "fmt"
    "os"

    "github.com/spf13/cobra"
)

// searchCmd is a deprecated alias for findCmd.
// It prints a one-line deprecation notice then delegates all args/flags to find.
var searchCmd = &cobra.Command{
    Use:        "search [query] [flags]",
    Short:      "Deprecated: use 'axon find' instead",
    Deprecated: "use 'axon find' instead",
    Hidden:     true,
    Args:       cobra.MaximumNArgs(1),
    RunE: func(cmd *cobra.Command, args []string) error {
        fmt.Fprintln(os.Stderr, "Note: 'axon search' is deprecated, use 'axon find' instead.")
        return runFind(cmd, args)
    },
}

func init() {
    // Mirror the flags that find supports so existing invocations don't break.
    searchCmd.Flags().BoolVar(&flagSearchSemantic, "semantic", false, "Deprecated: semantic search is now the default when a query is provided.")
    searchCmd.Flags().StringVar(&findType[0:0:0], "type", "", "Filter by node type")
    searchCmd.Flags().IntVar(&findLimit, "limit", 10, "Maximum number of results")
}
```

Wait — flag aliasing with shared vars is tricky. Simpler: just drop all flags on
the shim and let users who hit errors know to switch to `find`. The shim only
needs to handle the positional-arg case that was most common:

```go
package main

import (
    "fmt"
    "os"

    "github.com/spf13/cobra"
)

var searchCmd = &cobra.Command{
    Use:        "search [query]",
    Short:      "Deprecated: use 'axon find <query>' instead",
    Deprecated: "use 'axon find' instead",
    Hidden:     true,
    Args:       cobra.MaximumNArgs(1),
    RunE: func(cmd *cobra.Command, args []string) error {
        fmt.Fprintln(os.Stderr, "Note: 'axon search' is deprecated — use 'axon find' instead.")
        return runFind(cmd, args)
    },
}
```

In `main.go`, add `rootCmd.AddCommand(searchCmd)` alongside the other commands.
Remove the `rootCmd.AddCommand(searchCmd)` from `search.go`'s `init()` since
we're moving registration to `main.go`.

**Verification:**
```bash
go build ./...
./bin/axon search "error handling"    # prints deprecation notice + results
./bin/axon search --help              # shows deprecated notice
```

---

## Task 5 — Update `README.md`

**File:** `README.md`  
**Estimated time:** 8 minutes

### 5a. Quick reference list (around line 83–89)

Replace:
```
- `axon find` - Search nodes with filters
- `axon search "<question>"` - Natural language code search
- `axon search --semantic "<query>"` - Semantic vector similarity search
```

With:
```
- `axon find` - Search nodes with flags (--type, --name, --ext, --label, …)
- `axon find "<query>"` - Semantic vector similarity search (requires embeddings)
```

### 5b. Rewrite the `axon find` section (around line 328)

Add semantic examples below the existing flag examples:
```bash
# Semantic search (requires 'axon index --embed' first)
axon find "error handling"
axon find "concurrency and goroutines" --type go:func
axon find "recent logo commits"        --type vcs:commit --limit 5
axon find "storage interface design"   --type go:interface
```

### 5c. Replace the `axon search` section (around line 428)

Replace the entire `### axon search` section with:

```markdown
### axon search (deprecated)

`axon search` is deprecated — use `axon find "<query>"` instead.

Semantic search is now built directly into `axon find`. Any positional
text argument triggers vector similarity search:

```bash
axon find "handles token budget overflow"
axon find "error recovery" --type go:func
axon find "commit message about logo" --type vcs:commit
```
```

Keep the embedding provider subsections (Ollama, Hugot) — they are still
relevant — but move them up into the `axon find` section or into a top-level
`## Embeddings` section so they're not orphaned.

**Verification:**
```bash
# Scan for stale references
grep -n "axon search" README.md | grep -v "deprecated\|use.*find"
```
Expected: zero non-deprecated references.

---

## Task 6 — Update `AGENTS.md`

**File:** `AGENTS.md`  
**Estimated time:** 3 minutes

Find the CLI command table (around line 435–440):
```
- `search` - Natural language search (with `--semantic` for vector similarity search)
```

Replace with:
```
- `find` - Search nodes with filters (with `--type`, `--name`, `--ext`, `--global`);
           pass a text argument for semantic vector similarity search:
           `axon find "error handling" --type go:func`
```

Remove any other references to `axon search` that aren't marked deprecated.

**Verification:**
```bash
grep -n "axon search\|search.*semantic" AGENTS.md | grep -v deprecated
```
Expected: zero.

---

## Task 7 — Update `.agents/skills/axon/SKILL.md`

**File:** `.agents/skills/axon/SKILL.md`  
**Estimated time:** 3 minutes

Find the section showing search/find examples (around line 52–70):

Replace all `axon search "…"` lines with `axon find "…"` equivalents:

```bash
# Semantic search (requires embeddings)
axon find "what is the Indexer interface"
axon find "who calls NewNode"
axon find "what implements Storage"
axon find "handles token budget overflow"
axon find "error recovery" --type go:func
```

Remove the `--semantic` flag from all examples — it is now implicit when a
positional query is given.

**Verification:**
```bash
grep -n "axon search" .agents/skills/axon/SKILL.md
```
Expected: zero.

---

## Task 8 — Update `.agents/skills/axon-improve/references/exploration-queries.md`

**File:** `.agents/skills/axon-improve/references/exploration-queries.md`  
**Estimated time:** 3 minutes

Replace every `axon search "…"` occurrence with `axon find "…"`. The queries
themselves stay identical — only the command name changes.

```bash
# Before:
axon search "what is Node"
# After:
axon find "what is Node"
```

**Verification:**
```bash
grep -n "axon search" .agents/skills/axon-improve/references/exploration-queries.md
```
Expected: zero.

---

## Task 9 — Final build, vet, and smoke tests

**Estimated time:** 3 minutes

```bash
# Build and static analysis
go build ./...
go vet ./...

# Unit tests
go test ./...

# Rebuild CLI binary
go build -o ./bin/axon ./cmd/axon

# Smoke tests
./bin/axon find "error handling"                        # semantic, returns results
./bin/axon find "error handling" --type go:func         # semantic + type filter
./bin/axon find "logo" --type vcs:commit --limit 5      # commits
./bin/axon find --type go:struct                        # flag-only, unchanged
./bin/axon find --name main.go                          # flag-only, unchanged
./bin/axon search "error handling"                      # deprecated shim, still works
./bin/axon find --help                                  # shows updated help text
```

---

## Summary of changed files

| File | Change type |
|---|---|
| `adapters/sqlite/sqlite.go` | Fix `FindSimilar` truncation; extend `nodeMatchesFilter` |
| `cmd/axon/find.go` | Add semantic path; add `outputSemanticResults` |
| `cmd/axon/search.go` | Replace with deprecated shim |
| `cmd/axon/main.go` | Add `searchCmd` registration |
| `README.md` | Rewrite find section; deprecate search section |
| `AGENTS.md` | Update CLI command table |
| `.agents/skills/axon/SKILL.md` | Replace `axon search` → `axon find` |
| `.agents/skills/axon-improve/references/exploration-queries.md` | Replace `axon search` → `axon find` |
