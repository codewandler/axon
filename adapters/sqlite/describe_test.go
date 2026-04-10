package sqlite

import (
	"context"
	"testing"

	"github.com/codewandler/axon/graph"
)

func TestDescribeSchema_Empty(t *testing.T) {
	ctx := context.Background()
	s := setupTestDB(t)

	desc, err := s.DescribeSchema(ctx, false)
	if err != nil {
		t.Fatalf("DescribeSchema failed: %v", err)
	}
	if desc == nil {
		t.Fatal("expected non-nil SchemaDescription")
	}
	if len(desc.NodeTypes) != 0 {
		t.Errorf("expected 0 node types, got %d", len(desc.NodeTypes))
	}
	if len(desc.EdgeTypes) != 0 {
		t.Errorf("expected 0 edge types, got %d", len(desc.EdgeTypes))
	}
}

func TestDescribeSchema_NodesOnly(t *testing.T) {
	ctx := context.Background()
	s := setupTestDB(t)

	// Insert nodes of two types.
	dir1 := graph.NewNode("fs:dir").WithURI("file:///a").WithName("a")
	dir2 := graph.NewNode("fs:dir").WithURI("file:///b").WithName("b")
	file1 := graph.NewNode("fs:file").WithURI("file:///a/x.go").WithName("x.go")

	for _, n := range []*graph.Node{dir1, dir2, file1} {
		if err := s.PutNode(ctx, n); err != nil {
			t.Fatalf("PutNode(%s): %v", n.URI, err)
		}
	}
	if err := s.Flush(ctx); err != nil {
		t.Fatalf("Flush: %v", err)
	}

	desc, err := s.DescribeSchema(ctx, false)
	if err != nil {
		t.Fatalf("DescribeSchema failed: %v", err)
	}

	if len(desc.NodeTypes) != 2 {
		t.Fatalf("expected 2 node types, got %d", len(desc.NodeTypes))
	}

	// fs:dir has count 2, fs:file has count 1 → ordered by count desc.
	if desc.NodeTypes[0].Type != "fs:dir" || desc.NodeTypes[0].Count != 2 {
		t.Errorf("expected fs:dir=2 first, got %+v", desc.NodeTypes[0])
	}
	if desc.NodeTypes[1].Type != "fs:file" || desc.NodeTypes[1].Count != 1 {
		t.Errorf("expected fs:file=1 second, got %+v", desc.NodeTypes[1])
	}

	// includeFields=false → no fields.
	for _, nt := range desc.NodeTypes {
		if len(nt.Fields) != 0 {
			t.Errorf("expected no fields for %s when includeFields=false, got %v", nt.Type, nt.Fields)
		}
	}

	if len(desc.EdgeTypes) != 0 {
		t.Errorf("expected 0 edge types, got %d", len(desc.EdgeTypes))
	}
}

func TestDescribeSchema_EdgeConnections(t *testing.T) {
	ctx := context.Background()
	s := setupTestDB(t)

	dir := graph.NewNode("fs:dir").WithURI("file:///a").WithName("a")
	file := graph.NewNode("fs:file").WithURI("file:///a/x.go").WithName("x.go")
	pkg := graph.NewNode("go:package").WithURI("pkg:///a").WithName("a")

	for _, n := range []*graph.Node{dir, file, pkg} {
		if err := s.PutNode(ctx, n); err != nil {
			t.Fatalf("PutNode: %v", err)
		}
	}

	// dir -contains-> file
	e1 := graph.NewEdge("contains", dir.ID, file.ID)
	// dir -contains-> pkg (same edge type, different to-type)
	e2 := graph.NewEdge("contains", dir.ID, pkg.ID)
	// file -belongs_to-> dir
	e3 := graph.NewEdge("belongs_to", file.ID, dir.ID)

	for _, e := range []*graph.Edge{e1, e2, e3} {
		if err := s.PutEdge(ctx, e); err != nil {
			t.Fatalf("PutEdge: %v", err)
		}
	}
	if err := s.Flush(ctx); err != nil {
		t.Fatalf("Flush: %v", err)
	}

	desc, err := s.DescribeSchema(ctx, false)
	if err != nil {
		t.Fatalf("DescribeSchema failed: %v", err)
	}

	if len(desc.EdgeTypes) != 2 {
		t.Fatalf("expected 2 edge types, got %d: %+v", len(desc.EdgeTypes), desc.EdgeTypes)
	}

	// "contains" has count 2 → should be first (sorted by count desc).
	containsInfo := desc.EdgeTypes[0]
	if containsInfo.Type != "contains" {
		t.Errorf("expected 'contains' first, got %s", containsInfo.Type)
	}
	if containsInfo.Count != 2 {
		t.Errorf("expected contains count=2, got %d", containsInfo.Count)
	}
	if len(containsInfo.Connections) != 2 {
		t.Errorf("expected 2 connections for 'contains', got %d", len(containsInfo.Connections))
	}

	// "belongs_to" has count 1.
	belongsInfo := desc.EdgeTypes[1]
	if belongsInfo.Type != "belongs_to" {
		t.Errorf("expected 'belongs_to' second, got %s", belongsInfo.Type)
	}
	if belongsInfo.Count != 1 {
		t.Errorf("expected belongs_to count=1, got %d", belongsInfo.Count)
	}
	if len(belongsInfo.Connections) != 1 {
		t.Errorf("expected 1 connection for 'belongs_to', got %d", len(belongsInfo.Connections))
	}
	conn := belongsInfo.Connections[0]
	if conn.From != "fs:file" || conn.To != "fs:dir" {
		t.Errorf("unexpected belongs_to connection: %+v", conn)
	}
}

func TestDescribeSchema_IncludeFields(t *testing.T) {
	ctx := context.Background()
	s := setupTestDB(t)

	// Insert a node with structured data.
	type fileData struct {
		Name string `json:"name"`
		Ext  string `json:"ext"`
		Size int    `json:"size"`
	}
	n := graph.NewNode("fs:file").
		WithURI("file:///foo.go").
		WithName("foo.go").
		WithData(fileData{Name: "foo.go", Ext: "go", Size: 1234})

	if err := s.PutNode(ctx, n); err != nil {
		t.Fatalf("PutNode: %v", err)
	}
	if err := s.Flush(ctx); err != nil {
		t.Fatalf("Flush: %v", err)
	}

	tests := []struct {
		name          string
		includeFields bool
		wantFields    bool
	}{
		{"without fields", false, false},
		{"with fields", true, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			desc, err := s.DescribeSchema(ctx, tt.includeFields)
			if err != nil {
				t.Fatalf("DescribeSchema(%v): %v", tt.includeFields, err)
			}
			if len(desc.NodeTypes) != 1 {
				t.Fatalf("expected 1 node type, got %d", len(desc.NodeTypes))
			}
			nt := desc.NodeTypes[0]
			if tt.wantFields {
				if len(nt.Fields) == 0 {
					t.Errorf("expected fields for %s when includeFields=true, got none", nt.Type)
				}
				// Check that known fields are present.
				fieldSet := make(map[string]bool)
				for _, f := range nt.Fields {
					fieldSet[f] = true
				}
				for _, expected := range []string{"name", "ext", "size"} {
					if !fieldSet[expected] {
						t.Errorf("expected field %q in %v", expected, nt.Fields)
					}
				}
			} else {
				if len(nt.Fields) != 0 {
					t.Errorf("expected no fields when includeFields=false, got %v", nt.Fields)
				}
			}
		})
	}
}
