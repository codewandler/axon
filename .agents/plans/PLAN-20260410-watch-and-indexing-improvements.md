# Plan: Watch & Indexing Improvements

**Design ref**: `DESIGN-20260410-watch-and-indexing-improvements.md`  
**Estimated total**: ~25 minutes  
**Parallelisation**: Wave 1 (Tasks 1 & 2) runs concurrently; Wave 2 (Tasks 3 & 4) runs concurrently after Wave 1; Wave 3 (Task 5) runs last.

---

## Wave 1 — Independent tasks (run in parallel)

---

### Task 1 — `indexer/fs/indexer.go`: Config, include/exclude, export ShouldIgnore

**Files modified**: `indexer/fs/indexer.go`  
**Estimated time**: 8 minutes

**What to do**:

#### 1a. Update `Config` struct

Replace:
```go
type Config struct {
    Ignore []string
}
```
With:
```go
type Config struct {
    // Include contains glob patterns. When non-empty, only paths matching
    // at least one pattern are indexed (applied to files only; directories
    // are always traversed). Patterns matched against name and full path.
    Include []string

    // Exclude contains glob patterns to skip. Matched against file name
    // and full absolute path. Takes precedence over Include.
    Exclude []string

    // Ignore is a deprecated alias for Exclude. Merged with Exclude in New().
    Ignore []string
}
```

#### 1b. Update `New()` to merge Ignore into Exclude

```go
func New(cfg Config) *Indexer {
    // Merge deprecated Ignore into Exclude so callers using either field work.
    cfg.Exclude = append(cfg.Exclude, cfg.Ignore...)
    cfg.Ignore = nil
    return &Indexer{
        config: cfg,
        tagger: tagger.New(tagger.Config{}),
    }
}
```

#### 1c. Replace `shouldIgnore` with exported `ShouldIgnore` + `shouldInclude`

Remove the old `shouldIgnore` entirely. Add these two methods:

```go
// ShouldIgnore reports whether path should be excluded from indexing.
// Exported so the watcher can apply the same logic to fsnotify events.
func (i *Indexer) ShouldIgnore(path, name string) bool {
    for _, pattern := range i.config.Exclude {
        if matched, _ := filepath.Match(pattern, name); matched {
            return true
        }
        if matched, _ := filepath.Match(pattern, path); matched {
            return true
        }
    }
    return false
}

// shouldInclude reports whether path passes the include filter.
// Always returns true when no include patterns are configured.
// Applied to files only; directories are never filtered by include patterns.
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

#### 1d. Update `Index` walk to use new methods

In the `filepath.WalkDir` callback, replace the `i.shouldIgnore(path, d.Name())` call:

```go
// Check exclude patterns first (takes precedence over include)
if i.ShouldIgnore(path, d.Name()) {
    if d.IsDir() {
        entries = append(entries, discoveredEntry{
            path:    path,
            entry:   d,
            ignored: true,
        })
        return filepath.SkipDir
    }
    return nil
}

