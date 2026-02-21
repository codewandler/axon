package aql

import (
	"testing"
)

func TestBuilder_SimpleSelect(t *testing.T) {
	// SELECT * FROM nodes
	q := SelectStar().From("nodes").Build()

	if q.Select == nil {
		t.Fatal("expected Select to be non-nil")
	}
	if len(q.Select.Columns) != 1 {
		t.Fatalf("expected 1 column, got %d", len(q.Select.Columns))
	}
	if _, ok := q.Select.Columns[0].Expr.(*Star); !ok {
		t.Fatalf("expected Star, got %T", q.Select.Columns[0].Expr)
	}
	src, ok := q.Select.From.(*TableSource)
	if !ok {
		t.Fatalf("expected TableSource, got %T", q.Select.From)
	}
	if src.Table != "nodes" {
		t.Fatalf("expected 'nodes', got %q", src.Table)
	}
}

func TestBuilder_SelectWithColumns(t *testing.T) {
	// SELECT name, type FROM nodes
	q := Select(Col("name"), Col("type")).From("nodes").Build()

	if len(q.Select.Columns) != 2 {
		t.Fatalf("expected 2 columns, got %d", len(q.Select.Columns))
	}

	col0, ok := q.Select.Columns[0].Expr.(*Selector)
	if !ok {
		t.Fatalf("expected Selector, got %T", q.Select.Columns[0].Expr)
	}
	if col0.String() != "name" {
		t.Fatalf("expected 'name', got %q", col0.String())
	}

	col1, ok := q.Select.Columns[1].Expr.(*Selector)
	if !ok {
		t.Fatalf("expected Selector, got %T", q.Select.Columns[1].Expr)
	}
	if col1.String() != "type" {
		t.Fatalf("expected 'type', got %q", col1.String())
	}
}

func TestBuilder_SelectWithWhere(t *testing.T) {
	// SELECT * FROM nodes WHERE type = 'fs:file'
	q := SelectStar().
		From("nodes").
		Where(Eq("type", String("fs:file"))).
		Build()

	if q.Select.Where == nil {
		t.Fatal("expected Where to be non-nil")
	}
	comp, ok := q.Select.Where.(*ComparisonExpr)
	if !ok {
		t.Fatalf("expected ComparisonExpr, got %T", q.Select.Where)
	}
	if comp.Op != OpEq {
		t.Fatalf("expected OpEq, got %v", comp.Op)
	}
	if comp.Left.String() != "type" {
		t.Fatalf("expected 'type', got %q", comp.Left.String())
	}
	str, ok := comp.Right.(*StringLit)
	if !ok {
		t.Fatalf("expected StringLit, got %T", comp.Right)
	}
	if str.Value != "fs:file" {
		t.Fatalf("expected 'fs:file', got %q", str.Value)
	}
}

func TestBuilder_AndOrNot(t *testing.T) {
	// WHERE type = 'fs:file' AND NOT name LIKE '%.tmp'
	expr := And(
		Eq("type", String("fs:file")),
		Not(Like("name", String("%.tmp"))),
	)

	bin, ok := expr.(*BinaryExpr)
	if !ok {
		t.Fatalf("expected BinaryExpr, got %T", expr)
	}
	if bin.Op != OpAnd {
		t.Fatalf("expected OpAnd, got %v", bin.Op)
	}

	unary, ok := bin.Right.(*UnaryExpr)
	if !ok {
		t.Fatalf("expected UnaryExpr, got %T", bin.Right)
	}
	if unary.Op != OpNot {
		t.Fatalf("expected OpNot, got %v", unary.Op)
	}
}

func TestBuilder_ComplexWhere(t *testing.T) {
	// WHERE (type = 'fs:file' OR type = 'fs:dir') AND data.size > 100
	expr := And(
		Paren(Or(
			Eq("type", String("fs:file")),
			Eq("type", String("fs:dir")),
		)),
		Gt("data.size", Int(100)),
	)

	q := SelectStar().From("nodes").Where(expr).Build()

	if q.Select.Where == nil {
		t.Fatal("expected Where to be non-nil")
	}
}

func TestBuilder_OrderByLimitOffset(t *testing.T) {
	// SELECT * FROM nodes ORDER BY name, type DESC LIMIT 10 OFFSET 5
	q := SelectStar().
		From("nodes").
		OrderBy("name").
		OrderByDesc("type").
		Limit(10).
		Offset(5).
		Build()

	if len(q.Select.OrderBy) != 2 {
		t.Fatalf("expected 2 order clauses, got %d", len(q.Select.OrderBy))
	}
	if q.Select.OrderBy[0].Descending {
		t.Fatal("expected first to be ascending")
	}
	if !q.Select.OrderBy[1].Descending {
		t.Fatal("expected second to be descending")
	}
	if q.Select.Limit == nil || *q.Select.Limit != 10 {
		t.Fatalf("expected limit 10, got %v", q.Select.Limit)
	}
	if q.Select.Offset == nil || *q.Select.Offset != 5 {
		t.Fatalf("expected offset 5, got %v", q.Select.Offset)
	}
}

