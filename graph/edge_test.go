package graph

import (
	"testing"
	"time"
)

func TestNewEdge(t *testing.T) {
	edgeType := "contains"
	from := "node-1"
	to := "node-2"

	before := time.Now()
	edge := NewEdge(edgeType, from, to)
	after := time.Now()

	if edge.ID == "" {
		t.Error("NewEdge() did not generate ID")
	}
	if edge.Type != edgeType {
		t.Errorf("expected Type %q, got %q", edgeType, edge.Type)
	}
	if edge.From != from {
		t.Errorf("expected From %q, got %q", from, edge.From)
	}
	if edge.To != to {
		t.Errorf("expected To %q, got %q", to, edge.To)
	}
	if edge.CreatedAt.Before(before) || edge.CreatedAt.After(after) {
		t.Error("CreatedAt not set correctly")
	}
}

func TestEdgeBuilders(t *testing.T) {
	edge := NewEdge("depends_on", "a", "b").
		WithData(map[string]any{"version": "1.0"}).
		WithGeneration("gen-002")

	if edge.Data == nil {
		t.Error("Data not set")
	}
	if edge.Generation != "gen-002" {
		t.Errorf("expected Generation %q, got %q", "gen-002", edge.Generation)
	}
}

func TestEdgeBuilderChaining(t *testing.T) {
	edge := NewEdge("test", "a", "b")
	result := edge.WithData("test")

	if result != edge {
		t.Error("WithData did not return same edge instance")
	}
}