// Check include filter (files only — directories always traversed)
if !d.IsDir() && !i.shouldInclude(path, d.Name()) {
    return nil
}
```

Also remove the old blanket dot check (the entire `if strings.HasPrefix(name, ".") { return true }` block is already gone by removing `shouldIgnore`).

**Verification**:
```bash
cd /home/timo/projects/axon
go build ./indexer/fs/...
go test ./indexer/fs/...
```

---

### Task 2 — CLI restructure: rename `init`→`index`, add `--watch`, delete `watch.go`

**Files modified**: `cmd/axon/main.go`  
**Files created**: `cmd/axon/index.go`  
**Files deleted**: `cmd/axon/init.go`, `cmd/axon/watch.go`  
**Estimated time**: 8 minutes

#### 2a. Create `cmd/axon/index.go`

This is `cmd/axon/init.go` with the following changes:
- Variable names: `initCmd` → `indexCmd`, `flagNoGC`/`flagEmbed` stay the same
- Command `Use`: `"init [path]"` → `"index [path]"`
- Command `Short`/`Long`: update wording accordingly
- Add `Aliases: []string{"init"}` for backward compatibility
- Add new flag vars and flags:
  ```go
  var (
      flagNoGC          bool
      flagEmbed         bool
      flagWatch         bool
      flagWatchDebounce time.Duration
      flagWatchQuiet    bool
  )
  ```
  And in `func init()`:
  ```go
  indexCmd.Flags().BoolVar(&flagWatch,           "watch",          false,                "watch for changes and re-index automatically (Ctrl+C to stop)")
  indexCmd.Flags().DurationVar(&flagWatchDebounce, "watch-debounce", 150*time.Millisecond, "debounce window for change events (only with --watch)")
  indexCmd.Flags().BoolVar(&flagWatchQuiet,       "watch-quiet",    false,                "suppress per-change output (only with --watch)")
  ```
- Rename `runInit` → `runIndex`
- At the end of `runIndex`, after printing the index summary, add the watch loop:
  ```go
  if !flagWatch {
      return nil
  }

  // Enter watch mode
  fmt.Fprintf(os.Stderr, "Watching %s — press Ctrl+C to stop.\n", absPath)
  watchCtx, watchCancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
  defer watchCancel()

  return ax.Watch(watchCtx, absPath, axon.WatchOptions{
      IndexOptions: axon.IndexOptions{SkipGC: flagNoGC},
      Debounce:     flagWatchDebounce,
      OnReindex: func(path string, result *axon.IndexResult, err error) {
          if err != nil {
              fmt.Fprintf(os.Stderr, "✗  re-index error: %v\n", err)
              return
          }
          if !flagWatchQuiet {
              rel, _ := filepath.Rel(absPath, path)
              if rel == "" || rel == "." {
                  rel = "."
              }
              fmt.Fprintf(os.Stderr, "↻  ./%s — %d files\n", rel, result.Files)
          }
      },
  })
  ```
- Add missing imports: `"os/signal"`, `"syscall"` (and `axon` package import if not already present — it is)
- Keep `runIndexWithProgress` (renamed from `runInitWithProgress`)

#### 2b. Delete `cmd/axon/init.go`
```bash
rm cmd/axon/init.go
```

#### 2c. Delete `cmd/axon/watch.go`
```bash
rm cmd/axon/watch.go
```

#### 2d. Update `cmd/axon/main.go`

- Replace `rootCmd.AddCommand(initCmd)` → `rootCmd.AddCommand(indexCmd)`
- Remove `rootCmd.AddCommand(watchCmd)`
- Remove the `watchCmd` line entirely

**Verification**:
```bash
cd /home/timo/projects/axon
go build ./cmd/axon/...
```

---

## Wave 2 — After Wave 1 completes (run in parallel)

---

### Task 3 — `axon.go`: DefaultFSIgnore, Config, New(), Watch rewrite, DeleteByPath

**Files modified**: `axon.go`  
**Estimated time**: 15 minutes  
**Depends on**: Task 1 (uses `fs.Indexer.ShouldIgnore`)

#### 3a. Update `DefaultFSIgnore`

Replace the existing `DefaultFSIgnore` var with:
```go
// DefaultFSIgnore contains the default patterns to exclude when indexing.
// Dot-prefixed paths are no longer blanket-excluded; specific entries are
// listed here instead so that useful dotfiles (.agents/, .claude/, etc.)
// are indexed.
var DefaultFSIgnore = []string{
    // Version control internals — indexed as marker dirs for deletion detection
    ".git",

    // Build and dependency output
    "node_modules",
    "__pycache__",
    "target",        // Rust/Cargo
    "vendor",        // Go vendor, PHP Composer
    "venv",          // Python virtualenvs
    "env",           // Python virtualenvs (alt)
    "dist",          // JS/TS build output
    "build",         // generic build output
    "site-packages", // Python packages

    // Tool-specific directories
    ".devspace",
    ".DS_Store",

    // Log files (often large, low signal)
    "*.log",
}
```

#### 3b. Update `Config` struct

Replace:
```go
// FSIgnore contains glob patterns to ignore when indexing filesystem.
FSIgnore []string
```
With:
```go
// FSExclude contains glob patterns to exclude from indexing.
// When empty, DefaultFSIgnore is used. To index everything (no defaults),
// set FSExclude to a non-nil empty slice: []string{}.
// Patterns are matched against file name and full path.
FSExclude []string