func TestBuilder_GroupByHaving(t *testing.T) {
	// SELECT type, COUNT(*) FROM nodes GROUP BY type HAVING COUNT(*) > 10
	q := Select(Col("type"), Count()).
		From("nodes").
		GroupByCol("type").
		Having(Gt("COUNT(*)", Int(10))).
		Build()

	if len(q.Select.GroupBy) != 1 {
		t.Fatalf("expected 1 group by, got %d", len(q.Select.GroupBy))
	}
	if q.Select.GroupBy[0].String() != "type" {
		t.Fatalf("expected 'type', got %q", q.Select.GroupBy[0].String())
	}
	if q.Select.Having == nil {
		t.Fatal("expected Having to be non-nil")
	}
}

func TestBuilder_PatternQuery(t *testing.T) {
	// SELECT file FROM (dir:fs:dir)-[:contains]->(file:fs:file)
	pattern := Pat(NodeType("dir", "fs:dir")).
		To(AnyEdgeOfType("contains"), NodeType("file", "fs:file")).
		Build()

	q := Select(Col("file")).FromPattern(pattern).Build()

	ps, ok := q.Select.From.(*PatternSource)
	if !ok {
		t.Fatalf("expected PatternSource, got %T", q.Select.From)
	}
	if len(ps.Patterns) != 1 {
		t.Fatalf("expected 1 pattern, got %d", len(ps.Patterns))
	}
	if len(ps.Patterns[0].Elements) != 3 {
		t.Fatalf("expected 3 elements, got %d", len(ps.Patterns[0].Elements))
	}

	// Check node patterns
	node0, ok := ps.Patterns[0].Elements[0].(*NodePattern)
	if !ok {
		t.Fatalf("expected NodePattern, got %T", ps.Patterns[0].Elements[0])
	}
	if node0.Variable != "dir" || node0.Type != "fs:dir" {
		t.Fatalf("expected dir:fs:dir, got %s:%s", node0.Variable, node0.Type)
	}

	// Check edge pattern
	edge, ok := ps.Patterns[0].Elements[1].(*EdgePattern)
	if !ok {
		t.Fatalf("expected EdgePattern, got %T", ps.Patterns[0].Elements[1])
	}
	if edge.Type != "contains" || edge.Direction != Outgoing {
		t.Fatalf("expected contains/Outgoing, got %s/%v", edge.Type, edge.Direction)
	}
}

func TestBuilder_VariableLengthEdge(t *testing.T) {
	// (a)-[:contains*1..5]->(b)
	pattern := Pat(N("a")).
		To(AnyEdgeOfType("contains").WithHops(1, 5), N("b")).
		Build()

	edge := pattern.Elements[1].(*EdgePattern)
	if edge.MinHops == nil || *edge.MinHops != 1 {
		t.Fatalf("expected min 1, got %v", edge.MinHops)
	}
	if edge.MaxHops == nil || *edge.MaxHops != 5 {
		t.Fatalf("expected max 5, got %v", edge.MaxHops)
	}
}

func TestBuilder_InBetween(t *testing.T) {
	// WHERE type IN ('a', 'b', 'c') AND size BETWEEN 10 AND 100
	expr := And(
		In("type", String("a"), String("b"), String("c")),
		Between("size", Int(10), Int(100)),
	)

	bin, ok := expr.(*BinaryExpr)
	if !ok {
		t.Fatalf("expected BinaryExpr, got %T", expr)
	}

	inExpr, ok := bin.Left.(*InExpr)
	if !ok {
		t.Fatalf("expected InExpr, got %T", bin.Left)
	}
	if len(inExpr.Values) != 3 {
		t.Fatalf("expected 3 values, got %d", len(inExpr.Values))
	}

	between, ok := bin.Right.(*BetweenExpr)
	if !ok {
		t.Fatalf("expected BetweenExpr, got %T", bin.Right)
	}
	if between.Left.String() != "size" {
		t.Fatalf("expected 'size', got %q", between.Left.String())
	}
}

func TestBuilder_IsNull(t *testing.T) {
	// WHERE data IS NOT NULL
	expr := IsNotNull("data")

	if expr.Selector.String() != "data" {
		t.Fatalf("expected 'data', got %q", expr.Selector.String())
	}
	if !expr.Not {
		t.Fatal("expected Not to be true")
	}
}

