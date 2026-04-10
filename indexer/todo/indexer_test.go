package todo

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/codewandler/axon/graph"
	"github.com/codewandler/axon/indexer"
	"github.com/codewandler/axon/types"
)

// setupTestFile writes content to a temp file and returns its path.
func setupTestFile(t *testing.T, name, content string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	return path
}

// indexFileEvent runs the indexer's HandleEvent for a visited file and returns
// all emitted nodes and edges.
func indexFileEvent(t *testing.T, idx *Indexer, filePath, fileNodeID string) (*indexer.CollectingEmitter, error) {
	t.Helper()
	ctx := context.Background()
	emitter := &indexer.CollectingEmitter{}

	s, err := setupInMemStorage(t)
	if err != nil {
		return nil, err
	}

	g := graph.New(s, graph.NewRegistry())
	ictx := &indexer.Context{
		Root:       "file://" + filepath.Dir(filePath),
		Generation: "gen-1",
		Graph:      g,
		Emitter:    emitter,
	}

	event := indexer.Event{
		Type:     indexer.EventEntryVisited,
		URI:      types.PathToURI(filePath),
		Path:     filePath,
		Name:     filepath.Base(filePath),
		NodeType: types.TypeFile,
		NodeID:   fileNodeID,
	}

	err = idx.HandleEvent(ctx, ictx, event)
	return emitter, err
}

func setupInMemStorage(t *testing.T) (graph.Storage, error) {
	t.Helper()
	// Use the sqlite adapter in :memory: mode
	// We import it indirectly via the graph package.
	// For test isolation just use a temp file.
	dir := t.TempDir()
	return newStorage(t, filepath.Join(dir, "test.db"))
}

func TestIndexer_BasicAnnotations(t *testing.T) {
	content := strings.Join([]string{
		"package main",
		"",
		"func main() {",
		"\t// TODO: implement me",
		"\tx := 1",
		"\t// FIXME: this is broken",
		"}",
	}, "\n")

	path := setupTestFile(t, "main.go", content)
	idx := New()
	emitter, err := indexFileEvent(t, idx, path, "file-node-id")
	if err != nil {
		t.Fatalf("HandleEvent: %v", err)
	}

	if len(emitter.Nodes) != 2 {
		t.Fatalf("expected 2 todo nodes, got %d", len(emitter.Nodes))
	}

	// Check first node (TODO)
	todo := emitter.Nodes[0]
	if todo.Type != types.TypeTodo {
		t.Errorf("type = %q, want %q", todo.Type, types.TypeTodo)
	}
	data, ok := todo.Data.(types.TodoData)
	if !ok {
		t.Fatalf("Data is not TodoData: %T", todo.Data)
	}
	if data.Kind != "TODO" {
		t.Errorf("kind = %q, want TODO", data.Kind)
	}
	if data.Text != "implement me" {
		t.Errorf("text = %q, want %q", data.Text, "implement me")
	}
	if data.Line != 4 {
		t.Errorf("line = %d, want 4", data.Line)
	}
	if !strings.Contains(todo.Name, "TODO") {
		t.Errorf("Name %q does not contain TODO", todo.Name)
	}
	if !todo.HasLabel("todo") {
		t.Errorf("node missing label 'todo'")
	}

	// Check second node (FIXME)
	fixme := emitter.Nodes[1]
	fdata, _ := fixme.Data.(types.TodoData)
	if fdata.Kind != "FIXME" {
		t.Errorf("kind = %q, want FIXME", fdata.Kind)
	}
	if fdata.Line != 6 {
		t.Errorf("line = %d, want 6", fdata.Line)
	}
	if !fixme.HasLabel("fixme") {
		t.Errorf("node missing label 'fixme'")
	}

	// Containment edges: each node should have 2 edges (contains + contained_by)
	if len(emitter.Edges) != 4 {
		t.Errorf("expected 4 edges (2 per node), got %d", len(emitter.Edges))
	}
}