// FSInclude contains glob patterns to include. When non-empty, only files
// matching at least one pattern are indexed (directories always traversed).
FSInclude []string

// FSIgnore is a deprecated alias for FSExclude.
// If both are set they are merged (union). Prefer FSExclude.
FSIgnore []string
```

#### 3c. Update `New()` — pass Include/Exclude to fs.Config

Replace the block:
```go
ignore := cfg.FSIgnore
if len(ignore) == 0 {
    ignore = DefaultFSIgnore
}
idxRegistry.Register(fs.New(fs.Config{Ignore: ignore}))
```
With:
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

#### 3d. Add `DeleteByPath` method

Add after `RegisterIndexer`:
```go
// DeleteByPath removes the graph node for the given filesystem path and all
// edges referencing it. If no node exists for the path it returns nil.
// For directory paths it removes all nodes whose URI has the directory's
// URI as a prefix (i.e. the whole subtree).
func (a *Axon) DeleteByPath(ctx context.Context, path string) error {
    absPath, err := filepath.Abs(path)
    if err != nil {
        return fmt.Errorf("resolving path %q: %w", path, err)
    }
    uri := types.PathToURI(absPath)
    if err := a.storage.DeleteByURIPrefix(ctx, uri); err != nil {
        return fmt.Errorf("deleting nodes for %s: %w", uri, err)
    }
    if _, err := a.storage.DeleteOrphanedEdges(ctx); err != nil {
        return fmt.Errorf("cleaning orphaned edges after delete: %w", err)
    }
    return nil
}
```

#### 3e. Rewrite `Watch` — file-level re-indexing

Replace the entire `Watch` method body. Key changes:
- `pending` becomes `map[string]fsnotify.Op` (tracks last op per path)
- In the event case: `pending[event.Name] |= event.Op`
- Drop the blanket dot filter; use `fsIdx.ShouldIgnore` instead
- In the debounce case: iterate pending, dispatch per path

The new debounce handler replaces:
```go
case <-debounce:
    reindexRoot := watchCommonAncestorDir(pending, absPath)
    pending = make(map[string]struct{})
    debounce = nil
    reindexOpts := opts.IndexOptions
    reindexOpts.Path = reindexRoot
    res, rerr := a.IndexWithOptions(ctx, reindexOpts)
    if opts.OnReindex != nil {
        opts.OnReindex(reindexRoot, res, rerr)
    }
```
With:
```go
case <-debounce:
    if len(pending) == 0 {
        debounce = nil
        continue
    }
    snapshot := pending
    pending = make(map[string]fsnotify.Op)
    debounce = nil

    for changedPath, op := range snapshot {
        var res *axon.IndexResult  // local alias not needed — this IS axon package
        var rerr error

        if op&(fsnotify.Remove|fsnotify.Rename) != 0 {
            // File/dir removed — delete from graph
            rerr = a.DeleteByPath(ctx, changedPath)
            // Also stop watching the directory if it was one
            _ = watcher.Remove(changedPath)
            if opts.OnReindex != nil {
                opts.OnReindex(changedPath, &IndexResult{}, rerr)
            }
            continue
        }

        // File/dir created or modified
        info, statErr := os.Stat(changedPath)
        if statErr != nil {
            // Disappeared between event and debounce — treat as removal
            _ = a.DeleteByPath(ctx, changedPath)
            continue
        }

        reindexOpts := opts.IndexOptions
        reindexOpts.Path = changedPath

        if info.IsDir() {
            // New directory — watch it and index it
            if watchErr := watcher.Add(changedPath); watchErr != nil {
                log.Printf("axon: watch: failed to watch new dir %s: %v", changedPath, watchErr)
            }
        }

        res, rerr = a.IndexWithOptions(ctx, reindexOpts)
        if opts.OnReindex != nil {
            opts.OnReindex(changedPath, res, rerr)
        }
    }
