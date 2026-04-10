# Axon - Agent Guidelines

Graph-based storage system for AI agent context management, retrieval, and exploration.

## Build & Test Commands

```bash
# Build all packages
go build ./...

# Build CLI binary (output to ./bin/)
go build -o ./bin/axon ./cmd/axon

# Run all tests
go test ./...

# Run tests with verbose output
go test -v ./...

# Run a single test by name
go test -v -run TestAxonIndex ./...

# Run tests in a specific package
go test -v ./adapters/sqlite

# Run a single test in a specific package
go test -v -run TestDeleteStaleNodes ./adapters/sqlite

# Run tests with race detection
go test -race ./...

# Check for lint issues (if golangci-lint is installed)
golangci-lint run

# Format code
gofmt -w .

# Install CLI globally — always use task, not go install directly.
# go install skips -ldflags and leaves the version as "dev".
task install
```

## Project Structure

```
axon/
├── axon.go              # Main Axon type and IndexWithProgress
├── graph/               # Core graph types (Node, Edge, Storage interface)
├── adapters/sqlite/     # SQLite storage implementation (also supports :memory: mode)
├── storage/             # Storage error types
├── aql/                 # AQL (Axon Query Language) - parser, AST, compiler
│   ├── parser.go        # AQL parser using participle
│   ├── ast.go           # AST node types
│   ├── builder.go       # Fluent builder API for programmatic queries
│   ├── validate.go      # Query validation
│   ├── doc.go           # Package documentation with examples
│   └── grammar.md       # Full AQL grammar specification
├── indexer/             # Indexer interface, registry, events, emitter
│   ├── fs/              # Filesystem indexer
│   ├── git/             # Git repository indexer
│   ├── markdown/        # Markdown document indexer
│   ├── golang/          # Go source code indexer
│   ├── project/         # Project manifest indexer
│   ├── tagger/          # Rule-based label tagger
│   └── embeddings/      # Embedding providers (Ollama, Hugot, null)
├── context/             # Context engine for AI agents (budget, walker, source, format)
├── types/               # Node/edge type definitions (fs, vcs, markdown)
├── progress/            # Progress reporting (coordinator, bubbletea UI)
├── render/              # Tree rendering utilities
├── cmd/axon/            # CLI commands (init, query, tree, find, show, etc.)
└── cmd/axontui/         # Terminal UI explorer
```

## Code Style Guidelines

### Imports

Group imports in this order, separated by blank lines:
1. Standard library
2. External packages
3. Internal packages (github.com/codewandler/axon/...)

```go
import (
    "context"
    "path/filepath"
    "sync"

    "github.com/go-git/go-git/v5"

    "github.com/codewandler/axon/graph"
    "github.com/codewandler/axon/indexer"
)
```

### Naming Conventions

- **Types**: PascalCase (`Config`, `IndexResult`, `GraphEmitter`)
- **Interfaces**: PascalCase, often noun-based (`Storage`, `Indexer`, `Emitter`)
- **Functions/Methods**: PascalCase for exported, camelCase for internal
- **Constants**: PascalCase for exported, camelCase for internal
- **Node types**: Use `domain:name` format (`fs:file`, `fs:dir`, `vcs:repo`)
- **Edge types**: Use snake_case, prefer generic edges (`contains`, `has`, `belongs_to`)

### Error Handling

- Use sentinel errors for common cases:
  ```go
  var ErrNodeNotFound = errors.New("node not found")
  ```
- Wrap errors with context using `fmt.Errorf("...: %w", err)`
- Check errors immediately after function calls
- Use `errors.Is()` for sentinel error comparison

### Structs and Methods

- Use pointer receivers for methods that modify state
- Use value receivers for simple getters
- Builder pattern with `With*` methods returning `*T` for chaining:
  ```go
  node := graph.NewNode("fs:file").
      WithURI("file:///path").
      WithKey("/path").
      WithData(data)
  ```

### Context Usage

- Always pass `context.Context` as first parameter
- Use `ctx` as the parameter name
- Create indexer-specific context types for domain data:
  ```go
  type Context struct {
      Root       string
      Generation string
      Graph      *graph.Graph
      Emitter    Emitter
  }
  ```

### Testing

