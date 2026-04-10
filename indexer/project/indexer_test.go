package project

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

func setupGraph(t *testing.T) *graph.Graph {
	t.Helper()
	r := graph.NewRegistry()
	types.RegisterCommonEdges(r)
	types.RegisterFSTypes(r)
	types.RegisterProjectTypes(r)
	s, err := sqlite.New(":memory:")
	if err != nil {
		t.Fatalf("sqlite.New failed: %v", err)
	}
	t.Cleanup(func() { s.Close() })
	return graph.New(s, r)
}

// setupProjectDir creates a temp directory and emits the dir node.
func setupProjectDir(t *testing.T, ctx context.Context, g *graph.Graph, emitter indexer.Emitter) string {
	t.Helper()
	dir := t.TempDir()

	// Emit directory node (simulating what FS indexer would do)
	dirNode := graph.NewNode(types.TypeDir).
		WithURI(types.PathToURI(dir)).
		WithKey(dir).
		WithName(filepath.Base(dir))
	if err := emitter.EmitNode(ctx, dirNode); err != nil {
		t.Fatalf("failed to emit dir node: %v", err)
	}
	if err := g.Storage().Flush(ctx); err != nil {
		t.Fatalf("Flush failed: %v", err)
	}

	return dir
}

func TestParseGoMod(t *testing.T) {
	ctx := context.Background()
	g := setupGraph(t)
	emitter := indexer.NewGraphEmitter(g, "gen-1")
	dir := setupProjectDir(t, ctx, g, emitter)

	// Create go.mod
	goMod := `module github.com/example/myproject

go 1.21

require (
	github.com/pkg/errors v0.9.1
	golang.org/x/sync v0.5.0
)
`
	goModPath := filepath.Join(dir, "go.mod")
	if err := os.WriteFile(goModPath, []byte(goMod), 0644); err != nil {
		t.Fatalf("failed to write go.mod: %v", err)
	}

	// Create file node
	fileNode := graph.NewNode(types.TypeFile).
		WithURI(types.PathToURI(goModPath)).
		WithKey(goModPath).
		WithName("go.mod")
	if err := emitter.EmitNode(ctx, fileNode); err != nil {
		t.Fatalf("failed to emit file node: %v", err)
	}
	if err := g.Storage().Flush(ctx); err != nil {
		t.Fatalf("Flush failed: %v", err)
	}

	idx := New()
	ictx := &indexer.Context{
		Root:       types.ProjectPathToURI(dir),
		Generation: "gen-1",
		Graph:      g,
		Emitter:    emitter,
	}

	event := indexer.Event{
		Type:     indexer.EventEntryVisited,
		URI:      types.PathToURI(goModPath),
		Path:     goModPath,
		Name:     "go.mod",
		NodeType: types.TypeFile,
		NodeID:   fileNode.ID,
		Node:     fileNode,
	}

	if err := idx.HandleEvent(ctx, ictx, event); err != nil {
		t.Fatalf("HandleEvent failed: %v", err)
	}
	if err := g.Storage().Flush(ctx); err != nil {
		t.Fatalf("Flush failed: %v", err)
	}

	// Verify project node was created
	projects, err := g.FindNodes(ctx, graph.NodeFilter{Type: types.TypeProject}, graph.QueryOptions{})
	if err != nil {
		t.Fatalf("FindNodes failed: %v", err)
	}
	if len(projects) != 1 {
		t.Fatalf("expected 1 project, got %d", len(projects))
	}

	proj := projects[0]
	if proj.Name != "github.com/example/myproject" {
		t.Errorf("expected name 'github.com/example/myproject', got %q", proj.Name)
	}

	data, ok := proj.Data.(map[string]any)
	if !ok {
		t.Fatalf("expected data to be map, got %T", proj.Data)
	}

	if data["language"] != types.LangGo {
		t.Errorf("expected language %q, got %q", types.LangGo, data["language"])
	}

	if int(data["dep_count"].(float64)) != 2 {
		t.Errorf("expected 2 dependencies, got %v", data["dep_count"])
	}
}

