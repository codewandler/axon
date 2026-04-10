package axon

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestAxon_Describe_NoFields(t *testing.T) {
	ctx := context.Background()
	dir := setupTestDir(t)

	ax, err := New(Config{Dir: dir})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	if _, err := ax.Index(ctx, dir); err != nil {
		t.Fatalf("Index: %v", err)
	}

	desc, err := ax.Describe(ctx, false)
	if err != nil {
		t.Fatalf("Describe(false): %v", err)
	}
	if desc == nil {
		t.Fatal("expected non-nil SchemaDescription")
	}

	// The test dir has files and directories, so expect at least fs:file and fs:dir.
	typeSet := make(map[string]int)
	totalNodes := 0
	for _, nt := range desc.NodeTypes {
		typeSet[nt.Type] = nt.Count
		totalNodes += nt.Count
	}

	if typeSet["fs:file"] == 0 {
		t.Errorf("expected at least one fs:file node type, got %+v", typeSet)
	}
	if typeSet["fs:dir"] == 0 {
		t.Errorf("expected at least one fs:dir node type, got %+v", typeSet)
	}

	// No fields when includeFields=false.
	for _, nt := range desc.NodeTypes {
		if len(nt.Fields) != 0 {
			t.Errorf("expected no fields for %s when includeFields=false, got %v", nt.Type, nt.Fields)
		}
	}

	// totalNodes must be positive — we indexed a real directory.
	if totalNodes == 0 {
		t.Errorf("expected at least one node, got 0")
	}
}

func TestAxon_Describe_WithFields(t *testing.T) {
	ctx := context.Background()
	dir := setupTestDir(t)

	ax, err := New(Config{Dir: dir})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	if _, err := ax.Index(ctx, dir); err != nil {
		t.Fatalf("Index: %v", err)
	}

	desc, err := ax.Describe(ctx, true)
	if err != nil {
		t.Fatalf("Describe(true): %v", err)
	}
	if desc == nil {
		t.Fatal("expected non-nil SchemaDescription")
	}

	// With includeFields=true, fs:file nodes should have at least a "name" or "ext" field.
	for _, nt := range desc.NodeTypes {
		if nt.Type == "fs:file" {
			if len(nt.Fields) == 0 {
				t.Errorf("expected fields for fs:file when includeFields=true, got none")
			}
			return
		}
	}
	t.Error("fs:file not found in node types")
}

func TestAxon_Describe_GoFiles(t *testing.T) {
	ctx := context.Background()

	// Set up a temp dir with a Go file so the Go indexer runs.
	dir := t.TempDir()
	goFile := filepath.Join(dir, "main.go")
	if err := os.WriteFile(goFile, []byte(`package main

// Add adds two integers.
func Add(a, b int) int { return a + b }
`), 0644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	modFile := filepath.Join(dir, "go.mod")
	if err := os.WriteFile(modFile, []byte("module example.com/test\n\ngo 1.21\n"), 0644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	ax, err := New(Config{Dir: dir})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	if _, err := ax.Index(ctx, dir); err != nil {
		t.Fatalf("Index: %v", err)
	}

	desc, err := ax.Describe(ctx, false)
	if err != nil {
		t.Fatalf("Describe: %v", err)
	}

	typeSet := make(map[string]int)
	for _, nt := range desc.NodeTypes {
		typeSet[nt.Type] = nt.Count
	}

	// Go indexer should have produced go:func and go:package nodes.
	if typeSet["go:func"] == 0 {
		t.Errorf("expected go:func nodes, got types: %v", typeSet)
	}
	if typeSet["go:package"] == 0 {
		t.Errorf("expected go:package nodes, got types: %v", typeSet)
	}

	// There should be edge types too (e.g. contains, defines).
	if len(desc.EdgeTypes) == 0 {
		t.Errorf("expected at least one edge type after Go indexing, got none")
	}
}