- Use `t.Helper()` in test helper functions
- Use `t.TempDir()` for temporary directories (auto-cleaned)
- Use `t.Cleanup()` for deferred cleanup
- Use table-driven tests for multiple cases
- Name test functions as `TestFunctionName` or `TestType_Method`

```go
func setupTestDB(t *testing.T) *Storage {
    t.Helper()
    dir := t.TempDir()
    s, err := New(filepath.Join(dir, "test.db"))
    if err != nil {
        t.Fatalf("New failed: %v", err)
    }
    t.Cleanup(func() { s.Close() })
    return s
}
```

### Concurrency

- Use `sync.Mutex` for simple locking
- Use `sync.RWMutex` when reads greatly outnumber writes
- Use channels for communication between goroutines
- Use `sync.WaitGroup` for coordinating goroutine completion

### Storage Interface

All storage implementations must implement `graph.Storage`:
- `PutNode`, `GetNode`, `DeleteNode`
- `PutEdge`, `GetEdge`, `DeleteEdge`
- `GetEdgesFrom`, `GetEdgesTo`
- `FindNodes`
- `FindStaleByURIPrefix`, `DeleteStaleByURIPrefix`, `DeleteByURIPrefix` - for indexer-owned cleanup
- `DeleteStaleEdges`, `DeleteOrphanedEdges` - framework-level cleanup
- `Flush`

SQLite adapter uses buffered writes (5000 items or 100ms) for performance.
Always call `Flush()` before reads if writes are buffered.

### Indexer Interface

Indexers must implement:
- `Name() string` - identifier (e.g., "fs", "git")
- `Schemes() []string` - URI schemes handled
- `Handles(uri string) bool` - can process this URI?
- `Subscriptions() []Subscription` - events to subscribe to
- `Index(ctx, ictx) error` - perform indexing

### AQL (Axon Query Language)

AQL is a SQL-like query language with graph pattern matching capabilities:

**Phase 1 - Table Queries** (✅ implemented):
- SELECT with WHERE, GROUP BY, HAVING, ORDER BY, LIMIT, OFFSET
- JSON field access: `data.ext = 'go'`
- Label operations: `CONTAINS ANY/ALL`, `NOT CONTAINS`
- All standard operators: `=`, `!=`, `<`, `>`, `LIKE`, `GLOB`, `IN`, `BETWEEN`, `IS NULL`

**Phase 2 - Pattern Queries** (✅ implemented):
- Node patterns: `(var:type)`
- Edge directions: `->`, `<-`, `-` (undirected)
- Multi-type edges: `[:contains|has]`
- Edge variables: `[e:contains]`
- Multiple patterns with shared variables
- WHERE with variable resolution: `file.data.ext = 'go'`
- ORDER BY, GROUP BY with patterns

**Phase 3 - Variable-Length Paths** (✅ implemented):
- Bounded: `[:type*1..3]`
- Exact: `[:type*2]`
- Unbounded: `[:type*2..]`
- Uses SQLite recursive CTEs for efficient traversal

**Phase 4 - Table Functions** (✅ implemented):
- `json_each(column)` - unpack JSON arrays into rows
- Syntax: `FROM nodes, json_each(labels)`
- Produces `key` (index) and `value` columns
- Works with EXISTS patterns for scoped queries
- Builder: `FromJoined("nodes", "json_each", "labels")`

**Performance Optimizations**:
- EXISTS with variable-length paths → CTE+JOIN rewrite (avoids correlated subqueries)
- For nodes table: correlates on `id`
- For edges table: correlates on `from_id`
- Scoped queries complete in milliseconds even on large graphs

**Key Files**:
- `aql/parser.go` - PEG parser using participle
- `aql/ast.go` - AST types for all query components
- `aql/builder.go` - Fluent builder API
- `aql/validate.go` - Semantic validation
- `aql/grammar.md` - Complete EBNF grammar specification
- `aql/doc.go` - Examples and usage documentation
- `adapters/sqlite/aql.go` - AQL→SQL compiler
- `adapters/sqlite/aql_test.go` - 54 tests covering all phases

**Testing**: When modifying AQL, always run: `go test -v ./adapters/sqlite -run TestQuery`

### New: Type-Safe Fluent AQL API

The AQL builder now provides a type-safe fluent API that eliminates magic strings and provides better IDE support:

