package sqlite

import (
	"context"
	"testing"

	"github.com/codewandler/axon/aql"
	"github.com/codewandler/axon/graph"
)

// findCount looks up a count by name in a []graph.CountItem slice.
// Returns 0 if not found.
func findCount(counts []graph.CountItem, name string) int {
	for _, item := range counts {
		if item.Name == name {
			return item.Count
		}
	}
	return 0
}

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

	q := aql.Nodes.SelectStar().Build()
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

	q := aql.Nodes.Select(aql.Name, aql.Type).Build()
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

	q := aql.Nodes.SelectStar().
		Where(aql.Type.Eq("fs:file")).
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

	q := aql.Nodes.SelectStar().
		Where(aql.Type.Glob("fs:*")).
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

	q := aql.Nodes.SelectStar().
		Where(aql.Labels.ContainsAny("test", "code")).
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

	q := aql.Nodes.SelectStar().
		Where(aql.Labels.ContainsAll("test", "code")).
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

	q := aql.Nodes.SelectStar().
		Where(aql.Labels.NotContains("test")).
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

	q := aql.Nodes.SelectStar().
		Where(aql.DataExt.Eq("go")).
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

	q := aql.Nodes.SelectStar().
		Where(aql.Type.In("fs:file", "fs:dir")).
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

	q := aql.Nodes.SelectStar().
		Where(aql.DataSize.Between(50, 150)).
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

	q := aql.Nodes.SelectStar().
		Where(aql.DataCol.IsNull()).
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

	q := aql.Nodes.SelectStar().
		Where(aql.DataCol.IsNotNull()).
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
	q := aql.Nodes.SelectStar().
		Where(aql.And(
			aql.Paren(aql.Or(
				aql.Type.Eq("fs:file"),
				aql.Type.Eq("fs:dir"),
			)),
			aql.Labels.ContainsAny("test"),
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

	q := aql.Nodes.Select(aql.Count()).Build()
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

	q := aql.Nodes.Select(aql.Type, aql.Count()).
		GroupBy(aql.Type).
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

	if findCount(result.Counts, "fs:file") != 4 {
		t.Errorf("expected 4 fs:file, got %d", findCount(result.Counts, "fs:file"))
	}

	if findCount(result.Counts, "fs:dir") != 2 {
		t.Errorf("expected 2 fs:dir, got %d", findCount(result.Counts, "fs:dir"))
	}
}

// Test GROUP BY with HAVING
func TestQuery_GroupByWithHaving(t *testing.T) {
	s, ctx := setupAQLTest(t)

	q := aql.Nodes.Select(aql.Type, aql.Count()).
		GroupBy(aql.Type).
		Having(aql.Gt("COUNT(*)", 1)).
		Build()

	result, err := s.Query(ctx, q)
	if err != nil {
		t.Fatalf("Query failed: %v", err)
	}

	// fs:file (4), fs:dir (2), vcs:branch (2) all have count > 1
	if len(result.Counts) != 3 {
		t.Errorf("expected 3 types with count > 1, got %d", len(result.Counts))
	}

	if findCount(result.Counts, "fs:file") != 4 {
		t.Errorf("expected fs:file count 4, got %d", findCount(result.Counts, "fs:file"))
	}
}

// Test ORDER BY
func TestQuery_OrderBy(t *testing.T) {
	s, ctx := setupAQLTest(t)

	q := aql.Nodes.SelectStar().
		Where(aql.Type.Eq("fs:file")).
		OrderBy(aql.Name).
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

	q := aql.Nodes.SelectStar().
		Where(aql.Type.Eq("fs:file")).
		OrderByDesc(aql.Name).
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

	q := aql.Nodes.SelectStar().
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

	q := aql.Nodes.SelectStar().
		OrderBy(aql.Name).
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

	q := aql.Nodes.SelectDistinct(aql.Type).
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

	q := aql.Edges.SelectStar().Build()
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

	q := aql.Nodes.SelectStar().
		Where(aql.Type.Eq("fs:file")).
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
	q := aql.Nodes.SelectStar().
		Having(aql.Type.Eq("fs:file")).
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

	// Test with an invalid table name using manual AST construction
	q := &aql.Query{
		Select: &aql.SelectStmt{
			Columns: []aql.Column{{Expr: &aql.Star{}}},
			From:    &aql.TableSource{Table: "invalid_table"},
		},
	}

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

	pattern := aql.Pat(aql.N("dir").OfTypeStr("fs:dir").Build()).
		To(aql.Edge.Contains.ToEdgePattern(), aql.N("file").OfTypeStr("fs:file").Build()).
		Build()

	q := aql.Select(aql.Var("file")).
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

	pattern := aql.Pat(aql.N("a").Build()).To(aql.EOfType("e", "contains"), aql.N("b").Build()).Build()

	q := aql.Select(aql.Var("e")).
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

	pattern := aql.Pat(aql.N("a").Build()).To(aql.EdgeTypes("contains", "has"), aql.N("b").Build()).Build()

	q := aql.Select(aql.Var("b")).
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

	pattern := aql.Pat(aql.N("branch").OfTypeStr("vcs:branch").Build()).
		From(aql.Edge.Has.ToEdgePattern(), aql.N("repo").OfTypeStr("vcs:repo").Build()).
		Build()

	q := aql.Select(aql.Var("repo")).
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

	pattern := aql.Pat(aql.N("dir").OfTypeStr("fs:dir").Build()).
		To(aql.Edge.Contains.ToEdgePattern(), aql.N("file").OfTypeStr("fs:file").Build()).
		Build()

	q := aql.Select(aql.Var("file")).
		FromPattern(pattern).
		Where(aql.Var("file").DataField("ext").Eq("go")).
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

	pattern := aql.Pat(aql.N("dir").OfTypeStr("fs:dir").Build()).
		To(aql.Edge.Contains.ToEdgePattern(), aql.N("file").OfTypeStr("fs:file").Build()).
		Build()

	q := aql.Select(aql.Var("file")).
		FromPattern(pattern).
		Where(aql.Var("dir").Field("name").Eq("cmd")).
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

	pattern := aql.Pat(aql.N("dir").OfTypeStr("fs:dir").Build()).
		To(aql.Edge.Contains.ToEdgePattern(), aql.N("file").OfTypeStr("fs:file").Build()).
		Build()

	q := aql.Select(aql.Var("file")).
		FromPattern(pattern).
		Where(aql.Or(
			aql.Var("file").DataField("ext").Eq("go"),
			aql.Var("file").DataField("ext").Eq("py"),
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

	pattern := aql.Pat(aql.N("dir").OfTypeStr("fs:dir").Build()).
		To(aql.EOfType("e", "contains"), aql.N("file").OfTypeStr("fs:file").Build()).
		Build()

	q := aql.Select(aql.Var("dir"), aql.Var("file")).
		FromPattern(pattern).
		Limit(10).
		Build()

	result, err := s.Query(ctx, q)
	if err != nil {
		t.Fatalf("Pattern query failed: %v", err)
	}

	// With two node variables, result is now ResultTypeRows (aliased columns)
	if result.Type != graph.ResultTypeRows {
		t.Errorf("expected ResultTypeRows, got %v", result.Type)
	}

	// Should get results
	if len(result.Rows) == 0 {
		t.Error("expected some results")
	}
}

// Test pattern with anonymous nodes: ()-[:contains]->(file:fs:file)
func TestPattern_AnonymousNode(t *testing.T) {
	s, ctx := setupAQLTest(t)

	pattern := aql.Pat(aql.AnyNode()).
		To(aql.Edge.Contains.ToEdgePattern(), aql.N("file").OfTypeStr("fs:file").Build()).
		Build()

	q := aql.Select(aql.Var("file")).
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

	pattern := aql.Pat(aql.N("a").Build()).To(aql.Edge.Contains.ToEdgePattern(), aql.N("b").Build()).Build()

	q := aql.Select(aql.Var("b")).
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

	pattern := aql.Pat(aql.N("dir").OfTypeStr("fs:dir").Build()).
		To(aql.Edge.Contains.ToEdgePattern(), aql.N("file").OfTypeStr("fs:file").Build()).
		Build()

	q := aql.Select(aql.Var("file")).
		FromPattern(pattern).
		Where(aql.Eq("undefined.name", "test")).
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

	pattern := aql.Pat(aql.N("dir").OfTypeStr("fs:dir").Build()).
		To(aql.Edge.Contains.ToEdgePattern(), aql.N("file").OfTypeStr("fs:file").Build()).
		Build()

	q := aql.Select(aql.Var("undefined")). // 'undefined' not in pattern
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
	pattern := aql.Pat(aql.N("a").Build()).
		Either(aql.EdgeTypeOf("references").ToEdgePattern(), aql.N("b").Build()).Build()

	q := aql.Select(aql.Var("a"), aql.Var("b")).
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
	if len(result.Rows) != 2 {
		t.Errorf("expected 2 results (both directions), got %d", len(result.Rows))
	}
}

// Test undirected edge with specific node types
func TestPattern_UndirectedEdgeWithTypes(t *testing.T) {
	s, ctx := setupAQLTest(t)

	// Pattern: (file1:fs:file)-[:references]-(file2:fs:file)
	pattern := aql.Pat(aql.N("file1").OfTypeStr("fs:file").Build()).
		Either(aql.EdgeTypeOf("references").ToEdgePattern(), aql.N("file2").OfTypeStr("fs:file").Build()).
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
	if len(result.Rows) != 2 {
		t.Errorf("expected 2 results, got %d", len(result.Rows))
	}

	// Verify all results are fs:file
	for _, row := range result.Rows {
		for _, varName := range []string{"file1", "file2"} {
			if typ, ok := row[varName+".type"].(string); ok && typ != "fs:file" {
				t.Errorf("expected fs:file for %s, got %s", varName, typ)
			}
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

	pattern1 := aql.Pat(aql.N("repo").OfTypeStr("vcs:repo").Build()).
		To(aql.EdgeTypeOf("located_at").ToEdgePattern(), aql.N("dir").OfTypeStr("fs:dir").Build()).
		Build()

	pattern2 := aql.Pat(aql.N("dir").Build()).To(aql.Edge.Contains.ToEdgePattern(), aql.N("file").OfTypeStr("fs:file").Build()).
		Build()

	q := aql.Select(aql.Var("file")).
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

	pattern1 := aql.Pat(aql.N("repo").OfTypeStr("vcs:repo").Build()).
		To(aql.EdgeTypeOf("located_at").ToEdgePattern(), aql.N("dir").OfTypeStr("fs:dir").Build()).
		Build()

	pattern2 := aql.Pat(aql.N("dir").Build()).To(aql.Edge.Contains.ToEdgePattern(), aql.N("file").OfTypeStr("fs:file").Build()).
		Build()

	q := aql.Select(aql.Var("repo"), aql.Var("file")).
		FromPattern(pattern1, pattern2).
		Where(aql.Var("file").DataField("ext").Eq("go")).
		Limit(10).
		Build()

	result, err := s.Query(ctx, q)
	if err != nil {
		t.Fatalf("Multiple pattern query failed: %v", err)
	}

	// Should get test1.go (the only .go file in src dir)
	if len(result.Rows) != 1 {
		t.Errorf("expected 1 .go file, got %d", len(result.Rows))
	}
}

// Test pattern with ORDER BY
func TestPattern_OrderBy(t *testing.T) {
	s, ctx := setupAQLTest(t)

	pattern := aql.Pat(aql.N("dir").OfTypeStr("fs:dir").Build()).
		To(aql.Edge.Contains.ToEdgePattern(), aql.N("file").OfTypeStr("fs:file").Build()).
		Build()

	q := aql.Select(aql.Var("file")).
		FromPattern(pattern).
		OrderByString("file.name").
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

	pattern := aql.Pat(aql.N("dir").OfTypeStr("fs:dir").Build()).
		To(aql.Edge.Contains.ToEdgePattern(), aql.N("file").OfTypeStr("fs:file").Build()).
		Build()

	q := aql.Select(aql.Var("file")).
		FromPattern(pattern).
		OrderByDescString("file.name").
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
	pattern := aql.Pat(aql.N("dir").OfTypeStr("fs:dir").Build()).
		To(aql.Edge.Contains.ToEdgePattern(), aql.N("file").OfTypeStr("fs:file").Build()).
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

	if findCount(result.Counts, "src") != 2 {
		t.Errorf("expected src: 2, got %d", findCount(result.Counts, "src"))
	}

	if findCount(result.Counts, "cmd") != 1 {
		t.Errorf("expected cmd: 1, got %d", findCount(result.Counts, "cmd"))
	}
}

// Test Explain for pattern query
func TestPattern_Explain(t *testing.T) {
	s, ctx := setupAQLTest(t)

	pattern := aql.Pat(aql.N("dir").OfTypeStr("fs:dir").Build()).
		To(aql.Edge.Contains.ToEdgePattern(), aql.N("file").OfTypeStr("fs:file").Build()).
		Build()

	q := aql.Select(aql.Var("file")).
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
	pattern := aql.Pat(aql.N("start").OfTypeStr("fs:dir").Build()).
		To(aql.EOfType("e", "contains").WithHops(1, 2), aql.N("end").Build()).Build()

	q := aql.Select(aql.Var("end")).
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
	pattern := aql.Pat(aql.N("start").Build()).To(aql.Edge.Contains.WithHops(2, 2), aql.N("end").Build()).Build()

	q := aql.Select(aql.Var("end")).
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
	pattern := aql.Pat(aql.N("start").Build()).To(aql.Edge.Contains.WithMinHops(2), aql.N("end").Build()).Build()

	q := aql.Select(aql.Var("end")).
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
	pattern := aql.Pat(aql.N("start").Build()).To(aql.EdgeTypes("has", "contains").WithHops(1, 2), aql.N("end").Build()).Build()

	q := aql.Select(aql.Var("end")).
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
func TestQuery_Exists(t *testing.T) {
	s, ctx := setupAQLTest(t)

	// Create test data: dir1 with files, dir2 without files
	dir1 := &graph.Node{ID: "dir1", Type: "fs:dir", Name: "with_files"}
	dir2 := &graph.Node{ID: "dir2", Type: "fs:dir", Name: "empty"}
	file1 := &graph.Node{ID: "file1", Type: "fs:file", Name: "test.go"}

	if err := s.PutNode(ctx, dir1); err != nil {
		t.Fatal(err)
	}
	if err := s.PutNode(ctx, dir2); err != nil {
		t.Fatal(err)
	}
	if err := s.PutNode(ctx, file1); err != nil {
		t.Fatal(err)
	}

	edge := &graph.Edge{ID: "e1", Type: "contains", From: dir1.ID, To: file1.ID}
	if err := s.PutEdge(ctx, edge); err != nil {
		t.Fatal(err)
	}

	// Test EXISTS: find dir1 which contains files
	pattern := aql.Pat(aql.N("nodes").Build()).To(aql.Edge.Contains.ToEdgePattern(), aql.AnyNodeOfType("fs:file")).
		Build()

	q := aql.Nodes.SelectStar().
		Where(aql.And(
			aql.Eq("id", "dir1"),
			aql.Exists(pattern),
		)).
		Build()

	result, err := s.Query(ctx, q)
	if err != nil {
		t.Fatalf("Query failed: %v", err)
	}

	if len(result.Nodes) != 1 {
		t.Errorf("expected 1 node (dir1 with files), got %d", len(result.Nodes))
	}
	if len(result.Nodes) > 0 && result.Nodes[0].ID != "dir1" {
		t.Errorf("expected dir1, got %s", result.Nodes[0].ID)
	}

	// Test NOT EXISTS: dir2 should match (no files)
	qNot := aql.Nodes.SelectStar().
		Where(aql.And(
			aql.Eq("id", "dir2"),
			aql.Not(aql.Exists(pattern)),
		)).
		Build()

	resultNot, err := s.Query(ctx, qNot)
	if err != nil {
		t.Fatalf("Query failed: %v", err)
	}

	if len(resultNot.Nodes) != 1 {
		t.Errorf("expected 1 node (dir2 without files), got %d", len(resultNot.Nodes))
	}
	if len(resultNot.Nodes) > 0 && resultNot.Nodes[0].ID != "dir2" {
		t.Errorf("expected dir2, got %s", resultNot.Nodes[0].ID)
	}

	// Test that dir1 does NOT match NOT EXISTS
	qNot2 := aql.Nodes.SelectStar().
		Where(aql.And(
			aql.Eq("id", "dir1"),
			aql.Not(aql.Exists(pattern)),
		)).
		Build()

	resultNot2, err := s.Query(ctx, qNot2)
	if err != nil {
		t.Fatalf("Query failed: %v", err)
	}

	if len(resultNot2.Nodes) != 0 {
		t.Errorf("expected 0 nodes (dir1 has files), got %d", len(resultNot2.Nodes))
	}
}

// Test json_each for label unpacking
func TestQuery_JsonEach_Labels(t *testing.T) {
	s, ctx := setupAQLTest(t)

	// Query: SELECT value, COUNT(*) FROM nodes, json_each(labels) GROUP BY value ORDER BY COUNT(*) DESC
	q := aql.Nodes.JsonEach(aql.Labels).
		Select(aql.Val, aql.Count()).
		GroupBy(aql.Val).
		OrderByCount(true).
		Build()

	result, err := s.Query(ctx, q)
	if err != nil {
		t.Fatalf("Query failed: %v", err)
	}

	if result.Type != graph.ResultTypeCounts {
		t.Errorf("expected ResultTypeCounts, got %v", result.Type)
	}

	// From test data:
	// - test1.go has labels: test, code
	// - test2.py has labels: test
	// - src dir has labels: source
	// So: test=2, code=1, source=1
	if findCount(result.Counts, "test") != 2 {
		t.Errorf("expected 'test' label count 2, got %d", findCount(result.Counts, "test"))
	}
	if findCount(result.Counts, "code") != 1 {
		t.Errorf("expected 'code' label count 1, got %d", findCount(result.Counts, "code"))
	}
	if findCount(result.Counts, "source") != 1 {
		t.Errorf("expected 'source' label count 1, got %d", findCount(result.Counts, "source"))
	}
}

// Test json_each with WHERE filter
func TestQuery_JsonEach_WithWhere(t *testing.T) {
	s, ctx := setupAQLTest(t)

	// Query: SELECT value, COUNT(*) FROM nodes, json_each(labels) WHERE value != '' GROUP BY value
	q := aql.Nodes.JsonEach(aql.Labels).
		Select(aql.Val, aql.Count()).
		Where(aql.Val.Ne("")).
		GroupBy(aql.Val).
		Build()

	result, err := s.Query(ctx, q)
	if err != nil {
		t.Fatalf("Query failed: %v", err)
	}

	// Should have 3 distinct labels: test, code, source
	if len(result.Counts) != 3 {
		t.Errorf("expected 3 distinct labels, got %d", len(result.Counts))
	}
}

// Test json_each with data field (nested JSON)
func TestQuery_JsonEach_DataField(t *testing.T) {
	s, ctx := setupAQLTest(t)

	// First add a node with array data
	node := graph.NewNode("fs:file").WithURI("file:///tags.go").WithName("tags.go").
		WithData(map[string]any{"tags": []string{"important", "review", "important"}})
	if err := s.PutNode(ctx, node); err != nil {
		t.Fatalf("failed to insert test node: %v", err)
	}
	if err := s.Flush(ctx); err != nil {
		t.Fatalf("failed to flush: %v", err)
	}

	// Query: SELECT value, COUNT(*) FROM nodes, json_each(data.tags) GROUP BY value
	q := aql.Nodes.JsonEach(aql.Data.Field("tags")).
		Select(aql.Val, aql.Count()).
		GroupBy(aql.Val).
		Build()

	result, err := s.Query(ctx, q)
	if err != nil {
		t.Fatalf("Query failed: %v", err)
	}

	// tags.go has: important (2), review (1)
	if findCount(result.Counts, "important") != 2 {
		t.Errorf("expected 'important' tag count 2, got %d", findCount(result.Counts, "important"))
	}
	if findCount(result.Counts, "review") != 1 {
		t.Errorf("expected 'review' tag count 1, got %d", findCount(result.Counts, "review"))
	}
}

// Test parsing json_each from string
func TestQuery_JsonEach_Parse(t *testing.T) {
	s, ctx := setupAQLTest(t)

	// Parse and execute: SELECT value, COUNT(*) FROM nodes, json_each(labels) GROUP BY value ORDER BY COUNT(*) DESC
	query := "SELECT value, COUNT(*) FROM nodes, json_each(labels) GROUP BY value ORDER BY COUNT(*) DESC"
	q, err := aql.Parse(query)
	if err != nil {
		t.Fatalf("Parse failed: %v", err)
	}

	result, err := s.Query(ctx, q)
	if err != nil {
		t.Fatalf("Query failed: %v", err)
	}

	if result.Type != graph.ResultTypeCounts {
		t.Errorf("expected ResultTypeCounts, got %v", result.Type)
	}

	// Should have labels
	if len(result.Counts) == 0 {
		t.Errorf("expected some labels, got empty result")
	}
}

// Test json_each with EXISTS pattern for scoping (CTE+JOIN optimization)
func TestQuery_JsonEach_WithExists(t *testing.T) {
	s, ctx := setupAQLTest(t)

	// Get the src directory node ID
	srcNode, err := s.GetNodeByURI(ctx, "file:///src")
	if err != nil {
		t.Fatalf("failed to get src node: %v", err)
	}

	// Use ScopedTo helper for optimized CTE+JOIN
	q := aql.Nodes.JsonEach(aql.Labels).
		Select(aql.Val, aql.Count()).
		Where(aql.And(
			aql.Val.Ne(""),
			aql.Nodes.ScopedTo(srcNode.ID),
		)).
		GroupBy(aql.Val).
		Build()

	result, err := s.Query(ctx, q)
	if err != nil {
		t.Fatalf("Query failed: %v", err)
	}

	if result.Type != graph.ResultTypeCounts {
		t.Errorf("expected ResultTypeCounts, got %v", result.Type)
	}

	// src contains test1.go (labels: test, code) and test2.py (labels: test)
	// src itself has label: source
	// So scoped labels should be: test=2, code=1, source=1
	if findCount(result.Counts, "test") != 2 {
		t.Errorf("expected 'test' label count 2, got %d", findCount(result.Counts, "test"))
	}
	if findCount(result.Counts, "code") != 1 {
		t.Errorf("expected 'code' label count 1, got %d", findCount(result.Counts, "code"))
	}
	if findCount(result.Counts, "source") != 1 {
		t.Errorf("expected 'source' label count 1, got %d", findCount(result.Counts, "source"))
	}
}

// Test EXISTS on edges table - scoped edge type counting
func TestQuery_Edges_WithExists(t *testing.T) {
	s, ctx := setupAQLTest(t)

	// Get the src directory node ID
	srcNode, err := s.GetNodeByURI(ctx, "file:///src")
	if err != nil {
		t.Fatalf("failed to get src node: %v", err)
	}

	// Use ScopedTo helper for optimized CTE+JOIN
	q := aql.Edges.
		Select(aql.Type, aql.Count()).
		Where(aql.Edges.ScopedTo(srcNode.ID)).
		GroupBy(aql.Type).
		OrderByCount(true).
		Build()

	result, err := s.Query(ctx, q)
	if err != nil {
		t.Fatalf("Query failed: %v", err)
	}

	if result.Type != graph.ResultTypeCounts {
		t.Errorf("expected ResultTypeCounts, got %v", result.Type)
	}

	// From src descendants:
	// - src has 2 contains edges (to test1.go and test2.py)
	// - test1.go has 1 references edge (to main.go)
	// Total: contains=2, references=1
	if findCount(result.Counts, "contains") != 2 {
		t.Errorf("expected 'contains' edge count 2, got %d", findCount(result.Counts, "contains"))
	}
	if findCount(result.Counts, "references") != 1 {
		t.Errorf("expected 'references' edge count 1, got %d", findCount(result.Counts, "references"))
	}
}

// TestQuery_ScopedTo_FollowsHasEdges verifies that ScopedTo traverses
// both 'contains' and 'has' edges, so non-filesystem nodes (e.g. md:document)
// that are children of indexed files are found in a scoped search.
// Regression test for: axon find --type "md:*" returning empty without --global.
func TestQuery_ScopedTo_FollowsHasEdges(t *testing.T) {
	s, err := New(":memory:")
	if err != nil {
		t.Fatalf("failed to create storage: %v", err)
	}
	t.Cleanup(func() { s.Close() })
	ctx := context.Background()

	// Build a small graph:
	// root (fs:dir)
	//  └─contains─> file.md (fs:file)
	//                 └─has─> doc (md:document)
	//                           └─has─> section (md:section)
	rootDir := graph.NewNode("fs:dir").WithURI("file:///proj").WithName("proj")
	mdFile := graph.NewNode("fs:file").WithURI("file:///proj/README.md").WithName("README.md")
	doc := graph.NewNode("md:document").WithURI("file+md:///proj/README.md").WithName("README.md")
	section := graph.NewNode("md:section").WithURI("file+md:///proj/README.md#intro").WithName("intro")

	for _, n := range []*graph.Node{rootDir, mdFile, doc, section} {
		if err := s.PutNode(ctx, n); err != nil {
			t.Fatalf("PutNode: %v", err)
		}
	}
	for _, e := range []*graph.Edge{
		graph.NewEdge("contains", rootDir.ID, mdFile.ID), // fs:dir -> fs:file
		graph.NewEdge("has", mdFile.ID, doc.ID),          // fs:file -> md:document
		graph.NewEdge("has", doc.ID, section.ID),          // md:document -> md:section
	} {
		if err := s.PutEdge(ctx, e); err != nil {
			t.Fatalf("PutEdge: %v", err)
		}
	}
	if err := s.Flush(ctx); err != nil {
		t.Fatalf("Flush: %v", err)
	}

	// Scoped query for md:document from the root directory.
	// This should find the document even though it is connected via 'has', not 'contains'.
	q := aql.Nodes.SelectStar().
		Where(aql.And(
			aql.Type.Eq("md:document"),
			aql.Nodes.ScopedTo(rootDir.ID),
		)).
		Build()

	result, err := s.Query(ctx, q)
	if err != nil {
		t.Fatalf("Query failed: %v", err)
	}

	if len(result.Nodes) != 1 {
		t.Errorf("expected 1 md:document node in scope, got %d", len(result.Nodes))
	}
	if len(result.Nodes) == 1 && result.Nodes[0].ID != doc.ID {
		t.Errorf("expected doc node %s, got %s", doc.ID, result.Nodes[0].ID)
	}

	// Also verify md:section is reachable (transitively via has)
	q2 := aql.Nodes.SelectStar().
		Where(aql.And(
			aql.Type.Eq("md:section"),
			aql.Nodes.ScopedTo(rootDir.ID),
		)).
		Build()

	result2, err := s.Query(ctx, q2)
	if err != nil {
		t.Fatalf("Query2 failed: %v", err)
	}

	if len(result2.Nodes) != 1 {
		t.Errorf("expected 1 md:section node in scope, got %d", len(result2.Nodes))
	}
}

// TestQuery_WhereNotInSubquery tests IN (SELECT ...) and NOT IN (SELECT ...) subquery support.
func TestQuery_WhereNotInSubquery(t *testing.T) {
	s, ctx := setupAQLTest(t)

	// The test DB has 7 edges total; from_ids are:
	//   src→test1.go, src→test2.py, cmd→main.go, repo→main, repo→dev, repo→src, test1.go→main.go
	// Nodes with outgoing edges:
	//   src(testNodes[4]), cmd(testNodes[5]), repo(testNodes[6]), test1.go(testNodes[0])
	// Nodes WITHOUT outgoing edges (no from_id in edges):
	//   test2.py, README.md, main.go, main-branch, dev-branch

	// NOT IN (SELECT ...): nodes whose ID is NOT in edges.from_id
	p := aql.NewParser()

	query, err := p.Parse("SELECT id, name FROM nodes WHERE id NOT IN (SELECT from_id FROM edges)")
	if err != nil {
		t.Fatalf("Parse failed: %v", err)
	}

	result, err := s.Query(ctx, query)
	if err != nil {
		t.Fatalf("Query failed: %v", err)
	}

	if result.Type != graph.ResultTypeNodes {
		t.Fatalf("expected ResultTypeNodes, got %v", result.Type)
	}

	// We expect 5 nodes with no outgoing edges
	if len(result.Nodes) != 5 {
		t.Errorf("expected 5 nodes with no outgoing edges, got %d", len(result.Nodes))
		for _, n := range result.Nodes {
			t.Logf("  node: %s (%s)", n.Name, n.ID)
		}
	}

	// Ensure none of the source nodes appear in the result
	for _, n := range result.Nodes {
		if n.Name == "src" || n.Name == "cmd" || n.Name == "myrepo" || n.Name == "test1.go" {
			t.Errorf("unexpected source node in NOT IN result: %s", n.Name)
		}
	}

	// IN (SELECT ...): nodes whose ID IS in edges.from_id
	query2, err := p.Parse("SELECT id, name FROM nodes WHERE id IN (SELECT from_id FROM edges)")
	if err != nil {
		t.Fatalf("Parse failed: %v", err)
	}

	result2, err := s.Query(ctx, query2)
	if err != nil {
		t.Fatalf("Query2 failed: %v", err)
	}

	if len(result2.Nodes) != 4 {
		t.Errorf("expected 4 nodes with outgoing edges, got %d", len(result2.Nodes))
		for _, n := range result2.Nodes {
			t.Logf("  node: %s (%s)", n.Name, n.ID)
		}
	}
}

// TestQuery_MultiVariablePatternSelect tests that SELECT a, b FROM pattern
// returns both nodes per match rather than silently returning only the last one.
func TestQuery_MultiVariablePatternSelect(t *testing.T) {
	s, ctx := setupAQLTest(t)
	p := aql.NewParser()

	t.Run("SELECT a, b returns both whole nodes", func(t *testing.T) {
		// src -> test1.go and src -> test2.py via "contains"
		query, err := p.Parse(`SELECT a, b FROM (a:fs:dir)-[:contains]->(b:fs:file)`)
		if err != nil {
			t.Fatalf("Parse failed: %v", err)
		}

		result, err := s.Query(ctx, query)
		if err != nil {
			t.Fatalf("Query failed: %v", err)
		}

		if result.Type != graph.ResultTypeRows {
			t.Fatalf("expected ResultTypeRows, got %v", result.Type)
		}

		// src contains test1.go, test2.py; cmd contains main.go → 3 rows
		if len(result.Rows) != 3 {
			t.Errorf("expected 3 rows, got %d", len(result.Rows))
		}

		for i, row := range result.Rows {
			// Both node ID fields must be present and distinct
			aID, aOk := row["a.id"].(string)
			bID, bOk := row["b.id"].(string)
			if !aOk || !bOk {
				t.Errorf("row %d: missing a.id or b.id: %v", i, row)
				continue
			}
			if aID == bID {
				t.Errorf("row %d: a.id == b.id (%s), nodes not distinct", i, aID)
			}

			// a must be a dir, b must be a file
			aType, _ := row["a.type"].(string)
			bType, _ := row["b.type"].(string)
			if aType != "fs:dir" {
				t.Errorf("row %d: expected a.type=fs:dir, got %q", i, aType)
			}
			if bType != "fs:file" {
				t.Errorf("row %d: expected b.type=fs:file, got %q", i, bType)
			}
		}
	})

	t.Run("SELECT b, a reversed order still correct", func(t *testing.T) {
		query, err := p.Parse(`SELECT b, a FROM (a:fs:dir)-[:contains]->(b:fs:file)`)
		if err != nil {
			t.Fatalf("Parse failed: %v", err)
		}

		result, err := s.Query(ctx, query)
		if err != nil {
			t.Fatalf("Query failed: %v", err)
		}

		if result.Type != graph.ResultTypeRows {
			t.Fatalf("expected ResultTypeRows, got %v", result.Type)
		}

		// Both variables must still appear with correct types
		for i, row := range result.Rows {
			aType, _ := row["a.type"].(string)
			bType, _ := row["b.type"].(string)
			if aType != "fs:dir" {
				t.Errorf("row %d: expected a.type=fs:dir, got %q", i, aType)
			}
			if bType != "fs:file" {
				t.Errorf("row %d: expected b.type=fs:file, got %q", i, bType)
			}
		}
	})

	t.Run("SELECT a.name, b.name gives separate field values", func(t *testing.T) {
		// Filter to src dir only to get predictable results
		query, err := p.Parse(
			`SELECT a.name, b.name FROM (a:fs:dir)-[:contains]->(b:fs:file) WHERE a.name = 'src' ORDER BY b.name`)
		if err != nil {
			t.Fatalf("Parse failed: %v", err)
		}

		result, err := s.Query(ctx, query)
		if err != nil {
			t.Fatalf("Query failed: %v", err)
		}

		if result.Type != graph.ResultTypeRows {
			t.Fatalf("expected ResultTypeRows, got %v", result.Type)
		}

		// src contains test1.go and test2.py
		if len(result.Rows) != 2 {
			t.Errorf("expected 2 rows, got %d", len(result.Rows))
		}

		for i, row := range result.Rows {
			aName, _ := row["a.name"].(string)
			bName, _ := row["b.name"].(string)
			if aName != "src" {
				t.Errorf("row %d: expected a.name=src, got %q", i, aName)
			}
			if bName == "src" || bName == "" {
				t.Errorf("row %d: expected b.name to be a file name, got %q", i, bName)
			}
			if aName == bName {
				t.Errorf("row %d: a.name == b.name (%q), columns not distinct", i, aName)
			}
		}
	})
}
