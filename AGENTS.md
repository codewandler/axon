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

AQL supports: table queries (SELECT/WHERE/GROUP BY/HAVING/ORDER BY/LIMIT), JSON field
access (`data.ext`), label operations (`CONTAINS ANY/ALL/NOT CONTAINS`), pattern matching
(`(var:type)-[:edge]->(var:type)`), variable-length paths (`[:contains*1..3]`), table
functions (`json_each`), and EXISTS scoped queries. Scoped queries use a CTE+JOIN rewrite
for performance (milliseconds even on large graphs).

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

### AQL Builder API

Use the type-safe fluent builder for all programmatic AQL construction.
Full reference: `.agents/skills/axon/references/aql_go_querybuilder.md`

```go
aql.Nodes.Select(aql.Type, aql.Count()).GroupBy(aql.Type).Build()
aql.Nodes.JsonEach(aql.Labels).Select(aql.Val, aql.Count()).GroupBy(aql.Val).Build()
aql.Nodes.SelectStar().Where(aql.Nodes.ScopedTo(cwdNodeID)).Build()
aql.FromPattern(aql.Pat(aql.N("dir").OfType(aql.NodeType.Dir).Build()).
    To(aql.Edge.Contains, aql.N("file").OfType(aql.NodeType.File).Build()).Build()).
    Select(aql.Var("file")).Build()
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
- `describe` - Show graph schema: node types, edge types, fields, and connection patterns (`-o json`, `--fields`)
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

## GitHub Issue Workflow

This is the end-to-end autonomous workflow for working on a GitHub issue.
All steps are performed by the agent without waiting for human input **unless
an open question is found during design** (see step 4). Every other step
proceeds automatically.

### 1. Read the issue

```bash
gh issue view <N>
```

Capture: problem statement, proposed solution, any acceptance criteria or
examples given in the issue body. Note the issue number — it will appear in
commit messages and the PR body.

### 2. Explore the codebase

Before touching any file, read the code:

```bash
# Orient: project structure and recent history
git log --oneline -10
ls <relevant packages>

# Find the right layer to change
grep -rn "<key type or function>" --include="*.go" .