**Core Improvements:**
- No more string literals - everything is type-safe
- Auto-wrap values in expressions (`Type.Eq("fs:file")` instead of `Eq("type", String("fs:file"))`)
- Fluent chainable syntax
- Better IDE support with autocomplete and error detection
- Full compatibility with existing AST and validation system

**New API Examples:**

```go
// Basic queries - much cleaner
aql.Nodes.Select(aql.Type, aql.Count()).
    Where(aql.Type.Eq(aql.NodeType.File)).
    GroupBy(aql.Type).
    OrderByCount(true).
    Build()

// JsonEach for JSON arrays
aql.Nodes.JsonEach(aql.Labels).
    Select(aql.Val, aql.Count()).
    Where(aql.Val.Ne("")).
    GroupBy(aql.Val).
    Build()

// Scoped queries (optimized)
aql.Nodes.Select(aql.Type, aql.Count()).
    Where(aql.Nodes.ScopedTo(rootNodeID)).
    GroupBy(aql.Type).
    Build()

// Pattern queries
aql.FromPattern(
    aql.Pat(aql.N("dir").OfType(aql.NodeType.Dir).Build()).
        To(aql.Edge.Contains, aql.N("file").OfType(aql.NodeType.File).Build()).
        Build(),
).Select(aql.Var("file")).Build()
```

**Key Constants:**

```go
// Node types
aql.NodeType.File      // "fs:file"
aql.NodeType.Dir       // "fs:dir"  
aql.NodeType.Repo      // "vcs:repo"
aql.NodeType.Branch    // "vcs:branch"

// Edge types  
aql.Edge.Contains      // "contains"
aql.Edge.Has           // "has"
aql.Edge.LocatedAt     // "located_at"
aql.Edge.References    // "references"

// Common fields
aql.Type               // "type"
aql.Name               // "name"
aql.URI                // "uri"
aql.Labels             // "labels"
aql.DataExt            // "data.ext"
aql.DataSize           // "data.size"

// JsonEach fields
aql.Key                // "key" (array index)
aql.Val                // "value" (array element)
```

**Expression Methods (Auto-wrap values):**

```go
// Comparisons
aql.Type.Eq("fs:file")              // type = 'fs:file'
aql.Type.Ne("fs:dir")               // type != 'fs:dir'
aql.DataSize.Gt(1000)               // data.size > 1000
aql.DataSize.Between(100, 1000)   // data.size BETWEEN 100 AND 1000

// Pattern matching
aql.Name.Like("README%")            // name LIKE 'README%'
aql.Type.Glob("fs:*")               // type GLOB 'fs:*'

// Set operations
aql.Type.In("fs:file", "fs:dir")    // type IN ('fs:file', 'fs:dir')
aql.Labels.ContainsAny("test", "code") // labels CONTAINS ANY ('test', 'code')

// Null checks
aql.DataExt.IsNull()                // data.ext IS NULL
aql.DataExt.IsNotNull()             // data.ext IS NOT NULL
```

**Variable References:**

```go
// Variable field access
aql.Var("file").DataField("ext").Eq("go")     // file.data.ext = 'go'
aql.Var("file").Field("name").Glob("*.go")    // file.name GLOB '*.go'

// Variable as column
aql.Select(aql.Var("file"))                     // SELECT file
aql.Select(aql.Var("repo"), aql.Var("branch")) // SELECT repo, branch
```

**Performance Optimizations:**

The new API automatically enables optimizations:

- **Scoped queries** use CTE+JOIN rewrite for EXISTS patterns
- **JsonEach queries** are optimized with proper cross joins
- **Pattern queries** use efficient recursive CTEs for variable-length paths

**Migration from Old API:**

```go
// OLD: String-based API
SelectStar().From("nodes")
Select(Col("type"), Count()).From("nodes")
FromJoined("nodes", "json_each", "labels")
Eq("type", String("fs:file"))
Gt("data.size", Int(1000))
ContainsAny("labels", String("test"))
N("file")
NodeType("file", "fs:file")
AnyEdgeOfType("contains")

// NEW: Type-safe fluent API
Nodes.SelectStar()
Nodes.Select(Type, Count())
Nodes.JsonEach(Labels)
Type.Eq("fs:file")
DataSize.Gt(1000)
Labels.ContainsAny("test")
N("file").Build()
N("file").OfType(NodeType.File).Build()
Edge.Contains
```

**CLI Commands Using New API:**

