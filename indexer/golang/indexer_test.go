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

// TestEndLineCapture verifies that EndLine is captured for all symbol types.
func TestEndLineCapture(t *testing.T) {
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

	// Create fs:file node for go.mod
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

	// Trigger indexing
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

	// Check that functions have EndLine > Line
	funcs, err := g.FindNodes(ctx, graph.NodeFilter{Type: types.TypeGoFunc}, graph.QueryOptions{})
	if err != nil {
		t.Fatalf("FindNodes for functions failed: %v", err)
	}

	for _, fn := range funcs {
		data, ok := fn.Data.(map[string]any)
		if !ok {
			t.Errorf("expected func data to be map, got %T", fn.Data)
			continue
		}
		pos, ok := data["position"].(map[string]any)
		if !ok {
			t.Errorf("expected position in func data, got %v", data)
			continue
		}
		line := int(pos["line"].(float64))
		endLine := int(pos["end_line"].(float64))

		if endLine < line {
			t.Errorf("func %s: EndLine (%d) should be >= Line (%d)", fn.Name, endLine, line)
		}

		// Multi-line functions should have EndLine > Line
		if fn.Name == "New" || fn.Name == "Max" {
			if endLine <= line {
				t.Errorf("func %s: expected EndLine > Line for multi-line function, got Line=%d EndLine=%d", fn.Name, line, endLine)
			}
		}
	}

	// Check that structs have EndLine > Line
	structs, err := g.FindNodes(ctx, graph.NodeFilter{Type: types.TypeGoStruct}, graph.QueryOptions{})
	if err != nil {
		t.Fatalf("FindNodes for structs failed: %v", err)
	}

	for _, st := range structs {
		data, ok := st.Data.(map[string]any)
		if !ok {
			continue
		}
		pos, ok := data["position"].(map[string]any)
		if !ok {
			continue
		}
		line := int(pos["line"].(float64))
		endLine := int(pos["end_line"].(float64))

		if endLine < line {
			t.Errorf("struct %s: EndLine (%d) should be >= Line (%d)", st.Name, endLine, line)
		}

		// Multi-line structs should have EndLine > Line
		if st.Name == "Config" || st.Name == "User" {
			if endLine <= line {
				t.Errorf("struct %s: expected EndLine > Line for multi-line struct, got Line=%d EndLine=%d", st.Name, line, endLine)
			}
		}
	}

	// Check interfaces
	ifaces, err := g.FindNodes(ctx, graph.NodeFilter{Type: types.TypeGoInterface}, graph.QueryOptions{})
	if err != nil {
		t.Fatalf("FindNodes for interfaces failed: %v", err)
	}

	for _, iface := range ifaces {
		data, ok := iface.Data.(map[string]any)
		if !ok {
			continue
		}
		pos, ok := data["position"].(map[string]any)
		if !ok {
			continue
		}
		line := int(pos["line"].(float64))
		endLine := int(pos["end_line"].(float64))

		if endLine < line {
			t.Errorf("interface %s: EndLine (%d) should be >= Line (%d)", iface.Name, endLine, line)
		}
	}

	// Check methods
	methods, err := g.FindNodes(ctx, graph.NodeFilter{Type: types.TypeGoMethod}, graph.QueryOptions{})
	if err != nil {
		t.Fatalf("FindNodes for methods failed: %v", err)
	}

	for _, m := range methods {
		data, ok := m.Data.(map[string]any)
		if !ok {
			continue
		}
		pos, ok := data["position"].(map[string]any)
		if !ok {
			continue
		}
		line := int(pos["line"].(float64))
		endLine := int(pos["end_line"].(float64))

		if endLine < line {
			t.Errorf("method %s: EndLine (%d) should be >= Line (%d)", m.Name, endLine, line)
		}
	}

	t.Logf("Verified EndLine for %d functions, %d structs, %d interfaces, %d methods",
		len(funcs), len(structs), len(ifaces), len(methods))
}

