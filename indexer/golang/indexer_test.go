package golang

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

// setupTestModule creates a minimal Go module in a temp directory.
func setupTestModule(t *testing.T) string {
	t.Helper()

	dir := t.TempDir()

	// Create go.mod
	goMod := `module example.com/testmod

go 1.21
`
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte(goMod), 0644); err != nil {
		t.Fatalf("failed to write go.mod: %v", err)
	}

	// Create main.go with exported symbols
	mainGo := `// Package main is the entry point.
package main

import "fmt"

// Version is the application version.
const Version = "1.0.0"

// Config holds application configuration.
type Config struct {
	// Name is the application name.
	Name string
	// Debug enables debug mode.
	Debug bool
}

// User represents a user in the system.
type User struct {
	ID   int
	Name string
	// unexported field
	password string
}

// String returns a string representation.
func (u *User) String() string {
	return u.Name
}

// validate is unexported.
func (u *User) validate() bool {
	return u.Name != ""
}

// Reader is an interface for reading data.
type Reader interface {
	// Read reads data.
	Read(p []byte) (n int, err error)
}

// DefaultConfig is the default configuration.
var DefaultConfig = Config{Name: "app", Debug: false}

// New creates a new application.
func New(cfg Config) *Config {
	return &cfg
}

// internal is unexported.
func internal() {}

func main() {
	fmt.Println("Hello")
}
`
	if err := os.WriteFile(filepath.Join(dir, "main.go"), []byte(mainGo), 0644); err != nil {
		t.Fatalf("failed to write main.go: %v", err)
	}

	// Create a subpackage
	pkgDir := filepath.Join(dir, "pkg", "util")
	if err := os.MkdirAll(pkgDir, 0755); err != nil {
		t.Fatalf("failed to create pkg dir: %v", err)
	}

	utilGo := `// Package util provides utility functions.
package util

// Max returns the maximum of two integers.
func Max(a, b int) int {
	if a > b {
		return a
	}
	return b
}

// Helper is a helper type.
type Helper struct {
	Value int
}

// Get returns the value.
func (h *Helper) Get() int {
	return h.Value
}
`
	if err := os.WriteFile(filepath.Join(pkgDir, "util.go"), []byte(utilGo), 0644); err != nil {
		t.Fatalf("failed to write util.go: %v", err)
	}

	return dir
}

func setupGraph(t *testing.T) *graph.Graph {
	t.Helper()
	r := graph.NewRegistry()
	types.RegisterCommonEdges(r)
	types.RegisterFSTypes(r)
	types.RegisterGoTypes(r)
	s, err := sqlite.New(":memory:")
	if err != nil {
		t.Fatalf("sqlite.New failed: %v", err)
	}
	t.Cleanup(func() { s.Close() })
	return graph.New(s, r)
}

