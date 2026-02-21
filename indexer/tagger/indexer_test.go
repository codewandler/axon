package tagger

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/codewandler/axon/adapters/sqlite"
	"github.com/codewandler/axon/graph"
	"github.com/codewandler/axon/indexer"
	"github.com/codewandler/axon/types"
)

func setupGraph(t *testing.T) (*graph.Graph, *sqlite.Storage) {
	t.Helper()
	r := graph.NewRegistry()
	types.RegisterCommonEdges(r)
	types.RegisterFSTypes(r)
	s, err := sqlite.New(":memory:")
	if err != nil {
		t.Fatalf("sqlite.New failed: %v", err)
	}
	t.Cleanup(func() { s.Close() })
	return graph.New(s, r), s
}

func TestRuleMatches(t *testing.T) {
	tests := []struct {
		name     string
		rule     Rule
		nodeType string
		filename string
		path     string
		want     bool
	}{
		// Type filter
		{
			name:     "type matches",
			rule:     Rule{Type: types.TypeFile},
			nodeType: types.TypeFile,
			filename: "test.txt",
			path:     "test.txt",
			want:     true,
		},
		{
			name:     "type does not match",
			rule:     Rule{Type: types.TypeFile},
			nodeType: types.TypeDir,
			filename: "dir",
			path:     "dir",
			want:     false,
		},
		{
			name:     "empty type matches all",
			rule:     Rule{},
			nodeType: types.TypeFile,
			filename: "test.txt",
			path:     "test.txt",
			want:     true,
		},

		// Name pattern filter
		{
			name:     "name pattern matches exact",
			rule:     Rule{NamePattern: "AGENTS.md"},
			nodeType: types.TypeFile,
			filename: "AGENTS.md",
			path:     "AGENTS.md",
			want:     true,
		},
		{
			name:     "name pattern matches glob",
			rule:     Rule{NamePattern: "*.yml"},
			nodeType: types.TypeFile,
			filename: "config.yml",
			path:     "config.yml",
			want:     true,
		},
		{
			name:     "name pattern does not match",
			rule:     Rule{NamePattern: "*.yml"},
			nodeType: types.TypeFile,
			filename: "config.yaml",
			path:     "config.yaml",
			want:     false,
		},
		{
			name:     "name pattern prefix wildcard",
			rule:     Rule{NamePattern: "Dockerfile.*"},
			nodeType: types.TypeFile,
			filename: "Dockerfile.dev",
			path:     "Dockerfile.dev",
			want:     true,
		},
		{
			name:     "name pattern README*",
			rule:     Rule{NamePattern: "README*"},
			nodeType: types.TypeFile,
			filename: "README.md",
			path:     "README.md",
			want:     true,
		},

		// Path pattern filter (without **)
		{
			name:     "path pattern simple",
			rule:     Rule{PathPattern: ".github/workflows/*.yml"},
			nodeType: types.TypeFile,
			filename: "build.yml",
			path:     ".github/workflows/build.yml",
			want:     true,
		},
		{
			name:     "path pattern does not match",
			rule:     Rule{PathPattern: ".github/workflows/*.yml"},
			nodeType: types.TypeFile,
			filename: "build.yml",
			path:     "workflows/build.yml",
			want:     false,
		},

		// Path pattern with **
		{
			name:     "path pattern ** matches nested",
			rule:     Rule{PathPattern: "**/k8s/**/*.yaml"},
			nodeType: types.TypeFile,
			filename: "deployment.yaml",
			path:     "infra/k8s/prod/deployment.yaml",
			want:     true,
		},
		{
			name:     "path pattern ** matches at root",
			rule:     Rule{PathPattern: "**/k8s/**/*.yaml"},
			nodeType: types.TypeFile,
			filename: "service.yaml",
			path:     "k8s/service.yaml",
			want:     true,
		},
		{
			name:     "path pattern ** matches deep",
			rule:     Rule{PathPattern: "**/tests/*.rs"},
			nodeType: types.TypeFile,
			filename: "lib_test.rs",
			path:     "crates/mylib/tests/lib_test.rs",
			want:     true,
		},
		{
			name:     "path pattern ** does not match wrong extension",
			rule:     Rule{PathPattern: "**/k8s/**/*.yaml"},
			nodeType: types.TypeFile,
			filename: "deployment.yml",
			path:     "infra/k8s/prod/deployment.yml",
			want:     false,
		},

		// Combined filters
		{
			name:     "type and name pattern",
			rule:     Rule{Type: types.TypeFile, NamePattern: "*_test.go"},
			nodeType: types.TypeFile,
			filename: "main_test.go",
			path:     "cmd/main_test.go",
			want:     true,
		},
		{
			name:     "type mismatch with name pattern match",
			rule:     Rule{Type: types.TypeFile, NamePattern: "*_test.go"},
			nodeType: types.TypeDir,
			filename: "test_test.go",
			path:     "test_test.go",
			want:     false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.rule.Matches(tt.nodeType, tt.filename, tt.path)
			if got != tt.want {
				t.Errorf("Rule.Matches(%q, %q, %q) = %v, want %v",
					tt.nodeType, tt.filename, tt.path, got, tt.want)
			}
		})
	}
}