// TestImportGraph verifies that import edges are emitted between intra-module packages.
func TestImportGraph(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()

	// Create go.mod
	goMod := `module example.com/testmod

go 1.21
`
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte(goMod), 0644); err != nil {
		t.Fatalf("failed to write go.mod: %v", err)
	}

	// Create package A
	aDir := filepath.Join(dir, "pkg", "a")
	if err := os.MkdirAll(aDir, 0755); err != nil {
		t.Fatalf("failed to create pkg/a dir: %v", err)
	}
	aGo := `package a

// Hello returns a greeting.
func Hello() string {
	return "hello"
}
`
	if err := os.WriteFile(filepath.Join(aDir, "a.go"), []byte(aGo), 0644); err != nil {
		t.Fatalf("failed to write a.go: %v", err)
	}

	// Create package B that imports package A
	bDir := filepath.Join(dir, "pkg", "b")
	if err := os.MkdirAll(bDir, 0755); err != nil {
		t.Fatalf("failed to create pkg/b dir: %v", err)
	}
	bGo := `package b

import "example.com/testmod/pkg/a"

// UseA uses package A.
func UseA() string {
	return a.Hello()
}
`
	if err := os.WriteFile(filepath.Join(bDir, "b.go"), []byte(bGo), 0644); err != nil {
		t.Fatalf("failed to write b.go: %v", err)
	}

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

	// Verify package nodes exist
	pkgNodes, err := g.FindNodes(ctx, graph.NodeFilter{Type: types.TypeGoPackage}, graph.QueryOptions{})
	if err != nil {
		t.Fatalf("FindNodes for packages failed: %v", err)
	}

	moduleURI := types.GoModulePathToURI(dir)
	aPkgURI := moduleURI + "/pkg/example.com/testmod/pkg/a"
	bPkgURI := moduleURI + "/pkg/example.com/testmod/pkg/b"
	aID := graph.IDFromURI(aPkgURI)
	bID := graph.IDFromURI(bPkgURI)

	foundA, foundB := false, false
	for _, pkg := range pkgNodes {
		if pkg.ID == aID {
			foundA = true
		}
		if pkg.ID == bID {
			foundB = true
		}
	}
	if !foundA {
		t.Error("expected to find go:package node for package A")
	}
	if !foundB {
		t.Error("expected to find go:package node for package B")
	}

	// Verify imports edge from B to A
	bEdges, err := g.GetEdgesFrom(ctx, bID)
	if err != nil {
		t.Fatalf("GetEdgesFrom B failed: %v", err)
	}
	foundImportsEdge := false
	for _, e := range bEdges {
		if e.Type == types.EdgeImports && e.To == aID {
			foundImportsEdge = true
			break
		}
	}
	if !foundImportsEdge {
		t.Errorf("expected imports edge from B to A; B edges: %v", bEdges)
	}

	// Verify no imports edge from A to B (A does not import B)
	aEdges, err := g.GetEdgesFrom(ctx, aID)
	if err != nil {
		t.Fatalf("GetEdgesFrom A failed: %v", err)
	}
	for _, e := range aEdges {
		if e.Type == types.EdgeImports && e.To == bID {
			t.Error("unexpected imports edge from A to B")
		}
	}
}

// setupAndIndex is a helper to set up a graph, run the indexer on a module dir, and flush.
func setupAndIndex(t *testing.T, dir string) (*graph.Graph, *indexer.Context) {
	t.Helper()
	ctx := context.Background()
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
	return g, ictx
}

