package indexer

import (
	"net/url"
	"sync"
)

// Registry holds registered indexers and routes URIs to them.
type Registry struct {
	mu       sync.RWMutex
	indexers []Indexer
}

// NewRegistry creates a new indexer registry.
func NewRegistry() *Registry {
	return &Registry{
		indexers: make([]Indexer, 0),
	}
}

// Register adds an indexer to the registry.
func (r *Registry) Register(idx Indexer) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.indexers = append(r.indexers, idx)
}

// ForURI returns the first indexer that can handle the given URI.
// Returns nil if no indexer handles the URI.
func (r *Registry) ForURI(uri string) Indexer {
	r.mu.RLock()
	defer r.mu.RUnlock()

	for _, idx := range r.indexers {
		if idx.Handles(uri) {
			return idx
		}
	}
	return nil
}

// ForScheme returns all indexers that handle the given URI scheme.
func (r *Registry) ForScheme(scheme string) []Indexer {
	r.mu.RLock()
	defer r.mu.RUnlock()

	var result []Indexer
	for _, idx := range r.indexers {
		for _, s := range idx.Schemes() {
			if s == scheme {
				result = append(result, idx)
				break
			}
		}
	}
	return result
}

// All returns all registered indexers.
func (r *Registry) All() []Indexer {
	r.mu.RLock()
	defer r.mu.RUnlock()

	result := make([]Indexer, len(r.indexers))
	copy(result, r.indexers)
	return result
}

// GetScheme extracts the scheme from a URI.
// Returns empty string if the URI is invalid or has no scheme.
func GetScheme(uri string) string {
	u, err := url.Parse(uri)
	if err != nil {
		return ""
	}
	return u.Scheme
}