func TestParsePackageJSON(t *testing.T) {
	ctx := context.Background()
	g := setupGraph(t)
	emitter := indexer.NewGraphEmitter(g, "gen-1")
	dir := setupProjectDir(t, ctx, g, emitter)

	// Create package.json
	pkgJSON := `{
  "name": "@myorg/mypackage",
  "version": "2.0.0",
  "dependencies": {
    "lodash": "^4.17.21",
    "express": "^4.18.0"
  },
  "devDependencies": {
    "jest": "^29.0.0"
  }
}
`
	pkgPath := filepath.Join(dir, "package.json")
	if err := os.WriteFile(pkgPath, []byte(pkgJSON), 0644); err != nil {
		t.Fatalf("failed to write package.json: %v", err)
	}

	fileNode := graph.NewNode(types.TypeFile).
		WithURI(types.PathToURI(pkgPath)).
		WithKey(pkgPath).
		WithName("package.json")
	if err := emitter.EmitNode(ctx, fileNode); err != nil {
		t.Fatalf("failed to emit file node: %v", err)
	}
	if err := g.Storage().Flush(ctx); err != nil {
		t.Fatalf("Flush failed: %v", err)
	}

	idx := New()
	ictx := &indexer.Context{
		Root:       types.ProjectPathToURI(dir),
		Generation: "gen-1",
		Graph:      g,
		Emitter:    emitter,
	}

	event := indexer.Event{
		Type:     indexer.EventEntryVisited,
		URI:      types.PathToURI(pkgPath),
		Path:     pkgPath,
		Name:     "package.json",
		NodeType: types.TypeFile,
		NodeID:   fileNode.ID,
		Node:     fileNode,
	}

	if err := idx.HandleEvent(ctx, ictx, event); err != nil {
		t.Fatalf("HandleEvent failed: %v", err)
	}
	if err := g.Storage().Flush(ctx); err != nil {
		t.Fatalf("Flush failed: %v", err)
	}

	projects, err := g.FindNodes(ctx, graph.NodeFilter{Type: types.TypeProject}, graph.QueryOptions{})
	if err != nil {
		t.Fatalf("FindNodes failed: %v", err)
	}
	if len(projects) != 1 {
		t.Fatalf("expected 1 project, got %d", len(projects))
	}

	proj := projects[0]
	if proj.Name != "@myorg/mypackage" {
		t.Errorf("expected name '@myorg/mypackage', got %q", proj.Name)
	}

	data, ok := proj.Data.(map[string]any)
	if !ok {
		t.Fatalf("expected data to be map, got %T", proj.Data)
	}

	if data["language"] != types.LangNode {
		t.Errorf("expected language %q, got %q", types.LangNode, data["language"])
	}

	if data["version"] != "2.0.0" {
		t.Errorf("expected version '2.0.0', got %q", data["version"])
	}

	if int(data["dep_count"].(float64)) != 3 {
		t.Errorf("expected 3 dependencies, got %v", data["dep_count"])
	}
}

func TestParseCargoToml(t *testing.T) {
	ctx := context.Background()
	g := setupGraph(t)
	emitter := indexer.NewGraphEmitter(g, "gen-1")
	dir := setupProjectDir(t, ctx, g, emitter)

	// Create Cargo.toml
	cargoToml := `[package]
name = "my-rust-app"
version = "0.1.0"

[dependencies]
serde = "1.0"
tokio = { version = "1.0", features = ["full"] }

[dev-dependencies]
criterion = "0.5"
`
	cargoPath := filepath.Join(dir, "Cargo.toml")
	if err := os.WriteFile(cargoPath, []byte(cargoToml), 0644); err != nil {
		t.Fatalf("failed to write Cargo.toml: %v", err)
	}

	fileNode := graph.NewNode(types.TypeFile).
		WithURI(types.PathToURI(cargoPath)).
		WithKey(cargoPath).
		WithName("Cargo.toml")
	if err := emitter.EmitNode(ctx, fileNode); err != nil {
		t.Fatalf("failed to emit file node: %v", err)
	}
	if err := g.Storage().Flush(ctx); err != nil {
		t.Fatalf("Flush failed: %v", err)
	}

	idx := New()
	ictx := &indexer.Context{
		Root:       types.ProjectPathToURI(dir),
		Generation: "gen-1",
		Graph:      g,
		Emitter:    emitter,
	}

	event := indexer.Event{
		Type:     indexer.EventEntryVisited,
		URI:      types.PathToURI(cargoPath),
		Path:     cargoPath,
		Name:     "Cargo.toml",
		NodeType: types.TypeFile,
		NodeID:   fileNode.ID,
		Node:     fileNode,
	}

	if err := idx.HandleEvent(ctx, ictx, event); err != nil {
		t.Fatalf("HandleEvent failed: %v", err)
	}
	if err := g.Storage().Flush(ctx); err != nil {
		t.Fatalf("Flush failed: %v", err)
	}

	projects, err := g.FindNodes(ctx, graph.NodeFilter{Type: types.TypeProject}, graph.QueryOptions{})
	if err != nil {
		t.Fatalf("FindNodes failed: %v", err)
	}
	if len(projects) != 1 {
		t.Fatalf("expected 1 project, got %d", len(projects))
	}

	proj := projects[0]
	if proj.Name != "my-rust-app" {
		t.Errorf("expected name 'my-rust-app', got %q", proj.Name)
	}

	data, ok := proj.Data.(map[string]any)
	if !ok {
		t.Fatalf("expected data to be map, got %T", proj.Data)
	}

	if data["language"] != types.LangRust {
		t.Errorf("expected language %q, got %q", types.LangRust, data["language"])
	}

	if data["version"] != "0.1.0" {
		t.Errorf("expected version '0.1.0', got %q", data["version"])
	}

	if int(data["dep_count"].(float64)) != 3 {
		t.Errorf("expected 3 dependencies, got %v", data["dep_count"])
	}
}

