package graph

import "time"

// Edge represents a directed relationship between two nodes.
type Edge struct {
	ID         string    `json:"id"`
	Type       string    `json:"type"`
	From       string    `json:"from"`
	To         string    `json:"to"`
	Data       any       `json:"data,omitempty"`
	Generation string    `json:"generation,omitempty"`
	CreatedAt  time.Time `json:"created_at"`
}

// NewEdge creates a new edge with the given type and endpoints.
// The edge ID is deterministic based on (type, from, to), ensuring
// the same edge always has the same ID.
func NewEdge(edgeType, from, to string) *Edge {
	return &Edge{
		ID:        IDFromEdgeKey(edgeType, from, to),
		Type:      edgeType,
		From:      from,
		To:        to,
		CreatedAt: time.Now(),
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
