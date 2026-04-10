package todo

import (
	"testing"

	"github.com/codewandler/axon/adapters/sqlite"
	"github.com/codewandler/axon/graph"
)

// newStorage opens a SQLite storage at the given path for testing.
func newStorage(t *testing.T, path string) (graph.Storage, error) {
	t.Helper()
	s, err := sqlite.New(path)
	if err != nil {
		return nil, err
	}
	t.Cleanup(func() { s.Close() })
	return s, nil
}
