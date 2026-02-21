package graph

import (
	"time"

	gonanoid "github.com/matoous/go-nanoid/v2"
)

// Node represents a vertex in the graph.
type Node struct {
	ID         string    `json:"id"`
	Type       string    `json:"type"`
	URI        string    `json:"uri,omitempty"`
	Key        string    `json:"key,omitempty"`
	Data       any       `json:"data,omitempty"`
	Generation string    `json:"generation,omitempty"`
	CreatedAt  time.Time `json:"created_at"`
	UpdatedAt  time.Time `json:"updated_at"`
}

// NewID generates a new unique node ID using gonanoid.
func NewID() string {
	id, err := gonanoid.New()
	if err != nil {
		// gonanoid only errors if the random source fails,
		// which is extremely unlikely. Panic is acceptable here.
		panic(err)
	}
	return id
}

// NewNode creates a new node with the given type and a generated ID.
func NewNode(nodeType string) *Node {
	now := time.Now()
	return &Node{
		ID:        NewID(),
		Type:      nodeType,
		CreatedAt: now,
		UpdatedAt: now,
	}
}

// WithURI sets the node's URI and returns the node for chaining.
func (n *Node) WithURI(uri string) *Node {
	n.URI = uri
	return n
}

// WithKey sets the node's natural key and returns the node for chaining.
func (n *Node) WithKey(key string) *Node {
	n.Key = key
	return n
}

// WithData sets the node's data payload and returns the node for chaining.
func (n *Node) WithData(data any) *Node {
	n.Data = data
	return n
}

// WithGeneration sets the node's generation for staleness tracking.
func (n *Node) WithGeneration(gen string) *Node {
	n.Generation = gen
	return n
}
