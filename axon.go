package axon

import (
	"context"
	"errors"
	"fmt"
	iofs "io/fs"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/fsnotify/fsnotify"

	"github.com/codewandler/axon/adapters/sqlite"
	"github.com/codewandler/axon/aql"
	"github.com/codewandler/axon/graph"
	"github.com/codewandler/axon/indexer"
	"github.com/codewandler/axon/indexer/embeddings"
	fspkg "github.com/codewandler/axon/indexer/fs"
	"github.com/codewandler/axon/indexer/git"
	"github.com/codewandler/axon/indexer/golang"
	"github.com/codewandler/axon/indexer/markdown"
	"github.com/codewandler/axon/indexer/project"
	todo "github.com/codewandler/axon/indexer/todo"
	"github.com/codewandler/axon/progress"
	"github.com/codewandler/axon/types"
)

const (
	// eventChannelBuffer is the buffer size for event channels.
	// This provides backpressure - if subscribers are slow, events will queue
	// up to this limit before the dispatcher drops them with a warning.
	eventChannelBuffer = 10000
)

// DefaultFSIgnore contains the default patterns to exclude when indexing.
// Dot-prefixed paths are no longer blanket-excluded; specific entries are
// listed here so that useful dotfiles (.agents/, .claude/, etc.) remain
// visible to the graph.
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

// Config holds configuration for an Axon instance.
type Config struct {
	// Dir is the working directory. Defaults to current directory.
	Dir string

	// Storage is the storage backend. Defaults to in-memory storage.
	Storage graph.Storage

	// FSExclude contains glob patterns to exclude from indexing.
	// When empty, DefaultFSIgnore is used. To clear all defaults, set FSExclude
	// to a non-nil empty slice: []string{}.
	// Patterns matched against file name and full absolute path.
	FSExclude []string

	// FSInclude contains glob patterns to include. When non-empty, only files
	// matching at least one pattern are indexed (directories always traversed).
	FSInclude []string

	// FSIgnore is a deprecated alias for FSExclude.
	// If both are set they are merged. Prefer FSExclude.
	FSIgnore []string

	// EmbeddingProvider is an optional embedding provider for semantic search.
	// When set, a PostIndexer will generate and store embeddings for Go symbols
	// and Markdown sections after each indexing run.
	// If nil (default), no embeddings are generated.
	EmbeddingProvider embeddings.Provider

	// GitConfig holds configuration for the git indexer.
	// Controls how many commits are indexed per repository (default: 500).
	GitConfig git.Config
}

// Axon is the main entry point for the axon library.
type Axon struct {
	graph    *graph.Graph
	storage  graph.Storage
	indexers *indexer.Registry
	config   Config
}

// New creates a new Axon instance with the given configuration.
func New(cfg Config) (*Axon, error) {
	if cfg.Dir == "" {
		cfg.Dir = "."
	}

	// Resolve to absolute path
	absDir, err := filepath.Abs(cfg.Dir)
	if err != nil {
		return nil, fmt.Errorf("resolving directory %q: %w", cfg.Dir, err)
	}
	cfg.Dir = absDir

	// Default storage (in-memory SQLite)
	if cfg.Storage == nil {
		s, err := sqlite.New(":memory:")
		if err != nil {
			return nil, fmt.Errorf("creating default storage: %w", err)
		}
		cfg.Storage = s
	}

	// Create registry with built-in types
	registry := graph.NewRegistry()
	types.RegisterCommonEdges(registry)
	types.RegisterFSTypes(registry)
	types.RegisterVCSTypes(registry)
	types.RegisterMarkdownTypes(registry)
	types.RegisterGoTypes(registry)
	types.RegisterProjectTypes(registry)
	types.RegisterTodoTypes(registry)

	// Create graph
	g := graph.New(cfg.Storage, registry)

	// Create indexer registry with built-in indexers
	idxRegistry := indexer.NewRegistry()

	// Merge exclude patterns (FSExclude + deprecated FSIgnore alias).
	// Use an explicit copy to avoid mutating the caller's backing array.
	exclude := append(append([]string(nil), cfg.FSExclude...), cfg.FSIgnore...)
	if len(exclude) == 0 {
		exclude = DefaultFSIgnore
	}
	idxRegistry.Register(fspkg.New(fspkg.Config{
		Include: cfg.FSInclude,
		Exclude: exclude,
	}))
	idxRegistry.Register(git.New(cfg.GitConfig))
	idxRegistry.Register(golang.New())
	idxRegistry.Register(markdown.New())
	idxRegistry.Register(project.New())
	idxRegistry.Register(todo.New())
	// Note: tagger is now called directly by fs indexer, not via events

	// Register embedding PostIndexer if a provider is configured
	if cfg.EmbeddingProvider != nil {
		idxRegistry.Register(embeddings.New(cfg.EmbeddingProvider))
	}

	return &Axon{
		graph:    g,
		storage:  cfg.Storage,
		indexers: idxRegistry,
		config:   cfg,
	}, nil
}

