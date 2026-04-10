package types

import (
	"fmt"

	"github.com/codewandler/axon/graph"
)

// TypeTodo is the node type for TODO/FIXME/HACK/XXX/NOTE annotations.
const TypeTodo = "code:todo"

// TodoData holds data for a code annotation node.
type TodoData struct {
	File    string `json:"file"`    // absolute file path
	Line    int    `json:"line"`    // 1-based line number
	Kind    string `json:"kind"`    // "TODO" | "FIXME" | "HACK" | "XXX" | "NOTE"
	Text    string `json:"text"`    // comment text after the keyword
	Context string `json:"context"` // preceding non-comment line for orientation
}

// TodoURI returns the URI for a specific annotation node.
func TodoURI(filePath string, line int) string {
	return fmt.Sprintf("file+todo://%s#L%d", filePath, line)
}

// TodoURIPrefix returns the URI prefix for all annotation nodes belonging to a file.
func TodoURIPrefix(filePath string) string {
	return "file+todo://" + filePath
}

// RegisterTodoTypes registers code annotation node types with the graph registry.
func RegisterTodoTypes(r *graph.Registry) {
	graph.RegisterNodeType[TodoData](r, graph.NodeSpec{
		Type:        TypeTodo,
		Description: "A TODO/FIXME/HACK/XXX/NOTE annotation comment in source code",
	})
}
