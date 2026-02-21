package axon

import (
	"context"
	"path/filepath"
	"sync"

	"github.com/codewandler/axon/adapters/sqlite"
	"github.com/codewandler/axon/graph"
	"github.com/codewandler/axon/indexer"
	"github.com/codewandler/axon/indexer/fs"
	"github.com/codewandler/axon/indexer/git"
	"github.com/codewandler/axon/indexer/markdown"
	"github.com/codewandler/axon/progress"
	"github.com/codewandler/axon/types"
)

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
		return nil, err
	}
	cfg.Dir = absDir

	// Default storage (in-memory SQLite)
	if cfg.Storage == nil {
		s, err := sqlite.New(":memory:")
		if err != nil {
			return nil, err
		}
		cfg.Storage = s
	}

	// Create registry with built-in types
	registry := graph.NewRegistry()
	types.RegisterCommonEdges(registry)
	types.RegisterFSTypes(registry)
	types.RegisterVCSTypes(registry)
	types.RegisterMarkdownTypes(registry)

	// Create graph
	g := graph.New(cfg.Storage, registry)

	// Create indexer registry with built-in indexers
	idxRegistry := indexer.NewRegistry()

	// Default ignore patterns
	ignore := cfg.FSIgnore
	if len(ignore) == 0 {
		ignore = []string{".git", ".axon", "node_modules", "__pycache__", ".DS_Store"}
	}
	idxRegistry.Register(fs.New(fs.Config{Ignore: ignore}))
	idxRegistry.Register(git.New())
	idxRegistry.Register(markdown.New())

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

// Index indexes the given path and updates the graph.
// If path is empty, indexes the configured directory.
func (a *Axon) Index(ctx context.Context, path string) (*IndexResult, error) {
	return a.IndexWithProgress(ctx, path, nil)
}

// IndexWithProgress indexes the given path and reports progress on the provided channel.
// If progress is nil, progress reporting is disabled.
func (a *Axon) IndexWithProgress(ctx context.Context, path string, prog chan<- progress.Event) (*IndexResult, error) {
	if path == "" {
		path = a.config.Dir
	}

	// Resolve to absolute path
	absPath, err := filepath.Abs(path)
	if err != nil {
		return nil, err
	}

	uri := types.PathToURI(absPath)
	generation := graph.NewID()

	// Find indexer for this URI
	idx := a.indexers.ForURI(uri)
	if idx == nil {
		return nil, &ErrNoIndexer{URI: uri}
	}

	// Create emitter
	emitter := indexer.NewGraphEmitter(a.graph, generation)

	// Create event channel for indexer communication
	events := make(chan indexer.Event, 100)

	// Create index context
	ictx := &indexer.Context{
		Root:       uri,
		Generation: generation,
		Graph:      a.graph,
		Emitter:    emitter,
		Progress:   prog,
		Events:     events,
	}

	// Track active indexers
	var wg sync.WaitGroup
	var indexerErrors []error
	var errorsMu sync.Mutex

	// Event dispatcher goroutine
	wg.Add(1)
	go func() {
		defer wg.Done()
		for event := range events {
			// Find subscribers for this event
			subscribers := a.indexers.SubscribersFor(event)
			for _, sub := range subscribers {
				// Determine the root path for the subscriber
				// For git indexer reacting to .git, root is parent dir
				subRoot := event.Path
				if event.Name == ".git" {
					subRoot = filepath.Dir(event.Path)
				}

				// Capture event for closure
				triggerEvent := event

				wg.Add(1)
				go func(subIdx indexer.Indexer, root string, trigger indexer.Event) {
					defer wg.Done()

					// Create context for subscribed indexer
					subCtx := &indexer.Context{
						Root:         types.RepoPathToURI(root),
						Generation:   generation,
						Graph:        a.graph,
						Emitter:      emitter,
						Progress:     prog,
						Events:       events, // Allow chaining events
						TriggerEvent: &trigger,
					}

					if err := subIdx.Index(ctx, subCtx); err != nil {
						errorsMu.Lock()
						indexerErrors = append(indexerErrors, err)
						errorsMu.Unlock()
					}
				}(sub, subRoot, triggerEvent)
			}
		}
	}()

	// Run primary indexer
	if err := idx.Index(ctx, ictx); err != nil {
		close(events)
		wg.Wait()
		return nil, err
	}

	// Close events channel and wait for all indexers
	close(events)
	wg.Wait()

	// Flush storage buffer before post-index (so all nodes are queryable)
	if err := a.storage.Flush(ctx); err != nil {
		return nil, err
	}

	// Run post-index stage for indexers that implement PostIndexer
	for _, idx := range a.indexers.All() {
		if post, ok := idx.(indexer.PostIndexer); ok {
			postCtx := &indexer.Context{
				Root:       uri,
				Generation: generation,
				Graph:      a.graph,
				Emitter:    emitter,
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
		return nil, err
	}

	// Clean up orphaned edges (edges pointing to deleted nodes)
	// Note: We don't use DeleteStaleEdges because it's global and would delete
	// edges from other indexed directories. Indexers clean up their own stale
	// nodes, and orphaned edges are removed here.
	orphanedEdges, err := a.storage.DeleteOrphanedEdges(ctx)
	if err != nil {
		return nil, err
	}

	// Count what we indexed
	nodes, err := a.graph.FindNodes(ctx, graph.NodeFilter{})
	if err != nil {
		return nil, err
	}

	var files, dirs, repos int
	for _, n := range nodes {
		switch n.Type {
		case types.TypeFile:
			files++
		case types.TypeDir:
			dirs++
		case types.TypeRepo:
			repos++
		}
	}

	return &IndexResult{
		Files:        files,
		Directories:  dirs,
		Repos:        repos,
		StaleRemoved: orphanedEdges,
		RootURI:      uri,
		Generation:   generation,
		Errors:       indexerErrors,
	}, nil
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
