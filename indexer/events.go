package indexer

// EventType represents the type of indexing event.
type EventType int

const (
	// EventEntryVisited is emitted when an entry (file, dir, etc.) is visited.
	EventEntryVisited EventType = iota

	// EventNodeDeleting is emitted when a node is about to be deleted.
	// Subscribers can react by cleaning up related nodes.
	EventNodeDeleting
)

// Event represents an indexing event that other indexers can subscribe to.
type Event struct {
	// Type is the event type.
	Type EventType

	// URI is the full URI of the entry.
	URI string

	// Path is the filesystem path (if applicable).
	Path string

	// Name is the basename of the entry.
	Name string

	// NodeType is the type of node (e.g., "fs:dir", "fs:file").
	NodeType string

	// NodeID is the ID of the emitted node (empty if entry was skipped/ignored).
	NodeID string
}

// Subscription defines what events an indexer wants to receive.
type Subscription struct {
	// EventType is the type of event to subscribe to.
	EventType EventType

	// NodeType filters by node type (empty = all types).
	NodeType string

	// Name filters by entry name (empty = all names).
	Name string
}

// Matches returns true if the event matches this subscription.
func (s Subscription) Matches(e Event) bool {
	if s.EventType != e.Type {
		return false
	}
	if s.NodeType != "" && s.NodeType != e.NodeType {
		return false
	}
	if s.Name != "" && s.Name != e.Name {
		return false
	}
	return true
}
