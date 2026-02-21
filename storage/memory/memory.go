package memory

import (
	"context"
	"strings"
	"sync"

	"github.com/codewandler/axon/graph"
	"github.com/codewandler/axon/storage"
)

// Ensure Storage implements graph.Storage.
var _ graph.Storage = (*Storage)(nil)

// Storage is an in-memory implementation of the graph.Storage interface.
type Storage struct {
	mu sync.RWMutex

	// Primary storage
	nodes map[string]*graph.Node // id -> node
	edges map[string]*graph.Edge // id -> edge

	// Indexes
	nodesByURI  map[string]string          // uri -> id
	nodesByKey  map[string]string          // "type:key" -> id
	nodesByType map[string]map[string]bool // type -> set of ids
	edgesFrom   map[string]map[string]bool // nodeID -> set of edge ids
	edgesTo     map[string]map[string]bool // nodeID -> set of edge ids
}

// New creates a new in-memory storage.
func New() *Storage {
	return &Storage{
		nodes:       make(map[string]*graph.Node),
		edges:       make(map[string]*graph.Edge),
		nodesByURI:  make(map[string]string),
		nodesByKey:  make(map[string]string),
		nodesByType: make(map[string]map[string]bool),
		edgesFrom:   make(map[string]map[string]bool),
		edgesTo:     make(map[string]map[string]bool),
	}
}

func (s *Storage) PutNode(ctx context.Context, node *graph.Node) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Remove old indexes if updating
	if old, exists := s.nodes[node.ID]; exists {
		s.removeNodeIndexes(old)
	}

	// Store a copy to avoid mutation issues
	nodeCopy := *node
	s.nodes[node.ID] = &nodeCopy

	// Update indexes
	if node.URI != "" {
		s.nodesByURI[node.URI] = node.ID
	}
	if node.Key != "" {
		s.nodesByKey[node.Type+":"+node.Key] = node.ID
	}
	if s.nodesByType[node.Type] == nil {
		s.nodesByType[node.Type] = make(map[string]bool)
	}
	s.nodesByType[node.Type][node.ID] = true

	return nil
}

func (s *Storage) GetNode(ctx context.Context, id string) (*graph.Node, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	node, ok := s.nodes[id]
	if !ok {
		return nil, storage.ErrNodeNotFound
	}
	// Return a copy to prevent external mutation
	nodeCopy := *node
	return &nodeCopy, nil
}

func (s *Storage) GetNodeByURI(ctx context.Context, uri string) (*graph.Node, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	id, ok := s.nodesByURI[uri]
	if !ok {
		return nil, storage.ErrNodeNotFound
	}
	nodeCopy := *s.nodes[id]
	return &nodeCopy, nil
}

func (s *Storage) GetNodeByKey(ctx context.Context, nodeType, key string) (*graph.Node, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	id, ok := s.nodesByKey[nodeType+":"+key]
	if !ok {
		return nil, storage.ErrNodeNotFound
	}
	nodeCopy := *s.nodes[id]
	return &nodeCopy, nil
}

func (s *Storage) DeleteNode(ctx context.Context, id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	node, ok := s.nodes[id]
	if !ok {
		return storage.ErrNodeNotFound
	}

	s.removeNodeIndexes(node)
	delete(s.nodes, id)

	return nil
}

func (s *Storage) removeNodeIndexes(node *graph.Node) {
	if node.URI != "" {
		delete(s.nodesByURI, node.URI)
	}
	if node.Key != "" {
		delete(s.nodesByKey, node.Type+":"+node.Key)
	}
	if typeSet := s.nodesByType[node.Type]; typeSet != nil {
		delete(typeSet, node.ID)
	}
}

func (s *Storage) PutEdge(ctx context.Context, edge *graph.Edge) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Remove old indexes if updating
	if old, exists := s.edges[edge.ID]; exists {
		s.removeEdgeIndexes(old)
	}

	// Store a copy to avoid mutation issues
	edgeCopy := *edge
	s.edges[edge.ID] = &edgeCopy

	// Update indexes
	if s.edgesFrom[edge.From] == nil {
		s.edgesFrom[edge.From] = make(map[string]bool)
	}
	s.edgesFrom[edge.From][edge.ID] = true

	if s.edgesTo[edge.To] == nil {
		s.edgesTo[edge.To] = make(map[string]bool)
	}
	s.edgesTo[edge.To][edge.ID] = true

	return nil
}

func (s *Storage) GetEdge(ctx context.Context, id string) (*graph.Edge, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	edge, ok := s.edges[id]
	if !ok {
		return nil, storage.ErrEdgeNotFound
	}
	edgeCopy := *edge
	return &edgeCopy, nil
}

