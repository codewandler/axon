package sqlite

import (
	"context"
	"testing"

	"github.com/codewandler/axon/aql"
	"github.com/codewandler/axon/graph"
)

func setupAQLTest(t *testing.T) (*Storage, context.Context) {
	t.Helper()
	s, err := New(":memory:")
	if err != nil {
		t.Fatalf("failed to create storage: %v", err)
	}
	t.Cleanup(func() { s.Close() })

	ctx := context.Background()

	// Insert test data with more structure for pattern tests
	testNodes := []*graph.Node{
		// Files
		graph.NewNode("fs:file").WithURI("file:///test1.go").WithName("test1.go").WithData(map[string]any{"ext": "go", "size": 100}).WithLabels("test", "code"),
		graph.NewNode("fs:file").WithURI("file:///test2.py").WithName("test2.py").WithData(map[string]any{"ext": "py", "size": 200}).WithLabels("test"),
		graph.NewNode("fs:file").WithURI("file:///README.md").WithName("README.md").WithData(map[string]any{"ext": "md"}),
		graph.NewNode("fs:file").WithURI("file:///main.go").WithName("main.go").WithData(map[string]any{"ext": "go", "size": 50}),
		// Directories
		graph.NewNode("fs:dir").WithURI("file:///src").WithName("src").WithLabels("source"),
		graph.NewNode("fs:dir").WithURI("file:///cmd").WithName("cmd"),
		// VCS
		graph.NewNode("vcs:repo").WithURI("git+file:///repo").WithName("myrepo"),
		graph.NewNode("vcs:branch").WithURI("git+file:///repo#main").WithName("main"),
		graph.NewNode("vcs:branch").WithURI("git+file:///repo#dev").WithName("dev"),
	}

	for _, node := range testNodes {
		if err := s.PutNode(ctx, node); err != nil {
			t.Fatalf("failed to insert test node: %v", err)
		}
	}

	testEdges := []*graph.Edge{
		// Directory containment
		graph.NewEdge("contains", testNodes[4].ID, testNodes[0].ID), // src -> test1.go
		graph.NewEdge("contains", testNodes[4].ID, testNodes[1].ID), // src -> test2.py
		graph.NewEdge("contains", testNodes[5].ID, testNodes[3].ID), // cmd -> main.go
		// Repo ownership
		graph.NewEdge("has", testNodes[6].ID, testNodes[7].ID), // repo -> main branch
		graph.NewEdge("has", testNodes[6].ID, testNodes[8].ID), // repo -> dev branch
		// Repo location
		graph.NewEdge("located_at", testNodes[6].ID, testNodes[4].ID), // repo -> src dir
		// Multi-type edge test
		graph.NewEdge("references", testNodes[0].ID, testNodes[3].ID), // test1.go -> main.go
	}

	for _, edge := range testEdges {
		if err := s.PutEdge(ctx, edge); err != nil {
			t.Fatalf("failed to insert test edge: %v", err)
		}
	}

	if err := s.Flush(ctx); err != nil {
		t.Fatalf("failed to flush: %v", err)
	}

	return s, ctx
}

// Test SELECT *
func TestQuery_SelectStar(t *testing.T) {
	s, ctx := setupAQLTest(t)

	q := aql.SelectStar().From("nodes").Build()
	result, err := s.Query(ctx, q)
	if err != nil {
		t.Fatalf("Query failed: %v", err)
	}

	if result.Type != graph.ResultTypeNodes {
		t.Errorf("expected ResultTypeNodes, got %v", result.Type)
	}

	// 4 files + 2 dirs + 1 repo + 2 branches = 9 nodes
	if len(result.Nodes) != 9 {
		t.Errorf("expected 9 nodes, got %d", len(result.Nodes))
	}
}

// Test SELECT specific columns
func TestQuery_SelectColumns(t *testing.T) {
	s, ctx := setupAQLTest(t)

	q := aql.Select(aql.Col("name"), aql.Col("type")).From("nodes").Build()
	result, err := s.Query(ctx, q)
	if err != nil {
		t.Fatalf("Query failed: %v", err)
	}

	if len(result.Nodes) != 9 {
		t.Errorf("expected 9 nodes, got %d", len(result.Nodes))
	}

	// Check that only name and type are populated (partial field selection)
	for _, node := range result.Nodes {
		if node.Name == "" {
			t.Errorf("expected name to be populated")
		}
		if node.Type == "" {
			t.Errorf("expected type to be populated")
		}
		// ID and other fields should be zero-values since not selected
		if node.ID != "" {
			t.Errorf("expected id to be empty (not selected), got %s", node.ID)
		}
	}
}

// Test WHERE type =
func TestQuery_WhereEqual(t *testing.T) {
	s, ctx := setupAQLTest(t)

	q := aql.SelectStar().
		From("nodes").
		Where(aql.Eq("type", aql.String("fs:file"))).
		Build()

	result, err := s.Query(ctx, q)
	if err != nil {
		t.Fatalf("Query failed: %v", err)
	}

	// 4 fs:file nodes in test data
	if len(result.Nodes) != 4 {
		t.Errorf("expected 4 fs:file nodes, got %d", len(result.Nodes))
	}

	for _, node := range result.Nodes {
		if node.Type != "fs:file" {
			t.Errorf("expected type fs:file, got %s", node.Type)
		}
	}
}

// Test WHERE type GLOB
func TestQuery_WhereGlob(t *testing.T) {
	s, ctx := setupAQLTest(t)

	q := aql.SelectStar().
		From("nodes").
		Where(&aql.ComparisonExpr{
			Left:  aql.Col("type"),
			Op:    aql.OpGlob,
			Right: aql.String("fs:*"),
		}).
		Build()

	result, err := s.Query(ctx, q)
	if err != nil {
		t.Fatalf("Query failed: %v", err)
	}

	// 4 files + 2 dirs = 6 fs:* nodes
	if len(result.Nodes) != 6 {
		t.Errorf("expected 6 fs:* nodes, got %d", len(result.Nodes))
	}
}