// Graph returns the underlying graph.
func (a *Axon) Graph() *graph.Graph {
	return a.graph
}

// IndexOptions configures the indexing behavior.
type IndexOptions struct {
	// Path is the path to index. If empty, uses the configured directory.
	Path string

	// Progress is an optional channel for reporting indexing progress.
	// Events are sent as indexers start, make progress, and complete.
	// The caller owns the channel; it is never closed by the library.
	// Mutually exclusive with ShowProgress — if both are set, Progress
	// takes precedence and ShowProgress is ignored.
	Progress chan<- progress.Event

	// ShowProgress writes a compact human-readable progress log to
	// os.Stderr. Intended for programmatic callers that want feedback
	// without wiring up a full progress channel or a bubbletea UI.
	//
	// Default: false (completely silent).
	ShowProgress bool

	// SkipGC skips garbage collection (orphaned edge cleanup) after indexing.
	// This can speed up indexing when you know cleanup isn't needed,
	// or when you plan to run `axon gc` separately.
	SkipGC bool
}

// Index indexes the given path and updates the graph.
// If path is empty, indexes the configured directory.
func (a *Axon) Index(ctx context.Context, path string) (*IndexResult, error) {
	return a.IndexWithOptions(ctx, IndexOptions{Path: path})
}

// IndexWithProgress indexes the given path and reports progress on the provided channel.
// If progress is nil, progress reporting is disabled.
func (a *Axon) IndexWithProgress(ctx context.Context, path string, prog chan<- progress.Event) (*IndexResult, error) {
	return a.IndexWithOptions(ctx, IndexOptions{Path: path, Progress: prog})
}

