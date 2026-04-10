# Design: Expose Query and Search on axon.Client

**Date:** 2026-04-10  
**Status:** Proposed

---

## Problem

A lib integrator who embeds axon as a Go library currently has to reach through
two indirections to run any query:

```go
ax, _ := axon.New(axon.Config{Dir: "."})
ax.Index(ctx, ".")

// To run an AQL query today:
q, _ := aql.Parse("SELECT * FROM nodes WHERE type = 'fs:file'")
result, _ := ax.Graph().Storage().Query(ctx, q)   // ← clunky chain

// To find nodes structurally:
nodes, _ := ax.Graph().FindNodes(ctx, graph.NodeFilter{Type: "fs:file"}, graph.QueryOptions{})

// To do semantic search (already on *Axon, but name is verbose):
results, _ := ax.SemanticSearch(ctx, []string{"auth logic"}, 10, nil)
```

Pain points:
1. **AQL query** requires `.Graph().Storage().Query()` — 3-level chain, plus manual `aql.Parse()`
2. **Node filter search** requires `.Graph().FindNodes()` — 2-level chain
3. **No read-only entry point** — There is no way to open an existing `.axon/graph.db` without spinning up the full indexer machinery (`New` + all indexers registered)
4. **No shared interface** — Integrators cannot write a function that accepts "any axon query source" because `*Axon` does not expose a clean `Querier` interface

---

## Goals

1. Flatten the query API: expose `Query`, `Explain`, `Find`, and `Search` directly on the existing `*Axon` type.
2. Introduce a lightweight `*Client` for read-only access to an existing axon database (no indexers loaded).
3. Define a `Querier` interface so integrators can accept either `*Axon` or `*Client` interchangeably.
4. Preserve 100% backward compatibility — no existing callers break.

## Non-goals

- Changing `graph.Storage`, `aql`, or any indexer packages.
- Removing the existing `ax.Graph().Storage()` escape hatch.
- Changing the CLI commands.

---

## Design

### 1. `Querier` interface (in `axon.go`)

```go
// Querier is the read-only interface for executing queries against an axon graph.
// Both *Axon (after indexing) and *Client (opened from a DB file) satisfy it.
type Querier interface {
    // Query executes a pre-built AQL query (from the aql builder or aql.Parse).
    Query(ctx context.Context, q *aql.Query) (*graph.QueryResult, error)

    // QueryString parses the AQL string and executes it. Convenience wrapper
    // around aql.Parse + Query.
    QueryString(ctx context.Context, q string) (*graph.QueryResult, error)

    // Explain returns the query execution plan without running the query.
    Explain(ctx context.Context, q *aql.Query) (*graph.QueryPlan, error)

    // Find returns nodes matching the structural filter.
    Find(ctx context.Context, filter graph.NodeFilter, opts graph.QueryOptions) ([]*graph.Node, error)

    // Search performs semantic vector similarity search across all queries and
    // returns up to limit results sorted by score descending.
    // Returns ErrNoEmbeddingProvider if no embedding provider is configured.
    Search(ctx context.Context, queries []string, limit int, filter *graph.NodeFilter) ([]*SemanticSearchResult, error)
}
```

`*Axon` already has `SemanticSearch`. We add the four missing methods and a `Search`
alias (delegating to `SemanticSearch`), making `*Axon` satisfy `Querier`.

### 2. New methods on `*Axon` (in `axon.go`)

```go
// Query executes a pre-built AQL query.
func (a *Axon) Query(ctx context.Context, q *aql.Query) (*graph.QueryResult, error) {
    return a.storage.Query(ctx, q)
}

// QueryString parses and executes a raw AQL string.
func (a *Axon) QueryString(ctx context.Context, q string) (*graph.QueryResult, error) {
    parsed, err := aql.Parse(q)
    if err != nil {
        return nil, fmt.Errorf("parse AQL: %w", err)
    }
    return a.storage.Query(ctx, parsed)
}

// Explain returns the execution plan for a pre-built AQL query.
func (a *Axon) Explain(ctx context.Context, q *aql.Query) (*graph.QueryPlan, error) {
    return a.storage.Explain(ctx, q)
}

// Find returns nodes matching the structural filter.
func (a *Axon) Find(ctx context.Context, filter graph.NodeFilter, opts graph.QueryOptions) ([]*graph.Node, error) {
    return a.storage.FindNodes(ctx, filter, opts)
}

// Search is a convenience alias for SemanticSearch.
func (a *Axon) Search(ctx context.Context, queries []string, limit int, filter *graph.NodeFilter) ([]*SemanticSearchResult, error) {
    return a.SemanticSearch(ctx, queries, limit, filter)
}
```