func TestParsePyprojectToml(t *testing.T) {
	ctx := context.Background()
	g := setupGraph(t)
	emitter := indexer.NewGraphEmitter(g, "gen-1")
	dir := setupProjectDir(t, ctx, g, emitter)

	// Create pyproject.toml with standard format
	pyproject := `[project]
name = "my-python-app"
version = "1.2.3"
dependencies = [
    "requests>=2.28.0",
    "click>=8.0.0",
]
`
	pyPath := filepath.Join(dir, "pyproject.toml")
	if err := os.WriteFile(pyPath, []byte(pyproject), 0644); err != nil {
		t.Fatalf("failed to write pyproject.toml: %v", err)
	}

	fileNode := graph.NewNode(types.TypeFile).
		WithURI(types.PathToURI(pyPath)).
		WithKey(pyPath).
		WithName("pyproject.toml")
	if err := emitter.EmitNode(ctx, fileNode); err != nil {
		t.Fatalf("failed to emit file node: %v", err)
	}
	if err := g.Storage().Flush(ctx); err != nil {
		t.Fatalf("Flush failed: %v", err)
	}

	idx := New()
	ictx := &indexer.Context{
		Root:       types.ProjectPathToURI(dir),
		Generation: "gen-1",
		Graph:      g,
		Emitter:    emitter,
	}

	event := indexer.Event{
		Type:     indexer.EventEntryVisited,
		URI:      types.PathToURI(pyPath),
		Path:     pyPath,
		Name:     "pyproject.toml",
		NodeType: types.TypeFile,
		NodeID:   fileNode.ID,
		Node:     fileNode,
	}

	if err := idx.HandleEvent(ctx, ictx, event); err != nil {
		t.Fatalf("HandleEvent failed: %v", err)
	}
	if err := g.Storage().Flush(ctx); err != nil {
		t.Fatalf("Flush failed: %v", err)
	}

	projects, err := g.FindNodes(ctx, graph.NodeFilter{Type: types.TypeProject}, graph.QueryOptions{})
	if err != nil {
		t.Fatalf("FindNodes failed: %v", err)
	}
	if len(projects) != 1 {
		t.Fatalf("expected 1 project, got %d", len(projects))
	}

	proj := projects[0]
	if proj.Name != "my-python-app" {
		t.Errorf("expected name 'my-python-app', got %q", proj.Name)
	}

	data, ok := proj.Data.(map[string]any)
	if !ok {
		t.Fatalf("expected data to be map, got %T", proj.Data)
	}

	if data["language"] != types.LangPython {
		t.Errorf("expected language %q, got %q", types.LangPython, data["language"])
	}

	if data["version"] != "1.2.3" {
		t.Errorf("expected version '1.2.3', got %q", data["version"])
	}
}