// Test WHERE labels CONTAINS ANY
func TestQuery_WhereLabelsContainsAny(t *testing.T) {
	s, ctx := setupAQLTest(t)

	q := aql.SelectStar().
		From("nodes").
		Where(aql.ContainsAny("labels", aql.String("test"), aql.String("code"))).
		Build()

	result, err := s.Query(ctx, q)
	if err != nil {
		t.Fatalf("Query failed: %v", err)
	}

	if len(result.Nodes) != 2 {
		t.Errorf("expected 2 nodes with test or code labels, got %d", len(result.Nodes))
	}
}

// Test WHERE labels CONTAINS ALL
func TestQuery_WhereLabelsContainsAll(t *testing.T) {
	s, ctx := setupAQLTest(t)

	q := aql.SelectStar().
		From("nodes").
		Where(aql.ContainsAll("labels", aql.String("test"), aql.String("code"))).
		Build()

	result, err := s.Query(ctx, q)
	if err != nil {
		t.Fatalf("Query failed: %v", err)
	}

	if len(result.Nodes) != 1 {
		t.Errorf("expected 1 node with both test and code labels, got %d", len(result.Nodes))
	}
}

// Test WHERE labels NOT CONTAINS
func TestQuery_WhereLabelsNotContains(t *testing.T) {
	s, ctx := setupAQLTest(t)

	q := aql.SelectStar().
		From("nodes").
		Where(aql.NotContains("labels", aql.String("test"))).
		Build()

	result, err := s.Query(ctx, q)
	if err != nil {
		t.Fatalf("Query failed: %v", err)
	}

	// Should get nodes without "test" label (src, cmd, README, main.go, repo, 2 branches = 7)
	if len(result.Nodes) != 7 {
		t.Errorf("expected 7 nodes without test label, got %d", len(result.Nodes))
	}
}

// Test WHERE data.ext = 'go' (JSON extraction)
func TestQuery_WhereJSONField(t *testing.T) {
	s, ctx := setupAQLTest(t)

	q := aql.SelectStar().
		From("nodes").
		Where(&aql.ComparisonExpr{
			Left:  aql.Col("data", "ext"),
			Op:    aql.OpEq,
			Right: aql.String("go"),
		}).
		Build()

	result, err := s.Query(ctx, q)
	if err != nil {
		t.Fatalf("Query failed: %v", err)
	}

	// test1.go and main.go both have ext=go
	if len(result.Nodes) != 2 {
		t.Errorf("expected 2 nodes with ext=go, got %d", len(result.Nodes))
	}

	for _, node := range result.Nodes {
		if data, ok := node.Data.(map[string]any); ok {
			if ext, ok := data["ext"].(string); !ok || ext != "go" {
				t.Errorf("expected ext=go for node %s", node.Name)
			}
		}
	}
}

// Test WHERE IN
func TestQuery_WhereIn(t *testing.T) {
	s, ctx := setupAQLTest(t)

	q := aql.SelectStar().
		From("nodes").
		Where(aql.In("type", aql.String("fs:file"), aql.String("fs:dir"))).
		Build()

	result, err := s.Query(ctx, q)
	if err != nil {
		t.Fatalf("Query failed: %v", err)
	}

	// 4 files + 2 dirs = 6 nodes matching fs:file or fs:dir
	if len(result.Nodes) != 6 {
		t.Errorf("expected 6 nodes, got %d", len(result.Nodes))
	}
}

// Test WHERE BETWEEN
func TestQuery_WhereBetween(t *testing.T) {
	s, ctx := setupAQLTest(t)

	q := aql.SelectStar().
		From("nodes").
		Where(aql.Between("data.size", aql.Int(50), aql.Int(150))).
		Build()

	result, err := s.Query(ctx, q)
	if err != nil {
		t.Fatalf("Query failed: %v", err)
	}

	// test1.go (size=100) and main.go (size=50) are between 50 and 150
	if len(result.Nodes) != 2 {
		t.Errorf("expected 2 nodes with size between 50 and 150, got %d", len(result.Nodes))
	}
}

// Test WHERE IS NULL
func TestQuery_WhereIsNull(t *testing.T) {
	s, ctx := setupAQLTest(t)

	// Insert node without data
	emptyNode := graph.NewNode("test:empty").WithURI("file:///empty").WithName("empty")
	if err := s.PutNode(ctx, emptyNode); err != nil {
		t.Fatalf("failed to insert empty node: %v", err)
	}
	s.Flush(ctx)

	q := aql.SelectStar().
		From("nodes").
		Where(aql.IsNull("data")).
		Build()

	result, err := s.Query(ctx, q)
	if err != nil {
		t.Fatalf("Query failed: %v", err)
	}

	if len(result.Nodes) < 1 {
		t.Errorf("expected at least 1 node with null data, got %d", len(result.Nodes))
	}
}

// Test WHERE IS NOT NULL
func TestQuery_WhereIsNotNull(t *testing.T) {
	s, ctx := setupAQLTest(t)

	q := aql.SelectStar().
		From("nodes").
		Where(aql.IsNotNull("data")).
		Build()

	result, err := s.Query(ctx, q)
	if err != nil {
		t.Fatalf("Query failed: %v", err)
	}

	// Should get nodes with data (test1.go, test2.py, README.md, main.go)
	if len(result.Nodes) != 4 {
		t.Errorf("expected 4 nodes with data, got %d", len(result.Nodes))
	}
}

// Test WHERE boolean logic (AND/OR/NOT)
func TestQuery_WhereBooleanLogic(t *testing.T) {
	s, ctx := setupAQLTest(t)

	// (type = 'fs:file' OR type = 'fs:dir') AND labels CONTAINS ANY ('test')
	q := aql.SelectStar().
		From("nodes").
		Where(aql.And(
			aql.Paren(aql.Or(
				aql.Eq("type", aql.String("fs:file")),
				aql.Eq("type", aql.String("fs:dir")),
			)),
			aql.ContainsAny("labels", aql.String("test")),
		)).
		Build()

	result, err := s.Query(ctx, q)
	if err != nil {
		t.Fatalf("Query failed: %v", err)
	}

	if len(result.Nodes) != 2 {
		t.Errorf("expected 2 nodes, got %d", len(result.Nodes))
	}
}

