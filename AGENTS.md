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

# Install CLI globally (final step after impl, test, e2e test)
go install ./cmd/axon
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
│   └── markdown/        # Markdown document indexer
├── types/               # Node/edge type definitions (fs, vcs, markdown)
├── progress/            # Progress reporting (coordinator, bubbletea UI)
├── render/              # Tree rendering utilities
└── cmd/axon/            # CLI commands (init, query, tree, find, show, etc.)
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

**Key Files**:
- `aql/parser.go` - PEG parser using participle
- `aql/ast.go` - AST types for all query components
- `aql/builder.go` - Fluent builder API
- `aql/validate.go` - Semantic validation
- `aql/grammar.md` - Complete EBNF grammar specification
- `aql/doc.go` - Examples and usage documentation
- `adapters/sqlite/aql.go` - AQL→SQL compiler
- `adapters/sqlite/aql_test.go` - 52 tests covering all phases

**Testing**: When modifying AQL, always run: `go test -v ./adapters/sqlite -run TestQuery`

### CLI Commands

- Use cobra for command structure
- Global flags: `--db-dir`, `--local`
- DB auto-lookup: walk up directories, fallback to `~/.axon/graph.db`
- Print "Using database: <path>" for transparency

**Available commands**:
- `init` - Index directories and create graph
- `query` - Execute AQL queries (with `--explain`, `--output table|json|count`)
- `tree` - Display graph as tree (with `--depth`, `--ids`, `--types`)
- `find` - Search nodes with filters (with `--type`, `--name`, `--ext`, `--global`)
- `show` - Display node details
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
|------|---------|-----------|---------|
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
