package axon

import (
	"context"
	"os"
	"path/filepath"

	"github.com/codewandler/axon/graph"
	"github.com/codewandler/axon/indexer"
	"github.com/codewandler/axon/indexer/fs"
	"github.com/codewandler/axon/indexer/git"
	"github.com/codewandler/axon/storage/memory"
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

	// Default storage
	if cfg.Storage == nil {
		cfg.Storage = memory.New()
	}

	// Create registry with built-in types
	registry := graph.NewRegistry()
	types.RegisterFSTypes(registry)
	types.RegisterVCSTypes(registry)

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

	// Create index context
	ictx := &indexer.Context{
		Root:       uri,
		Generation: generation,
		Graph:      a.graph,
		Emitter:    emitter,
	}

	// Run indexer
	if err := idx.Index(ctx, ictx); err != nil {
		return nil, err
	}

	// Auto-detect and index git repositories
	if err := a.indexGitRepos(ctx, absPath, emitter, generation); err != nil {
		// Log but don't fail - git indexing is supplementary
		_ = err
	}

	// Clean up stale nodes and edges
	staleNodes, err := a.storage.DeleteStaleNodes(ctx, uri, generation)
	if err != nil {
		return nil, err
	}

	staleEdges, err := a.storage.DeleteStaleEdges(ctx, generation)
	if err != nil {
		return nil, err
	}

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
		StaleRemoved: staleNodes + staleEdges + orphanedEdges,
		RootURI:      uri,
		Generation:   generation,
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
}

// ErrNoIndexer is returned when no indexer can handle a URI.
type ErrNoIndexer struct {
	URI string
}

func (e *ErrNoIndexer) Error() string {
	return "no indexer for URI: " + e.URI
}

// indexGitRepos scans for .git directories and indexes them.
func (a *Axon) indexGitRepos(ctx context.Context, rootPath string, emitter indexer.Emitter, generation string) error {
	// Find all .git directories
	return filepath.WalkDir(rootPath, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return nil // Skip errors
		}

		// Check for .git directory
		if d.IsDir() && d.Name() == ".git" {
			// The repo is in the parent directory
			repoPath := filepath.Dir(path)
			if err := git.IndexFromPath(ctx, a.graph, emitter, repoPath, generation); err != nil {
				// Log but continue
				_ = err
			}
			return filepath.SkipDir // Don't descend into .git
		}

		// Skip common non-repo directories
		if d.IsDir() {
			switch d.Name() {
			case "node_modules", "__pycache__", ".axon", "vendor":
				return filepath.SkipDir
			}
		}

		return nil
	})
}
