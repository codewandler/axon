# Design: Watch & Indexing Improvements

**Date**: 2026-04-10  
**Status**: Draft  
**Scope**: `axon.go`, `indexer/fs/indexer.go`, `cmd/axon/init.go`, `cmd/axon/watch.go`, `cmd/axon/main.go`

---

## Problems

### 1. Watcher re-indexes whole directories on single-file changes

When any file changes in `/project/foo/`, the watcher computes the common ancestor directory (`/project/foo/`) and calls `IndexWithOptions` with that directory as root. This triggers a full `filepath.WalkDir` of the entire subtree — even if only one file changed.

**Root cause** (`axon.go`, `Watch`):
```go
reindexRoot := watchCommonAncestorDir(pending, absPath)
// ...
a.IndexWithOptions(ctx, reindexOpts)  // walks entire subtree
```

**Impact**: Editing a single `.go` file in a large directory causes indexing of all files in that subtree. Slow, wasteful.

---

### 2. Blanket dot-exclusion hides useful dotfiles

`shouldIgnore` in `indexer/fs/indexer.go` unconditionally excludes **all** files and directories whose name starts with `.`:

```go
if strings.HasPrefix(name, ".") {
    return true
}
```

This was added to exclude things like `.devspace` and `.DS_Store`, but it also silently hides `.agents/`, `.claude/`, `.env` (legitimate config), and any other dotfile a project might use meaningfully.

The `DefaultFSIgnore` comment even acknowledges this:
> "All hidden files/dirs (names starting with '.') are unconditionally excluded by the FS indexer, so they don't need to be listed here."

**Impact**: `.agents/` plans and context files are invisible to the graph. Users cannot query or search them.

---

### 3. No include-pattern support for programmatic callers

`axon.Config` only supports `FSIgnore []string` (exclusion). Programmatic callers like `flai` (via `AxonPlugin`) have no way to say "only index these file types" or "include only these paths." They can only exclude.

---

### 4. `axon watch` is a redundant top-level command

`axon init` and `axon watch` both index a directory; `watch` just keeps running. This creates two entry points for the same workflow. The mental model should be:
- `axon index` — index once
- `axon index --watch` — index and keep watching

---

## Proposed Changes

---

### Change 1: File-level re-indexing in Watch

**Strategy**: Classify each `fsnotify` event by operation type and path type, then take the minimal action needed.

#### Data model change

Replace `pending map[string]struct{}` with `pending map[string]fsnotify.Op`:

```go
pending := make(map[string]fsnotify.Op)
// on each event:
pending[event.Name] |= event.Op   // accumulate ops (e.g. Write|Rename)
```

This lets us know, per path, what happened (created, modified, deleted).

#### Debounce handler logic

After the debounce window elapses, iterate `pending` and dispatch per path:

```
for each (path, ops) in pending:
    if ops has Remove or Rename:
        → call ax.DeleteByPath(ctx, path)
        → if path was a directory: remove it from the fsnotify watcher
    else:
        stat(path)
        if stat error (file disappeared):
            → call ax.DeleteByPath(ctx, path)
        else if path is a directory:
            → ensure watcher covers new dir: watcher.Add(path)
            → call ax.IndexWithOptions(ctx, {Path: path})
        else (regular file):
            → call ax.IndexWithOptions(ctx, {Path: path})
```

#### New method: `ax.DeleteByPath`

```go
// DeleteByPath removes a node (and its edges) from the graph by filesystem path.
// Emits EventNodeDeleting so subscribers (git indexer, etc.) can react.
// Returns nil if the node does not exist (already deleted).
func (a *Axon) DeleteByPath(ctx context.Context, path string) error
```

Implementation:
1. Compute URI from path: `uri := types.PathToURI(absPath)`
2. Look up node by URI: `node, err := a.storage.GetNodeByURI(ctx, uri)`
3. If not found: return nil (already gone)
4. Emit `EventNodeDeleting` to any active event channel
5. `a.storage.DeleteByURIPrefix(ctx, uri)` — removes the node and any sub-nodes if it was a directory
6. `a.storage.DeleteOrphanedEdges(ctx)`

#### Single-file indexing via `IndexWithOptions`

`filepath.WalkDir` on a **file path** (not a directory) visits exactly that one file — this is standard Go stdlib behaviour. So `IndexWithOptions` with a file path already works for single-file indexing. No changes needed to `IndexWithOptions` itself.

**Staleness scoping**: when `ictx.Root = file:///path/to/foo.go`, the cleanup call `FindStaleByURIPrefix(ctx, ictx.Root, generation)` only finds stale nodes for that exact file URI — correct and efficient.

**Downstream indexers**: the FS indexer emits `EventEntryVisited` for the single file, which triggers golang/markdown indexers via their subscriptions. These indexers run `HandleEvent` for that one file — they already have correct per-file scoping.

#### Remove the common-ancestor fallback

`watchCommonAncestorDir` is no longer needed. Remove it along with `watchLongestCommonPathPrefix`.

#### Watcher event filter update

