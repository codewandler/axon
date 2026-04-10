# Plan: Expose Query and Search on axon.Client

**Date:** 2026-04-10  
**Design:** DESIGN-20260410-client-query-search.md  
**Estimated total:** ~25 minutes

---

## Task 1: Add `Querier` interface and new methods to `*Axon`

**Files modified:** `axon.go`  
**Files created:** *(none)*  
**Estimated time:** 5 minutes

**What to write:**

Add the `Querier` interface and five new methods near the bottom of `axon.go`,
after the existing `SemanticSearch` method (around line 575). Also add the import
for `"github.com/codewandler/axon/aql"` if it is not already present.

```go
// Querier is the read-only interface for executing queries against an axon graph.
// Both *Axon (after indexing) and *Client (opened from a DB file) satisfy it.
type Querier interface {
    Query(ctx context.Context, q *aql.Query) (*graph.QueryResult, error)
    QueryString(ctx context.Context, q string) (*graph.QueryResult, error)
    Explain(ctx context.Context, q *aql.Query) (*graph.QueryPlan, error)
    Find(ctx context.Context, filter graph.NodeFilter, opts graph.QueryOptions) ([]*graph.Node, error)
    Search(ctx context.Context, queries []string, limit int, filter *graph.NodeFilter) ([]*SemanticSearchResult, error)
}

// compile-time interface check
var _ Querier = (*Axon)(nil)

// Query executes a pre-built AQL query against the graph.
func (a *Axon) Query(ctx context.Context, q *aql.Query) (*graph.QueryResult, error) {
    return a.storage.Query(ctx, q)
}

// QueryString parses the AQL string and executes it.
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

**Verification:**
```bash
go build ./...
go vet ./...
```

---

## Task 2: Create `client.go` with `Client` type and `Open`

**Files modified:** *(none)*  
**Files created:** `client.go`  
**Estimated time:** 8 minutes

**What to write:**

```go
package axon

import (
    "context"
    "fmt"

    "github.com/codewandler/axon/adapters/sqlite"
    "github.com/codewandler/axon/aql"
    "github.com/codewandler/axon/graph"
    "github.com/codewandler/axon/indexer/embeddings"
)

// compile-time interface check
var _ Querier = (*Client)(nil)

// Client is a read-only query client for an existing axon graph database.
// It exposes the same Querier interface as *Axon but without any indexing
// machinery. Obtain via axon.Open.
type Client struct {
    storage           graph.Storage
    embeddingProvider embeddings.Provider
}

// ClientOption configures a Client created by Open.
type ClientOption func(*Client)

// WithClientEmbeddingProvider sets the embedding provider used by Client.Search.
// Without this option, Search returns ErrNoEmbeddingProvider.
func WithClientEmbeddingProvider(p embeddings.Provider) ClientOption {
    return func(c *Client) { c.embeddingProvider = p }
}

// Open opens an existing axon SQLite database file for read-only querying.
// dbPath must point to an existing SQLite file (e.g., ".axon/graph.db").
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
// The Client must not be used after Close returns.
func (c *Client) Close() error {
    if closer, ok := c.storage.(interface{ Close() error }); ok {
        return closer.Close()
    }
    return nil
}

// Query executes a pre-built AQL query.
func (c *Client) Query(ctx context.Context, q *aql.Query) (*graph.QueryResult, error) {
    return c.storage.Query(ctx, q)
}

// QueryString parses the AQL string and executes it.
func (c *Client) QueryString(ctx context.Context, q string) (*graph.QueryResult, error) {
    parsed, err := aql.Parse(q)
    if err != nil {
        return nil, fmt.Errorf("parse AQL: %w", err)
    }
    return c.storage.Query(ctx, parsed)
}

// Explain returns the execution plan for a pre-built AQL query.
func (c *Client) Explain(ctx context.Context, q *aql.Query) (*graph.QueryPlan, error) {
    return c.storage.Explain(ctx, q)
}

// Find returns nodes matching the structural filter.
func (c *Client) Find(ctx context.Context, filter graph.NodeFilter, opts graph.QueryOptions) ([]*graph.Node, error) {
    return c.storage.FindNodes(ctx, filter, opts)
}

// Search performs semantic vector similarity search.
// Returns ErrNoEmbeddingProvider if no embedding provider was configured via
// WithClientEmbeddingProvider.
func (c *Client) Search(ctx context.Context, queries []string, limit int, filter *graph.NodeFilter) ([]*SemanticSearchResult, error) {
    if c.embeddingProvider == nil {
        return nil, ErrNoEmbeddingProvider
    }
    if limit <= 0 {
        limit = 20
    }

    best := make(map[string]*SemanticSearchResult)
    for _, q := range queries {
        vec, err := c.embeddingProvider.Embed(ctx, q)
        if err != nil {
            return nil, fmt.Errorf("embedding query %q: %w", q, err)
        }
        results, err := c.storage.FindSimilar(ctx, vec, limit, filter)
        if err != nil {
            return nil, fmt.Errorf("similarity search for %q: %w", q, err)
        }
        for _, r := range results {
            if existing, ok := best[r.ID]; !ok || r.Score > existing.Score {
                best[r.ID] = &SemanticSearchResult{NodeWithScore: r, MatchedQuery: q}
            }
        }
    }

    out := make([]*SemanticSearchResult, 0, len(best))
    for _, r := range best {
        out = append(out, r)
    }
    sort.Slice(out, func(i, j int) bool { return out[i].Score > out[j].Score })
    if len(out) > limit {
        out = out[:limit]
    }
    return out, nil
}
```

Note: `sort` must be added to the imports. It is already imported in `axon.go`;
since `client.go` is in the same package it needs its own import block.

**Verification:**
```bash
go build ./...
go vet ./...
```

---

## Task 3: Write `client_test.go`

**Files modified:** *(none)*  
**Files created:** `client_test.go`  
**Estimated time:** 8 minutes

**What to write:**

```go
package axon_test