func TestParseComposerJSON(t *testing.T) {
	ctx := context.Background()
	g := setupGraph(t)
	emitter := indexer.NewGraphEmitter(g, "gen-1")
	dir := setupProjectDir(t, ctx, g, emitter)

	// Create composer.json
	composer := `{
  "name": "vendor/my-php-package",
  "version": "3.0.0",
  "require": {
    "php": "^8.0",
    "symfony/console": "^6.0"
  },
  "require-dev": {
    "phpunit/phpunit": "^10.0"
  }
}
`
	composerPath := filepath.Join(dir, "composer.json")
	if err := os.WriteFile(composerPath, []byte(composer), 0644); err != nil {
		t.Fatalf("failed to write composer.json: %v", err)
	}

	fileNode := graph.NewNode(types.TypeFile).
		WithURI(types.PathToURI(composerPath)).
		WithKey(composerPath).
		WithName("composer.json")
	if err := emitter.EmitNode(ctx, fileNode); err != nil {
		t.Fatalf("failed to emit file node: %v", err)
	}
	if err := g.Storage().Flush(ctx); err != nil {
		t.Fatalf("Flush failed: %v", err)
	}

	idx := New()
	ictx := &indexer.Context{
		Root:       types.ProjectPathToURI(dir),
		Generation: "gen-1",
		Graph:      g,
		Emitter:    emitter,
	}

	event := indexer.Event{
		Type:     indexer.EventEntryVisited,
		URI:      types.PathToURI(composerPath),
		Path:     composerPath,
		Name:     "composer.json",
		NodeType: types.TypeFile,
		NodeID:   fileNode.ID,
		Node:     fileNode,
	}

	if err := idx.HandleEvent(ctx, ictx, event); err != nil {
		t.Fatalf("HandleEvent failed: %v", err)
	}
	if err := g.Storage().Flush(ctx); err != nil {
		t.Fatalf("Flush failed: %v", err)
	}

	projects, err := g.FindNodes(ctx, graph.NodeFilter{Type: types.TypeProject}, graph.QueryOptions{})
	if err != nil {
		t.Fatalf("FindNodes failed: %v", err)
	}
	if len(projects) != 1 {
		t.Fatalf("expected 1 project, got %d", len(projects))
	}

	proj := projects[0]
	if proj.Name != "vendor/my-php-package" {
		t.Errorf("expected name 'vendor/my-php-package', got %q", proj.Name)
	}

	data, ok := proj.Data.(map[string]any)
	if !ok {
		t.Fatalf("expected data to be map, got %T", proj.Data)
	}

	if data["language"] != types.LangPHP {
		t.Errorf("expected language %q, got %q", types.LangPHP, data["language"])
	}

	if int(data["dep_count"].(float64)) != 3 {
		t.Errorf("expected 3 dependencies, got %v", data["dep_count"])
	}
}

func TestLocatedAtEdge(t *testing.T) {
	ctx := context.Background()
	g := setupGraph(t)
	emitter := indexer.NewGraphEmitter(g, "gen-1")
	dir := setupProjectDir(t, ctx, g, emitter)

	// Create package.json
	pkgJSON := `{"name": "test-project", "version": "1.0.0"}`
	pkgPath := filepath.Join(dir, "package.json")
	if err := os.WriteFile(pkgPath, []byte(pkgJSON), 0644); err != nil {
		t.Fatalf("failed to write package.json: %v", err)
	}

	fileNode := graph.NewNode(types.TypeFile).
		WithURI(types.PathToURI(pkgPath)).
		WithKey(pkgPath).
		WithName("package.json")
	if err := emitter.EmitNode(ctx, fileNode); err != nil {
		t.Fatalf("failed to emit file node: %v", err)
	}
	if err := g.Storage().Flush(ctx); err != nil {
		t.Fatalf("Flush failed: %v", err)
	}

	idx := New()
	ictx := &indexer.Context{
		Root:       types.ProjectPathToURI(dir),
		Generation: "gen-1",
		Graph:      g,
		Emitter:    emitter,
	}

	event := indexer.Event{
		Type:     indexer.EventEntryVisited,
		URI:      types.PathToURI(pkgPath),
		Path:     pkgPath,
		Name:     "package.json",
		NodeType: types.TypeFile,
		NodeID:   fileNode.ID,
		Node:     fileNode,
	}

	if err := idx.HandleEvent(ctx, ictx, event); err != nil {
		t.Fatalf("HandleEvent failed: %v", err)
	}
	if err := g.Storage().Flush(ctx); err != nil {
		t.Fatalf("Flush failed: %v", err)
	}

	// Find the project node
	projects, err := g.FindNodes(ctx, graph.NodeFilter{Type: types.TypeProject}, graph.QueryOptions{})
	if err != nil {
		t.Fatalf("FindNodes failed: %v", err)
	}
	if len(projects) != 1 {
		t.Fatalf("expected 1 project, got %d", len(projects))
	}

	// Check for located_at edge
	edges, err := g.GetEdgesFrom(ctx, projects[0].ID)
	if err != nil {
		t.Fatalf("GetEdgesFrom failed: %v", err)
	}

	var foundLocatedAt bool
	for _, e := range edges {
		if e.Type == types.EdgeLocatedAt {
			foundLocatedAt = true
			// Verify edge points to the directory
			dirID := graph.IDFromURI(types.PathToURI(dir))
			if e.To != dirID {
				t.Errorf("located_at edge should point to directory ID %s, got %s", dirID, e.To)
			}
			break
		}
	}

	if !foundLocatedAt {
		t.Error("expected project to have 'located_at' edge to directory")
	}
}

