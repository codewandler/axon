package indexer

import (
	"context"
	"strings"

	"github.com/codewandler/axon/graph"
)

// Context provides the execution environment for an indexer.
type Context struct {
	// Root is the URI where indexing started (defines the boundary).
	Root string

	// Generation is the current index generation for staleness tracking.
	Generation string

	// Graph provides access to the existing graph state.
	Graph *graph.Graph

	// Emitter is where discovered nodes and edges should be emitted.
	Emitter Emitter
}

// InBounds returns true if the given URI is within the root boundary.
// This is used to prevent indexers from traversing outside their scope.
func (c *Context) InBounds(uri string) bool {
	return strings.HasPrefix(uri, c.Root)
}

// Indexer discovers and indexes nodes/edges from a specific domain.
type Indexer interface {
	// Name returns the indexer identifier (e.g., "fs", "git", "golang").
	Name() string

	// Schemes returns the URI schemes this indexer handles (e.g., ["file"], ["git"]).
	Schemes() []string

	// Handles returns true if this indexer can process the given URI.
	Handles(uri string) bool

	// Index indexes starting from the root URI in the context.
	Index(ctx context.Context, ictx *Context) error
}