import (
    "context"
    "os"
    "path/filepath"
    "testing"

    "github.com/stretchr/testify/require"

    axon "github.com/codewandler/axon"
    "github.com/codewandler/axon/aql"
    "github.com/codewandler/axon/graph"
)

// setupIndexedDB creates a temp dir with a real axon index so we can test Open.
func setupIndexedDB(t *testing.T) (dbPath string) {
    t.Helper()
    dir := t.TempDir()

    // Write a couple of dummy files.
    require.NoError(t, os.WriteFile(filepath.Join(dir, "hello.go"), []byte("package main"), 0644))
    require.NoError(t, os.WriteFile(filepath.Join(dir, "README.md"), []byte("# hi"), 0644))

    dbFile := filepath.Join(dir, "test.db")
    s, err := sqlite.New(dbFile) // use adapters/sqlite directly to get a storage
    // ...actually easier: use axon.New with a file-based storage
    // We need the sqlite adapter. Let's just index with Axon and get the DB path.
    // Use axon.New with default storage (in-memory), then export...
    // Simpler: use axon.New, index, then return the storage path.
    // But in-memory has no file. Use a file-based storage via Config.

    // Use sqlite adapter path — we need to import it.
    _ = s
    _ = err
    // See note below.
    return dbFile
}
```

> **Note:** The test helper needs to create a file-based SQLite DB and run `ax.Index` against it.
> Use `adapters/sqlite.New(dbFile)` to create the storage, pass it via `Config.Storage`,
> then index, then close Axon and reopen with `axon.Open`.

Full test cases:

```go
func TestOpen_NonExistentFile_ReturnsError(t *testing.T) {
    _, err := axon.Open("/nonexistent/path/graph.db")
    require.Error(t, err)
}

func TestOpen_ValidDB_ReturnsClient(t *testing.T) {
    dir, dbPath := setupIndexedDB(t)
    _ = dir
    client, err := axon.Open(dbPath)
    require.NoError(t, err)
    require.NotNil(t, client)
    require.NoError(t, client.Close())
}

func TestClient_QueryString(t *testing.T) {
    dir, dbPath := setupIndexedDB(t)
    _ = dir
    client, err := axon.Open(dbPath)
    require.NoError(t, err)
    defer client.Close()

    ctx := context.Background()
    result, err := client.QueryString(ctx, "SELECT * FROM nodes")
    require.NoError(t, err)
    require.NotNil(t, result)
}

func TestClient_Query_Builder(t *testing.T) {
    _, dbPath := setupIndexedDB(t)
    client, err := axon.Open(dbPath)
    require.NoError(t, err)
    defer client.Close()

    ctx := context.Background()
    q := aql.Nodes.Select(aql.Type, aql.Count()).GroupBy(aql.Type).Build()
    result, err := client.Query(ctx, q)
    require.NoError(t, err)
    require.Equal(t, graph.ResultTypeCounts, result.Type)
}

func TestClient_Find(t *testing.T) {
    _, dbPath := setupIndexedDB(t)
    client, err := axon.Open(dbPath)
    require.NoError(t, err)
    defer client.Close()

    ctx := context.Background()
    nodes, err := client.Find(ctx, graph.NodeFilter{}, graph.QueryOptions{})
    require.NoError(t, err)
    require.NotEmpty(t, nodes)
}

func TestClient_Search_NoProvider_ReturnsError(t *testing.T) {
    _, dbPath := setupIndexedDB(t)
    client, err := axon.Open(dbPath)
    require.NoError(t, err)
    defer client.Close()

    ctx := context.Background()
    _, err = client.Search(ctx, []string{"hello"}, 5, nil)
    require.ErrorIs(t, err, axon.ErrNoEmbeddingProvider)
}

func TestAxon_Querier_Interface(t *testing.T) {
    // Compile-time check that *Axon satisfies Querier.
    dir := t.TempDir()
    ax, err := axon.New(axon.Config{Dir: dir})
    require.NoError(t, err)
    
    var _ axon.Querier = ax // must compile
}

func TestAxon_QueryString(t *testing.T) {
    dir := t.TempDir()
    ax, err := axon.New(axon.Config{Dir: dir})
    require.NoError(t, err)
    _, _ = ax.Index(context.Background(), dir)

    result, err := ax.QueryString(context.Background(), "SELECT * FROM nodes")
    require.NoError(t, err)
    require.NotNil(t, result)
}

func TestAxon_Find(t *testing.T) {
    dir := t.TempDir()
    ax, err := axon.New(axon.Config{Dir: dir})
    require.NoError(t, err)
    _, _ = ax.Index(context.Background(), dir)

    nodes, err := ax.Find(context.Background(), graph.NodeFilter{}, graph.QueryOptions{})
    require.NoError(t, err)
    require.NotNil(t, nodes)
}
```

**Verification:**
```bash
go test ./... -run "TestClient|TestAxon_Querier|TestAxon_QueryString|TestAxon_Find" -v
go vet ./...
```

---

## Task 4: Final verification

**Estimated time:** 2 minutes

```bash
go build ./...
go vet ./...
go test ./...
```

All existing tests must continue to pass. The compile-time interface assertions
(`var _ Querier = (*Axon)(nil)` and `var _ Querier = (*Client)(nil)`) must compile.