func TestIndexerMeta(t *testing.T) {
	idx := New()

	if idx.Name() != "project" {
		t.Errorf("expected name 'project', got %q", idx.Name())
	}

	schemes := idx.Schemes()
	if len(schemes) != 1 || schemes[0] != "project+file" {
		t.Errorf("expected schemes [project+file], got %v", schemes)
	}

	if !idx.Handles("project+file:///home/user/myproject") {
		t.Error("should handle project+file:// URIs")
	}

	if idx.Handles("file:///home/user/myproject") {
		t.Error("should not handle file:// URIs")
	}
}

func TestSubscriptions(t *testing.T) {
	idx := New()
	subs := idx.Subscriptions()

	// Should have 2 subscriptions per manifest file (visit + delete)
	expectedCount := len(manifestFiles) * 2
	if len(subs) != expectedCount {
		t.Fatalf("expected %d subscriptions, got %d", expectedCount, len(subs))
	}

	// Check that all manifest files are covered
	visitSubs := make(map[string]bool)
	deleteSubs := make(map[string]bool)

	for _, sub := range subs {
		switch sub.EventType {
		case indexer.EventEntryVisited:
			visitSubs[sub.Name] = true
		case indexer.EventNodeDeleting:
			deleteSubs[sub.Name] = true
		}
	}

	for name := range manifestFiles {
		if !visitSubs[name] {
			t.Errorf("missing EventEntryVisited subscription for %s", name)
		}
		if !deleteSubs[name] {
			t.Errorf("missing EventNodeDeleting subscription for %s", name)
		}
	}
}