func TestIndexer_MultipleKinds(t *testing.T) {
	content := strings.Join([]string{
		"// TODO: do this",
		"// FIXME: fix that",
		"// HACK: ugly workaround",
		"// XXX: danger",
		"// NOTE: remember this",
	}, "\n")

	path := setupTestFile(t, "kinds.go", content)
	idx := New()
	emitter, err := indexFileEvent(t, idx, path, "f1")
	if err != nil {
		t.Fatalf("HandleEvent: %v", err)
	}
	if len(emitter.Nodes) != 5 {
		t.Fatalf("expected 5 nodes, got %d", len(emitter.Nodes))
	}
	kinds := make(map[string]bool)
	for _, n := range emitter.Nodes {
		d, _ := n.Data.(types.TodoData)
		kinds[d.Kind] = true
	}
	for _, want := range []string{"TODO", "FIXME", "HACK", "XXX", "NOTE"} {
		if !kinds[want] {
			t.Errorf("missing kind %q", want)
		}
	}
}

func TestIndexer_CommentStyles(t *testing.T) {
	content := strings.Join([]string{
		"// TODO: go style",
		"# TODO: python style",
		"-- TODO: sql style",
		"; TODO: lisp style",
	}, "\n")

	path := setupTestFile(t, "styles.py", content)
	idx := New()
	emitter, err := indexFileEvent(t, idx, path, "f2")
	if err != nil {
		t.Fatalf("HandleEvent: %v", err)
	}
	if len(emitter.Nodes) != 4 {
		t.Fatalf("expected 4 nodes for 4 comment styles, got %d", len(emitter.Nodes))
	}
}

func TestIndexer_BinarySkipped(t *testing.T) {
	// File with null bytes → treated as binary, no nodes emitted
	content := "// TODO: should be skipped\x00\x00some binary data"
	path := setupTestFile(t, "binary.bin", content)

	idx := New()
	emitter, err := indexFileEvent(t, idx, path, "f3")
	if err != nil {
		t.Fatalf("HandleEvent: %v", err)
	}
	if len(emitter.Nodes) != 0 {
		t.Errorf("expected 0 nodes for binary file, got %d", len(emitter.Nodes))
	}
}

func TestIndexer_ContextLine(t *testing.T) {
	content := strings.Join([]string{
		"func Foo() {",
		"\t// TODO: implement",
	}, "\n")

	path := setupTestFile(t, "ctx.go", content)
	idx := New()
	emitter, err := indexFileEvent(t, idx, path, "f4")
	if err != nil {
		t.Fatalf("HandleEvent: %v", err)
	}
	if len(emitter.Nodes) != 1 {
		t.Fatalf("expected 1 node, got %d", len(emitter.Nodes))
	}
	d, _ := emitter.Nodes[0].Data.(types.TodoData)
	if d.Context != "func Foo() {" {
		t.Errorf("context = %q, want %q", d.Context, "func Foo() {")
	}
}

func TestIndexer_NameTruncation(t *testing.T) {
	long := strings.Repeat("a", 100)
	content := "// TODO: " + long
	path := setupTestFile(t, "long.go", content)

	idx := New()
	emitter, err := indexFileEvent(t, idx, path, "f5")
	if err != nil {
		t.Fatalf("HandleEvent: %v", err)
	}
	if len(emitter.Nodes) != 1 {
		t.Fatalf("expected 1 node, got %d", len(emitter.Nodes))
	}
	if len(emitter.Nodes[0].Name) > 80 {
		t.Errorf("Name length %d exceeds 80 chars", len(emitter.Nodes[0].Name))
	}
	if !strings.HasSuffix(emitter.Nodes[0].Name, "...") {
		t.Errorf("truncated name should end with '...': %q", emitter.Nodes[0].Name)
	}
}

func TestIndexer_NoMatchMidLine(t *testing.T) {
	// A TODO/FIXME that appears mid-line (e.g. inside backtick-quoted code in
	// markdown, or inside a string literal) must NOT produce a node.
	content := strings.Join([]string{
		"Use `// TODO` and `// FIXME` annotations in your code.",
		`fmt.Println("// TODO: not a real todo")`,
		"// TODO: this IS a real todo",
	}, "\n")

	path := setupTestFile(t, "prose.md", content)
	idx := New()
	emitter, err := indexFileEvent(t, idx, path, "f6")
	if err != nil {
		t.Fatalf("HandleEvent: %v", err)
	}
	if len(emitter.Nodes) != 1 {
		t.Errorf("expected 1 node (only the comment-start TODO), got %d", len(emitter.Nodes))
		for _, n := range emitter.Nodes {
			d, _ := n.Data.(types.TodoData)
			t.Logf("  line %d: %s", d.Line, n.Name)
		}
	}
}