### 3. `Client` type (new file: `client.go`)

Lightweight read-only query client. No indexers, no watcher, no FSExclude logic.
Useful when a lib integrator wants to query an already-indexed graph from a
different process or without triggering re-indexing.

```go
// Client is a read-only query client for an axon graph database.
// Obtain via axon.Open.
type Client struct {
    storage           graph.Storage
    embeddingProvider embeddings.Provider // optional
}

// ClientOption configures a Client.
type ClientOption func(*Client)

// WithClientEmbeddingProvider sets the embedding provider for semantic search.
func WithClientEmbeddingProvider(p embeddings.Provider) ClientOption {
    return func(c *Client) { c.embeddingProvider = p }
}

// Open opens an existing axon SQLite database at dbPath for read-only querying.
// dbPath is the path to the SQLite file (e.g., ".axon/graph.db").
func Open(dbPath string, opts ...ClientOption) (*Client, error) {
    s, err := sqlite.New(dbPath)
    if err != nil {
        return nil, fmt.Errorf("open axon db %q: %w", dbPath, err)
    }
    c := &Client{storage: s}
    for _, o := range opts {
        o(c)
    }
    return c, nil
}

// Close releases the underlying database connection.
func (c *Client) Close() error { ... }

// Query, QueryString, Explain, Find, Search — identical logic to *Axon methods.
```

`*Client` satisfies `Querier`.

### 4. `*Axon` gains `AsQuerier()` (optional convenience)

Not strictly needed since `*Axon` itself satisfies `Querier`, but documents intent:

```go
// AsQuerier returns *Axon as a Querier interface value.
// Useful when passing to functions that accept Querier.
func (a *Axon) AsQuerier() Querier { return a }
```

This can be omitted if the team finds it redundant.

---

## Usage After This Change

```go
// Existing workflow (Index + Query on same instance):
ax, _ := axon.New(axon.Config{Dir: "."})
ax.Index(ctx, ".")

result, _ := ax.QueryString(ctx, "SELECT * FROM nodes WHERE type = 'fs:file'")

q := aql.Nodes.Select(aql.Type, aql.Count()).GroupBy(aql.Type).Build()
result, _ := ax.Query(ctx, q)

nodes, _ := ax.Find(ctx, graph.NodeFilter{Type: "go:func"}, graph.QueryOptions{})
results, _ := ax.Search(ctx, []string{"authentication logic"}, 10, nil)

// New: read-only client for an existing DB:
client, _ := axon.Open(".axon/graph.db")
defer client.Close()
result, _ := client.QueryString(ctx, "SELECT type, COUNT(*) FROM nodes GROUP BY type")

// New: function accepting either *Axon or *Client:
func inspect(ctx context.Context, q axon.Querier) {
    nodes, _ := q.Find(ctx, graph.NodeFilter{Type: "fs:file"}, graph.QueryOptions{})
    ...
}
```

---

## File Layout

| File | Change |
|------|--------|
| `axon.go` | Add `Querier` interface, add `Query`, `QueryString`, `Explain`, `Find`, `Search` methods to `*Axon` |
| `client.go` | New file: `Client` type, `ClientOption`, `Open`, `Close`, and all `Querier` methods |
| `client_test.go` | New file: tests for `Open`, `Client.Query`, `Client.Find`, `Client.QueryString` |

Total new surface area: ~100 lines of implementation, ~80 lines of tests.

---

## Acceptance Criteria

- `*Axon` satisfies `Querier` (compile-time check: `var _ Querier = (*Axon)(nil)`)
- `*Client` satisfies `Querier` (compile-time check: `var _ Querier = (*Client)(nil)`)
- `ax.QueryString(ctx, "SELECT * FROM nodes")` works without `.Graph().Storage()`
- `ax.Find(ctx, graph.NodeFilter{Type: "fs:file"}, graph.QueryOptions{})` works
- `ax.Search(ctx, ...)` is an alias for `SemanticSearch` (same behaviour, same errors)
- `axon.Open(".axon/graph.db")` opens an existing database and allows querying
- All existing tests continue to pass
- New tests cover: `Open` success/failure, `Client.Query`, `Client.QueryString`, `Client.Find`, `Client.Close`