func TestMatchPathPattern(t *testing.T) {
	tests := []struct {
		pattern string
		path    string
		want    bool
	}{
		// No ** patterns
		{"a/b/c", "a/b/c", true},
		{"a/b/c", "a/b/d", false},
		{"a/*.txt", "a/file.txt", true},
		{"a/*.txt", "a/b/file.txt", false},

		// ** at start
		{"**/file.txt", "file.txt", true},
		{"**/file.txt", "a/file.txt", true},
		{"**/file.txt", "a/b/c/file.txt", true},
		{"**/file.txt", "a/b/file.yml", false},

		// ** in middle
		{"a/**/c", "a/c", true},
		{"a/**/c", "a/b/c", true},
		{"a/**/c", "a/b/b/c", true},
		{"a/**/c", "a/b/d", false},

		// ** at end
		{"a/**", "a", true},
		{"a/**", "a/b", true},
		{"a/**", "a/b/c", true},
		{"a/**", "b/c", false},

		// Multiple **
		{"**/k8s/**/*.yaml", "k8s/file.yaml", true},
		{"**/k8s/**/*.yaml", "infra/k8s/file.yaml", true},
		{"**/k8s/**/*.yaml", "infra/k8s/prod/file.yaml", true},
		{"**/k8s/**/*.yaml", "infra/k8s/prod/file.yml", false},

		// Edge cases
		{"**", "", true},
		{"**", "a", true},
		{"**", "a/b/c", true},
		{"**/a/**", "a", true},
		{"**/a/**", "x/a/y", true},
	}

	for _, tt := range tests {
		t.Run(tt.pattern+"_"+tt.path, func(t *testing.T) {
			got := matchPathPattern(tt.pattern, tt.path)
			if got != tt.want {
				t.Errorf("matchPathPattern(%q, %q) = %v, want %v",
					tt.pattern, tt.path, got, tt.want)
			}
		})
	}
}

func TestIndexerLabelsNodes(t *testing.T) {
	ctx := context.Background()

	g, storage := setupGraph(t)

	// Create a test node (simulating what FS indexer would create)
	node := graph.NewNode(types.TypeFile).
		WithURI("file:///project/AGENTS.md").
		WithKey("/project/AGENTS.md").
		WithName("AGENTS.md")
	if err := storage.PutNode(ctx, node); err != nil {
		t.Fatalf("PutNode failed: %v", err)
	}

	// Create the tagger indexer with custom rules for testing
	tagger := New(Config{
		Rules: []Rule{
			{Type: types.TypeFile, NamePattern: "AGENTS.md", Labels: []string{"agent:instructions"}},
		},
	})

	// Simulate the event that would be sent by FS indexer
	// Event now carries the Node directly
	event := indexer.Event{
		Type:     indexer.EventEntryVisited,
		URI:      "file:///project/AGENTS.md",
		Path:     "/project/AGENTS.md",
		Name:     "AGENTS.md",
		NodeType: types.TypeFile,
		NodeID:   node.ID,
		Node:     node,
	}

	// Create indexer context
	emitter := indexer.NewGraphEmitter(g, "gen-1")
	ictx := &indexer.Context{
		Root:       "file:///project",
		Generation: "gen-1",
		Graph:      g,
		Emitter:    emitter,
	}

	// Run the tagger
	if err := tagger.HandleEvent(ctx, ictx, event); err != nil {
		t.Fatalf("HandleEvent failed: %v", err)
	}

	// Flush and verify
	if err := storage.Flush(ctx); err != nil {
		t.Fatalf("Flush failed: %v", err)
	}

	// Get the updated node
	updated, err := storage.GetNode(ctx, node.ID)
	if err != nil {
		t.Fatalf("GetNode failed: %v", err)
	}

	// Check labels
	if !updated.HasLabel("agent:instructions") {
		t.Errorf("Node should have label 'agent:instructions', got labels: %v", updated.Labels)
	}
}