All CLI commands now use the new fluent API:

```go
// stats.go - Global statistics
aql.Nodes.Select(aql.Type, aql.Count()).GroupBy(aql.Type)

// stats.go - Scoped statistics  
aql.Nodes.Select(aql.Type, aql.Count()).Where(aql.Nodes.ScopedTo(cwdNodeID))

// labels.go - Global label counting
aql.Nodes.JsonEach(aql.Labels).Select(aql.Val, aql.Count()).GroupBy(aql.Val)

// edges.go - Global edge statistics
aql.Edges.Select(aql.Type, aql.Count()).GroupBy(aql.Type)

// find.go - Pattern-based search
aql.FromPattern(pattern).Select(aql.Var("file")).Where(aql.Var("file").DataField("ext").Eq("go"))
```

The new API provides:
- **Type safety**: No more string typos
- **Auto-completion**: IDEs can suggest valid options
- **Better error messages**: Compile-time validation
- **Cleaner code**: More readable queries
- **Performance**: Automatic optimizations

### Removed: GraphTraverser

The `Traverse` method and `GraphTraverser` interface were removed in favor of AQL:
- All traversal is now done via AQL pattern queries with recursive CTEs
- Scoped queries use `EXISTS` with variable-length paths
- This provides better performance and a consistent query interface

**Migration from Traverse to AQL**:
```go
// Old: Traverse with options
// results, _ := storage.Traverse(ctx, graph.TraverseOptions{
//     Seed: graph.NodeFilter{NodeIDs: []string{rootID}},
//     EdgeFilters: []graph.EdgeFilter{{Types: []string{"contains"}}},
// })

// New: AQL with EXISTS pattern
cwdPattern := aql.N("cwd").WithWhere(aql.Eq("id", aql.String(rootID)))
containsEdge := aql.AnyEdgeOfType("contains").WithMinHops(0)
pattern := aql.Pat(cwdPattern).To(containsEdge, aql.N("nodes")).Build()

q := aql.SelectStar().From("nodes").Where(aql.Exists(pattern)).Build()
result, _ := storage.Query(ctx, q)
```

### CLI Commands

- Use cobra for command structure
- Global flags: `--db-dir`, `--local`
- DB auto-lookup: walk up directories, fallback to `~/.axon/graph.db`
- Print "Using database: <path>" for transparency

**Available commands**:
- `init` / `index` - Index a directory and create/update the graph (also `--watch` to keep live)
- `index --embed` - Generate embeddings for semantic search (use `--embed-provider` to choose provider)
- `query` - Execute AQL queries (with `--explain`, `--output table|json|count`)
- `tree` - Display graph as tree (with `--depth`, `--ids`, `--types`)
- `find` - Search nodes with filters (with `--type`, `--name`, `--ext`, `--global`)
          Pass a text argument for semantic similarity search: `axon find "error handling" --type go:func`
- `show` - Display node details
- `search` - **Deprecated** — use `axon find "<query>"` instead
- `impact` - Show blast radius of changing a symbol
- `stats`, `labels`, `types`, `edges`, `gc` - Introspection and maintenance

### Key Patterns

1. **Generation-based cleanup**: Each index run has a generation ID; indexers use it to identify stale nodes
2. **Indexer-owned cleanup**: Each indexer is responsible for finding and deleting its own stale nodes using URI prefix matching
3. **Event-driven cascade**: When FS indexer detects stale nodes (deleted files/dirs), it emits `EventNodeDeleting` events so dependent indexers (git) can clean up their nodes
4. **URI prefix for scoping**: Cleanup uses `DeleteStaleByURIPrefix(uriPrefix, generation)` instead of root path
5. **Framework handles edges only**: The framework handles `DeleteStaleEdges` and `DeleteOrphanedEdges` after all indexers complete
6. **Ignored directories are indexed**: Ignored directories (like `.git`) are indexed as nodes (but contents skipped) so deletion can be detected
7. **Event-based indexing**: FS indexer emits `EventEntryVisited`; git indexer subscribes to `.git` directories
8. **TriggerEvent in Context**: Indexers receive `TriggerEvent *Event` to know if they were invoked directly or triggered by an event subscription
9. **URI schemes**: `file://` for filesystem, `git+file://` for local git repos

### Edge Type Design

#### Common Edge Types

