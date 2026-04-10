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
	"github.com/codewandler/axon/indexer/tagger"
	"github.com/codewandler/axon/progress"
	"github.com/codewandler/axon/types"
)

// Config holds configuration for the filesystem indexer.
type Config struct {
	// Include contains glob patterns. When non-empty, only paths matching
	// at least one pattern are indexed (applied to files only; directories
	// are always traversed). Patterns matched against name and full path.
	Include []string

	// Exclude contains glob patterns to skip. Matched against file name
	// and full absolute path. Takes precedence over Include.
	Exclude []string

	// Ignore is a deprecated alias for Exclude. Merged with Exclude in New().
	Ignore []string
}

// Indexer indexes filesystem directories and files.
type Indexer struct {
	config Config
	tagger *tagger.Indexer
}

// New creates a new filesystem indexer with the given configuration.
func New(cfg Config) *Indexer {
	// Merge deprecated Ignore into Exclude. Use an explicit copy to avoid
	// mutating the caller's backing array via append.
	cfg.Exclude = append(append([]string(nil), cfg.Exclude...), cfg.Ignore...)
	cfg.Ignore = nil
	return &Indexer{
		config: cfg,
		tagger: tagger.New(tagger.Config{}),
	}
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

// discoveredEntry holds a discovered filesystem entry for later indexing.
type discoveredEntry struct {
	path      string
	entry     os.DirEntry
	ignored   bool // If true, entry is ignored but included for deletion detection
}

func (i *Indexer) Index(ctx context.Context, ictx *indexer.Context) error {
	rootPath := types.URIToPath(ictx.Root)

	// Report start
	if ictx.Progress != nil {
		ictx.Progress <- progress.Started(i.Name())
	}

	// Phase 1: Discovery - walk the tree and collect all entries
	// Pre-allocate for large directories (will grow if needed)
	entries := make([]discoveredEntry, 0, 100000)

	err := filepath.WalkDir(rootPath, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			// Skip entries we can't read (permission denied, etc.)
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

		// Check exclude patterns first (takes precedence over include)
		if i.ShouldIgnore(path, d.Name()) {
			if d.IsDir() {
				// Include ignored dirs for deletion detection, but mark as ignored
				entries = append(entries, discoveredEntry{
					path:    path,
					entry:   d,
					ignored: true,
				})
				return filepath.SkipDir
			}
			// Ignored files are simply skipped
			return nil
		}

		// Check include filter (files only — directories always traversed)
		if !d.IsDir() && !i.shouldInclude(path, d.Name()) {
			return nil
		}

		// Check bounds
		if !ictx.InBounds(uri) {
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}

		entries = append(entries, discoveredEntry{
			path:  path,
			entry: d,
		})

		return nil
	})

	if err != nil {
		if ictx.Progress != nil {
			ictx.Progress <- progress.Error(i.Name(), err)
		}
		return err
	}

	// Phase 2: Indexing - process all discovered entries
	// Now we know the total count for accurate progress reporting
	total := len(entries)
	nodeIDs := make(map[string]string, total)
	count := 0
	lastProgressTime := time.Now()

	for idx, entry := range entries {
		// Check context cancellation periodically
		if idx%1000 == 0 {
			select {
			case <-ctx.Done():
				return ctx.Err()
			default:
			}
		}

		node, err := i.indexEntry(ctx, ictx, entry, nodeIDs)
		if err != nil {
			if ictx.Progress != nil {
				ictx.Progress <- progress.Error(i.Name(), err)
			}
			return err
		}

		if node != nil {
			count++
		}

		// Progress reporting: every 100 items OR every 100ms
		now := time.Now()
		if ictx.Progress != nil && (idx%100 == 0 || now.Sub(lastProgressTime) > 100*time.Millisecond) {
			ictx.Progress <- progress.ProgressWithTotal(i.Name(), idx+1, total, entry.path)
			lastProgressTime = now
		}
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

// indexEntry creates a node for a discovered entry and emits it.
func (i *Indexer) indexEntry(ctx context.Context, ictx *indexer.Context, entry discoveredEntry, nodeIDs map[string]string) (*graph.Node, error) {
	path := entry.path
	d := entry.entry
	uri := types.PathToURI(path)

	var node *graph.Node

	if entry.ignored {
		// Ignored directory - create minimal node for deletion detection
		info, err := d.Info()
		if err != nil {
			return nil, err
		}
		node = graph.NewNode(types.TypeDir).
			WithURI(uri).
			WithKey(path).
			WithName(d.Name()).
			WithData(types.DirData{Name: d.Name(), Mode: info.Mode()})
	} else if d.IsDir() {
		info, err := d.Info()
		if err != nil {
			return nil, err
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
			return nil, err
		}
		ext := filepath.Ext(d.Name())
		contentType := mime.TypeByExtension(ext)
		// Strip the leading dot so data.ext = 'go' works in AQL
		// (filepath.Ext returns ".go"; we store "go" for intuitive queries).
		ext = strings.TrimPrefix(ext, ".")
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

	// Apply labels via tagger (direct call, no event overhead)
	if node.Type == types.TypeFile {
		rootPath := types.URIToPath(ictx.Root)
		relPath, _ := filepath.Rel(rootPath, path)
		i.tagger.TagNode(node, node.Type, d.Name(), relPath)
	}

	if err := ictx.Emitter.EmitNode(ctx, node); err != nil {
		return nil, err
	}
	nodeIDs[path] = node.ID

	// Emit event for visited entry (including ignored dirs for git indexer)
	// Note: tagger no longer subscribes to these events (handled above)
	if ictx.Events != nil {
		ictx.Events <- indexer.Event{
			Type:     indexer.EventEntryVisited,
			URI:      uri,
			Path:     path,
			Name:     d.Name(),
			NodeType: node.Type,
			NodeID:   node.ID,
			Node:     node,
		}
	}

	// Create containment edges from parent to this node
	parentPath := filepath.Dir(path)
	if parentID, ok := nodeIDs[parentPath]; ok {
		if err := indexer.EmitContainment(ctx, ictx.Emitter, parentID, node.ID); err != nil {
			return nil, err
		}
	}

	return node, nil
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
				Node:     node,
			}
		}
	}

	// Delete stale nodes and track count
	deleted, err := ictx.Graph.Storage().DeleteStaleByURIPrefix(ctx, ictx.Root, ictx.Generation)
	if deleted > 0 {
		ictx.AddNodesDeleted(deleted)
	}
	return err
}

func (i *Indexer) HandleEvent(ctx context.Context, ictx *indexer.Context, event indexer.Event) error {
	// FS indexer doesn't subscribe to events, so this should not be called.
	return nil
}

// ShouldIgnore reports whether path should be excluded from indexing.
// Exported so the watcher can apply the same logic to fsnotify events.
func (i *Indexer) ShouldIgnore(path, name string) bool {
	for _, pattern := range i.config.Exclude {
		if matched, _ := filepath.Match(pattern, name); matched {
			return true
		}
		if matched, _ := filepath.Match(pattern, path); matched {
			return true
		}
	}
	return false
}

// shouldInclude reports whether path passes the include filter.
// Always returns true when no include patterns are configured.
// Applied to files only; directories are never filtered by include patterns.
func (i *Indexer) shouldInclude(path, name string) bool {
	if len(i.config.Include) == 0 {
		return true
	}
	for _, pattern := range i.config.Include {
		if matched, _ := filepath.Match(pattern, name); matched {
			return true
		}
		if matched, _ := filepath.Match(pattern, path); matched {
			return true
		}
	}
	return false
}
