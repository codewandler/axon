package graph

import (
	"testing"
	"time"
)

func TestNewID(t *testing.T) {
	id1 := NewID()
	id2 := NewID()

	if id1 == "" {
		t.Error("NewID() returned empty string")
	}
	if id2 == "" {
		t.Error("NewID() returned empty string")
	}
	if id1 == id2 {
		t.Error("NewID() returned duplicate IDs")
	}

	// gonanoid default length is 21
	if len(id1) != 21 {
		t.Errorf("expected ID length 21, got %d", len(id1))
	}
}

func TestNewNode(t *testing.T) {
	nodeType := "fs:file"
	before := time.Now()
	node := NewNode(nodeType)
	after := time.Now()

	if node.ID == "" {
		t.Error("NewNode() did not generate ID")
	}
	if node.Type != nodeType {
		t.Errorf("expected Type %q, got %q", nodeType, node.Type)
	}
	if node.CreatedAt == nil || node.CreatedAt.Before(before) || node.CreatedAt.After(after) {
		t.Error("CreatedAt not set correctly")
	}
	if node.UpdatedAt == nil || node.UpdatedAt.Before(before) || node.UpdatedAt.After(after) {
		t.Error("UpdatedAt not set correctly")
	}
}

func TestNodeBuilders(t *testing.T) {
	node := NewNode("fs:file").
		WithURI("file:///home/user/test.txt").
		WithKey("/home/user/test.txt").
		WithData(map[string]any{"size": 1024}).
		WithGeneration("gen-001")

	if node.URI != "file:///home/user/test.txt" {
		t.Errorf("expected URI %q, got %q", "file:///home/user/test.txt", node.URI)
	}
	if node.Key != "/home/user/test.txt" {
		t.Errorf("expected Key %q, got %q", "/home/user/test.txt", node.Key)
	}
	if node.Data == nil {
		t.Error("Data not set")
	}
	if node.Generation != "gen-001" {
		t.Errorf("expected Generation %q, got %q", "gen-001", node.Generation)
	}
}

func TestNodeBuilderChaining(t *testing.T) {
	// Ensure builders return the same node instance
	node := NewNode("test")
	result := node.WithURI("test://uri")

	if result != node {
		t.Error("WithURI did not return same node instance")
	}
}