# Read every file you intend to change
file_read <path>
```

Goal: confirm root cause / integration point before designing anything.

### 3. Create a worktree

Always work on a feature branch isolated in `./worktrees/`:

```bash
git checkout -b feature/<slug>
git worktree add ./worktrees/<slug> feature/<slug>
```

Verify the baseline is clean before touching anything:

```bash
cd ./worktrees/<slug>
go test ./...          # must pass
go test -race ./...    # must pass
```

### 4. Write the DESIGN — pause only here if questions exist

Save to `.agents/plans/DESIGN-<slug>.md`. The design must cover:

- **Problem** — what is broken or missing and why it matters
- **Proposed solution** — the key API / data shape / behaviour
- **Architecture** — which layers change and why (e.g. optional interface vs.
  monolithic interface, where types live, public API signature)
- **Key decisions** — the non-obvious choices and their rationale
- **Out of scope** — what is deliberately deferred
- **Files changed** — a table listing every file that will be touched

> **If the design reveals an open question** (ambiguous requirement, API
> conflict, missing information) — stop, present the design draft, list the
> questions, and wait for the user to answer. Otherwise proceed immediately
> to planning.

### 5. Write the PLAN

Save to `.agents/plans/PLAN-<slug>.md`. Break the design into numbered tasks.
Each task must include:

- Exact files to create or modify
- The code to write (snippets, not pseudocode)
- A verification command (`go build`, `go test -run TestX ./pkg/...`)

Tasks are ordered so each builds on the previous. Estimated 2–5 min each.

### 6. Implement — test-first

For each task:

1. **Write the failing test first.** Run it to confirm it fails for the right reason.
2. **Write the implementation.** Run the test to confirm it passes.
3. **Run the full suite** to confirm nothing regressed.

Use `go test -race` at least once per package touched — concurrency bugs
are invisible without it.

### 7. Self-review

After all tasks complete, review every changed file as if reading someone
else's PR. Check:

| Category | What to look for |
|---|---|
| Spec compliance | Does the code do exactly what the issue asked? Cross-check every bullet in the issue body — not just the happy path |
| Error handling | All errors wrapped with context, none silently dropped |
| Concurrency | No shared mutable state accessed from multiple goroutines without synchronisation; run `-race` |
| Test quality | No tautological assertions; edge cases covered |
| Adversarial inputs | For any parser, regex, or pattern matcher: write at least one test for a **false-positive** input (something that looks like a match but shouldn't be) and one for a **false-negative** (a valid input that uses an unusual but legal form) |
| New node type checklist | If a new node type was added: (1) is `printMapData` in `show.go` updated with a typed case? (2) is `getNodeSummary` updated? Run `axon show <real-node-id>` on an indexed result and read the output — do not accept a raw key=value map dump as "good enough" |
| API cleanliness | No unnecessary double-calls, pointless aliases, or dead code |
| JSON/wire format | `nil` vs `[]` matters for consumers; test empty cases explicitly |
| Docs | See Step 7a below — documentation is mandatory, not optional |

Fix every issue found before opening the PR. If a newly-written test reveals
a **pre-existing bug** in unrelated code, fix it in a separate commit with a
clear message explaining the root cause.

### 7a. Documentation (mandatory — not optional)

Every feature, flag, node type, edge type, or behaviour change **must** be
reflected in all three documentation targets before the PR is opened.
Skipping this step is what creates documentation debt — do not defer it.

#### Checklist

| Changed | What to update |
|---|---|
| New CLI flag | `README.md` — the relevant `axon <cmd>` section; `.agents/skills/axon/SKILL.md` — the matching section |
| New node type | `README.md` — add entry under the correct `## Node Types` subsection with data fields; `SKILL.md` — add to **Node Types** list; add a usage example to the **Finding Nodes** or **Semantic Search** section |
| New edge type | `README.md` — add to `## Edge Types`; `SKILL.md` — add to **Edge Types** list |
| Extended data fields on existing node | `README.md` — update that node type's entry with the new fields; `SKILL.md` — update if queried |
| New indexer | `README.md` — add to the Architecture indexers list |
| New AQL capability | `README.md` — add example to the AQL section |
| Changed agent/workflow behaviour | `AGENTS.md` — update the relevant step |

#### How to apply

```bash
# Always read the current state of both files before editing
file_read README.md
file_read .agents/skills/axon/SKILL.md

# Edit both files in the same commit as the feature code
# The PR description must mention which doc sections were updated
```

**The PR description must call out which documentation sections were updated.**
A PR that changes behaviour but has no documentation update is incomplete.

### 8. Final pre-flight

```bash
go build ./...
go vet ./...
go test -race ./...

# Sanity-check the feature end-to-end — read the output, don't just check exit codes
./bin/axon <new-command> --help
./bin/axon <new-command> -o json | head -20
```

All three commands must exit 0. Do not open the PR until they do.

**If a new node type was introduced**, also run:

```bash
# Re-index so the new type is actually populated
./bin/axon index .

# Confirm nodes appear and names look right (not raw URIs or hashes)
./bin/axon find --type <new-type> --limit 5

# Inspect a real result — verify show output is structured, not a raw map dump
./bin/axon show <node-id-from-above>
```

If `axon show` prints a generic `key: value` map with no formatting, a `printMapData`
case is missing in `cmd/axon/show.go`. Fix it before opening the PR.

**If a new node type should be semantically searchable**, verify it is in
`indexer/embeddings/indexer.go` `DefaultEmbedTypes`. If it is missing, add it
and extend `buildNodeText` to extract any type-specific text fields that provide
semantic signal beyond the node `Name`.

### 9. Open the PR

```bash
git add -A
git commit -m "feat: <summary>\n\n<detail bullets>\n\nRefs: #<N>"
git push origin feature/<slug>

gh pr create \
  --title "feat: <summary> (closes #<N>)" \
  --body  "<markdown body covering: summary, what was added, design notes, test coverage>" \
  --head  feature/<slug> \
  --base  main
```

