package fs

import (
	"context"
	"os"
	"path/filepath"
	"strings"

	"github.com/codewandler/axon/graph"
	"github.com/codewandler/axon/indexer"
	"github.com/codewandler/axon/types"
)

// Config holds configuration for the filesystem indexer.
type Config struct {
	// Ignore contains glob patterns to ignore (e.g., ".git", "node_modules").
	Ignore []string
}

// Indexer indexes filesystem directories and files.
type Indexer struct {
	config Config
}

// New creates a new filesystem indexer with the given configuration.
func New(cfg Config) *Indexer {
	return &Indexer{config: cfg}
}

func (i *Indexer) Name() string {
	return "fs"
}

func (i *Indexer) Schemes() []string {
	return []string{"file"}
}

func (i *Indexer) Handles(uri string) bool {
	return strings.HasPrefix(uri, "file://")
}

func (i *Indexer) Index(ctx context.Context, ictx *indexer.Context) error {
	rootPath := types.URIToPath(ictx.Root)

	// Track node IDs by path for creating edges
	nodeIDs := make(map[string]string)

	err := filepath.WalkDir(rootPath, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}

		// Check context cancellation
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		// Check ignore patterns
		if i.shouldIgnore(path, d.Name()) {
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}

		uri := types.PathToURI(path)

		// Check bounds
		if !ictx.InBounds(uri) {
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}

		var node *graph.Node

		if d.IsDir() {
			node = graph.NewNode(types.TypeDir).
				WithURI(uri).
				WithKey(path).
				WithData(types.DirData{Name: d.Name()})
		} else if d.Type()&os.ModeSymlink != 0 {
			target, _ := os.Readlink(path)
			node = graph.NewNode(types.TypeLink).
				WithURI(uri).
				WithKey(path).
				WithData(types.LinkData{Name: d.Name(), Target: target})
		} else {
			info, err := d.Info()
			if err != nil {
				return err
			}
			node = graph.NewNode(types.TypeFile).
				WithURI(uri).
				WithKey(path).
				WithData(types.FileData{
					Name:     d.Name(),
					Size:     info.Size(),
					Modified: info.ModTime(),
				})
		}

		if err := ictx.Emitter.EmitNode(ctx, node); err != nil {
			return err
		}
		nodeIDs[path] = node.ID

		// Create edge from parent to this node
		parentPath := filepath.Dir(path)
		if parentID, ok := nodeIDs[parentPath]; ok {
			edge := graph.NewEdge(types.EdgeContains, parentID, node.ID)
			if err := ictx.Emitter.EmitEdge(ctx, edge); err != nil {
				return err
			}
		}

		return nil
	})

	return err
}

func (i *Indexer) shouldIgnore(path, name string) bool {
	for _, pattern := range i.config.Ignore {
		// Check if pattern matches the name directly
		if matched, _ := filepath.Match(pattern, name); matched {
			return true
		}
		// Check if pattern matches the full path
		if matched, _ := filepath.Match(pattern, path); matched {
			return true
		}
	}
	return false
}