// IndexWithOptions indexes with the provided options.
//
// The library never writes to stdout or stderr unless
// opts.ShowProgress is true, in which case compact progress lines are
// written to stderr. Use opts.Progress for structured event access.
func (a *Axon) IndexWithOptions(ctx context.Context, opts IndexOptions) (*IndexResult, error) {
	startTime := time.Now()

	path := opts.Path
	prog := opts.Progress
	if path == "" {
		path = a.config.Dir
	}

	// ShowProgress: wire up a self-contained stderr drainer.
	// Only activated when the caller has not already supplied a Progress channel
	// (Progress takes precedence so callers can handle events themselves).
	var showProgCh chan progress.Event // non-nil only when we own the channel
	var showProgDone chan struct{}      // closed when drainer goroutine exits
	if opts.ShowProgress && prog == nil {
		showProgCh = make(chan progress.Event, 128)
		showProgDone = make(chan struct{})
		prog = showProgCh
		go func() {
			defer close(showProgDone)
			for evt := range showProgCh {
				switch evt.Type {
				case progress.EventStarted:
					fmt.Fprintf(os.Stderr, "[axon] %s: starting\n", evt.Indexer)
				case progress.EventCompleted:
					fmt.Fprintf(os.Stderr, "[axon] %s: done (%d items)\n", evt.Indexer, evt.Total)
				case progress.EventError:
					fmt.Fprintf(os.Stderr, "[axon] %s: error: %v\n", evt.Indexer, evt.Error)
				}
			}
		}()
	}
	// Ensure the drainer goroutine exits before IndexWithOptions returns so all
	// output is flushed. Works for every return path (error or success).
	defer func() {
		if showProgCh != nil {
			close(showProgCh)
			<-showProgDone // wait for last write to complete
		}
	}()

	// Resolve to absolute path
	absPath, err := filepath.Abs(path)
	if err != nil {
		return nil, fmt.Errorf("resolving path %q: %w", path, err)
	}

	uri := types.PathToURI(absPath)
	generation := graph.NewID()

	// Find indexer for this URI
	idx := a.indexers.ForURI(uri)
	if idx == nil {
		return nil, &ErrNoIndexer{URI: uri}
	}

	// Create event channel for indexer communication
	events := make(chan indexer.Event, eventChannelBuffer)

	// Create index context first (emitter needs it for counting)
	ictx := &indexer.Context{
		Root:       uri,
		Generation: generation,
		Graph:      a.graph,
		Progress:   prog,
		Events:     events,
	}

	// Create emitter with counting wrapper
	baseEmitter := indexer.NewGraphEmitter(a.graph, generation)
	ictx.Emitter = indexer.NewCountingEmitter(baseEmitter, ictx)

	// Track errors
	var indexerErrors []error
	var errorsMu sync.Mutex

	// Create per-subscriber channels and goroutines
	// Each subscriber gets ONE goroutine that processes events sequentially
	type subscriberInfo struct {
		idx     indexer.Indexer
		eventCh chan indexer.Event
	}
	subscriberMap := make(map[string]*subscriberInfo)

	var subscriberWg sync.WaitGroup
	for _, idx := range a.indexers.All() {
		if len(idx.Subscriptions()) > 0 {
			ch := make(chan indexer.Event, eventChannelBuffer)
			subscriberMap[idx.Name()] = &subscriberInfo{idx: idx, eventCh: ch}

			subscriberWg.Add(1)
			go func(subIdx indexer.Indexer, eventCh <-chan indexer.Event) {
				defer subscriberWg.Done()

				for {
					select {
					case <-ctx.Done():
						// Context cancelled, drain remaining events without processing
						for range eventCh {
						}
						return
					case event, ok := <-eventCh:
						if !ok {
							return // Channel closed
						}

						// Determine the root URI for the subscriber
						// Most indexers use the original root, but the git indexer
						// reacting to .git needs the repo path (parent of .git)
						eventRoot := uri
						if subIdx.Name() == "git" && event.Name == ".git" {
							eventRoot = types.RepoPathToURI(filepath.Dir(event.Path))
						}

						// Create event-specific context that shares the parent context's counters
						// We must create a new context per event because Root varies
						eventCtx := &indexer.Context{
							Root:       eventRoot,
							Generation: ictx.Generation,
							Graph:      ictx.Graph,
							Emitter:    ictx.Emitter,
							Progress:   ictx.Progress,
							Events:     ictx.Events,
						}

						if err := subIdx.HandleEvent(ctx, eventCtx, event); err != nil {
							errorsMu.Lock()
							indexerErrors = append(indexerErrors, err)
							errorsMu.Unlock()
						}

						// Aggregate deletions back to main context
						if deleted := eventCtx.NodesDeleted(); deleted > 0 {
							ictx.AddNodesDeleted(int(deleted))
						}
					}
				}
			}(idx, ch)
		}
	}

	// Event dispatcher goroutine - routes events to subscriber channels
	var dispatcherWg sync.WaitGroup
	dispatcherWg.Add(1)
	go func() {
		defer dispatcherWg.Done()
		defer func() {
			// Close all subscriber channels when dispatcher exits
			for _, info := range subscriberMap {
				close(info.eventCh)
			}
		}()

		for {
			select {
			case <-ctx.Done():
				// Context cancelled, drain events channel without dispatching
				for range events {
				}
				return
			case event, ok := <-events:
				if !ok {
					return // Events channel closed
				}
				// Find subscribers for this event and route to their channels
				subscribers := a.indexers.SubscribersFor(event)
				for _, sub := range subscribers {
					if info, ok := subscriberMap[sub.Name()]; ok {
						// When multiple subscribers share the same event, each must get
						// its own deep copy of the node. Without cloning, subscriber
						// goroutine N can start mutating the node (e.g. appending labels)
						// while the dispatcher is still reading it to clone for subscriber
						// N+1, causing a data race. All clones are made here, synchronously
						// in the dispatcher goroutine, before any subscriber goroutine
						// receives the event.
						eventCopy := event
						if len(subscribers) > 1 && event.Node != nil {
							eventCopy.Node = event.Node.Clone()
						}
						// Non-blocking send to subscriber channel
						// If channel is full, log warning and drop to prevent blocking the dispatcher.
						// This is intentional backpressure handling - subscribers should be fast.
						select {
						case info.eventCh <- eventCopy:
							// Event sent successfully
						case <-ctx.Done():
							return
						default:
							// Channel full - log warning and skip to prevent blocking
							log.Printf("axon: dispatcher: subscriber %s channel full, dropping event %v at %s",
								sub.Name(), eventCopy.Type, eventCopy.Path)
						}
					}
				}
			}
		}
	}()

	// Run primary indexer
	if err := idx.Index(ctx, ictx); err != nil {
		close(events)
		dispatcherWg.Wait()
		subscriberWg.Wait()
		return nil, fmt.Errorf("indexing %s: %w", uri, err)
	}

	// Close events channel and wait for dispatcher and all subscribers
	close(events)
	dispatcherWg.Wait()
	subscriberWg.Wait()

	// Flush storage buffer before post-index (so all nodes are queryable)
	if err := a.storage.Flush(ctx); err != nil {
		return nil, fmt.Errorf("flushing storage: %w", err)
	}

	// Run post-index stage for indexers that implement PostIndexer
	// (e.g., markdown indexer resolving links - it reports its own progress)
	for _, idx := range a.indexers.All() {
		if post, ok := idx.(indexer.PostIndexer); ok {
			postCtx := &indexer.Context{
				Root:       uri,
				Generation: generation,
				Graph:      a.graph,
				Emitter:    ictx.Emitter, // Use same counting emitter
				Progress:   prog,
			}
			if err := post.PostIndex(ctx, postCtx); err != nil {
				errorsMu.Lock()
				indexerErrors = append(indexerErrors, err)
				errorsMu.Unlock()
			}
		}
	}

	// Flush again after post-index
	if err := a.storage.Flush(ctx); err != nil {
		return nil, fmt.Errorf("flushing storage after post-index: %w", err)
	}

	// Clean up orphaned edges (edges pointing to deleted/missing nodes).
	// Always run unless explicitly skipped — the cost is a single fast SQL DELETE.
	var orphanedEdges int
	if !opts.SkipGC {
		if prog != nil {
			prog <- progress.Started("gc")
		}
		orphanedEdges, err = a.storage.DeleteOrphanedEdges(ctx)
		if err != nil {
			return nil, fmt.Errorf("deleting orphaned edges: %w", err)
		}
		if prog != nil {
			prog <- progress.Completed("gc", orphanedEdges)
		}
	}

	// Build result from actual indexing counts tracked in context
	result := &IndexResult{
		Files:        int(ictx.FilesIndexed()),
		Directories:  int(ictx.DirsIndexed()),
		Repos:        int(ictx.ReposIndexed()),
		StaleRemoved: orphanedEdges,
		RootURI:      uri,
		Generation:   generation,
		Errors:       indexerErrors,
	}

	// Record this indexing run for stats/history
	finishTime := time.Now()
	_ = a.storage.RecordIndexRun(ctx, graph.IndexRunRecord{
		StartedAt:    startTime,
		FinishedAt:   finishTime,
		DurationMs:   finishTime.Sub(startTime).Milliseconds(),
		RootPath:     absPath,
		FilesIndexed: result.Files,
		DirsIndexed:  result.Directories,
		ReposIndexed: result.Repos,
		StaleRemoved: result.StaleRemoved,
		Generation:   generation,
	})

	return result, nil
}