The current filter:
```go
if base := filepath.Base(event.Name); strings.HasPrefix(base, ".") {
    continue
}
```
Must be updated to use the configured ignore/exclude patterns instead (see Change 2), so that the watcher's exclusion logic matches the indexer's.

---

### Change 2: Remove blanket dot exclusion; add explicit exclusions + `*.log`

#### In `indexer/fs/indexer.go`

Remove from `shouldIgnore`:
```go
// DELETE these lines:
if strings.HasPrefix(name, ".") {
    return true
}
```

#### In `axon.go` — update `DefaultFSIgnore`

```go
var DefaultFSIgnore = []string{
    // Version control internals (indexed as ignored dirs for deletion detection)
    ".git",

    // Build and dependency output
    "node_modules",
    "__pycache__",
    "target",
    "vendor",
    "venv",
    "env",
    "dist",
    "build",
    "site-packages",

    // Tool-specific directories that should not be indexed
    ".devspace",
    ".DS_Store",

    // Log files (often large, low signal)
    "*.log",
}
```

**Note on `.git`**: The FS indexer treats ignored *directories* specially — they are added to `entries` with `ignored: true` (see `discoveredEntry`), which creates a minimal node for deletion detection and emits `EventEntryVisited` so the git indexer can react. Adding `.git` to `DefaultFSIgnore` preserves this behaviour exactly. No change needed to the git indexer.

#### Update watcher event filter

Replace the blanket dot check with the configured `shouldIgnore`:

```go
// OLD (in Watch loop):
if base := filepath.Base(event.Name); strings.HasPrefix(base, ".") {
    continue
}

// NEW: use the fs indexer's shouldIgnore so watcher and indexer agree
fsIdx := a.indexers.ByName("fs").(*fs.Indexer)
if fsIdx.ShouldIgnore(event.Name, filepath.Base(event.Name)) {
    continue
}
```

This requires exporting `shouldIgnore` → `ShouldIgnore` (public method on `fs.Indexer`).

---

### Change 3: Include/Exclude glob patterns

#### `indexer/fs/indexer.go` — `Config`

```go
type Config struct {
    // Include contains glob patterns. When non-empty, only paths matching
    // at least one pattern are indexed. Patterns are matched against both
    // the file name and the full absolute path.
    Include []string

    // Exclude contains glob patterns to skip. Matched against file name
    // and full absolute path. Takes precedence over Include.
    // Previously called Ignore (still accepted for backward compatibility).
    Exclude []string

    // Ignore is a deprecated alias for Exclude. Prefer Exclude.
    // If both are set, they are merged (union of exclusions).
    Ignore []string
}
```

Updated `shouldIgnore` (renamed to `shouldExclude`, kept as `ShouldIgnore` for the exported alias):

```go
// ShouldIgnore returns true if the given path should be excluded from indexing.
// Exported so the watcher can use the same logic.
func (i *Indexer) ShouldIgnore(path, name string) bool {
    exclude := append(i.config.Exclude, i.config.Ignore...)
    for _, pattern := range exclude {
        if matched, _ := filepath.Match(pattern, name); matched {
            return true
        }
        if matched, _ := filepath.Match(pattern, path); matched {
            return true
        }
    }
    return false
}

// shouldInclude returns true if the path passes the include filter.
// If no include patterns are configured, all paths pass.
func (i *Indexer) shouldInclude(path, name string) bool {
    if len(i.config.Include) == 0 {
        return true
    }
    for _, pattern := range i.config.Include {
        if matched, _ := filepath.Match(pattern, name); matched {
            return true
        }
        if matched, _ := filepath.Match(pattern, path); matched {
            return true
        }
    }
    return false
}
```

Decision order in `Index`'s walk:
1. Check `ShouldIgnore` first (exclude wins over include)
2. Then check `shouldInclude`

#### `axon.go` — `Config`

```go
type Config struct {
    Dir     string
    Storage graph.Storage

    // FSExclude contains glob patterns to exclude from indexing.
    // When non-empty, merged with DefaultFSIgnore (defaults apply unless
    // you set FSIgnore = []string{} to clear them).
    // Patterns are matched against file name and full path.
    FSExclude []string

    // FSInclude contains glob patterns to include. When non-empty, only
    // files matching at least one pattern are indexed (after exclusion).
    // Directories are always traversed regardless of this filter.
    FSInclude []string

    // FSIgnore is a deprecated alias for FSExclude. Kept for backward
    // compatibility. If both FSIgnore and FSExclude are set, they are merged.
    FSIgnore []string

    EmbeddingProvider embeddings.Provider
    GitConfig         git.Config
}
```

In `New(cfg Config)`:
```go
exclude := append(cfg.FSExclude, cfg.FSIgnore...)
if len(exclude) == 0 {
    exclude = DefaultFSIgnore
}
idxRegistry.Register(fs.New(fs.Config{
    Include: cfg.FSInclude,
    Exclude: exclude,
}))
```

#### Watcher directory walk — apply include/exclude

When the watcher initially walks to register directories with `fsnotify.Watcher.Add`, it should also apply the exclude patterns so it doesn't watch e.g. `node_modules/`:

```go
filepath.WalkDir(absPath, func(p string, d iofs.DirEntry, err error) error {
    if d.IsDir() {
        if skipDir(p) { return iofs.SkipDir }
        if fsIdx.ShouldIgnore(p, filepath.Base(p)) { return iofs.SkipDir }
        watcher.Add(p)
    }
    return nil
})
```

#### flai `AxonPlugin` — new config fields

In `../flai/plugins/axon/plugin.go`, `Config` gains:

```go
type Config struct {
    DBPath            string
    EmbeddingProvider embeddings.Provider
    NoAutoIndex       bool

    // FSExclude overrides the default exclude patterns.
    // Leave nil to use axon.DefaultFSIgnore.
    FSExclude []string

    // FSInclude restricts indexing to files matching these patterns.
    // Leave nil to index all non-excluded files.
    FSInclude []string
}
```

Passed through to `goaxon.Config` in `Init` and `EnsureLocal`.

---

### Change 4: CLI — rename `init` → `index`, add `--watch` flag, remove `watch` command

#### `cmd/axon/init.go` → `cmd/axon/index.go`

- File renamed
- Variable `initCmd` → `indexCmd`
- Command `Use` field: `"init [path]"` → `"index [path]"` (add `Aliases: []string{"init"}` for backward compatibility)
- Function `runInit` → `runIndex`
- Add flags:
  ```go
  indexCmd.Flags().BoolVar(&flagWatch,          "watch",          false, "watch for changes and re-index automatically after initial index")
  indexCmd.Flags().DurationVar(&flagWatchDebounce, "watch-debounce", 150*time.Millisecond, "debounce window for file-change events (only with --watch)")
  indexCmd.Flags().BoolVar(&flagWatchQuiet,     "watch-quiet",    false, "suppress per-change output (only with --watch)")
  ```
- New vars `flagWatch bool`, `flagWatchDebounce time.Duration`, `flagWatchQuiet bool`

`runIndex` logic at end (after printing summary):
```go
if !flagWatch {
    return nil
}
// Enter watch mode
ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
defer cancel()
if !flagWatchQuiet {
    fmt.Fprintf(os.Stderr, "Watching %s — press Ctrl+C to stop.\n", absPath)
}
return ax.Watch(ctx, absPath, axon.WatchOptions{
    IndexOptions: axon.IndexOptions{SkipGC: flagNoGC},
    Debounce:     flagWatchDebounce,
    OnReindex: func(path string, result *axon.IndexResult, err error) {
        if err != nil {
            fmt.Fprintf(os.Stderr, "✗  re-index error: %v\n", err)
            return
        }
        if !flagWatchQuiet {
            rel, _ := filepath.Rel(absPath, path)
            fmt.Fprintf(os.Stderr, "↻  %s — %d files\n", rel, result.Files)
        }
    },
})
```

#### `cmd/axon/watch.go`

**Delete this file.**

#### `cmd/axon/main.go`

```go
// Change:
rootCmd.AddCommand(initCmd)   // → indexCmd
// Remove:
rootCmd.AddCommand(watchCmd)
```

---

## Affected Files

| File | Change |
|------|--------|
| `axon.go` | `DefaultFSIgnore` update; `Config` new fields; `Watch` rewrite (file-level); add `DeleteByPath`; remove `watchCommonAncestorDir` + `watchLongestCommonPathPrefix` |
| `indexer/fs/indexer.go` | `Config` rename fields; remove dot rule; export `ShouldIgnore`; add `shouldInclude` |
| `cmd/axon/init.go` → `cmd/axon/index.go` | Rename file and command; add `--watch` flag and watch loop |
| `cmd/axon/watch.go` | **Delete** |
| `cmd/axon/main.go` | Register `indexCmd` not `initCmd`; remove `watchCmd` |
| `../flai/plugins/axon/plugin.go` | Add `FSInclude`, `FSExclude` to `Config`; pass through to `goaxon.Config` |

---

## Out of Scope

- Changing how `GetNodeByURI` works in the SQLite adapter
- Changing the golang or markdown indexers' internal staleness handling
- Recursive glob patterns (`**`) in include/exclude — `filepath.Match` is non-recursive; this is a known limitation to document
- `axon search` command changes

---

## Open Questions

1. **Backward compat for `axon init`**: Keep as an alias (via `Aliases: []string{"init"}`) or do a hard rename? The alias approach is safer for scripts.
2. **Include patterns and directories**: Should include patterns apply to directories (preventing descent) or only to files? Recommended: apply only to files; always descend into non-excluded directories.
3. **`DeleteByPath` on a directory path**: Should it recursively delete all nodes under that directory's URI prefix? Yes — use `DeleteByURIPrefix(ctx, dirURI)` which the storage layer already supports.
4. **Event channel lifetime in `DeleteByPath`**: `DeleteByPath` needs to emit events but `Watch` manages the event channel internally. Options:
   - Make event emission optional (best-effort via a registered callback)
   - Accept an event channel as parameter
   - Recommended: `Watch` continues to own events; `DeleteByPath` just does storage deletion (subscribers will eventually see stale nodes cleaned up by next GC cycle, or by the follow-up `IndexWithOptions` call if the parent dir is re-indexed)