```

Also update the watcher's initial walk (directory registration) to apply `ShouldIgnore`:
```go
// Retrieve the fs indexer to reuse its ShouldIgnore logic in the watcher.
var fsIdx *fspkg.Indexer
if raw := a.indexers.ByName("fs"); raw != nil {
    fsIdx, _ = raw.(*fspkg.Indexer)
}

shouldSkipWatch := func(p string) bool {
    if skipDir(p) {
        return true
    }
    if fsIdx != nil && fsIdx.ShouldIgnore(p, filepath.Base(p)) {
        return true
    }
    return false
}

if err := filepath.WalkDir(absPath, func(p string, d iofs.DirEntry, err error) error {
    if err != nil { return nil }
    if d.IsDir() {
        if shouldSkipWatch(p) { return iofs.SkipDir }
        if watchErr := watcher.Add(p); watchErr != nil {
            log.Printf("axon: watch: failed to watch %s: %v", p, watchErr)
        }
    }
    return nil
}); err != nil {
    return fmt.Errorf("walking directory for watch: %w", err)
}
```

Also update the event filter in the select loop:
```go
// OLD:
if base := filepath.Base(event.Name); strings.HasPrefix(base, ".") {
    continue
}
// NEW:
if fsIdx != nil && fsIdx.ShouldIgnore(event.Name, filepath.Base(event.Name)) {
    continue
}
if skipDir(event.Name) {
    continue
}
```

Add import for fs package:
```go
fspkg "github.com/codewandler/axon/indexer/fs"
```

Also add `ByName` method to the indexer registry if it doesn't exist (check first with grep).

#### 3f. Delete `watchCommonAncestorDir` and `watchLongestCommonPathPrefix`

Remove both functions entirely from `axon.go` (they are no longer needed).

**Verification**:
```bash
cd /home/timo/projects/axon
go build ./...
go test ./...
```

---

### Task 4 — `../flai/plugins/axon/plugin.go`: FSInclude/FSExclude config fields

**Files modified**: `../flai/plugins/axon/plugin.go`  
**Estimated time**: 5 minutes  
**Depends on**: Task 3 (new `goaxon.Config` fields)

#### 4a. Add fields to `Config`

```go
type Config struct {
    DBPath            string
    EmbeddingProvider embeddings.Provider
    NoAutoIndex       bool

    // FSExclude overrides the default exclude patterns for the filesystem indexer.
    // Leave nil to use axon.DefaultFSIgnore.
    FSExclude []string

    // FSInclude restricts indexing to files matching these glob patterns.
    // Leave nil to index all non-excluded files.
    FSInclude []string
}
```

#### 4b. Pass fields through in `Init`

In the `goaxon.New(goaxon.Config{...})` call inside `Init`:
```go
client, err := goaxon.New(goaxon.Config{
    Dir:               workDir,
    Storage:           storage,
    EmbeddingProvider: p.cfg.EmbeddingProvider,
    FSExclude:         p.cfg.FSExclude,
    FSInclude:         p.cfg.FSInclude,
})
```

#### 4c. Pass fields through in `EnsureLocal`

Same change in the `goaxon.New(goaxon.Config{...})` call inside `EnsureLocal`.

**Verification**:
```bash
cd /home/timo/projects/flai
go build ./...
```

---

## Wave 3 — Final verification

### Task 5 — Build and test everything

```bash
cd /home/timo/projects/axon
go build ./...
go test ./...
go install ./cmd/axon

cd /home/timo/projects/flai
go build ./...
```

Confirm:
- [ ] `axon index .` works
- [ ] `axon index --watch .` enters watch mode
- [ ] `axon init .` still works (alias)
- [ ] `axon watch` is gone (gives "unknown command" error)
- [ ] All tests pass

---

## Execution order

```
Wave 1:  [Agent A: Task 1]  [Agent B: Task 2]   ← parallel
              ↓                    ↓
Wave 2:  [Agent C: Task 3]  [Agent D: Task 4]   ← parallel (Task 3 after Task 1; Task 4 after Task 3's Config)
              ↓
Wave 3:  [Main: Task 5 — build + test]
```