// RegisterIndexer adds a custom indexer.
func (a *Axon) RegisterIndexer(idx indexer.Indexer) {
	a.indexers.Register(idx)
}

// DeleteByPath removes the graph node(s) for the given filesystem path and
// cleans up orphaned edges. For directory paths it removes all nodes whose
// URI has the directory URI as a prefix (entire subtree).
// Returns nil when no node exists for the path (idempotent).
func (a *Axon) DeleteByPath(ctx context.Context, path string) error {
	absPath, err := filepath.Abs(path)
	if err != nil {
		return fmt.Errorf("resolving path %q: %w", path, err)
	}
	uri := types.PathToURI(absPath)
	if _, err := a.storage.DeleteByURIPrefix(ctx, uri); err != nil {
		return fmt.Errorf("deleting nodes for %s: %w", uri, err)
	}
	if _, err := a.storage.DeleteOrphanedEdges(ctx); err != nil {
		return fmt.Errorf("cleaning orphaned edges after delete: %w", err)
	}
	return nil
}

// SemanticSearchResult is a node with its best-match score and the query that produced it.
type SemanticSearchResult struct {
	*graph.NodeWithScore
	MatchedQuery string
}

// ErrNoEmbeddingProvider is returned when SemanticSearch is called but no
// EmbeddingProvider was configured.
var ErrNoEmbeddingProvider = errors.New("axon: no embedding provider configured; use axon init --embed to generate embeddings")

