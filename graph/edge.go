package graph

import "time"

// Edge represents a directed relationship between two nodes.
type Edge struct {
	ID         string     `json:"id,omitempty"`
	Type       string     `json:"type,omitempty"`
	From       string     `json:"from,omitempty"`
	To         string     `json:"to,omitempty"`
	Data       any        `json:"data,omitempty"`
	Generation string     `json:"generation,omitempty"`
	CreatedAt  *time.Time `json:"created_at,omitempty"`
	ExpiresAt  *time.Time `json:"expires_at,omitempty"` // nil means the edge never expires (immortal)
}

// NewEdge creates a new edge with the given type and endpoints.
// The edge ID is deterministic based on (type, from, to), ensuring
// the same edge always has the same ID.
func NewEdge(edgeType, from, to string) *Edge {
	now := time.Now()
	return &Edge{
		ID:        IDFromEdgeKey(edgeType, from, to),
		Type:      edgeType,
		From:      from,
		To:        to,
		CreatedAt: &now,
	}
}

// WithData sets the edge's data payload and returns the edge for chaining.
func (e *Edge) WithData(data any) *Edge {
	e.Data = data
	return e
}

// WithGeneration sets the edge's generation for staleness tracking.
func (e *Edge) WithGeneration(gen string) *Edge {
	e.Generation = gen
	return e
}

// WithTTL sets the edge's expiry time to now + d. After d has elapsed, the
// edge is treated as non-existent by all read paths. Pass 0 to leave the edge
// immortal (same as not calling WithTTL at all).
// GC must run (axon gc) to physically remove expired rows.
func (e *Edge) WithTTL(d time.Duration) *Edge {
	if d > 0 {
		t := time.Now().Add(d)
		e.ExpiresAt = &t
	}
	return e
}