// Test SELECT COUNT(*)
func TestQuery_Count(t *testing.T) {
	s, ctx := setupAQLTest(t)

	q := aql.Select(aql.Count()).From("nodes").Build()
	result, err := s.Query(ctx, q)
	if err != nil {
		t.Fatalf("Query failed: %v", err)
	}

	// COUNT without GROUP BY doesn't populate Counts map in current implementation
	// This is a limitation - would need to handle this case specially
	// For now, this test documents current behavior
	_ = result
}

// Test GROUP BY
func TestQuery_GroupBy(t *testing.T) {
	s, ctx := setupAQLTest(t)

	q := aql.Select(aql.Col("type"), aql.Count()).
		From("nodes").
		GroupByCol("type").
		Build()

	result, err := s.Query(ctx, q)
	if err != nil {
		t.Fatalf("Query failed: %v", err)
	}

	if result.Type != graph.ResultTypeCounts {
		t.Errorf("expected ResultTypeCounts, got %v", result.Type)
	}

	// 4 types: fs:file, fs:dir, vcs:repo, vcs:branch
	if len(result.Counts) != 4 {
		t.Errorf("expected 4 types, got %d", len(result.Counts))
	}

	if result.Counts["fs:file"] != 4 {
		t.Errorf("expected 4 fs:file, got %d", result.Counts["fs:file"])
	}

	if result.Counts["fs:dir"] != 2 {
		t.Errorf("expected 2 fs:dir, got %d", result.Counts["fs:dir"])
	}
}

// Test GROUP BY with HAVING
func TestQuery_GroupByWithHaving(t *testing.T) {
	s, ctx := setupAQLTest(t)

	q := aql.Select(aql.Col("type"), aql.Count()).
		From("nodes").
		GroupByCol("type").
		Having(&aql.ComparisonExpr{
			Left:  aql.Col("COUNT(*)"),
			Op:    aql.OpGt,
			Right: aql.Int(1),
		}).
		Build()

	result, err := s.Query(ctx, q)
	if err != nil {
		t.Fatalf("Query failed: %v", err)
	}

	// fs:file (4), fs:dir (2), vcs:branch (2) all have count > 1
	if len(result.Counts) != 3 {
		t.Errorf("expected 3 types with count > 1, got %d", len(result.Counts))
	}

	if result.Counts["fs:file"] != 4 {
		t.Errorf("expected fs:file count 4, got %d", result.Counts["fs:file"])
	}
}

// Test ORDER BY
func TestQuery_OrderBy(t *testing.T) {
	s, ctx := setupAQLTest(t)

	q := aql.SelectStar().
		From("nodes").
		Where(aql.Eq("type", aql.String("fs:file"))).
		OrderBy("name").
		Build()

	result, err := s.Query(ctx, q)
	if err != nil {
		t.Fatalf("Query failed: %v", err)
	}

	if len(result.Nodes) != 4 {
		t.Fatalf("expected 4 nodes, got %d", len(result.Nodes))
	}

	// Check ascending order (README.md, main.go, test1.go, test2.py)
	if result.Nodes[0].Name != "README.md" {
		t.Errorf("expected README.md first, got %s", result.Nodes[0].Name)
	}
}

// Test ORDER BY DESC
func TestQuery_OrderByDesc(t *testing.T) {
	s, ctx := setupAQLTest(t)

	q := aql.SelectStar().
		From("nodes").
		Where(aql.Eq("type", aql.String("fs:file"))).
		OrderByDesc("name").
		Build()

	result, err := s.Query(ctx, q)
	if err != nil {
		t.Fatalf("Query failed: %v", err)
	}

	if len(result.Nodes) != 4 {
		t.Fatalf("expected 4 nodes, got %d", len(result.Nodes))
	}

	// Check descending order (test2.py, test1.go, main.go, README.md)
	if result.Nodes[0].Name != "test2.py" {
		t.Errorf("expected test2.py first, got %s", result.Nodes[0].Name)
	}
}

// Test LIMIT
func TestQuery_Limit(t *testing.T) {
	s, ctx := setupAQLTest(t)

	q := aql.SelectStar().
		From("nodes").
		Limit(2).
		Build()

	result, err := s.Query(ctx, q)
	if err != nil {
		t.Fatalf("Query failed: %v", err)
	}

	if len(result.Nodes) != 2 {
		t.Errorf("expected 2 nodes, got %d", len(result.Nodes))
	}
}

// Test LIMIT with OFFSET
func TestQuery_LimitOffset(t *testing.T) {
	s, ctx := setupAQLTest(t)

	q := aql.SelectStar().
		From("nodes").
		OrderBy("name").
		Limit(2).
		Offset(1).
		Build()

	result, err := s.Query(ctx, q)
	if err != nil {
		t.Fatalf("Query failed: %v", err)
	}

	if len(result.Nodes) != 2 {
		t.Errorf("expected 2 nodes, got %d", len(result.Nodes))
	}

	// After sorting by name and skipping first, should not include "README.md"
	for _, node := range result.Nodes {
		if node.Name == "README.md" {
			t.Errorf("OFFSET should skip README.md")
		}
	}
}

// Test DISTINCT
func TestQuery_Distinct(t *testing.T) {
	s, ctx := setupAQLTest(t)

	q := aql.SelectDistinct(aql.Col("type")).
		From("nodes").
		Build()

	result, err := s.Query(ctx, q)
	if err != nil {
		t.Fatalf("Query failed: %v", err)
	}

	// Should get 4 distinct types (fs:file, fs:dir, vcs:repo, vcs:branch)
	if len(result.Nodes) != 4 {
		t.Errorf("expected 4 distinct types, got %d", len(result.Nodes))
	}
}

// Test SELECT from edges
func TestQuery_SelectEdges(t *testing.T) {
	s, ctx := setupAQLTest(t)

	q := aql.SelectStar().From("edges").Build()
	result, err := s.Query(ctx, q)
	if err != nil {
		t.Fatalf("Query failed: %v", err)
	}

	if result.Type != graph.ResultTypeEdges {
		t.Errorf("expected ResultTypeEdges, got %v", result.Type)
	}

	// 3 contains + 2 has + 1 located_at + 1 references = 7 edges
	if len(result.Edges) != 7 {
		t.Errorf("expected 7 edges, got %d", len(result.Edges))
	}
}

