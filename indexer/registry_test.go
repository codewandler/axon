package indexer

import (
	"context"
	"testing"
)

// mockIndexer is a test indexer
type mockIndexer struct {
	name    string
	schemes []string
}

func (m *mockIndexer) Name() string      { return m.name }
func (m *mockIndexer) Schemes() []string { return m.schemes }
func (m *mockIndexer) Handles(uri string) bool {
	scheme := GetScheme(uri)
	for _, s := range m.schemes {
		if s == scheme {
			return true
		}
	}
	return false
}
func (m *mockIndexer) Index(ctx context.Context, ictx *Context) error { return nil }

func TestRegistryForURI(t *testing.T) {
	r := NewRegistry()

	fsIndexer := &mockIndexer{name: "fs", schemes: []string{"file"}}
	gitIndexer := &mockIndexer{name: "git", schemes: []string{"git", "ssh"}}

	r.Register(fsIndexer)
	r.Register(gitIndexer)

	// Test file URI
	idx := r.ForURI("file:///home/user/test.txt")
	if idx == nil {
		t.Fatal("expected indexer for file URI")
	}
	if idx.Name() != "fs" {
		t.Errorf("expected fs indexer, got %s", idx.Name())
	}

	// Test git URI
	idx = r.ForURI("git://github.com/user/repo.git")
	if idx == nil {
		t.Fatal("expected indexer for git URI")
	}
	if idx.Name() != "git" {
		t.Errorf("expected git indexer, got %s", idx.Name())
	}

	// Test unknown URI
	idx = r.ForURI("https://example.com")
	if idx != nil {
		t.Error("expected nil for unknown scheme")
	}
}

func TestRegistryForScheme(t *testing.T) {
	r := NewRegistry()

	idx1 := &mockIndexer{name: "fs", schemes: []string{"file"}}
	idx2 := &mockIndexer{name: "git", schemes: []string{"git", "ssh"}}

	r.Register(idx1)
	r.Register(idx2)

	// Test single scheme
	indexers := r.ForScheme("file")
	if len(indexers) != 1 {
		t.Errorf("expected 1 indexer for file scheme, got %d", len(indexers))
	}

	// Test scheme with multiple indexers
	indexers = r.ForScheme("ssh")
	if len(indexers) != 1 {
		t.Errorf("expected 1 indexer for ssh scheme, got %d", len(indexers))
	}

	// Test unknown scheme
	indexers = r.ForScheme("https")
	if len(indexers) != 0 {
		t.Errorf("expected 0 indexers for https scheme, got %d", len(indexers))
	}
}

func TestRegistryAll(t *testing.T) {
	r := NewRegistry()

	r.Register(&mockIndexer{name: "fs"})
	r.Register(&mockIndexer{name: "git"})

	all := r.All()
	if len(all) != 2 {
		t.Errorf("expected 2 indexers, got %d", len(all))
	}
}

func TestGetScheme(t *testing.T) {
	tests := []struct {
		uri    string
		scheme string
	}{
		{"file:///home/user/test.txt", "file"},
		{"git://github.com/user/repo.git", "git"},
		{"https://example.com", "https"},
		{"/just/a/path", ""},
		{"", ""},
	}

	for _, tt := range tests {
		got := GetScheme(tt.uri)
		if got != tt.scheme {
			t.Errorf("GetScheme(%q) = %q, want %q", tt.uri, got, tt.scheme)
		}
	}
}