func TestIndexerBasic(t *testing.T) {
	ctx := context.Background()
	dir := setupTestModule(t)
	g := setupGraph(t)

	idx := New()
	emitter := indexer.NewGraphEmitter(g, "gen-1")

	ictx := &indexer.Context{
		Root:       types.GoModulePathToURI(dir),
		Generation: "gen-1",
		Graph:      g,
		Emitter:    emitter,
	}

	// Create a mock fs:file node for go.mod (simulating what FS indexer would do)
	goModPath := filepath.Join(dir, "go.mod")
	goModNode := graph.NewNode(types.TypeFile).
		WithURI(types.PathToURI(goModPath)).
		WithKey(goModPath).
		WithName("go.mod")
	if err := emitter.EmitNode(ctx, goModNode); err != nil {
		t.Fatalf("failed to emit go.mod node: %v", err)
	}
	if err := g.Storage().Flush(ctx); err != nil {
		t.Fatalf("Flush failed: %v", err)
	}

	// Simulate the event that FS indexer would send when visiting go.mod
	event := indexer.Event{
		Type:     indexer.EventEntryVisited,
		URI:      types.PathToURI(goModPath),
		Path:     goModPath,
		Name:     "go.mod",
		NodeType: types.TypeFile,
		NodeID:   goModNode.ID,
		Node:     goModNode,
	}

	if err := idx.HandleEvent(ctx, ictx, event); err != nil {
		t.Fatalf("HandleEvent failed: %v", err)
	}

	// Flush storage
	if err := g.Storage().Flush(ctx); err != nil {
		t.Fatalf("Flush failed: %v", err)
	}

	// Should have module node
	modules, err := g.FindNodes(ctx, graph.NodeFilter{Type: types.TypeGoModule}, graph.QueryOptions{})
	if err != nil {
		t.Fatalf("FindNodes for modules failed: %v", err)
	}
	if len(modules) != 1 {
		t.Fatalf("expected 1 module, got %d", len(modules))
	}

	module := modules[0]
	if module.Name != "example.com/testmod" {
		t.Errorf("expected module name 'example.com/testmod', got %q", module.Name)
	}

	// Check module data
	if data, ok := module.Data.(types.ModuleData); ok {
		if data.GoVer != "1.21" {
			t.Errorf("expected Go version '1.21', got %q", data.GoVer)
		}
	}

	// Should have packages
	packages, err := g.FindNodes(ctx, graph.NodeFilter{Type: types.TypeGoPackage}, graph.QueryOptions{})
	if err != nil {
		t.Fatalf("FindNodes for packages failed: %v", err)
	}
	if len(packages) < 2 {
		t.Errorf("expected at least 2 packages (main + util), got %d", len(packages))
	}

	// Check for main package
	foundMain := false
	for _, pkg := range packages {
		if pkg.Name == "main" {
			foundMain = true
			break
		}
	}
	if !foundMain {
		t.Error("expected to find main package")
	}

	// Should have structs
	structs, err := g.FindNodes(ctx, graph.NodeFilter{Type: types.TypeGoStruct}, graph.QueryOptions{})
	if err != nil {
		t.Fatalf("FindNodes for structs failed: %v", err)
	}
	// Config, User, Helper = 3 exported structs
	if len(structs) < 3 {
		t.Errorf("expected at least 3 structs, got %d", len(structs))
	}

	// Should have functions
	funcs, err := g.FindNodes(ctx, graph.NodeFilter{Type: types.TypeGoFunc}, graph.QueryOptions{})
	if err != nil {
		t.Fatalf("FindNodes for funcs failed: %v", err)
	}
	// New, Max = at least 2 exported funcs
	if len(funcs) < 2 {
		t.Errorf("expected at least 2 functions, got %d", len(funcs))
	}

	// Should have methods
	methods, err := g.FindNodes(ctx, graph.NodeFilter{Type: types.TypeGoMethod}, graph.QueryOptions{})
	if err != nil {
		t.Fatalf("FindNodes for methods failed: %v", err)
	}
	// String, Get (+ interface Read method) = at least 2
	if len(methods) < 2 {
		t.Errorf("expected at least 2 methods, got %d", len(methods))
	}

	// Should have constants
	consts, err := g.FindNodes(ctx, graph.NodeFilter{Type: types.TypeGoConst}, graph.QueryOptions{})
	if err != nil {
		t.Fatalf("FindNodes for consts failed: %v", err)
	}
	if len(consts) < 1 {
		t.Errorf("expected at least 1 constant (Version), got %d", len(consts))
	}

	// Should have interface
	interfaces, err := g.FindNodes(ctx, graph.NodeFilter{Type: types.TypeGoInterface}, graph.QueryOptions{})
	if err != nil {
		t.Fatalf("FindNodes for interfaces failed: %v", err)
	}
	if len(interfaces) < 1 {
		t.Errorf("expected at least 1 interface (Reader), got %d", len(interfaces))
	}
}

func TestIndexerExportedOnly(t *testing.T) {
	ctx := context.Background()
	dir := setupTestModule(t)
	g := setupGraph(t)

	idx := New()
	idx.ExportedOnly = true // This is the default
	emitter := indexer.NewGraphEmitter(g, "gen-1")

	ictx := &indexer.Context{
		Root:       types.GoModulePathToURI(dir),
		Generation: "gen-1",
		Graph:      g,
		Emitter:    emitter,
	}

	// Create go.mod node
	goModPath := filepath.Join(dir, "go.mod")
	goModNode := graph.NewNode(types.TypeFile).
		WithURI(types.PathToURI(goModPath)).
		WithKey(goModPath).
		WithName("go.mod")
	if err := emitter.EmitNode(ctx, goModNode); err != nil {
		t.Fatalf("failed to emit go.mod node: %v", err)
	}
	if err := g.Storage().Flush(ctx); err != nil {
		t.Fatalf("Flush failed: %v", err)
	}

	event := indexer.Event{
		Type:     indexer.EventEntryVisited,
		URI:      types.PathToURI(goModPath),
		Path:     goModPath,
		Name:     "go.mod",
		NodeType: types.TypeFile,
		NodeID:   goModNode.ID,
		Node:     goModNode,
	}

	if err := idx.HandleEvent(ctx, ictx, event); err != nil {
		t.Fatalf("HandleEvent failed: %v", err)
	}

	if err := g.Storage().Flush(ctx); err != nil {
		t.Fatalf("Flush failed: %v", err)
	}

	// Check that unexported symbols are not indexed
	funcs, _ := g.FindNodes(ctx, graph.NodeFilter{Type: types.TypeGoFunc}, graph.QueryOptions{})
	for _, fn := range funcs {
		if fn.Name == "internal" {
			t.Error("unexported function 'internal' should not be indexed")
		}
	}

	methods, _ := g.FindNodes(ctx, graph.NodeFilter{Type: types.TypeGoMethod}, graph.QueryOptions{})
	for _, m := range methods {
		if m.Name == "validate" {
			t.Error("unexported method 'validate' should not be indexed")
		}
	}
}