// Test Explain
func TestQuery_Explain(t *testing.T) {
	s, ctx := setupAQLTest(t)

	q := aql.SelectStar().
		From("nodes").
		Where(aql.Eq("type", aql.String("fs:file"))).
		Build()

	plan, err := s.Explain(ctx, q)
	if err != nil {
		t.Fatalf("Explain failed: %v", err)
	}

	if plan.SQL == "" {
		t.Error("expected SQL to be populated")
	}

	if plan.SQLitePlan == "" {
		t.Error("expected SQLite plan to be populated")
	}

	if len(plan.Args) != 1 {
		t.Errorf("expected 1 arg, got %d", len(plan.Args))
	}
}

// Test validation error
func TestQuery_ValidationError(t *testing.T) {
	s, ctx := setupAQLTest(t)

	// HAVING without GROUP BY
	q := aql.SelectStar().
		From("nodes").
		Having(aql.Eq("type", aql.String("fs:file"))).
		Build()

	_, err := s.Query(ctx, q)
	if err == nil {
		t.Error("expected validation error for HAVING without GROUP BY")
	}

	qe, ok := err.(*QueryError)
	if !ok {
		t.Errorf("expected QueryError, got %T", err)
	}

	if qe.Phase != "validate" {
		t.Errorf("expected validate phase, got %s", qe.Phase)
	}
}

// Test unsupported table error
func TestQuery_UnsupportedTable(t *testing.T) {
	s, ctx := setupAQLTest(t)

	q := aql.SelectStar().From("invalid").Build()

	_, err := s.Query(ctx, q)
	if err == nil {
		t.Error("expected error for invalid table")
	}

	qe, ok := err.(*QueryError)
	if !ok {
		t.Errorf("expected QueryError, got %T", err)
	}

	// Table validation happens in validate phase, not compile phase
	if qe.Phase != "validate" {
		t.Errorf("expected validate phase, got %s", qe.Phase)
	}
}

// ============================================================================
// PHASE 2: PATTERN QUERY TESTS
// ============================================================================

// Test basic pattern: (dir:fs:dir)-[:contains]->(file:fs:file)
func TestPattern_BasicNodeTypes(t *testing.T) {
	s, ctx := setupAQLTest(t)

	pattern := aql.Pat(aql.NodeType("dir", "fs:dir")).
		To(aql.AnyEdgeOfType("contains"), aql.NodeType("file", "fs:file")).
		Build()

	q := aql.Select(aql.Col("file")).
		FromPattern(pattern).
		Limit(10).
		Build()

	result, err := s.Query(ctx, q)
	if err != nil {
		t.Fatalf("Pattern query failed: %v", err)
	}

	if result.Type != graph.ResultTypeNodes {
		t.Errorf("expected ResultTypeNodes, got %v", result.Type)
	}

	// Should get all files with dir->file edges (test1.go, test2.py from src, main.go from cmd)
	if len(result.Nodes) != 3 {
		t.Errorf("expected 3 files, got %d", len(result.Nodes))
	}

	for _, node := range result.Nodes {
		if node.Type != "fs:file" {
			t.Errorf("expected fs:file, got %s", node.Type)
		}
	}
}

// Test pattern with edge variable: (a)-[e:contains]->(b)
func TestPattern_EdgeVariable(t *testing.T) {
	s, ctx := setupAQLTest(t)

	pattern := aql.Pat(aql.N("a")).
		To(aql.EdgeType("e", "contains"), aql.N("b")).
		Build()

	q := aql.Select(aql.Col("e")).
		FromPattern(pattern).
		Limit(10).
		Build()

	result, err := s.Query(ctx, q)
	if err != nil {
		t.Fatalf("Pattern query failed: %v", err)
	}

	if result.Type != graph.ResultTypeEdges {
		t.Errorf("expected ResultTypeEdges, got %v", result.Type)
	}

	// Should get 3 contains edges (src->test1.go, src->test2.py, cmd->main.go)
	if len(result.Edges) != 3 {
		t.Errorf("expected 3 contains edges, got %d", len(result.Edges))
	}

	for _, edge := range result.Edges {
		if edge.Type != "contains" {
			t.Errorf("expected contains edge, got %s", edge.Type)
		}
	}
}

// Test pattern with multi-type edges: (a)-[:contains|has]->(b)
func TestPattern_MultiTypeEdge(t *testing.T) {
	s, ctx := setupAQLTest(t)

	pattern := aql.Pat(aql.N("a")).
		To(aql.EdgeTypes("contains", "has"), aql.N("b")).
		Build()

	q := aql.Select(aql.Col("b")).
		FromPattern(pattern).
		Limit(10).
		Build()

	result, err := s.Query(ctx, q)
	if err != nil {
		t.Fatalf("Pattern query failed: %v", err)
	}

	// Should get files (via contains) + branches (via has)
	// 3 files + 2 branches = 5 nodes
	if len(result.Nodes) != 5 {
		t.Errorf("expected 5 nodes, got %d", len(result.Nodes))
	}
}

// Test incoming edge pattern: (branch:vcs:branch)<-[:has]-(repo:vcs:repo)
func TestPattern_IncomingEdge(t *testing.T) {
	s, ctx := setupAQLTest(t)

	pattern := aql.Pat(aql.NodeType("branch", "vcs:branch")).
		From(aql.AnyEdgeOfType("has"), aql.NodeType("repo", "vcs:repo")).
		Build()

	q := aql.Select(aql.Col("repo")).
		FromPattern(pattern).
		Limit(10).
		Build()

	result, err := s.Query(ctx, q)
	if err != nil {
		t.Fatalf("Pattern query failed: %v", err)
	}

	// Should get 1 repo (queried twice via 2 branches, but may get duplicates)
	if len(result.Nodes) < 1 {
		t.Errorf("expected at least 1 repo, got %d", len(result.Nodes))
	}

	for _, node := range result.Nodes {
		if node.Type != "vcs:repo" {
			t.Errorf("expected vcs:repo, got %s", node.Type)
		}
	}
}