// TestImplements verifies that implements edges are emitted for structs that implement interfaces.
func TestImplements(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()

	goMod := `module example.com/testmod

go 1.21
`
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte(goMod), 0644); err != nil {
		t.Fatalf("failed to write go.mod: %v", err)
	}

	// Create a package with an interface and implementing structs
	mainGo := `package main

import "errors"

// Doer is an interface with a Do method.
type Doer interface {
	Do() error
}

// Concrete implements Doer with a value receiver.
type Concrete struct{}

// Do implements Doer.
func (c Concrete) Do() error {
	return nil
}

// PtrImpl implements Doer with a pointer receiver.
type PtrImpl struct{}

// Do implements Doer via pointer receiver.
func (p *PtrImpl) Do() error {
	return errors.New("ptr")
}

// NoImpl does not implement Doer.
type NoImpl struct{}

func main() {}
`
	if err := os.WriteFile(filepath.Join(dir, "main.go"), []byte(mainGo), 0644); err != nil {
		t.Fatalf("failed to write main.go: %v", err)
	}

	g, _ := setupAndIndex(t, dir)

	moduleURI := types.GoModulePathToURI(dir)
	pkgBase := moduleURI + "/pkg/example.com/testmod"
	concreteURI := pkgBase + "/struct/Concrete"
	ptrImplURI := pkgBase + "/struct/PtrImpl"
	noImplURI := pkgBase + "/struct/NoImpl"
	doerURI := pkgBase + "/interface/Doer"

	concreteID := graph.IDFromURI(concreteURI)
	ptrImplID := graph.IDFromURI(ptrImplURI)
	noImplID := graph.IDFromURI(noImplURI)
	doerID := graph.IDFromURI(doerURI)

	// Helper to check if an implements edge exists
	hasImplements := func(fromID, toID string) bool {
		t.Helper()
		edges, err := g.GetEdgesFrom(ctx, fromID)
		if err != nil {
			t.Fatalf("GetEdgesFrom %s failed: %v", fromID, err)
		}
		for _, e := range edges {
			if e.Type == types.EdgeImplements && e.To == toID {
				return true
			}
		}
		return false
	}

	// Concrete (value receiver) should implement Doer
	if !hasImplements(concreteID, doerID) {
		t.Error("expected implements edge from Concrete to Doer")
	}

	// PtrImpl (pointer receiver) should implement Doer
	if !hasImplements(ptrImplID, doerID) {
		t.Error("expected implements edge from PtrImpl to Doer (via pointer receiver)")
	}

	// NoImpl should NOT implement Doer
	if hasImplements(noImplID, doerID) {
		t.Error("unexpected implements edge from NoImpl to Doer")
	}
}

// TestTestLinkage verifies that tests edges are emitted from test packages to source packages.
func TestTestLinkage(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()

	goMod := `module example.com/testmod

go 1.21
`
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte(goMod), 0644); err != nil {
		t.Fatalf("failed to write go.mod: %v", err)
	}

	// Create mypkg with an exported function
	mypkgDir := filepath.Join(dir, "mypkg")
	if err := os.MkdirAll(mypkgDir, 0755); err != nil {
		t.Fatalf("failed to create mypkg dir: %v", err)
	}
	mypkgGo := `package mypkg

// Greet returns a greeting.
func Greet() string {
	return "hello"
}
`
	if err := os.WriteFile(filepath.Join(mypkgDir, "mypkg.go"), []byte(mypkgGo), 0644); err != nil {
		t.Fatalf("failed to write mypkg.go: %v", err)
	}

	// Create external test file (package mypkg_test)
	testGo := `package mypkg_test

import (
	"testing"
	"example.com/testmod/mypkg"
)

func TestGreet(t *testing.T) {
	if mypkg.Greet() == "" {
		t.Error("expected non-empty greeting")
	}
}
`
	if err := os.WriteFile(filepath.Join(mypkgDir, "mypkg_test.go"), []byte(testGo), 0644); err != nil {
		t.Fatalf("failed to write mypkg_test.go: %v", err)
	}

	g, _ := setupAndIndex(t, dir)

	moduleURI := types.GoModulePathToURI(dir)
	mypkgURI := moduleURI + "/pkg/example.com/testmod/mypkg"
	testPkgURI := moduleURI + "/pkg/example.com/testmod/mypkg_test"
	mypkgID := graph.IDFromURI(mypkgURI)
	testPkgID := graph.IDFromURI(testPkgURI)

	// Both package nodes should exist
	pkgNodes, err := g.FindNodes(ctx, graph.NodeFilter{Type: types.TypeGoPackage}, graph.QueryOptions{})
	if err != nil {
		t.Fatalf("FindNodes failed: %v", err)
	}

	foundMypkg, foundTestPkg := false, false
	for _, pkg := range pkgNodes {
		if pkg.ID == mypkgID {
			foundMypkg = true
		}
		if pkg.ID == testPkgID {
			foundTestPkg = true
		}
	}
	if !foundMypkg {
		t.Error("expected go:package node for mypkg")
	}
	if !foundTestPkg {
		t.Error("expected go:package node for mypkg_test")
	}

	// tests edge should exist from mypkg_test to mypkg
	testEdges, err := g.GetEdgesFrom(ctx, testPkgID)
	if err != nil {
		t.Fatalf("GetEdgesFrom testPkg failed: %v", err)
	}
	foundTestsEdge := false
	for _, e := range testEdges {
		if e.Type == types.EdgeTests && e.To == mypkgID {
			foundTestsEdge = true
			break
		}
	}
	if !foundTestsEdge {
		t.Errorf("expected tests edge from mypkg_test to mypkg; edges: %v", testEdges)
	}

	// Verify IsTest flag on package nodes
	for _, pkg := range pkgNodes {
		data, ok := pkg.Data.(map[string]any)
		if !ok {
			continue
		}
		isTest, _ := data["is_test"].(bool)
		if pkg.ID == mypkgID && isTest {
			t.Error("mypkg should have IsTest=false")
		}
		if pkg.ID == testPkgID && !isTest {
			t.Error("mypkg_test should have IsTest=true")
		}
	}
}