All edge types are defined in `types/edges.go`. Use generic edges rather than domain-specific ones.

| Edge | Inverse | Semantics | Example |
|------|---------|-----------|-------------------------------|
| `contains` | `contained_by` | Structural containment | dir → file |
| `has` | `belongs_to` | Logical ownership | repo → branch, doc → section |
| `located_at` | - | Physical location | repo → dir |
| `links_to` | - | Explicit hyperlink | section → file |
| `references` | - | Soft cross-reference | go:ref → go:func |
| `depends_on` | - | Dependency | module → module |
| `imports` | - | Package import graph | go:package → go:package |
| `implements` | - | Struct implements interface | go:struct → go:interface |
| `tests` | - | Test package tests source | go:package(_test) → go:package |
| `defines` | - | Symbol definition | go:package → go:func/struct/etc. |
| `contains` | `contained_by` | Structural containment | dir → file |
| `has` | `belongs_to` | Logical ownership | repo → branch, doc → section |
| `located_at` | - | Physical location | repo → dir |
| `links_to` | - | Explicit hyperlink | section → file |
| `references` | - | Soft cross-reference | code → code |
| `depends_on` | - | Dependency | module → module |
| `imports` | - | Import | file → file |
| `defines` | - | Symbol definition | file → symbol |

#### Design Rules

1. **Structural vs Logical**:
   - `contains` / `contained_by` = physical/structural hierarchy (directories, DOM trees)
   - `has` / `belongs_to` = logical ownership (repos have branches, documents have sections)

2. **Bidirectional Relationships**:
   - Use `EmitContainment(parentID, childID)` for structural containment (creates both `contains` and `contained_by`)
   - Use `EmitOwnership(ownerID, ownedID)` for logical ownership (creates both `has` and `belongs_to`)
   - Both helpers are in `indexer/emitter.go`

3. **Avoid Domain-Scoped Edges**:
   - Prefer generic edges + node types over scoped edges like `git::has_branch`
   - Node types already provide domain context
   - Query pattern: `GetEdgesFrom(repo.ID)` then filter by `e.Type == "has"` and target node type == `vcs:branch`
   - Only create scoped edges (e.g., `git::is_submodule_of`) for truly unique semantics

4. **Query Patterns**:
   - "All children of X": `GetEdgesFrom(X.ID)` where `e.Type == "contains"` or `"has"`
   - "Parent of X": `GetEdgesFrom(X.ID)` where `e.Type == "contained_by"` or `"belongs_to"`
   - "All tags of repo": `GetEdgesFrom(repo.ID)` where `e.Type == "has"` and target type == `vcs:tag`

5. **Registration**:
   - Call `types.RegisterCommonEdges(registry)` before domain-specific registrations
   - Common edges have no FromTypes/ToTypes constraints (any-to-any)
   - Domain-specific constraints are added in domain registration functions

## Documentation

- **README.md** - User-facing documentation with quickstart, AQL tutorial, and CLI reference
- **AGENTS.md** (this file) - Developer guidelines for AI agents and contributors
- **aql/grammar.md** - Complete EBNF grammar specification for AQL
- **aql/doc.go** - Package documentation with usage examples
- **TODO.md** - Project roadmap and planned features
- **LICENSE** - MIT License

When making changes that affect user-facing behavior (new features, CLI changes, AQL syntax), update README.md accordingly.

## Logo & Assets

Logo source lives in `assets/logo.svg`. The PNG is a pre-rendered 2× (HiDPI) export and must be regenerated whenever the SVG changes.

**Regenerate the PNG after any SVG edit:**

```bash
rsvg-convert --zoom=2 assets/logo.svg -o assets/logo.png
```

This produces `assets/logo.png` at exactly 2× the SVG canvas size (e.g. 860×240 SVG → 1720×480 PNG).

**Key layout facts:**
- SVG canvas: `860×240` — the `viewBox`, `width`, and `height` attributes on the root `<svg>` element all use this value
- Both background `<rect>` elements (solid fill `#070d1a` and radial-gradient overlay) must match the canvas width; update them whenever the canvas size changes
- The neuron illustration spans roughly x=18–548; text is centred around x=718
- Animation cycle: 2.8 s (action potential soma → terminals)
- `rsvg-convert` renders the static base state of the SVG (SMIL animations are not captured in the PNG)

### New Go Package Data Fields