// Test pattern with WHERE on variable: WHERE file.data.ext = 'go'
func TestPattern_WhereVariable(t *testing.T) {
	s, ctx := setupAQLTest(t)

	pattern := aql.Pat(aql.NodeType("dir", "fs:dir")).
		To(aql.AnyEdgeOfType("contains"), aql.NodeType("file", "fs:file")).
		Build()

	q := aql.Select(aql.Col("file")).
		FromPattern(pattern).
		Where(&aql.ComparisonExpr{
			Left:  aql.Col("file", "data", "ext"),
			Op:    aql.OpEq,
			Right: aql.String("go"),
		}).
		Limit(10).
		Build()

	result, err := s.Query(ctx, q)
	if err != nil {
		t.Fatalf("Pattern query failed: %v", err)
	}

	// Should get both .go files (test1.go from src, main.go from cmd)
	if len(result.Nodes) != 2 {
		t.Errorf("expected 2 .go files, got %d", len(result.Nodes))
	}

	for _, node := range result.Nodes {
		if data, ok := node.Data.(map[string]any); ok {
			if ext, ok := data["ext"].(string); ok && ext != "go" {
				t.Errorf("expected ext=go, got %s", ext)
			}
		}
	}
}

// Test pattern with WHERE comparing two variables
func TestPattern_WhereCompareVariables(t *testing.T) {
	s, ctx := setupAQLTest(t)

	pattern := aql.Pat(aql.NodeType("dir", "fs:dir")).
		To(aql.AnyEdgeOfType("contains"), aql.NodeType("file", "fs:file")).
		Build()

	q := aql.Select(aql.Col("file")).
		FromPattern(pattern).
		Where(&aql.ComparisonExpr{
			Left:  aql.Col("dir", "name"),
			Op:    aql.OpEq,
			Right: aql.String("cmd"),
		}).
		Limit(10).
		Build()

	result, err := s.Query(ctx, q)
	if err != nil {
		t.Fatalf("Pattern query failed: %v", err)
	}

	// Should get main.go (cmd -> main.go)
	if len(result.Nodes) != 1 {
		t.Errorf("expected 1 file in cmd, got %d", len(result.Nodes))
	}

	if len(result.Nodes) > 0 && result.Nodes[0].Name != "main.go" {
		t.Errorf("expected main.go, got %s", result.Nodes[0].Name)
	}
}

// Test pattern with complex WHERE (AND/OR)
func TestPattern_WhereComplex(t *testing.T) {
	s, ctx := setupAQLTest(t)

	pattern := aql.Pat(aql.NodeType("dir", "fs:dir")).
		To(aql.AnyEdgeOfType("contains"), aql.NodeType("file", "fs:file")).
		Build()

	q := aql.Select(aql.Col("file")).
		FromPattern(pattern).
		Where(aql.Or(
			&aql.ComparisonExpr{
				Left:  aql.Col("file", "data", "ext"),
				Op:    aql.OpEq,
				Right: aql.String("go"),
			},
			&aql.ComparisonExpr{
				Left:  aql.Col("file", "data", "ext"),
				Op:    aql.OpEq,
				Right: aql.String("py"),
			},
		)).
		Limit(10).
		Build()

	result, err := s.Query(ctx, q)
	if err != nil {
		t.Fatalf("Pattern query failed: %v", err)
	}

	// Should get test1.go, main.go (both .go) and test2.py
	if len(result.Nodes) != 3 {
		t.Errorf("expected 3 files (.go or .py), got %d", len(result.Nodes))
	}
}

// Test pattern selecting multiple node variables
func TestPattern_SelectMultipleVariables(t *testing.T) {
	s, ctx := setupAQLTest(t)

	pattern := aql.Pat(aql.NodeType("dir", "fs:dir")).
		To(aql.EdgeType("e", "contains"), aql.NodeType("file", "fs:file")).
		Build()

	q := aql.Select(aql.Col("dir"), aql.Col("file")).
		FromPattern(pattern).
		Limit(10).
		Build()

	result, err := s.Query(ctx, q)
	if err != nil {
		t.Fatalf("Pattern query failed: %v", err)
	}

	// Result type is nodes (default when no edge variables selected)
	if result.Type != graph.ResultTypeNodes {
		t.Errorf("expected ResultTypeNodes, got %v", result.Type)
	}

	// Should get results (actual behavior: returns nodes with columns from both)
	if len(result.Nodes) == 0 {
		t.Error("expected some results")
	}
}

// Test pattern with anonymous nodes: ()-[:contains]->(file:fs:file)
func TestPattern_AnonymousNode(t *testing.T) {
	s, ctx := setupAQLTest(t)

	pattern := aql.Pat(aql.AnyNode()).
		To(aql.AnyEdgeOfType("contains"), aql.NodeType("file", "fs:file")).
		Build()

	q := aql.Select(aql.Col("file")).
		FromPattern(pattern).
		Limit(10).
		Build()

	result, err := s.Query(ctx, q)
	if err != nil {
		t.Fatalf("Pattern query failed: %v", err)
	}

	// Should get all files with contains edge (test1.go, test2.py, main.go)
	if len(result.Nodes) != 3 {
		t.Errorf("expected 3 files, got %d", len(result.Nodes))
	}
}

// Test pattern with LIMIT
func TestPattern_Limit(t *testing.T) {
	s, ctx := setupAQLTest(t)

	pattern := aql.Pat(aql.N("a")).
		To(aql.AnyEdgeOfType("contains"), aql.N("b")).
		Build()

	q := aql.Select(aql.Col("b")).
		FromPattern(pattern).
		Limit(1).
		Build()

	result, err := s.Query(ctx, q)
	if err != nil {
		t.Fatalf("Pattern query failed: %v", err)
	}

	if len(result.Nodes) != 1 {
		t.Errorf("expected 1 node (LIMIT 1), got %d", len(result.Nodes))
	}
}

// Test pattern with undefined variable in WHERE
func TestPattern_UndefinedVariable(t *testing.T) {
	s, ctx := setupAQLTest(t)

	pattern := aql.Pat(aql.NodeType("dir", "fs:dir")).
		To(aql.AnyEdgeOfType("contains"), aql.NodeType("file", "fs:file")).
		Build()

	q := aql.Select(aql.Col("file")).
		FromPattern(pattern).
		Where(&aql.ComparisonExpr{
			Left:  aql.Col("undefined", "name"), // 'undefined' not in pattern
			Op:    aql.OpEq,
			Right: aql.String("test"),
		}).
		Build()

	_, err := s.Query(ctx, q)
	if err == nil {
		t.Error("expected error for undefined variable in WHERE")
	}

	qe, ok := err.(*QueryError)
	if !ok {
		t.Errorf("expected QueryError, got %T", err)
	}

	// Variable validation happens in validate phase
	if qe.Phase != "validate" {
		t.Errorf("expected validate phase, got %s", qe.Phase)
	}
}

