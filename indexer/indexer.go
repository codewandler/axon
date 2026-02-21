package indexer

import (
	"context"
	"strings"

	"github.com/codewandler/axon/graph"
	"github.com/codewandler/axon/progress"
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

	// Progress is an optional channel for reporting indexing progress.
	// If nil, progress reporting is disabled.
	Progress chan<- progress.Event

	// Events is an optional channel for broadcasting indexer events.
	// Other indexers can subscribe to these events to react dynamically.
	// If nil, event broadcasting is disabled.
	Events chan<- Event

	// TriggerEvent is the event that triggered this indexer invocation.
	// Nil for direct invocations (primary indexers).
	// Set when the indexer is triggered by an event subscription.
	TriggerEvent *Event
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

	// Subscriptions returns the events this indexer subscribes to.
	// Return nil or empty slice if this indexer doesn't subscribe to events
	// (i.e., it's a primary indexer triggered directly, not by events).
	Subscriptions() []Subscription

	// Index indexes starting from the root URI in the context.
	Index(ctx context.Context, ictx *Context) error
}

// PostIndexer is an optional interface for indexers that need a post-processing stage.
// This is called after all indexers have completed their initial Index() pass,
// allowing deferred resolution (e.g., resolving local links to files indexed later).
type PostIndexer interface {
	Indexer
	// PostIndex is called after all indexers complete their Index() pass.
	PostIndex(ctx context.Context, ictx *Context) error
}