func TestBuilder_Exists(t *testing.T) {
	// WHERE EXISTS (dir)-[:contains]->(:fs:file)
	pattern := Pat(N("dir")).
		To(AnyEdgeOfType("contains"), AnyNodeOfType("fs:file")).
		Build()

	expr := Exists(pattern)
	if expr.Not {
		t.Fatal("expected Not to be false")
	}
	if expr.Pattern != pattern {
		t.Fatal("expected pattern to match")
	}
}

func TestBuilder_LabelOperations(t *testing.T) {
	// labels CONTAINS ANY ('tag1', 'tag2')
	expr := ContainsAny("labels", String("tag1"), String("tag2"))

	if expr.Op != OpContainsAny {
		t.Fatalf("expected OpContainsAny, got %v", expr.Op)
	}
	if len(expr.Labels) != 2 {
		t.Fatalf("expected 2 labels, got %d", len(expr.Labels))
	}
}

func TestBuilder_Parameters(t *testing.T) {
	// WHERE type = $type AND id = $1
	expr := And(
		Eq("type", Param("type")),
		Eq("id", ParamN(1)),
	)

	bin, ok := expr.(*BinaryExpr)
	if !ok {
		t.Fatalf("expected BinaryExpr, got %T", expr)
	}

	comp0 := bin.Left.(*ComparisonExpr)
	param0, ok := comp0.Right.(*Parameter)
	if !ok {
		t.Fatalf("expected Parameter, got %T", comp0.Right)
	}
	if !param0.IsNamed() || param0.Name != "type" {
		t.Fatalf("expected named param 'type', got %v", param0)
	}

	comp1 := bin.Right.(*ComparisonExpr)
	param1, ok := comp1.Right.(*Parameter)
	if !ok {
		t.Fatalf("expected Parameter, got %T", comp1.Right)
	}
	if !param1.IsPositional() || param1.Index != 1 {
		t.Fatalf("expected positional param 1, got %v", param1)
	}
}

func TestBuilder_Values(t *testing.T) {
	tests := []struct {
		name  string
		value Value
		check func(Value) bool
	}{
		{
			name:  "string",
			value: String("hello"),
			check: func(v Value) bool { return v.(*StringLit).Value == "hello" },
		},
		{
			name:  "int",
			value: Int(42),
			check: func(v Value) bool { n := v.(*NumberLit); return n.IsInt && n.IntValue() == 42 },
		},
		{
			name:  "float",
			value: Float(3.14),
			check: func(v Value) bool { n := v.(*NumberLit); return !n.IsInt && n.Value == 3.14 },
		},
		{
			name:  "true",
			value: True(),
			check: func(v Value) bool { return v.(*BoolLit).Value == true },
		},
		{
			name:  "false",
			value: False(),
			check: func(v Value) bool { return v.(*BoolLit).Value == false },
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if !tt.check(tt.value) {
				t.Fatalf("value check failed for %s", tt.name)
			}
		})
	}
}

func TestBuilder_MultiPattern(t *testing.T) {
	// SELECT a, c FROM (a:fs:dir)-[:contains]->(b:fs:dir), (b)-[:contains]->(c:fs:file)
	p1 := Pat(NodeType("a", "fs:dir")).
		To(AnyEdgeOfType("contains"), NodeType("b", "fs:dir")).
		Build()
	p2 := Pat(N("b")).
		To(AnyEdgeOfType("contains"), NodeType("c", "fs:file")).
		Build()

	q := Select(Col("a"), Col("c")).FromPattern(p1, p2).Build()

	ps := q.Select.From.(*PatternSource)
	if len(ps.Patterns) != 2 {
		t.Fatalf("expected 2 patterns, got %d", len(ps.Patterns))
	}
}

func TestBuilder_Distinct(t *testing.T) {
	// SELECT DISTINCT type FROM nodes
	q := SelectDistinct(Col("type")).From("nodes").Build()
	if !q.Select.Distinct {
		t.Fatal("expected Distinct to be true")
	}

	// Alternative via method
	q2 := Select(Col("type")).From("nodes").Distinct().Build()
	if !q2.Select.Distinct {
		t.Fatal("expected Distinct to be true")
	}
}

func TestBuilder_Validation(t *testing.T) {
	// Build a query and validate it
	// Note: Col("file", "data", "ext") creates selector file.data.ext
	q := Select(Col("file")).
		FromPattern(
			Pat(NodeType("dir", "fs:dir")).
				To(AnyEdgeOfType("contains"), NodeType("file", "fs:file")).
				Build(),
		).
		Where(&ComparisonExpr{
			Left:  Col("file", "data", "ext"),
			Op:    OpEq,
			Right: String("go"),
		}).
		Limit(100).
		Build()

	errs := Validate(q)
	if len(errs) > 0 {
		t.Fatalf("unexpected validation errors: %v", errs)
	}
}
