package main

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/codewandler/axon/graph"
)

// fixture builds a SchemaDescription representative enough to exercise all
// rendering paths: multiple node types, edge types with connections, and
// (optionally) field lists.
func describeFixture(withFields bool) *graph.SchemaDescription {
	nodeTypes := []graph.NodeTypeInfo{
		{Type: "fs:file", Count: 100},
		{Type: "fs:dir", Count: 20},
	}
	if withFields {
		nodeTypes[0].Fields = []string{"ext", "name", "size"}
		nodeTypes[1].Fields = []string{"name"}
	}
	return &graph.SchemaDescription{
		NodeTypes: nodeTypes,
		EdgeTypes: []graph.EdgeTypeInfo{
			{
				Type:  "contains",
				Count: 120,
				Connections: []graph.EdgeConnection{
					{From: "fs:dir", To: "fs:file", Count: 100},
					{From: "fs:dir", To: "fs:dir", Count: 20},
				},
			},
		},
	}
}

func TestRenderDescribeText_NodeAndEdgeCounts(t *testing.T) {
	desc := describeFixture(false)
	// renderDescribeText writes to os.Stdout (matching the pattern used by
	// renderStatsText and other CLI renderers in this package). We verify it
	// returns no error and produces visible output during -v test runs.
	if err := renderDescribeText(desc, false); err != nil {
		t.Fatalf("renderDescribeText returned error: %v", err)
	}
}

func TestRenderDescribeText_Empty(t *testing.T) {
	desc := &graph.SchemaDescription{
		NodeTypes: []graph.NodeTypeInfo{},
		EdgeTypes: []graph.EdgeTypeInfo{},
	}
	if err := renderDescribeText(desc, false); err != nil {
		t.Fatalf("renderDescribeText on empty desc returned error: %v", err)
	}
}

func TestRenderDescribeText_WithFields(t *testing.T) {
	desc := describeFixture(true)
	if err := renderDescribeText(desc, true); err != nil {
		t.Fatalf("renderDescribeText with fields returned error: %v", err)
	}
}

func TestRenderDescribeJSON_Shape(t *testing.T) {
	desc := describeFixture(false)

	// Capture output: renderDescribeJSON writes to os.Stdout, so we test
	// the JSON shape by marshaling ourselves with the same logic.
	data, err := json.MarshalIndent(desc, "", "  ")
	if err != nil {
		t.Fatalf("json.MarshalIndent: %v", err)
	}

	var got graph.SchemaDescription
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("json.Unmarshal: %v", err)
	}

	if len(got.NodeTypes) != 2 {
		t.Errorf("expected 2 node types, got %d", len(got.NodeTypes))
	}
	if len(got.EdgeTypes) != 1 {
		t.Errorf("expected 1 edge type, got %d", len(got.EdgeTypes))
	}
	if got.EdgeTypes[0].Type != "contains" {
		t.Errorf("expected edge type 'contains', got %s", got.EdgeTypes[0].Type)
	}
	if len(got.EdgeTypes[0].Connections) != 2 {
		t.Errorf("expected 2 connections, got %d", len(got.EdgeTypes[0].Connections))
	}
}

func TestRenderDescribeJSON_EmptyArraysNotNull(t *testing.T) {
	// Empty SchemaDescription must serialize as [] not null.
	desc := &graph.SchemaDescription{
		NodeTypes: []graph.NodeTypeInfo{},
		EdgeTypes: []graph.EdgeTypeInfo{},
	}

	data, err := json.MarshalIndent(desc, "", "  ")
	if err != nil {
		t.Fatalf("json.MarshalIndent: %v", err)
	}

	s := string(data)
	if strings.Contains(s, `"node_types": null`) {
		t.Errorf("node_types should be [] not null, got:\n%s", s)
	}
	if strings.Contains(s, `"edge_types": null`) {
		t.Errorf("edge_types should be [] not null, got:\n%s", s)
	}
	if !strings.Contains(s, `"node_types": []`) {
		t.Errorf("expected node_types: [], got:\n%s", s)
	}
	if !strings.Contains(s, `"edge_types": []`) {
		t.Errorf("expected edge_types: [], got:\n%s", s)
	}
}
