package graph

import (
	"crypto/sha256"
	"encoding/base64"
	"time"

	gonanoid "github.com/matoous/go-nanoid/v2"
)

// Node represents a vertex in the graph.
type Node struct {
	ID         string    `json:"id"`
	Type       string    `json:"type"`
	URI        string    `json:"uri,omitempty"`
	Key        string    `json:"key,omitempty"`
	Name       string    `json:"name,omitempty"` // Human-readable name (filename, branch name, section title)
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

// IDFromURI generates a deterministic ID from a URI.
// The ID is URL-safe base64 encoded (22 characters) derived from SHA256.
// Same URI always produces the same ID.
func IDFromURI(uri string) string {
	hash := sha256.Sum256([]byte(uri))
	// Use first 16 bytes (128 bits), encode as URL-safe base64
	// This gives us 22 characters, similar length to nanoid
	return base64.RawURLEncoding.EncodeToString(hash[:16])
}

// IDFromEdgeKey generates a deterministic ID from an edge's natural key.
// The natural key is (type, from, to). Same key always produces the same ID.
func IDFromEdgeKey(edgeType, from, to string) string {
	key := edgeType + "\x00" + from + "\x00" + to // null-separated to avoid collisions
	hash := sha256.Sum256([]byte(key))
	return base64.RawURLEncoding.EncodeToString(hash[:16])
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
// It also sets the node's ID to a deterministic value derived from the URI,
// ensuring the same URI always produces the same node ID.
func (n *Node) WithURI(uri string) *Node {
	n.URI = uri
	n.ID = IDFromURI(uri)
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

// WithName sets the node's human-readable name and returns the node for chaining.
func (n *Node) WithName(name string) *Node {
	n.Name = name
	return n
}

// WithGeneration sets the node's generation for staleness tracking.
func (n *Node) WithGeneration(gen string) *Node {
	n.Generation = gen
	return n
}