func TestCleanup(t *testing.T) {
	ctx := context.Background()
	g := setupGraph(t)
	emitter := indexer.NewGraphEmitter(g, "gen-1")
	dir := setupProjectDir(t, ctx, g, emitter)

	// Create and index a project
	pkgJSON := `{"name": "test-project", "version": "1.0.0"}`
	pkgPath := filepath.Join(dir, "package.json")
	if err := os.WriteFile(pkgPath, []byte(pkgJSON), 0644); err != nil {
		t.Fatalf("failed to write package.json: %v", err)
	}

	fileNode := graph.NewNode(types.TypeFile).
		WithURI(types.PathToURI(pkgPath)).
		WithKey(pkgPath).
		WithName("package.json")
	if err := emitter.EmitNode(ctx, fileNode); err != nil {
		t.Fatalf("failed to emit file node: %v", err)
	}
	if err := g.Storage().Flush(ctx); err != nil {
		t.Fatalf("Flush failed: %v", err)
	}

	idx := New()
	ictx := &indexer.Context{
		Root:       types.ProjectPathToURI(dir),
		Generation: "gen-1",
		Graph:      g,
		Emitter:    emitter,
	}

	// First, create the project
	event := indexer.Event{
		Type:     indexer.EventEntryVisited,
		URI:      types.PathToURI(pkgPath),
		Path:     pkgPath,
		Name:     "package.json",
		NodeType: types.TypeFile,
		NodeID:   fileNode.ID,
		Node:     fileNode,
	}

	if err := idx.HandleEvent(ctx, ictx, event); err != nil {
		t.Fatalf("HandleEvent (create) failed: %v", err)
	}
	if err := g.Storage().Flush(ctx); err != nil {
		t.Fatalf("Flush failed: %v", err)
	}

	// Verify project exists
	projects, _ := g.FindNodes(ctx, graph.NodeFilter{Type: types.TypeProject}, graph.QueryOptions{})
	if len(projects) != 1 {
		t.Fatalf("expected 1 project before cleanup, got %d", len(projects))
	}

	// Now trigger deletion
	deleteEvent := indexer.Event{
		Type:     indexer.EventNodeDeleting,
		URI:      types.PathToURI(pkgPath),
		Path:     pkgPath,
		Name:     "package.json",
		NodeType: types.TypeFile,
		NodeID:   fileNode.ID,
		Node:     fileNode,
	}

	if err := idx.HandleEvent(ctx, ictx, deleteEvent); err != nil {
		t.Fatalf("HandleEvent (delete) failed: %v", err)
	}
	if err := g.Storage().Flush(ctx); err != nil {
		t.Fatalf("Flush failed: %v", err)
	}

	// Verify project was removed
	projects, _ = g.FindNodes(ctx, graph.NodeFilter{Type: types.TypeProject}, graph.QueryOptions{})
	if len(projects) != 0 {
		t.Errorf("expected 0 projects after cleanup, got %d", len(projects))
	}
}

func TestFallbackToDirectoryName(t *testing.T) {
	ctx := context.Background()
	g := setupGraph(t)
	emitter := indexer.NewGraphEmitter(g, "gen-1")
	dir := setupProjectDir(t, ctx, g, emitter)

	// Create Gemfile (no name/version inside)
	gemfile := `source 'https://rubygems.org'

gem 'rails', '~> 7.0'
gem 'pg'
`
	gemfilePath := filepath.Join(dir, "Gemfile")
	if err := os.WriteFile(gemfilePath, []byte(gemfile), 0644); err != nil {
		t.Fatalf("failed to write Gemfile: %v", err)
	}

	fileNode := graph.NewNode(types.TypeFile).
		WithURI(types.PathToURI(gemfilePath)).
		WithKey(gemfilePath).
		WithName("Gemfile")
	if err := emitter.EmitNode(ctx, fileNode); err != nil {
		t.Fatalf("failed to emit file node: %v", err)
	}
	if err := g.Storage().Flush(ctx); err != nil {
		t.Fatalf("Flush failed: %v", err)
	}

	idx := New()
	ictx := &indexer.Context{
		Root:       types.ProjectPathToURI(dir),
		Generation: "gen-1",
		Graph:      g,
		Emitter:    emitter,
	}

	event := indexer.Event{
		Type:     indexer.EventEntryVisited,
		URI:      types.PathToURI(gemfilePath),
		Path:     gemfilePath,
		Name:     "Gemfile",
		NodeType: types.TypeFile,
		NodeID:   fileNode.ID,
		Node:     fileNode,
	}

	if err := idx.HandleEvent(ctx, ictx, event); err != nil {
		t.Fatalf("HandleEvent failed: %v", err)
	}
	if err := g.Storage().Flush(ctx); err != nil {
		t.Fatalf("Flush failed: %v", err)
	}

	projects, err := g.FindNodes(ctx, graph.NodeFilter{Type: types.TypeProject}, graph.QueryOptions{})
	if err != nil {
		t.Fatalf("FindNodes failed: %v", err)
	}
	if len(projects) != 1 {
		t.Fatalf("expected 1 project, got %d", len(projects))
	}

	// Should use directory name as fallback
	proj := projects[0]
	expectedName := filepath.Base(dir)
	if proj.Name != expectedName {
		t.Errorf("expected name %q (directory name), got %q", expectedName, proj.Name)
	}

	data, ok := proj.Data.(map[string]any)
	if !ok {
		t.Fatalf("expected data to be map, got %T", proj.Data)
	}

	if data["language"] != types.LangRuby {
		t.Errorf("expected language %q, got %q", types.LangRuby, data["language"])
	}
}