func TestTagging(t *testing.T) {
	ctx := context.Background()
	dir := setupTestModule(t)
	g := setupGraph(t)

	idx := New()
	emitter := indexer.NewGraphEmitter(g, "gen-1")

	ictx := &indexer.Context{
		Root:       types.GoModulePathToURI(dir),
		Generation: "gen-1",
		Graph:      g,
		Emitter:    emitter,
	}

	// Test go.mod tagging
	goModPath := filepath.Join(dir, "go.mod")
	goModNode := graph.NewNode(types.TypeFile).
		WithURI(types.PathToURI(goModPath)).
		WithKey(goModPath).
		WithName("go.mod")
	if err := emitter.EmitNode(ctx, goModNode); err != nil {
		t.Fatalf("failed to emit go.mod node: %v", err)
	}
	if err := g.Storage().Flush(ctx); err != nil {
		t.Fatalf("Flush failed: %v", err)
	}

	event := indexer.Event{
		Type:     indexer.EventEntryVisited,
		URI:      types.PathToURI(goModPath),
		Path:     goModPath,
		Name:     "go.mod",
		NodeType: types.TypeFile,
		NodeID:   goModNode.ID,
		Node:     goModNode,
	}

	if err := idx.HandleEvent(ctx, ictx, event); err != nil {
		t.Fatalf("HandleEvent for go.mod failed: %v", err)
	}

	// Create and test go.sum tagging
	goSumPath := filepath.Join(dir, "go.sum")
	if err := os.WriteFile(goSumPath, []byte(""), 0644); err != nil {
		t.Fatalf("failed to write go.sum: %v", err)
	}

	goSumNode := graph.NewNode(types.TypeFile).
		WithURI(types.PathToURI(goSumPath)).
		WithKey(goSumPath).
		WithName("go.sum")
	if err := emitter.EmitNode(ctx, goSumNode); err != nil {
		t.Fatalf("failed to emit go.sum node: %v", err)
	}

	sumEvent := indexer.Event{
		Type:     indexer.EventEntryVisited,
		URI:      types.PathToURI(goSumPath),
		Path:     goSumPath,
		Name:     "go.sum",
		NodeType: types.TypeFile,
		NodeID:   goSumNode.ID,
		Node:     goSumNode,
	}

	if err := idx.HandleEvent(ctx, ictx, sumEvent); err != nil {
		t.Fatalf("HandleEvent for go.sum failed: %v", err)
	}

	if err := g.Storage().Flush(ctx); err != nil {
		t.Fatalf("Flush failed: %v", err)
	}

	// Check labels
	modNode, err := g.GetNodeByURI(ctx, types.PathToURI(goModPath))
	if err != nil {
		t.Fatalf("GetNodeByURI for go.mod failed: %v", err)
	}
	hasGoModLabel := false
	for _, l := range modNode.Labels {
		if l == types.LabelGoMod {
			hasGoModLabel = true
			break
		}
	}
	if !hasGoModLabel {
		t.Errorf("go.mod should have label %q, got labels %v", types.LabelGoMod, modNode.Labels)
	}

	sumNode, err := g.GetNodeByURI(ctx, types.PathToURI(goSumPath))
	if err != nil {
		t.Fatalf("GetNodeByURI for go.sum failed: %v", err)
	}
	hasGoSumLabel := false
	for _, l := range sumNode.Labels {
		if l == types.LabelGoSum {
			hasGoSumLabel = true
			break
		}
	}
	if !hasGoSumLabel {
		t.Errorf("go.sum should have label %q, got labels %v", types.LabelGoSum, sumNode.Labels)
	}
}

