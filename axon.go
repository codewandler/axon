package axon

import (
	"context"
	"fmt"
	"log"
	"path/filepath"
	"sync"
	"time"

	"github.com/codewandler/axon/adapters/sqlite"
	"github.com/codewandler/axon/graph"
	"github.com/codewandler/axon/indexer"
	"github.com/codewandler/axon/indexer/fs"
	"github.com/codewandler/axon/indexer/git"
	"github.com/codewandler/axon/indexer/golang"
	"github.com/codewandler/axon/indexer/markdown"
	"github.com/codewandler/axon/indexer/project"
	"github.com/codewandler/axon/progress"
	"github.com/codewandler/axon/types"
)

const (
	// eventChannelBuffer is the buffer size for event channels.
	// This provides backpressure - if subscribers are slow, events will queue
	// up to this limit before the dispatcher drops them with a warning.
	eventChannelBuffer = 10000
)

// DefaultFSIgnore contains the default patterns to ignore when indexing.
var DefaultFSIgnore = []string{
	".git",
	".axon",
	".idea",   // JetBrains IDE config
	".vscode", // VS Code config
	"node_modules",
	"__pycache__",
	".DS_Store",
	"target",      // Rust/Cargo build output
	"vendor",      // Go vendor, PHP composer
	".venv",       // Python virtual environments
	".virtualenv", // Python virtual environments (alt)
	"venv",        // Python virtual environments (alt)
	"env",         // Python virtual environments (alt)
	"dist",        // JS/TS build output
	"build",       // Generic build output
	".tox",        // Python tox testing
	".pytest_cache",
	".mypy_cache",
	"site-packages", // Python packages (catches nested ones)
}

// Config holds configuration for an Axon instance.
type Config struct {
	// Dir is the working directory. Defaults to current directory.
	Dir string

	// Storage is the storage backend. Defaults to in-memory storage.
	Storage graph.Storage

	// FSIgnore contains glob patterns to ignore when indexing filesystem.
	FSIgnore []string
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

	// Create graph
	g := graph.New(cfg.Storage, registry)

	// Create indexer registry with built-in indexers
	idxRegistry := indexer.NewRegistry()

	// Default ignore patterns
	ignore := cfg.FSIgnore
	if len(ignore) == 0 {
		ignore = DefaultFSIgnore
	}
	idxRegistry.Register(fs.New(fs.Config{Ignore: ignore}))
	idxRegistry.Register(git.New())
	idxRegistry.Register(golang.New())
	idxRegistry.Register(markdown.New())
	idxRegistry.Register(project.New())
	// Note: tagger is now called directly by fs indexer, not via events

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
	Progress chan<- progress.Event

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
func (a *Axon) IndexWithOptions(ctx context.Context, opts IndexOptions) (*IndexResult, error) {
	startTime := time.Now()

	path := opts.Path
	prog := opts.Progress
	if path == "" {
		path = a.config.Dir
	}

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
				for i, sub := range subscribers {
					if info, ok := subscriberMap[sub.Name()]; ok {
						// Clone the event's node for each subscriber to prevent data races
						// when multiple subscribers modify the node concurrently.
						// Only clone for 2nd+ subscriber to avoid unnecessary allocations.
						eventCopy := event
						if i > 0 && event.Node != nil {
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

	// Clean up orphaned edges (edges pointing to deleted nodes)
	// Optimization: Skip if no nodes were deleted (common case for re-indexing).
	// Also skip if SkipGC option is set.
	var orphanedEdges int
	if !opts.SkipGC && ictx.NodesDeleted() > 0 {
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

// ErrNoIndexer is returned when no indexer can handle a URI.
type ErrNoIndexer struct {
	URI string
}

func (e *ErrNoIndexer) Error() string {
	return "no indexer for URI: " + e.URI
}
