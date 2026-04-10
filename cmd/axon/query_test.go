package main

import (
	"testing"
	"time"

	"github.com/codewandler/axon/graph"
)

// node fixture with known field values
func makeTestNode() *graph.Node {
	now := time.Now()
	return &graph.Node{
		ID:        "test-id",
		Type:      "fs:file",
		URI:       "file:///foo.go",
		Key:       "/foo.go",
		Name:      "foo.go",
		Labels:    []string{"code"},
		Data:      map[string]any{"ext": ".go", "size": int64(1234), "exported": true},
		CreatedAt: &now,
	}
}

// Task 2: nodeFieldRaw must handle "var.field" selectors (e.g. "file.name")
// by stripping the variable prefix and delegating to the plain field lookup.
func TestNodeFieldRaw_VarDotField(t *testing.T) {
	node := makeTestNode()

	tests := []struct {
		col  string
		want any
	}{
		// plain fields still work
		{"name", "foo.go"},
		{"type", "fs:file"},
		{"id", "test-id"},
		// var.field selectors — the variable prefix must be stripped
		{"file.name", "foo.go"},
		{"file.type", "fs:file"},
		{"file.uri", "file:///foo.go"},
		{"n.name", "foo.go"},
		// var.data.field selectors — strip var prefix → "data.ext"
		{"file.data.ext", ".go"},
	}

	for _, tc := range tests {
		got := nodeFieldRaw(node, tc.col)
		if got != tc.want {
			t.Errorf("nodeFieldRaw(node, %q): got %v (%T), want %v (%T)",
				tc.col, got, got, tc.want, tc.want)
		}
	}
}

// Task 2: nodeFieldValue must do the same for table rendering.
func TestNodeFieldValue_VarDotField(t *testing.T) {
	node := makeTestNode()

	tests := []struct {
		col  string
		want string
	}{
		{"name", "foo.go"},
		{"file.name", "foo.go"},
		{"file.type", "fs:file"},
		{"file.data.ext", ".go"},
	}

	for _, tc := range tests {
		got := nodeFieldValue(node, tc.col)
		if got != tc.want {
			t.Errorf("nodeFieldValue(node, %q): got %q, want %q", tc.col, got, tc.want)
		}
	}
}