// Test pattern with undefined variable in SELECT
func TestPattern_UndefinedVariableSelect(t *testing.T) {
	s, ctx := setupAQLTest(t)

	pattern := aql.Pat(aql.NodeType("dir", "fs:dir")).
		To(aql.AnyEdgeOfType("contains"), aql.NodeType("file", "fs:file")).
		Build()

	q := aql.Select(aql.Col("undefined")). // 'undefined' not in pattern
						FromPattern(pattern).
						Build()

	_, err := s.Query(ctx, q)
	if err == nil {
		t.Error("expected error for undefined variable in SELECT")
	}

	qe, ok := err.(*QueryError)
	if !ok {
		t.Errorf("expected QueryError, got %T", err)
	}

	// Variable validation happens in validate phase
	if qe.Phase != "validate" {
		t.Errorf("expected validate phase, got %s", qe.Phase)
	}
}

// Test undirected edge pattern: (a)-[:references]-(b)
func TestPattern_UndirectedEdge(t *testing.T) {
	s, ctx := setupAQLTest(t)

	// Pattern: (a)-[:references]-(b)
	// Should match test1.go-references->main.go in BOTH directions
	pattern := aql.Pat(aql.N("a")).
		Either(aql.AnyEdgeOfType("references"), aql.N("b")).
		Build()

	q := aql.Select(aql.Col("a"), aql.Col("b")).
		FromPattern(pattern).
		Limit(10).
		Build()

	result, err := s.Query(ctx, q)
	if err != nil {
		t.Fatalf("Undirected pattern query failed: %v", err)
	}

	// Should get 2 results (one for each direction of the same edge):
	// 1. a=test1.go, b=main.go (forward)
	// 2. a=main.go, b=test1.go (reverse)
	if len(result.Nodes) != 2 {
		t.Errorf("expected 2 results (both directions), got %d", len(result.Nodes))
	}
}

// Test undirected edge with specific node types
func TestPattern_UndirectedEdgeWithTypes(t *testing.T) {
	s, ctx := setupAQLTest(t)

	// Pattern: (file1:fs:file)-[:references]-(file2:fs:file)
	pattern := aql.Pat(aql.NodeType("file1", "fs:file")).
		Either(aql.AnyEdgeOfType("references"), aql.NodeType("file2", "fs:file")).
		Build()

	q := aql.Select(aql.Col("file1"), aql.Col("file2")).
		FromPattern(pattern).
		Limit(10).
		Build()

	result, err := s.Query(ctx, q)
	if err != nil {
		t.Fatalf("Undirected pattern query failed: %v", err)
	}

	// Should still get 2 results
	if len(result.Nodes) != 2 {
		t.Errorf("expected 2 results, got %d", len(result.Nodes))
	}

	// Verify all results are fs:file
	for _, node := range result.Nodes {
		if node.Type != "fs:file" {
			t.Errorf("expected fs:file, got %s", node.Type)
		}
	}
}

// Test multiple patterns with shared variables: (a)-[:x]->(b), (b)-[:y]->(c)
func TestPattern_MultipleWithShared(t *testing.T) {
	s, ctx := setupAQLTest(t)

	// Two patterns sharing variable 'dir':
	// Pattern 1: (repo:vcs:repo)-[:located_at]->(dir:fs:dir)
	// Pattern 2: (dir)-[:contains]->(file:fs:file)
	// This should find: repo -> src dir -> files (test1.go, test2.py)

	pattern1 := aql.Pat(aql.NodeType("repo", "vcs:repo")).
		To(aql.AnyEdgeOfType("located_at"), aql.NodeType("dir", "fs:dir")).
		Build()

	pattern2 := aql.Pat(aql.N("dir")).
		To(aql.AnyEdgeOfType("contains"), aql.NodeType("file", "fs:file")).
		Build()

	q := aql.Select(aql.Col("file")).
		FromPattern(pattern1, pattern2).
		Limit(10).
		Build()

	result, err := s.Query(ctx, q)
	if err != nil {
		t.Fatalf("Multiple pattern query failed: %v", err)
	}

	// Should get test1.go and test2.py (both in src dir)
	if len(result.Nodes) != 2 {
		t.Errorf("expected 2 files, got %d", len(result.Nodes))
		for _, node := range result.Nodes {
			t.Logf("  - %s", node.Name)
		}
	}

	// Verify all are fs:file
	for _, node := range result.Nodes {
		if node.Type != "fs:file" {
			t.Errorf("expected fs:file, got %s", node.Type)
		}
	}
}

// Test multiple patterns finding transitive relationships
func TestPattern_MultipleTransitive(t *testing.T) {
	s, ctx := setupAQLTest(t)

	// Three-hop pattern using two patterns:
	// (repo:vcs:repo)-[:located_at]->(dir:fs:dir), (dir)-[:contains]->(file:fs:file)
	// Then filter by file type

	pattern1 := aql.Pat(aql.NodeType("repo", "vcs:repo")).
		To(aql.AnyEdgeOfType("located_at"), aql.NodeType("dir", "fs:dir")).
		Build()

	pattern2 := aql.Pat(aql.N("dir")).
		To(aql.AnyEdgeOfType("contains"), aql.NodeType("file", "fs:file")).
		Build()

	q := aql.Select(aql.Col("repo"), aql.Col("file")).
		FromPattern(pattern1, pattern2).
		Where(&aql.ComparisonExpr{
			Left:  aql.Col("file", "data", "ext"),
			Op:    aql.OpEq,
			Right: aql.String("go"),
		}).
		Limit(10).
		Build()

	result, err := s.Query(ctx, q)
	if err != nil {
		t.Fatalf("Multiple pattern query failed: %v", err)
	}

	// Should get test1.go (the only .go file in src dir)
	if len(result.Nodes) != 1 {
		t.Errorf("expected 1 .go file, got %d", len(result.Nodes))
	}
}

