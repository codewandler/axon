package fs

import (
	"context"
	"mime"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/codewandler/axon/graph"
	"github.com/codewandler/axon/indexer"
	"github.com/codewandler/axon/progress"
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

func (i *Indexer) Subscriptions() []indexer.Subscription {
	// FS indexer is a primary indexer, doesn't subscribe to events
	return nil
}

func (i *Indexer) Index(ctx context.Context, ictx *indexer.Context) error {
	rootPath := types.URIToPath(ictx.Root)

	// Report start
	if ictx.Progress != nil {
		ictx.Progress <- progress.Started(i.Name())
	}

	// Track node IDs by path for creating edges
	nodeIDs := make(map[string]string)
	count := 0
	lastProgressTime := time.Now()

	err := filepath.WalkDir(rootPath, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			// Skip entries we can't read (permission denied, etc.)
			// Return SkipDir for directories to avoid descending into them
			if d != nil && d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}

		// Check context cancellation
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		uri := types.PathToURI(path)

		// Check ignore patterns - create node for ignored dirs (so we can detect deletion)
		// but skip their contents
		if i.shouldIgnore(path, d.Name()) {
			if d.IsDir() {
				// Create a node for ignored directories so we can detect when they're deleted
				info, err := d.Info()
				if err != nil {
					return err
				}
				node := graph.NewNode(types.TypeDir).
					WithURI(uri).
					WithKey(path).
					WithName(d.Name()).
					WithData(types.DirData{Name: d.Name(), Mode: info.Mode()})

				if err := ictx.Emitter.EmitNode(ctx, node); err != nil {
					return err
				}
				nodeIDs[path] = node.ID

				// Emit event for ignored dirs so subscribers (like git indexer) can react
				if ictx.Events != nil {
					ictx.Events <- indexer.Event{
						Type:     indexer.EventEntryVisited,
						URI:      uri,
						Path:     path,
						Name:     d.Name(),
						NodeType: node.Type,
						NodeID:   node.ID,
					}
				}

				// Create containment edges from parent
				parentPath := filepath.Dir(path)
				if parentID, ok := nodeIDs[parentPath]; ok {
					if err := indexer.EmitContainment(ctx, ictx.Emitter, parentID, node.ID); err != nil {
						return err
					}
				}

				return filepath.SkipDir
			}
			// Ignored files are simply skipped
			return nil
		}

		// Check bounds
		if !ictx.InBounds(uri) {
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}

		var node *graph.Node

		if d.IsDir() {
			info, err := d.Info()
			if err != nil {
				return err
			}
			node = graph.NewNode(types.TypeDir).
				WithURI(uri).
				WithKey(path).
				WithName(d.Name()).
				WithData(types.DirData{Name: d.Name(), Mode: info.Mode()})
		} else if d.Type()&os.ModeSymlink != 0 {
			target, _ := os.Readlink(path)
			node = graph.NewNode(types.TypeLink).
				WithURI(uri).
				WithKey(path).
				WithName(d.Name()).
				WithData(types.LinkData{Name: d.Name(), Target: target})
		} else {
			info, err := d.Info()
			if err != nil {
				return err
			}
			ext := filepath.Ext(d.Name())
			contentType := mime.TypeByExtension(ext)
			node = graph.NewNode(types.TypeFile).
				WithURI(uri).
				WithKey(path).
				WithName(d.Name()).
				WithData(types.FileData{
					Name:        d.Name(),
					Size:        info.Size(),
					Modified:    info.ModTime(),
					Mode:        info.Mode(),
					Ext:         ext,
					ContentType: contentType,
				})
		}

		if err := ictx.Emitter.EmitNode(ctx, node); err != nil {
			return err
		}
		nodeIDs[path] = node.ID
		count++

		// Emit event for visited entry
		if ictx.Events != nil {
			ictx.Events <- indexer.Event{
				Type:     indexer.EventEntryVisited,
				URI:      uri,
				Path:     path,
				Name:     d.Name(),
				NodeType: node.Type,
				NodeID:   node.ID,
			}
		}

		// Hybrid progress: every 50 items OR every 100ms
		now := time.Now()
		if ictx.Progress != nil && (count%50 == 0 || now.Sub(lastProgressTime) > 100*time.Millisecond) {
			ictx.Progress <- progress.Progress(i.Name(), count, path)
			lastProgressTime = now
		}

		// Create containment edges from parent to this node
		parentPath := filepath.Dir(path)
		if parentID, ok := nodeIDs[parentPath]; ok {
			if err := indexer.EmitContainment(ctx, ictx.Emitter, parentID, node.ID); err != nil {
				return err
			}
		}

		return nil
	})

	if err != nil {
		if ictx.Progress != nil {
			ictx.Progress <- progress.Error(i.Name(), err)
		}
		return err
	}

	// Cleanup stale nodes (only for direct invocations, not event-triggered)
	if ictx.TriggerEvent == nil {
		if err := i.cleanupStale(ctx, ictx); err != nil {
			if ictx.Progress != nil {
				ictx.Progress <- progress.Error(i.Name(), err)
			}
			return err
		}
	}

	// Report completion
	if ictx.Progress != nil {
		ictx.Progress <- progress.Completed(i.Name(), count)
	}

	return nil
}

// cleanupStale finds and removes stale nodes, emitting deletion events.
func (i *Indexer) cleanupStale(ctx context.Context, ictx *indexer.Context) error {
	// Find stale nodes under our URI prefix
	staleNodes, err := ictx.Graph.Storage().FindStaleByURIPrefix(ctx, ictx.Root, ictx.Generation)
	if err != nil {
		return err
	}

	// Emit deletion events for each stale node
	for _, node := range staleNodes {
		if ictx.Events != nil {
			ictx.Events <- indexer.Event{
				Type:     indexer.EventNodeDeleting,
				URI:      node.URI,
				Path:     types.URIToPath(node.URI),
				Name:     filepath.Base(types.URIToPath(node.URI)),
				NodeType: node.Type,
				NodeID:   node.ID,
			}
		}
	}

	// Delete stale nodes
	_, err = ictx.Graph.Storage().DeleteStaleByURIPrefix(ctx, ictx.Root, ictx.Generation)
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
