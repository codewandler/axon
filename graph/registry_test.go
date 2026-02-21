package graph

import (
	"errors"
	"reflect"
	"testing"
)

type TestFileData struct {
	Name string `json:"name"`
	Size int64  `json:"size"`
}

type TestDirData struct {
	Name string `json:"name"`
}

func TestRegisterNodeType(t *testing.T) {
	r := NewRegistry()

	RegisterNodeType[TestFileData](r, NodeSpec{
		Type:        "fs:file",
		Description: "A file in the filesystem",
	})

	spec, ok := r.NodeSpec("fs:file")
	if !ok {
		t.Fatal("node type not registered")
	}
	if spec.Type != "fs:file" {
		t.Errorf("expected Type %q, got %q", "fs:file", spec.Type)
	}
	if spec.Description != "A file in the filesystem" {
		t.Errorf("unexpected Description: %q", spec.Description)
	}
	if spec.DataType != reflect.TypeOf(TestFileData{}) {
		t.Errorf("expected DataType TestFileData, got %v", spec.DataType)
	}
}

func TestRegisterEdgeType(t *testing.T) {
	r := NewRegistry()

	r.RegisterEdgeType(EdgeSpec{
		Type:        "contains",
		Description: "Parent contains child",
		FromTypes:   []string{"fs:dir"},
		ToTypes:     []string{"fs:file", "fs:dir"},
	})

	spec, ok := r.EdgeSpec("contains")
	if !ok {
		t.Fatal("edge type not registered")
	}
	if spec.Type != "contains" {
		t.Errorf("expected Type %q, got %q", "contains", spec.Type)
	}
	if len(spec.FromTypes) != 1 || spec.FromTypes[0] != "fs:dir" {
		t.Errorf("unexpected FromTypes: %v", spec.FromTypes)
	}
	if len(spec.ToTypes) != 2 {
		t.Errorf("unexpected ToTypes: %v", spec.ToTypes)
	}
}

func TestNodeTypes(t *testing.T) {
	r := NewRegistry()

	RegisterNodeType[TestFileData](r, NodeSpec{Type: "fs:file"})
	RegisterNodeType[TestDirData](r, NodeSpec{Type: "fs:dir"})

	types := r.NodeTypes()
	if len(types) != 2 {
		t.Errorf("expected 2 types, got %d", len(types))
	}
}

func TestEdgeTypes(t *testing.T) {
	r := NewRegistry()

	r.RegisterEdgeType(EdgeSpec{Type: "contains"})
	r.RegisterEdgeType(EdgeSpec{Type: "depends_on"})

	types := r.EdgeTypes()
	if len(types) != 2 {
		t.Errorf("expected 2 types, got %d", len(types))
	}
}

func TestValidateNode(t *testing.T) {
	r := NewRegistry()
	RegisterNodeType[TestFileData](r, NodeSpec{Type: "fs:file"})

	// Valid node
	node := NewNode("fs:file")
	if err := r.ValidateNode(node); err != nil {
		t.Errorf("unexpected error: %v", err)
	}

	// Invalid node type
	unknown := NewNode("unknown:type")
	err := r.ValidateNode(unknown)
	if err == nil {
		t.Error("expected error for unknown type")
	}
	if !errors.Is(err, ErrUnknownNodeType) {
		t.Errorf("expected ErrUnknownNodeType, got %v", err)
	}
}

func TestValidateEdge(t *testing.T) {
	r := NewRegistry()

	RegisterNodeType[TestDirData](r, NodeSpec{Type: "fs:dir"})
	RegisterNodeType[TestFileData](r, NodeSpec{Type: "fs:file"})

	r.RegisterEdgeType(EdgeSpec{
		Type:      "contains",
		FromTypes: []string{"fs:dir"},
		ToTypes:   []string{"fs:file", "fs:dir"},
	})

	dir := NewNode("fs:dir")
	file := NewNode("fs:file")
	edge := NewEdge("contains", dir.ID, file.ID)

	// Valid edge
	if err := r.ValidateEdge(edge, dir, file); err != nil {
		t.Errorf("unexpected error: %v", err)
	}

	// Invalid: file as source
	invalidEdge := NewEdge("contains", file.ID, dir.ID)
	if err := r.ValidateEdge(invalidEdge, file, dir); err == nil {
		t.Error("expected error for file as source of contains edge")
	}

	// Unknown edge type
	unknownEdge := NewEdge("unknown", dir.ID, file.ID)
	err := r.ValidateEdge(unknownEdge, dir, file)
	if !errors.Is(err, ErrUnknownEdgeType) {
		t.Errorf("expected ErrUnknownEdgeType, got %v", err)
	}
}

func TestValidateEdgeNoConstraints(t *testing.T) {
	r := NewRegistry()

	RegisterNodeType[TestDirData](r, NodeSpec{Type: "fs:dir"})
	RegisterNodeType[TestFileData](r, NodeSpec{Type: "fs:file"})

	// Edge with no type constraints
	r.RegisterEdgeType(EdgeSpec{
		Type: "references",
	})

	file1 := NewNode("fs:file")
	file2 := NewNode("fs:file")
	edge := NewEdge("references", file1.ID, file2.ID)

	// Should be valid - no constraints
	if err := r.ValidateEdge(edge, file1, file2); err != nil {
		t.Errorf("unexpected error: %v", err)
	}
}