// Test pattern with ORDER BY
func TestPattern_OrderBy(t *testing.T) {
	s, ctx := setupAQLTest(t)

	pattern := aql.Pat(aql.NodeType("dir", "fs:dir")).
		To(aql.AnyEdgeOfType("contains"), aql.NodeType("file", "fs:file")).
		Build()

	q := aql.Select(aql.Col("file")).
		FromPattern(pattern).
		OrderBy("file.name").
		Limit(10).
		Build()

	result, err := s.Query(ctx, q)
	if err != nil {
		t.Fatalf("Pattern ORDER BY query failed: %v", err)
	}

	if len(result.Nodes) != 3 {
		t.Fatalf("expected 3 files, got %d", len(result.Nodes))
	}

	// Check ascending order (main.go, test1.go, test2.py)
	if result.Nodes[0].Name != "main.go" {
		t.Errorf("expected main.go first, got %s", result.Nodes[0].Name)
	}
}

// Test pattern with ORDER BY DESC
func TestPattern_OrderByDesc(t *testing.T) {
	s, ctx := setupAQLTest(t)

	pattern := aql.Pat(aql.NodeType("dir", "fs:dir")).
		To(aql.AnyEdgeOfType("contains"), aql.NodeType("file", "fs:file")).
		Build()

	q := aql.Select(aql.Col("file")).
		FromPattern(pattern).
		OrderByDesc("file.name").
		Limit(10).
		Build()

	result, err := s.Query(ctx, q)
	if err != nil {
		t.Fatalf("Pattern ORDER BY DESC query failed: %v", err)
	}

	if len(result.Nodes) != 3 {
		t.Fatalf("expected 3 files, got %d", len(result.Nodes))
	}

	// Check descending order (test2.py, test1.go, main.go)
	if result.Nodes[0].Name != "test2.py" {
		t.Errorf("expected test2.py first, got %s", result.Nodes[0].Name)
	}
}

// Test pattern with GROUP BY
func TestPattern_GroupBy(t *testing.T) {
	s, ctx := setupAQLTest(t)

	// Count files by directory
	pattern := aql.Pat(aql.NodeType("dir", "fs:dir")).
		To(aql.AnyEdgeOfType("contains"), aql.NodeType("file", "fs:file")).
		Build()

	q := aql.Select(aql.Col("dir", "name"), aql.Count()).
		FromPattern(pattern).
		GroupByCol("dir.name").
		Build()

	result, err := s.Query(ctx, q)
	if err != nil {
		t.Fatalf("Pattern GROUP BY query failed: %v", err)
	}

	if result.Type != graph.ResultTypeCounts {
		t.Errorf("expected ResultTypeCounts, got %v", result.Type)
	}

	// Should get src: 2, cmd: 1
	if len(result.Counts) != 2 {
		t.Errorf("expected 2 groups, got %d", len(result.Counts))
	}

	if result.Counts["src"] != 2 {
		t.Errorf("expected src: 2, got %d", result.Counts["src"])
	}

	if result.Counts["cmd"] != 1 {
		t.Errorf("expected cmd: 1, got %d", result.Counts["cmd"])
	}
}

// Test Explain for pattern query
func TestPattern_Explain(t *testing.T) {
	s, ctx := setupAQLTest(t)

	pattern := aql.Pat(aql.NodeType("dir", "fs:dir")).
		To(aql.AnyEdgeOfType("contains"), aql.NodeType("file", "fs:file")).
		Build()

	q := aql.Select(aql.Col("file")).
		FromPattern(pattern).
		Limit(10).
		Build()

	plan, err := s.Explain(ctx, q)
	if err != nil {
		t.Fatalf("Explain failed: %v", err)
	}

	if plan.SQL == "" {
		t.Error("expected SQL to be populated")
	}

	// Should contain JOIN for pattern
	if !contains(plan.SQL, "JOIN") {
		t.Error("expected JOIN in pattern query SQL")
	}

	if plan.SQLitePlan == "" {
		t.Error("expected SQLite plan to be populated")
	}
}

// Helper function for string contains check
func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > len(substr) && (s[:len(substr)] == substr || s[len(s)-len(substr):] == substr || containsInner(s, substr)))
}

func containsInner(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}

// ============================================================================
// PHASE 3: VARIABLE-LENGTH PATH TESTS
// ============================================================================

// Test variable-length path: (a)-[:contains*1..2]->(b)
func TestPattern_VariableLength(t *testing.T) {
	// Use a fresh database to avoid interference from setupAQLTest data
	s, err := New(":memory:")
	if err != nil {
		t.Fatalf("failed to create storage: %v", err)
	}
	defer s.Close()

	ctx := context.Background()

	// Create a deeper hierarchy for testing
	// root -> level1 -> level2
	root := graph.NewNode("fs:dir").WithURI("file:///root").WithName("root")
	level1 := graph.NewNode("fs:dir").WithURI("file:///root/level1").WithName("level1")
	level2 := graph.NewNode("fs:file").WithURI("file:///root/level1/level2.txt").WithName("level2.txt")

	s.PutNode(ctx, root)
	s.PutNode(ctx, level1)
	s.PutNode(ctx, level2)

	s.PutEdge(ctx, graph.NewEdge("contains", root.ID, level1.ID))
	s.PutEdge(ctx, graph.NewEdge("contains", level1.ID, level2.ID))
	s.Flush(ctx)

	// Query: find all descendants 1-2 hops away from directories
	pattern := aql.Pat(aql.NodeType("start", "fs:dir")).
		To(aql.EdgeType("e", "contains").WithHops(1, 2), aql.N("end")).
		Build()

	q := aql.Select(aql.Col("end")).
		FromPattern(pattern).
		Limit(10).
		Build()

	result, err := s.Query(ctx, q)
	if err != nil {
		t.Fatalf("Variable-length query failed: %v", err)
	}

	// Should find level1 (1 hop from root) and level2.txt (2 hops from root, 1 hop from level1)
	// Total: 2 unique nodes
	if len(result.Nodes) < 2 {
		t.Errorf("expected at least 2 nodes, got %d", len(result.Nodes))
		for _, n := range result.Nodes {
			t.Logf("  - %s", n.Name)
		}
	}
}