func TestIndexerMultipleRulesAccumulate(t *testing.T) {
	ctx := context.Background()

	g, storage := setupGraph(t)

	// Create a test node
	node := graph.NewNode(types.TypeFile).
		WithURI("file:///project/go.mod").
		WithKey("/project/go.mod").
		WithName("go.mod")
	if err := storage.PutNode(ctx, node); err != nil {
		t.Fatalf("PutNode failed: %v", err)
	}

	// Create the tagger indexer with default rules (go.mod should match build:config and lang:go)
	tagger := New(Config{})

	// Simulate the event
	event := indexer.Event{
		Type:     indexer.EventEntryVisited,
		URI:      "file:///project/go.mod",
		Path:     "/project/go.mod",
		Name:     "go.mod",
		NodeType: types.TypeFile,
		NodeID:   node.ID,
		Node:     node,
	}

	// Create indexer context
	emitter := indexer.NewGraphEmitter(g, "gen-1")
	ictx := &indexer.Context{
		Root:       "file:///project",
		Generation: "gen-1",
		Graph:      g,
		Emitter:    emitter,
	}

	// Run the tagger
	if err := tagger.HandleEvent(ctx, ictx, event); err != nil {
		t.Fatalf("HandleEvent failed: %v", err)
	}

	// Flush and verify
	if err := storage.Flush(ctx); err != nil {
		t.Fatalf("Flush failed: %v", err)
	}

	// Get the updated node
	updated, err := storage.GetNode(ctx, node.ID)
	if err != nil {
		t.Fatalf("GetNode failed: %v", err)
	}

	// Check labels - go.mod should have both build:config and lang:go
	if !updated.HasLabel("build:config") {
		t.Errorf("Node should have label 'build:config', got labels: %v", updated.Labels)
	}
	if !updated.HasLabel("lang:go") {
		t.Errorf("Node should have label 'lang:go', got labels: %v", updated.Labels)
	}
}

func TestIndexerNoMatchNoLabels(t *testing.T) {
	ctx := context.Background()

	g, storage := setupGraph(t)

	// Create a test node with no matching rules
	node := graph.NewNode(types.TypeFile).
		WithURI("file:///project/random.xyz").
		WithKey("/project/random.xyz").
		WithName("random.xyz")
	if err := storage.PutNode(ctx, node); err != nil {
		t.Fatalf("PutNode failed: %v", err)
	}

	// Create the tagger indexer
	tagger := New(Config{})

	// Simulate the event
	event := indexer.Event{
		Type:     indexer.EventEntryVisited,
		URI:      "file:///project/random.xyz",
		Path:     "/project/random.xyz",
		Name:     "random.xyz",
		NodeType: types.TypeFile,
		NodeID:   node.ID,
		Node:     node,
	}

	// Create indexer context
	emitter := indexer.NewGraphEmitter(g, "gen-1")
	ictx := &indexer.Context{
		Root:       "file:///project",
		Generation: "gen-1",
		Graph:      g,
		Emitter:    emitter,
	}

	// Run the tagger - no labels should be added
	if err := tagger.HandleEvent(ctx, ictx, event); err != nil {
		t.Fatalf("HandleEvent failed: %v", err)
	}

	// The node wasn't modified (no labels match), so no need to flush/get
	// Just check the in-memory node
	if len(node.Labels) != 0 {
		t.Errorf("Node should have no labels, got: %v", node.Labels)
	}
}

func TestIndexerPathPatternWithDoublestar(t *testing.T) {
	ctx := context.Background()

	g, storage := setupGraph(t)

	// Create a test node in a k8s directory
	node := graph.NewNode(types.TypeFile).
		WithURI("file:///project/infra/k8s/prod/deployment.yaml").
		WithKey("/project/infra/k8s/prod/deployment.yaml").
		WithName("deployment.yaml")
	if err := storage.PutNode(ctx, node); err != nil {
		t.Fatalf("PutNode failed: %v", err)
	}

	// Create the tagger indexer with default rules
	tagger := New(Config{})

	// Simulate the event
	event := indexer.Event{
		Type:     indexer.EventEntryVisited,
		URI:      "file:///project/infra/k8s/prod/deployment.yaml",
		Path:     "/project/infra/k8s/prod/deployment.yaml",
		Name:     "deployment.yaml",
		NodeType: types.TypeFile,
		NodeID:   node.ID,
		Node:     node,
	}

	// Create indexer context
	emitter := indexer.NewGraphEmitter(g, "gen-1")
	ictx := &indexer.Context{
		Root:       "file:///project",
		Generation: "gen-1",
		Graph:      g,
		Emitter:    emitter,
	}

	// Run the tagger
	if err := tagger.HandleEvent(ctx, ictx, event); err != nil {
		t.Fatalf("HandleEvent failed: %v", err)
	}

	// Flush and verify
	if err := storage.Flush(ctx); err != nil {
		t.Fatalf("Flush failed: %v", err)
	}

	// Get the updated node
	updated, err := storage.GetNode(ctx, node.ID)
	if err != nil {
		t.Fatalf("GetNode failed: %v", err)
	}

	// Check labels - should have k8s:manifest
	if !updated.HasLabel("k8s:manifest") {
		t.Errorf("Node should have label 'k8s:manifest', got labels: %v", updated.Labels)
	}
}