// SemanticSearch embeds each query string using the configured EmbeddingProvider
// and runs vector similarity search for each. Results across all queries are merged
// and deduplicated — the best score per node wins. Returns up to limit results sorted
// by score descending.
//
// Returns ErrNoEmbeddingProvider if no provider is set in Config.
func (a *Axon) SemanticSearch(ctx context.Context, queries []string, limit int, filter *graph.NodeFilter) ([]*SemanticSearchResult, error) {
	if a.config.EmbeddingProvider == nil {
		return nil, ErrNoEmbeddingProvider
	}
	if limit <= 0 {
		limit = 20
	}

	// best maps nodeID → highest-scoring SemanticSearchResult across all queries.
	best := make(map[string]*SemanticSearchResult)

	for _, q := range queries {
		vec, err := a.config.EmbeddingProvider.Embed(ctx, q)
		if err != nil {
			return nil, fmt.Errorf("embedding query %q: %w", q, err)
		}
		results, err := a.storage.FindSimilar(ctx, vec, limit, filter)
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

// SearchOptions configures a call to (*Axon).Search.
type SearchOptions struct {
	// Limit is the maximum number of results to return. Defaults to 20.
	Limit int

	// Filter restricts the search to nodes matching the given criteria.
	// nil = no filter (search all nodes).
	Filter *graph.NodeFilter

	// MinScore drops results whose similarity score is below this threshold.
	// Range 0.0–1.0. Default 0 = return all results.
	MinScore float32
}

// Querier is the read-only interface for executing queries against an axon graph.
// *Axon satisfies it, allowing integrators to depend on the interface for easier
// testing and decoupling.
type Querier interface {
	// Query executes a pre-built AQL query (from aql.Builder.Build or aql.Parse).
	Query(ctx context.Context, q *aql.Query) (*graph.QueryResult, error)

	// QueryString parses an AQL string and executes it in one call.
	QueryString(ctx context.Context, q string) (*graph.QueryResult, error)

	// Explain returns the execution plan for a pre-built AQL query without
	// running it. Useful for debugging and performance analysis.
	Explain(ctx context.Context, q *aql.Query) (*graph.QueryPlan, error)

	// Find returns nodes matching the structural filter.
	Find(ctx context.Context, filter graph.NodeFilter, opts graph.QueryOptions) ([]*graph.Node, error)

	// Search performs semantic vector similarity search.
	// Returns ErrNoEmbeddingProvider if no embedding provider is configured.
	Search(ctx context.Context, queries []string, opts SearchOptions) ([]*SemanticSearchResult, error)

	// FindPath finds the shortest paths between two nodes identified by their
	// node IDs. Returns an empty slice (no error) when no path exists within
	// the configured depth limit.
	FindPath(ctx context.Context, fromID, toID string, opts PathOptions) ([]*Path, error)
}

// compile-time check: *Axon must satisfy Querier.
var _ Querier = (*Axon)(nil)

// Query executes a pre-built AQL query against the graph.
func (a *Axon) Query(ctx context.Context, q *aql.Query) (*graph.QueryResult, error) {
	return a.storage.Query(ctx, q)
}

// QueryString parses the AQL string and executes it. Convenience wrapper
// around aql.Parse + Query.
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

// Search performs semantic vector similarity search with the given options.
// It wraps SemanticSearch and applies MinScore filtering after the search.
func (a *Axon) Search(ctx context.Context, queries []string, opts SearchOptions) ([]*SemanticSearchResult, error) {
	limit := opts.Limit
	if limit <= 0 {
		limit = 20
	}
	results, err := a.SemanticSearch(ctx, queries, limit, opts.Filter)
	if err != nil {
		return nil, err
	}
	if opts.MinScore > 0 {
		filtered := results[:0]
		for _, r := range results {
			if r.Score >= opts.MinScore {
				filtered = append(filtered, r)
			}
		}
		results = filtered
	}
	return results, nil
}

// FindPath finds the shortest paths between two nodes in the knowledge graph.
//
// fromID and toID are node IDs (as returned by Find, Search, or GetNodeByURI).
// Paths are discovered by bidirectional BFS: both outgoing and incoming edges
// are followed so that structural and semantic relationships are bridged.
//
// The search is bounded by opts.MaxDepth edges (default 6) and returns at most
// opts.MaxPaths results (default 3), ordered by ascending length.
//
// Returns an empty slice (no error) when no connecting path exists within the
// depth limit. Returns an error only when the origin node cannot be loaded or
// the storage layer fails.
func (a *Axon) FindPath(ctx context.Context, fromID, toID string, opts PathOptions) ([]*Path, error) {
	if opts.MaxDepth <= 0 {
		opts.MaxDepth = 6
	}
	if opts.MaxPaths <= 0 {
		opts.MaxPaths = 3
	}

	// Build an O(1) edge-type filter set; nil means "accept all types".
	var edgeTypes map[string]bool
	if len(opts.EdgeTypes) > 0 {
		edgeTypes = make(map[string]bool, len(opts.EdgeTypes))
		for _, t := range opts.EdgeTypes {
			edgeTypes[t] = true
		}
	}

	return findPaths(ctx, a.storage, fromID, toID, opts.MaxDepth, opts.MaxPaths, edgeTypes)
}

// WriteNode writes a node to the graph, flushes it to storage, and
// automatically generates and stores an embedding if an EmbeddingProvider is
// configured. This is the preferred way to persist custom nodes programmatically
// — the node will be immediately findable via Search without requiring a full
// re-index run.
func (a *Axon) WriteNode(ctx context.Context, node *graph.Node) error {
	if err := a.storage.PutNode(ctx, node); err != nil {
		return fmt.Errorf("WriteNode: put: %w", err)
	}
	if err := a.storage.Flush(ctx); err != nil {
		return fmt.Errorf("WriteNode: flush: %w", err)
	}
	if a.config.EmbeddingProvider != nil {
		type embedStore interface {
			PutEmbedding(context.Context, string, []float32) error
		}
		if es, ok := a.storage.(embedStore); ok {
			vec, err := a.config.EmbeddingProvider.Embed(ctx, embeddings.BuildNodeText(node))
			if err == nil {
				_ = es.PutEmbedding(ctx, node.ID, vec)
			}
		}
	}
	return nil
}

// PutNode writes a node to the storage layer without flushing or embedding.
// Use WriteNode for the full write-flush-embed cycle.
func (a *Axon) PutNode(ctx context.Context, node *graph.Node) error {
	return a.storage.PutNode(ctx, node)
}

// GetNodeByURI returns the node with the given URI, or an error if not found.
func (a *Axon) GetNodeByURI(ctx context.Context, uri string) (*graph.Node, error) {
	return a.storage.GetNodeByURI(ctx, uri)
}

// Flush flushes any buffered writes to the underlying storage.
func (a *Axon) Flush(ctx context.Context) error {
	return a.storage.Flush(ctx)
}

// IndexResult contains statistics from an indexing operation.
type IndexResult struct {
	Files        int
	Directories  int
	Repos        int
	StaleRemoved int
	RootURI      string
	Generation   string
	Errors       []error // Errors from individual indexers (non-fatal)
}

// WatchOptions configures the Watch behavior.
type WatchOptions struct {
	IndexOptions

	// Debounce is how long to wait after the last file event before
	// triggering a re-index. Default: 150ms.
	Debounce time.Duration

	// OnReady is called once after the initial full index completes.
	// It is NOT called on subsequent change-triggered re-indexes.
	// If nil, the initial result is silently discarded.
	OnReady func(result *IndexResult, err error)

	// OnReindex is called after each change-triggered re-index.
	// It is NOT called for the initial index (use OnReady for that).
	// If nil, re-index results are silently discarded.
	OnReindex func(path string, result *IndexResult, err error)
}

// Watch watches the given path for filesystem changes and re-indexes affected
// files automatically. It performs an initial full index, then blocks until
// ctx is cancelled, re-indexing individual changed files/directories on each
// batch of changes after the debounce window elapses.
func (a *Axon) Watch(ctx context.Context, path string, opts WatchOptions) error {
	if opts.Debounce == 0 {
		opts.Debounce = 150 * time.Millisecond
	}

	// Resolve to absolute path.
	absPath, err := filepath.Abs(path)
	if err != nil {
		return fmt.Errorf("resolving path %q: %w", path, err)
	}

	// Initial full index.
	initOpts := opts.IndexOptions
	initOpts.Path = absPath
	result, indexErr := a.IndexWithOptions(ctx, initOpts)
	if opts.OnReady != nil {
		opts.OnReady(result, indexErr)
	}
	if indexErr != nil {
		return fmt.Errorf("initial index: %w", indexErr)
	}

	// Retrieve the fs indexer to reuse its ShouldIgnore logic in the watcher,
	// so the event filter matches the indexer's exclusion rules exactly.
	var fsIdx *fspkg.Indexer
	if raw := a.indexers.ByName("fs"); raw != nil {
		fsIdx, _ = raw.(*fspkg.Indexer)
	}

	// Create fsnotify watcher.
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		return fmt.Errorf("creating watcher: %w", err)
	}
	defer watcher.Close()

	// Resolve the DB directory so we can exclude it from the watcher.
	dbDir := ""
	if a.storage != nil {
		if dbPather, ok := a.storage.(interface{ Path() string }); ok {
			dbDir = filepath.Dir(dbPather.Path())
		}
	}
	if dbDir == "" {
		candidate := filepath.Join(absPath, ".axon")
		if info, err2 := os.Stat(candidate); err2 == nil && info.IsDir() {
			dbDir = candidate
		}
	}

	// skipDir returns true for the DB directory and its children.
	skipDir := func(p string) bool {
		if dbDir == "" {
			return false
		}
		rel, err2 := filepath.Rel(dbDir, p)
		return err2 == nil && !strings.HasPrefix(rel, "..")
	}

	// shouldSkipWatch returns true for paths that should not be watched or
	// trigger re-indexing (DB dir, excluded patterns).
	shouldSkipWatch := func(p string) bool {
		if skipDir(p) {
			return true
		}
		if fsIdx != nil && fsIdx.ShouldIgnore(p, filepath.Base(p)) {
			return true
		}
		return false
	}

	// Walk directory tree and register every subdirectory with the watcher.
	if err := filepath.WalkDir(absPath, func(p string, d iofs.DirEntry, err error) error {
		if err != nil {
			return nil // skip inaccessible paths
		}
		if d.IsDir() {
			if shouldSkipWatch(p) {
				return iofs.SkipDir
			}
			if watchErr := watcher.Add(p); watchErr != nil {
				log.Printf("axon: watch: failed to watch %s: %v", p, watchErr)
			}
		}
		return nil
	}); err != nil {
		return fmt.Errorf("walking directory for watch: %w", err)
	}

	// Debounce loop: accumulate changed paths with their operation types,
	// then fire targeted re-index (or delete) per path after quiet period.
	pending := make(map[string]fsnotify.Op)
	var debounce <-chan time.Time

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()

		case event, ok := <-watcher.Events:
			if !ok {
				return nil
			}
			// Skip events from the DB directory.
			if shouldSkipWatch(event.Name) {
				continue
			}
			// Accumulate ops: multiple events for the same path within the
			// debounce window are OR-ed together.
			pending[event.Name] |= event.Op
			debounce = time.After(opts.Debounce)

		case watchErr, ok := <-watcher.Errors:
			if !ok {
				return nil
			}
			log.Printf("axon: watcher error: %v", watchErr)

		case <-debounce:
			if len(pending) == 0 {
				debounce = nil
				continue
			}
			snapshot := pending
			pending = make(map[string]fsnotify.Op)
			debounce = nil

			for changedPath, op := range snapshot {
				// Deletion: remove from graph and stop watching.
				if op&(fsnotify.Remove|fsnotify.Rename) != 0 {
					// Path was deleted or renamed away — remove from graph.
					// OnReindex is intentionally NOT called for deletions;
					// callers can't distinguish "0 files indexed" from a deletion.
					if err := a.DeleteByPath(ctx, changedPath); err != nil {
						log.Printf("axon: delete %s: %v", changedPath, err)
					}
					_ = watcher.Remove(changedPath)
					continue
				}

				// Created or modified: stat to confirm it still exists.
				info, statErr := os.Stat(changedPath)
				if statErr != nil {
					// File disappeared between event and debounce.
					if err := a.DeleteByPath(ctx, changedPath); err != nil {
						log.Printf("axon: delete disappeared %s: %v", changedPath, err)
					}
					continue
				}

				// New directory: recursively register it and all its subdirectories
				// with the watcher so future events inside deeply nested paths are
				// captured (e.g. after `git clone` or `cp -r`).
				if info.IsDir() && op&fsnotify.Create != 0 {
					_ = filepath.WalkDir(changedPath, func(p string, d iofs.DirEntry, err error) error {
						if err != nil {
							return nil // skip inaccessible paths
						}
						if d.IsDir() {
							if shouldSkipWatch(p) {
								return iofs.SkipDir
							}
							if watchErr := watcher.Add(p); watchErr != nil {
								log.Printf("axon: watch: failed to watch new dir %s: %v", p, watchErr)
							}
						}
						return nil
					})
				}

				// Re-index the individual file or directory.
				reindexOpts := opts.IndexOptions
				reindexOpts.Path = changedPath
				res, rerr := a.IndexWithOptions(ctx, reindexOpts)
				if opts.OnReindex != nil {
					opts.OnReindex(changedPath, res, rerr)
				}
			}
		}
	}
}

// ErrNoIndexer is returned when no indexer can handle a URI.
type ErrNoIndexer struct {
	URI string
}

func (e *ErrNoIndexer) Error() string {
	return "no indexer for URI: " + e.URI
}