func TestIndexerMeta(t *testing.T) {
	idx := New()

	if idx.Name() != "golang" {
		t.Errorf("expected name 'golang', got %q", idx.Name())
	}

	schemes := idx.Schemes()
	if len(schemes) != 1 || schemes[0] != "go+file" {
		t.Errorf("expected schemes [go+file], got %v", schemes)
	}

	if !idx.Handles("go+file:///home/user/project") {
		t.Error("should handle go+file:// URIs")
	}

	if idx.Handles("file:///home/user/project") {
		t.Error("should not handle file:// URIs")
	}
}

func TestSubscriptions(t *testing.T) {
	idx := New()
	subs := idx.Subscriptions()

	if len(subs) != 3 {
		t.Fatalf("expected 3 subscriptions, got %d", len(subs))
	}

	// First subscription: EventEntryVisited for go.mod files
	sub := subs[0]
	if sub.EventType != indexer.EventEntryVisited {
		t.Error("expected first subscription to be EventEntryVisited")
	}
	if sub.NodeType != types.TypeFile {
		t.Errorf("expected NodeType fs:file, got %s", sub.NodeType)
	}
	if sub.Name != "go.mod" {
		t.Errorf("expected Name go.mod, got %s", sub.Name)
	}

	// Second subscription: EventEntryVisited for go.sum files
	sub2 := subs[1]
	if sub2.EventType != indexer.EventEntryVisited {
		t.Error("expected second subscription to be EventEntryVisited")
	}
	if sub2.Name != "go.sum" {
		t.Errorf("expected Name go.sum, got %s", sub2.Name)
	}

	// Third subscription: EventNodeDeleting for go.mod files
	sub3 := subs[2]
	if sub3.EventType != indexer.EventNodeDeleting {
		t.Error("expected third subscription to be EventNodeDeleting")
	}
	if sub3.Name != "go.mod" {
		t.Errorf("expected Name go.mod, got %s", sub3.Name)
	}
}

func TestEdges(t *testing.T) {
	ctx := context.Background()
	dir := setupTestModule(t)
	g := setupGraph(t)

	idx := New()
	emitter := indexer.NewGraphEmitter(g, "gen-1")

	ictx := &indexer.Context{
		Root:       types.GoModulePathToURI(dir),
		Generation: "gen-1",
		Graph:      g,
		Emitter:    emitter,
	}

	goModPath := filepath.Join(dir, "go.mod")
	goModNode := graph.NewNode(types.TypeFile).
		WithURI(types.PathToURI(goModPath)).
		WithKey(goModPath).
		WithName("go.mod")
	if err := emitter.EmitNode(ctx, goModNode); err != nil {
		t.Fatalf("failed to emit go.mod node: %v", err)
	}
	if err := g.Storage().Flush(ctx); err != nil {
		t.Fatalf("Flush failed: %v", err)
	}

	event := indexer.Event{
		Type:     indexer.EventEntryVisited,
		URI:      types.PathToURI(goModPath),
		Path:     goModPath,
		Name:     "go.mod",
		NodeType: types.TypeFile,
		NodeID:   goModNode.ID,
		Node:     goModNode,
	}

	if err := idx.HandleEvent(ctx, ictx, event); err != nil {
		t.Fatalf("HandleEvent failed: %v", err)
	}

	if err := g.Storage().Flush(ctx); err != nil {
		t.Fatalf("Flush failed: %v", err)
	}

	// Check module -> package edges (contains)
	modules, _ := g.FindNodes(ctx, graph.NodeFilter{Type: types.TypeGoModule}, graph.QueryOptions{})
	if len(modules) > 0 {
		edges, err := g.GetEdgesFrom(ctx, modules[0].ID)
		if err != nil {
			t.Fatalf("GetEdgesFrom failed: %v", err)
		}

		containsCount := 0
		for _, e := range edges {
			if e.Type == types.EdgeContains {
				containsCount++
			}
		}
		if containsCount < 1 {
			t.Error("expected module to have 'contains' edges to packages")
		}
	}

	// Check package -> struct edges (defines)
	packages, _ := g.FindNodes(ctx, graph.NodeFilter{Type: types.TypeGoPackage}, graph.QueryOptions{})
	definesFound := false
	for _, pkg := range packages {
		edges, _ := g.GetEdgesFrom(ctx, pkg.ID)
		for _, e := range edges {
			if e.Type == types.EdgeDefines {
				definesFound = true
				break
			}
		}
		if definesFound {
			break
		}
	}
	if !definesFound {
		t.Error("expected package to have 'defines' edges to symbols")
	}
}