Always reference the issue number in the title and body so GitHub auto-closes
it on merge.

### 10. Merge

Use squash-merge to keep main history linear:

```bash
gh pr merge <N> \
  --squash \
  --subject "<single line summary> (closes #<issue>)" \
  --body    "<brief bullets>"

git checkout main
git pull origin main
```

### 11. Update CHANGELOG and release

Follow the **Release & Tagging Workflow** section below. Features and notable
fixes always get a CHANGELOG entry and a GitHub release. Use semver:
- Patch for bug-only fixes
- Minor for new public API or new CLI commands

### 12. Clean up

```bash
# Remove worktree and feature branch
git worktree remove ./worktrees/<slug>
git branch -D feature/<slug>
```

### 13. Close the loop

```bash
# Comment on the issue with what shipped
gh issue comment <N> --body "## Implemented in v<X.Y.Z>\n\n<what was built, key decisions, follow-up links>"

# Commit the plan files to main so they survive as a record
git add .agents/plans/DESIGN-<slug>.md .agents/plans/PLAN-<slug>.md
git commit -m "docs: design and plan for <feature> (#<N>)"
git push origin main
```

### Summary

```
Read issue → Explore → Worktree → DESIGN → [pause if questions] → PLAN
  → Implement (TDD) → Self-review → Fix → Pre-flight → PR → Merge
  → CHANGELOG → Release → Cleanup → Comment + commit plans
```

The only mandatory pause is after the DESIGN if it surfaces open questions.
Everything else runs autonomously.

---

## Release & Tagging Workflow

When cutting a release **or** applying a git tag (even a standalone `tag` request):

> **Two non-negotiable rules:**
> 1. **Never commit work without its CHANGELOG entry.** All changes — code,
>    docs, config — and the corresponding CHANGELOG update must be staged
>    together and land in a single `chore(release)` commit. A release commit
>    that only touches `CHANGELOG.md` means clicking the tag on GitHub shows
>    a useless diff.
> 2. **Always `git push origin main` before `gh release create`.** `gh`
>    creates the tag on GitHub pointing to `origin/main` HEAD, not your local
>    HEAD. Push first or the tag points at the wrong commit.

1. **Determine the new version** — inspect the existing tags (`git tag --sort=-version:refname | head -5`) and choose the next semver:
   - Patch (`v0.x.Y+1`) for bug fixes and non-breaking changes
   - Minor (`v0.X+1.0`) for new features or any breaking CLI/API change
   - Major for stable-API breaks (post-v1)

2. **Identify changes since the last tag** — run:
   ```bash
   git log <last-tag>..HEAD --oneline
   ```
   Check `git status` too — any uncommitted work must be included.

3. **Update CHANGELOG.md** — write the entry for the new version. If there
   are no staged changes yet, this is the only file that needs staging.
   ```
   ## [0.4.0] — 2026-04-10
   ```

4. **Stage everything and commit as one** — CHANGELOG plus all uncommitted
   work in a single commit:
   ```bash
   git add -A                    # or stage specific files
   git commit -m "chore(release): v0.4.0

   - bullet summary of all changes"
   ```
   If work was already committed in separate commits, squash them first:
   ```bash
   git reset --soft <last-tag>   # unstage all commits since last tag
   git add -A
   git commit -m "chore(release): v0.4.0 ..."
   ```

5. **Push, then release** — in this order, never reversed:
   ```bash
   git push origin main
   gh release create v0.4.0 \
     --title "v0.4.0" \
     --notes "<changelog entry>" \
     --latest
   ```

6. **Verify** tag SHA matches HEAD:
   ```bash
   git fetch --tags
   git rev-parse v0.4.0 HEAD     # both lines must be identical
   gh release list | head -3
   ```

> **Never tag a commit that still has `[Unreleased]` in CHANGELOG.md.**
> **Always create a matching GitHub release — a tag alone is not a release.**