`PackageData` in `types/golang.go` has been extended with:
- `ImportPaths []string` — intra-module import paths (populated by import graph)
- `IsTest bool` — true for `_test` suffix packages
- `TestFor string` — import path of the package being tested

### Watch Mode Architecture

`Axon.Watch(ctx, path, WatchOptions)` in `axon.go`:
- Performs an initial index run, then starts an `fsnotify` watcher on the directory tree
- Debounces file events (default 150ms), finds the common ancestor of all changed paths
- Calls `IndexWithOptions()` on that subtree and invokes `opts.OnReindex` callback
- Loop terminates when `ctx` is cancelled

### Embedding Support

`graph.Storage` now includes `EmbeddingStore` interface:
- `PutEmbedding(ctx, nodeID, []float32)` — store a vector for a node
- `GetEmbedding(ctx, nodeID)` — retrieve a stored vector
- `FindSimilar(ctx, query, limit, filter)` — cosine similarity scan

`axon.Config.EmbeddingProvider` field (type `embeddings.Provider`) activates the embedding PostIndexer. When set, embeddings are generated for `go:func`, `go:struct`, `go:interface`, and `md:section` nodes after each indexing run.

The `embeddings.Provider` interface:

```go
type Provider interface {
    Embed(ctx context.Context, text string) ([]float32, error)
    EmbedBatch(ctx context.Context, texts []string) ([][]float32, error)
    Dimensions() int
    Name() string
    Close() error
}
```

`Embed` is a convenience wrapper around `EmbedBatch`. The `Indexer.PostIndex` collects all nodes and calls `EmbedBatch` in chunks of `DefaultBatchSize` (32) for efficiency — one request/inference pass per batch instead of one per node.

Built-in providers:
- `embeddings.NewNull(dims)` — zero vectors for testing
- `embeddings.NewOllama(baseURL, model, dims)` — Ollama `/api/embed` batch endpoint; dims=0 defaults to 768 (nomic-embed-text)
- `embeddings.NewHugot(modelPath, model)` — in-process ONNX via Hugot pure-Go backend; no daemon, no CGO, model downloaded once to `~/.axon/models/`

All providers are in `indexer/embeddings/`. The package is designed to be extractable as a standalone library — no file except `indexer.go` imports axon-internal packages.

## Release & Tagging Workflow

When cutting a release **or** applying a git tag (even a standalone `tag` request):

1. **Determine the new version** — inspect the existing tags (`git tag --sort=-version:refname | head -5`) and choose the next semver:
   - Patch (`v0.x.Y+1`) for bug fixes and non-breaking changes
   - Minor (`v0.X+1.0`) for new features or any breaking CLI/API change
   - Major for stable-API breaks (post-v1)

2. **Identify changes since the last tag** — run:
   ```bash
   git log <last-tag>..HEAD --oneline
   ```
   Read the commit messages to understand what changed.

3. **Update CHANGELOG.md before tagging** — replace the `[Unreleased]` section
   header (or create one if absent) with the concrete version and today's date:
   ```
   ## [0.4.0] — 2026-04-10
   ```
   Verify the entry accurately reflects the commits since the last tag.
   If the entry is missing or incomplete, write it now.

4. **Commit the CHANGELOG update** — use a `chore(release):` commit:
   ```
   chore(release): v0.4.0
   ```

5. **Create the GitHub release** (this also pushes the tag via `--tag`):
   ```bash
   gh release create v0.4.0 \
     --title "v0.4.0" \
     --notes "<release notes>" \
     --latest
   ```
   - Copy the CHANGELOG entry for this version as `--notes`.
   - Use `--latest` on the most recent release; omit it for backfills.
   - **Do not** run `git tag` manually — `gh release create` creates the tag on GitHub
     and `git fetch --tags` can pull it locally if needed. If the tag already exists
     locally, pass `--tag v0.4.0` explicitly.

   Alternatively, if the tag was already pushed separately:
   ```bash
   git push origin v0.4.0          # push tag first if not already pushed
   gh release create v0.4.0 --title "v0.4.0" --notes "..." --latest
   ```

6. **Verify** the release appears on GitHub:
   ```bash
   gh release list
   ```

> **Never tag a commit that still has `[Unreleased]` in CHANGELOG.md.**
> **Always create a matching GitHub release — a tag alone is not a release.**