// TestCallGraph verifies that:
//   - go:ref nodes have CallerURI/CallerName/CallerType set when the usage is
//     inside a function/method body.
//   - A deduplicated go:func -[calls]-> go:func edge is emitted for each unique
//     (caller, callee) pair.
func TestCallGraph(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()

	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module example.com/callgraph\n\ngo 1.21\n"), 0644); err != nil {
		t.Fatalf("write go.mod: %v", err)
	}

	// main.go: Greet is called multiple times by Run to verify edge de-dup.
	mainGo := `package main

import "fmt"

// Greet returns a greeting string.
func Greet(name string) string {
	return fmt.Sprintf("Hello, %s", name)
}

// Run calls Greet twice (edge must be deduplicated to one).
func Run() {
	_ = Greet("World")
	_ = Greet("again")
	fmt.Println("done")
}
`
	if err := os.WriteFile(filepath.Join(dir, "main.go"), []byte(mainGo), 0644); err != nil {
		t.Fatalf("write main.go: %v", err)
	}

	g, ictx := setupAndIndex(t, dir)

	moduleURI := ictx.Root
	pkgURI := moduleURI + "/pkg/example.com/callgraph"
	greetURI := pkgURI + "/func/Greet"
	runURI := pkgURI + "/func/Run"
	greetID := graph.IDFromURI(greetURI)
	runID := graph.IDFromURI(runURI)

	// --- 1. go:ref nodes for calls to Greet must have CallerURI pointing to Run ---
	refs, err := g.FindNodes(ctx, graph.NodeFilter{Type: types.TypeGoRef}, graph.QueryOptions{})
	if err != nil {
		t.Fatalf("FindNodes refs: %v", err)
	}

	var callRefsToGreet int
	for _, ref := range refs {
		data, ok := ref.Data.(map[string]any)
		if !ok {
			continue
		}
		if data["kind"] != types.RefKindCall || data["name"] != "Greet" {
			continue
		}
		callRefsToGreet++
		callerURI, _ := data["caller_uri"].(string)
		if callerURI != runURI {
			t.Errorf("ref.CallerURI = %q, want %q", callerURI, runURI)
		}
		callerName, _ := data["caller_name"].(string)
		if callerName != "Run" {
			t.Errorf("ref.CallerName = %q, want \"Run\"", callerName)
		}
		callerType, _ := data["caller_type"].(string)
		if callerType != types.TypeGoFunc {
			t.Errorf("ref.CallerType = %q, want %q", callerType, types.TypeGoFunc)
		}
	}
	if callRefsToGreet < 2 {
		// Greet is called twice, so at least 2 call-refs should exist
		t.Errorf("expected >= 2 call refs to Greet, got %d", callRefsToGreet)
	}

	// --- 2. Exactly one calls edge from Run -> Greet (de-duplicated) ---
	runEdges, err := g.GetEdgesFrom(ctx, runID)
	if err != nil {
		t.Fatalf("GetEdgesFrom Run: %v", err)
	}
	var callsEdgesToGreet int
	for _, e := range runEdges {
		if e.Type == types.EdgeCalls && e.To == greetID {
			callsEdgesToGreet++
		}
	}
	if callsEdgesToGreet != 1 {
		t.Errorf("expected exactly 1 calls edge Run->Greet, got %d", callsEdgesToGreet)
	}

	// --- 3. No calls edge in the reverse direction ---
	greetEdges, err := g.GetEdgesFrom(ctx, greetID)
	if err != nil {
		t.Fatalf("GetEdgesFrom Greet: %v", err)
	}
	for _, e := range greetEdges {
		if e.Type == types.EdgeCalls && e.To == runID {
			t.Error("unexpected calls edge Greet->Run")
		}
	}
}

