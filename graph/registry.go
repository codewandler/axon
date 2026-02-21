package graph

import (
	"errors"
	"fmt"
	"reflect"
	"sync"
)

// ErrUnknownNodeType is returned when a node type is not registered.
var ErrUnknownNodeType = errors.New("unknown node type")

// ErrUnknownEdgeType is returned when an edge type is not registered.
var ErrUnknownEdgeType = errors.New("unknown edge type")

// NodeSpec describes a registered node type.
type NodeSpec struct {
	Type        string
	Description string
	DataType    reflect.Type // The Go type for Data field
}

// EdgeSpec describes a registered edge type.
type EdgeSpec struct {
	Type        string
	Description string
	FromTypes   []string // Allowed source node types (empty = any)
	ToTypes     []string // Allowed target node types (empty = any)
}

// Registry holds registered node and edge type specifications.
type Registry struct {
	mu        sync.RWMutex
	nodeTypes map[string]NodeSpec
	edgeTypes map[string]EdgeSpec
}

// NewRegistry creates a new empty registry.
func NewRegistry() *Registry {
	return &Registry{
		nodeTypes: make(map[string]NodeSpec),
		edgeTypes: make(map[string]EdgeSpec),
	}
}

// RegisterNodeType registers a node type with its data schema.
// The generic type T specifies what type the node's Data field should hold.
func RegisterNodeType[T any](r *Registry, spec NodeSpec) {
	r.mu.Lock()
	defer r.mu.Unlock()

	spec.DataType = reflect.TypeOf((*T)(nil)).Elem()
	r.nodeTypes[spec.Type] = spec
}

// RegisterEdgeType registers an edge type.
func (r *Registry) RegisterEdgeType(spec EdgeSpec) {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.edgeTypes[spec.Type] = spec
}

// NodeSpec returns the spec for a node type, if registered.
func (r *Registry) NodeSpec(nodeType string) (NodeSpec, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	spec, ok := r.nodeTypes[nodeType]
	return spec, ok
}

// EdgeSpec returns the spec for an edge type, if registered.
func (r *Registry) EdgeSpec(edgeType string) (EdgeSpec, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	spec, ok := r.edgeTypes[edgeType]
	return spec, ok
}

// NodeTypes returns all registered node types.
func (r *Registry) NodeTypes() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()

	types := make([]string, 0, len(r.nodeTypes))
	for t := range r.nodeTypes {
		types = append(types, t)
	}
	return types
}

// EdgeTypes returns all registered edge types.
func (r *Registry) EdgeTypes() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()

	types := make([]string, 0, len(r.edgeTypes))
	for t := range r.edgeTypes {
		types = append(types, t)
	}
	return types
}

// ValidateNode validates that a node's type is registered.
func (r *Registry) ValidateNode(n *Node) error {
	r.mu.RLock()
	defer r.mu.RUnlock()

	if _, ok := r.nodeTypes[n.Type]; !ok {
		return fmt.Errorf("%w: %s", ErrUnknownNodeType, n.Type)
	}
	return nil
}

// ValidateEdge validates that an edge's type is registered and
// that the from/to node types are allowed (if constraints are specified).
func (r *Registry) ValidateEdge(e *Edge, fromNode, toNode *Node) error {
	r.mu.RLock()
	defer r.mu.RUnlock()

	spec, ok := r.edgeTypes[e.Type]
	if !ok {
		return fmt.Errorf("%w: %s", ErrUnknownEdgeType, e.Type)
	}

	if len(spec.FromTypes) > 0 && fromNode != nil {
		if !contains(spec.FromTypes, fromNode.Type) {
			return fmt.Errorf("edge type %q does not allow source node type %q", e.Type, fromNode.Type)
		}
	}

	if len(spec.ToTypes) > 0 && toNode != nil {
		if !contains(spec.ToTypes, toNode.Type) {
			return fmt.Errorf("edge type %q does not allow target node type %q", e.Type, toNode.Type)
		}
	}

	return nil
}

func contains(slice []string, item string) bool {
	for _, s := range slice {
		if s == item {
			return true
		}
	}
	return false
}