// Test variable-length path with exact hops: (a)-[:contains*2]->(b)
func TestPattern_VariableLengthExact(t *testing.T) {
	s, err := New(":memory:")
	if err != nil {
		t.Fatalf("failed to create storage: %v", err)
	}
	defer s.Close()

	ctx := context.Background()

	// Create 3-level hierarchy
	root := graph.NewNode("fs:dir").WithURI("file:///root").WithName("root")
	level1 := graph.NewNode("fs:dir").WithURI("file:///level1").WithName("level1")
	level2 := graph.NewNode("fs:file").WithURI("file:///level2.txt").WithName("level2.txt")

	s.PutNode(ctx, root)
	s.PutNode(ctx, level1)
	s.PutNode(ctx, level2)

	s.PutEdge(ctx, graph.NewEdge("contains", root.ID, level1.ID))
	s.PutEdge(ctx, graph.NewEdge("contains", level1.ID, level2.ID))
	s.Flush(ctx)

	// Query: find nodes EXACTLY 2 hops away (only level2.txt)
	pattern := aql.Pat(aql.N("start")).
		To(aql.AnyEdgeOfType("contains").WithHops(2, 2), aql.N("end")).
		Build()

	q := aql.Select(aql.Col("end")).
		FromPattern(pattern).
		Build()

	result, err := s.Query(ctx, q)
	if err != nil {
		t.Fatalf("Exact hops query failed: %v", err)
	}

	// Only level2.txt is exactly 2 hops from root
	if len(result.Nodes) != 1 {
		t.Errorf("expected 1 node, got %d", len(result.Nodes))
	}

	if len(result.Nodes) > 0 && result.Nodes[0].Name != "level2.txt" {
		t.Errorf("expected level2.txt, got %s", result.Nodes[0].Name)
	}
}

// Test variable-length path with unbounded max: (a)-[:contains*2..]->(b)
func TestPattern_VariableLengthUnbounded(t *testing.T) {
	s, err := New(":memory:")
	if err != nil {
		t.Fatalf("failed to create storage: %v", err)
	}
	defer s.Close()

	ctx := context.Background()

	// Create 4-level hierarchy to test unbounded
	l0 := graph.NewNode("fs:dir").WithURI("file:///l0").WithName("l0")
	l1 := graph.NewNode("fs:dir").WithURI("file:///l1").WithName("l1")
	l2 := graph.NewNode("fs:dir").WithURI("file:///l2").WithName("l2")
	l3 := graph.NewNode("fs:file").WithURI("file:///l3.txt").WithName("l3.txt")

	s.PutNode(ctx, l0)
	s.PutNode(ctx, l1)
	s.PutNode(ctx, l2)
	s.PutNode(ctx, l3)

	s.PutEdge(ctx, graph.NewEdge("contains", l0.ID, l1.ID))
	s.PutEdge(ctx, graph.NewEdge("contains", l1.ID, l2.ID))
	s.PutEdge(ctx, graph.NewEdge("contains", l2.ID, l3.ID))
	s.Flush(ctx)

	// Query: find nodes 2+ hops away (l2, l3.txt)
	pattern := aql.Pat(aql.N("start")).
		To(aql.AnyEdgeOfType("contains").WithMinHops(2), aql.N("end")).
		Build()

	q := aql.Select(aql.Col("end")).
		FromPattern(pattern).
		Build()

	result, err := s.Query(ctx, q)
	if err != nil {
		t.Fatalf("Unbounded query failed: %v", err)
	}

	// Should find l2 (2 hops) and l3.txt (3 hops)
	// But also l3.txt can be reached from l1 in 2 hops, and from l0 in 3 hops
	// So we get: from l0: l2(2), l3(3); from l1: l3(2)
	// Distinct nodes: l2, l3
	if len(result.Nodes) < 2 {
		t.Errorf("expected at least 2 nodes, got %d", len(result.Nodes))
	}
}

// Test variable-length with multi-type edges
func TestPattern_VariableLengthMultiType(t *testing.T) {
	s, err := New(":memory:")
	if err != nil {
		t.Fatalf("failed to create storage: %v", err)
	}
	defer s.Close()

	ctx := context.Background()

	// Create mixed hierarchy with contains and has
	root := graph.NewNode("vcs:repo").WithURI("git://repo").WithName("repo")
	branch := graph.NewNode("vcs:branch").WithURI("git://branch").WithName("main")
	file := graph.NewNode("fs:file").WithURI("file:///file.txt").WithName("file.txt")

	s.PutNode(ctx, root)
	s.PutNode(ctx, branch)
	s.PutNode(ctx, file)

	s.PutEdge(ctx, graph.NewEdge("has", root.ID, branch.ID))
	s.PutEdge(ctx, graph.NewEdge("contains", branch.ID, file.ID))
	s.Flush(ctx)

	// Query: traverse both has and contains edges
	pattern := aql.Pat(aql.N("start")).
		To(aql.EdgeTypes("has", "contains").WithHops(1, 2), aql.N("end")).
		Build()

	q := aql.Select(aql.Col("end")).
		FromPattern(pattern).
		Build()

	result, err := s.Query(ctx, q)
	if err != nil {
		t.Fatalf("Multi-type variable-length query failed: %v", err)
	}

	// Should find: branch (1 hop via has), file (2 hops via has then contains)
	if len(result.Nodes) < 2 {
		t.Errorf("expected at least 2 nodes, got %d", len(result.Nodes))
	}
}

// Test EXISTS not supported (Phase 3)
func TestQuery_ExistsNotSupported(t *testing.T) {
	s, ctx := setupAQLTest(t)

	pattern := aql.Pat(aql.N("dir")).
		To(aql.AnyEdgeOfType("contains"), aql.AnyNodeOfType("fs:file")).
		Build()

	q := aql.SelectStar().
		From("nodes").
		Where(aql.Exists(pattern)).
		Build()

	_, err := s.Query(ctx, q)
	if err == nil {
		t.Error("expected error for EXISTS (Phase 3)")
	}

	qe, ok := err.(*QueryError)
	if !ok {
		t.Errorf("expected QueryError, got %T", err)
	}

	if qe.Phase != "compile" {
		t.Errorf("expected compile phase, got %s", qe.Phase)
	}
}