func TestIndexerDirectInvocationDoesNothing(t *testing.T) {
	ctx := context.Background()

	g, _ := setupGraph(t)

	// Create the tagger indexer
	tagger := New(Config{})

	// Create indexer context for direct invocation
	emitter := indexer.NewGraphEmitter(g, "gen-1")
	ictx := &indexer.Context{
		Root:       "file:///project",
		Generation: "gen-1",
		Graph:      g,
		Emitter:    emitter,
	}

	// Index() is a no-op for tagger (event-driven only)
	if err := tagger.Index(ctx, ictx); err != nil {
		t.Fatalf("Index failed: %v", err)
	}

	// No error means success - tagger correctly ignores direct invocations
}

func TestIntegrationWithFSIndexer(t *testing.T) {
	// Create a temporary directory with test files
	tmpDir := t.TempDir()

	// Create test files
	files := map[string]string{
		"AGENTS.md":                   "# Agent Instructions",
		"go.mod":                      "module test",
		"main.go":                     "package main",
		"main_test.go":                "package main",
		".github/workflows/build.yml": "name: Build",
		"infra/k8s/deployment.yaml":   "apiVersion: apps/v1",
	}

	for path, content := range files {
		fullPath := filepath.Join(tmpDir, path)
		if err := os.MkdirAll(filepath.Dir(fullPath), 0755); err != nil {
			t.Fatalf("MkdirAll failed: %v", err)
		}
		if err := os.WriteFile(fullPath, []byte(content), 0644); err != nil {
			t.Fatalf("WriteFile failed: %v", err)
		}
	}

	ctx := context.Background()

	g, storage := setupGraph(t)

	// We'll manually simulate what the framework does:
	// 1. FS indexer creates nodes and emits events
	// 2. Events are dispatched to tagger
	// 3. Tagger adds labels

	generation := "gen-1"
	emitter := indexer.NewGraphEmitter(g, generation)
	rootURI := types.PathToURI(tmpDir)

	// Walk the directory and create nodes + run tagger
	tagger := New(Config{})

	walkErr := filepath.WalkDir(tmpDir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}

		uri := types.PathToURI(path)
		var node *graph.Node

		if d.IsDir() {
			node = graph.NewNode(types.TypeDir).
				WithURI(uri).
				WithKey(path).
				WithName(d.Name())
		} else {
			node = graph.NewNode(types.TypeFile).
				WithURI(uri).
				WithKey(path).
				WithName(d.Name())
		}

		if err := emitter.EmitNode(ctx, node); err != nil {
			return err
		}

		// Only run tagger for files
		if !d.IsDir() {
			event := indexer.Event{
				Type:     indexer.EventEntryVisited,
				URI:      uri,
				Path:     path,
				Name:     d.Name(),
				NodeType: node.Type,
				NodeID:   node.ID,
				Node:     node,
			}

			ictx := &indexer.Context{
				Root:       rootURI,
				Generation: generation,
				Graph:      g,
				Emitter:    emitter,
			}

			if err := tagger.HandleEvent(ctx, ictx, event); err != nil {
				return err
			}
		}

		return nil
	})
	if walkErr != nil {
		t.Fatalf("Walk failed: %v", walkErr)
	}

	if err := storage.Flush(ctx); err != nil {
		t.Fatalf("Flush failed: %v", err)
	}

	// Verify labels were applied correctly
	// Note: test files only get test:file label, not lang:* (language is apparent from extension)
	tests := []struct {
		name   string
		labels []string
	}{
		{"AGENTS.md", []string{"agent:instructions"}},
		{"go.mod", []string{"build:config", "lang:go"}},
		{"main_test.go", []string{"test:file"}},
		{"build.yml", []string{"ci:config", "ci:github"}},
		{"deployment.yaml", []string{"k8s:manifest"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Find the node by name
			nodes, err := storage.FindNodes(ctx, graph.NodeFilter{Name: tt.name}, graph.QueryOptions{})
			if err != nil {
				t.Fatalf("FindNodes failed: %v", err)
			}
			if len(nodes) == 0 {
				t.Fatalf("Node %q not found", tt.name)
			}
			if len(nodes) > 1 {
				t.Fatalf("Found multiple nodes with name %q", tt.name)
			}

			node := nodes[0]
			for _, label := range tt.labels {
				if !node.HasLabel(label) {
					t.Errorf("Node %q should have label %q, got labels: %v", tt.name, label, node.Labels)
				}
			}
		})
	}
}