// TestEmbeds verifies that go:struct -[embeds]-> go:struct edges are emitted
// for both value embeddings and pointer embeddings.
func TestEmbeds(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()

	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module example.com/embeds\n\ngo 1.21\n"), 0644); err != nil {
		t.Fatalf("write go.mod: %v", err)
	}

	mainGo := `package main

// Base is the embedded type.
type Base struct {
	ID int
}

// Derived embeds Base by value.
type Derived struct {
	Base
	Name string
}

// WithPtr embeds Base by pointer.
type WithPtr struct {
	*Base
	Label string
}
`
	if err := os.WriteFile(filepath.Join(dir, "main.go"), []byte(mainGo), 0644); err != nil {
		t.Fatalf("write main.go: %v", err)
	}

	g, ictx := setupAndIndex(t, dir)

	moduleURI := ictx.Root
	pkgURI := moduleURI + "/pkg/example.com/embeds"
	baseURI := pkgURI + "/struct/Base"
	derivedURI := pkgURI + "/struct/Derived"
	withPtrURI := pkgURI + "/struct/WithPtr"
	baseID := graph.IDFromURI(baseURI)
	derivedID := graph.IDFromURI(derivedURI)
	withPtrID := graph.IDFromURI(withPtrURI)

	// Derived -[embeds]-> Base
	derivedEdges, err := g.GetEdgesFrom(ctx, derivedID)
	if err != nil {
		t.Fatalf("GetEdgesFrom Derived: %v", err)
	}
	foundDerivedEmbedsBase := false
	for _, e := range derivedEdges {
		if e.Type == types.EdgeEmbeds && e.To == baseID {
			foundDerivedEmbedsBase = true
			break
		}
	}
	if !foundDerivedEmbedsBase {
		t.Errorf("expected Derived -[embeds]-> Base edge; Derived edges: %v", derivedEdges)
	}

	// WithPtr -[embeds]-> Base (pointer embed)
	withPtrEdges, err := g.GetEdgesFrom(ctx, withPtrID)
	if err != nil {
		t.Fatalf("GetEdgesFrom WithPtr: %v", err)
	}
	foundWithPtrEmbedsBase := false
	for _, e := range withPtrEdges {
		if e.Type == types.EdgeEmbeds && e.To == baseID {
			foundWithPtrEmbedsBase = true
			break
		}
	}
	if !foundWithPtrEmbedsBase {
		t.Errorf("expected WithPtr -[embeds]-> Base edge (pointer embed); WithPtr edges: %v", withPtrEdges)
	}
}