func (s *Storage) DeleteEdge(ctx context.Context, id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	edge, ok := s.edges[id]
	if !ok {
		return storage.ErrEdgeNotFound
	}

	s.removeEdgeIndexes(edge)
	delete(s.edges, id)

	return nil
}

func (s *Storage) removeEdgeIndexes(edge *graph.Edge) {
	if fromSet := s.edgesFrom[edge.From]; fromSet != nil {
		delete(fromSet, edge.ID)
	}
	if toSet := s.edgesTo[edge.To]; toSet != nil {
		delete(toSet, edge.ID)
	}
}

func (s *Storage) GetEdgesFrom(ctx context.Context, nodeID string) ([]*graph.Edge, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	edgeIDs := s.edgesFrom[nodeID]
	edges := make([]*graph.Edge, 0, len(edgeIDs))
	for id := range edgeIDs {
		if edge, ok := s.edges[id]; ok {
			edgeCopy := *edge
			edges = append(edges, &edgeCopy)
		}
	}
	return edges, nil
}

func (s *Storage) GetEdgesTo(ctx context.Context, nodeID string) ([]*graph.Edge, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	edgeIDs := s.edgesTo[nodeID]
	edges := make([]*graph.Edge, 0, len(edgeIDs))
	for id := range edgeIDs {
		if edge, ok := s.edges[id]; ok {
			edgeCopy := *edge
			edges = append(edges, &edgeCopy)
		}
	}
	return edges, nil
}

func (s *Storage) FindNodes(ctx context.Context, filter graph.NodeFilter) ([]*graph.Node, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var result []*graph.Node

	// If filtering by type, start with that index
	if filter.Type != "" {
		ids := s.nodesByType[filter.Type]
		for id := range ids {
			node := s.nodes[id]
			if matchesFilter(node, filter) {
				nodeCopy := *node
				result = append(result, &nodeCopy)
			}
		}
	} else {
		// Full scan
		for _, node := range s.nodes {
			if matchesFilter(node, filter) {
				nodeCopy := *node
				result = append(result, &nodeCopy)
			}
		}
	}

	return result, nil
}

func matchesFilter(node *graph.Node, filter graph.NodeFilter) bool {
	if filter.Type != "" && node.Type != filter.Type {
		return false
	}
	if filter.URIPrefix != "" && !strings.HasPrefix(node.URI, filter.URIPrefix) {
		return false
	}
	return true
}

func (s *Storage) FindStaleByURIPrefix(ctx context.Context, uriPrefix, currentGen string) ([]*graph.Node, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var stale []*graph.Node
	for _, node := range s.nodes {
		if strings.HasPrefix(node.URI, uriPrefix) && node.Generation != currentGen {
			nodeCopy := *node
			stale = append(stale, &nodeCopy)
		}
	}

	return stale, nil
}

func (s *Storage) DeleteStaleByURIPrefix(ctx context.Context, uriPrefix, currentGen string) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	var toDelete []string
	for id, node := range s.nodes {
		if strings.HasPrefix(node.URI, uriPrefix) && node.Generation != currentGen {
			toDelete = append(toDelete, id)
		}
	}

	for _, id := range toDelete {
		node := s.nodes[id]
		s.removeNodeIndexes(node)
		delete(s.nodes, id)
	}

	return len(toDelete), nil
}

func (s *Storage) DeleteByURIPrefix(ctx context.Context, uriPrefix string) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	var toDelete []string
	for id, node := range s.nodes {
		if strings.HasPrefix(node.URI, uriPrefix) {
			toDelete = append(toDelete, id)
		}
	}

	for _, id := range toDelete {
		node := s.nodes[id]
		s.removeNodeIndexes(node)
		delete(s.nodes, id)
	}

	return len(toDelete), nil
}

func (s *Storage) DeleteStaleEdges(ctx context.Context, currentGen string) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	var toDelete []string
	for id, edge := range s.edges {
		if edge.Generation != currentGen {
			toDelete = append(toDelete, id)
		}
	}

	for _, id := range toDelete {
		edge := s.edges[id]
		s.removeEdgeIndexes(edge)
		delete(s.edges, id)
	}

	return len(toDelete), nil
}

func (s *Storage) DeleteOrphanedEdges(ctx context.Context) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	var toDelete []string
	for id, edge := range s.edges {
		_, fromExists := s.nodes[edge.From]
		_, toExists := s.nodes[edge.To]
		if !fromExists || !toExists {
			toDelete = append(toDelete, id)
		}
	}

	for _, id := range toDelete {
		edge := s.edges[id]
		s.removeEdgeIndexes(edge)
		delete(s.edges, id)
	}

	return len(toDelete), nil
}

// Flush is a no-op for memory storage since all writes are immediate.
func (s *Storage) Flush(ctx context.Context) error {
	return nil
}
