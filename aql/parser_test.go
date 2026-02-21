package aql

import (
	"testing"
)

func TestParser_SelectBasics(t *testing.T) {
	p := NewParser()

	tests := []struct {
		name    string
		input   string
		check   func(t *testing.T, q *Query)
		wantErr bool
	}{
		{
			name:  "SELECT * FROM nodes",
			input: "SELECT * FROM nodes",
			check: func(t *testing.T, q *Query) {
				if q.Select == nil {
					t.Fatal("expected Select")
				}
				if len(q.Select.Columns) != 1 {
					t.Fatalf("expected 1 column, got %d", len(q.Select.Columns))
				}
				if _, ok := q.Select.Columns[0].Expr.(*Star); !ok {
					t.Fatal("expected Star column")
				}
				src, ok := q.Select.From.(*TableSource)
				if !ok {
					t.Fatal("expected TableSource")
				}
				if src.Table != "nodes" {
					t.Fatalf("expected nodes, got %s", src.Table)
				}
			},
		},
		{
			name:  "SELECT * FROM edges",
			input: "SELECT * FROM edges",
			check: func(t *testing.T, q *Query) {
				src := q.Select.From.(*TableSource)
				if src.Table != "edges" {
					t.Fatalf("expected edges, got %s", src.Table)
				}
			},
		},
		{
			name:  "case insensitive keywords",
			input: "select * from NODES",
			check: func(t *testing.T, q *Query) {
				src := q.Select.From.(*TableSource)
				if src.Table != "nodes" {
					t.Fatalf("expected nodes, got %s", src.Table)
				}
			},
		},
		{
			name:  "DISTINCT",
			input: "SELECT DISTINCT * FROM nodes",
			check: func(t *testing.T, q *Query) {
				if !q.Select.Distinct {
					t.Fatal("expected Distinct")
				}
			},
		},
		{
			name:  "single column",
			input: "SELECT name FROM nodes",
			check: func(t *testing.T, q *Query) {
				if len(q.Select.Columns) != 1 {
					t.Fatalf("expected 1 column, got %d", len(q.Select.Columns))
				}
				sel, ok := q.Select.Columns[0].Expr.(*Selector)
				if !ok {
					t.Fatal("expected Selector")
				}
				if sel.String() != "name" {
					t.Fatalf("expected name, got %s", sel.String())
				}
			},
		},
		{
			name:  "multiple columns",
			input: "SELECT name, type, data.ext FROM nodes",
			check: func(t *testing.T, q *Query) {
				if len(q.Select.Columns) != 3 {
					t.Fatalf("expected 3 columns, got %d", len(q.Select.Columns))
				}
				sel := q.Select.Columns[2].Expr.(*Selector)
				if sel.String() != "data.ext" {
					t.Fatalf("expected data.ext, got %s", sel.String())
				}
			},
		},
		{
			name:  "column with alias",
			input: "SELECT name AS n FROM nodes",
			check: func(t *testing.T, q *Query) {
				if q.Select.Columns[0].Alias != "n" {
					t.Fatalf("expected alias n, got %s", q.Select.Columns[0].Alias)
				}
			},
		},
		{
			name:  "COUNT(*)",
			input: "SELECT COUNT(*) FROM nodes",
			check: func(t *testing.T, q *Query) {
				_, ok := q.Select.Columns[0].Expr.(*CountCall)
				if !ok {
					t.Fatal("expected CountCall")
				}
			},
		},
		{
			name:  "COUNT(*) with alias",
			input: "SELECT COUNT(*) AS total FROM nodes",
			check: func(t *testing.T, q *Query) {
				if q.Select.Columns[0].Alias != "total" {
					t.Fatalf("expected alias total, got %s", q.Select.Columns[0].Alias)
				}
			},
		},
		{
			name:  "LIMIT",
			input: "SELECT * FROM nodes LIMIT 10",
			check: func(t *testing.T, q *Query) {
				if q.Select.Limit == nil || *q.Select.Limit != 10 {
					t.Fatal("expected LIMIT 10")
				}
			},
		},
		{
			name:  "LIMIT OFFSET",
			input: "SELECT * FROM nodes LIMIT 10 OFFSET 20",
			check: func(t *testing.T, q *Query) {
				if q.Select.Limit == nil || *q.Select.Limit != 10 {
					t.Fatal("expected LIMIT 10")
				}
				if q.Select.Offset == nil || *q.Select.Offset != 20 {
					t.Fatal("expected OFFSET 20")
				}
			},
		},
		{
			name:  "comment",
			input: "-- this is a comment\nSELECT * FROM nodes",
			check: func(t *testing.T, q *Query) {
				if q.Select == nil {
					t.Fatal("expected Select after comment")
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			q, err := p.Parse(tt.input)
			if (err != nil) != tt.wantErr {
				t.Fatalf("Parse() error = %v, wantErr %v", err, tt.wantErr)
			}
			if err == nil && tt.check != nil {
				tt.check(t, q)
			}
		})
	}
}

func TestParser_Where(t *testing.T) {
	p := NewParser()

	tests := []struct {
		name  string
		input string
		check func(t *testing.T, q *Query)
	}{
		{
			name:  "simple equality",
			input: "SELECT * FROM nodes WHERE type = 'fs:file'",
			check: func(t *testing.T, q *Query) {
				cmp, ok := q.Select.Where.(*ComparisonExpr)
				if !ok {
					t.Fatalf("expected ComparisonExpr, got %T", q.Select.Where)
				}
				if cmp.Op != OpEq {
					t.Fatalf("expected OpEq, got %s", cmp.Op)
				}
				if cmp.Left.String() != "type" {
					t.Fatalf("expected type, got %s", cmp.Left.String())
				}
				str, ok := cmp.Right.(*StringLit)
				if !ok {
					t.Fatal("expected StringLit")
				}
				if str.Value != "fs:file" {
					t.Fatalf("expected fs:file, got %s", str.Value)
				}
			},
		},
		{
			name:  "not equal",
			input: "SELECT * FROM nodes WHERE type != 'fs:file'",
			check: func(t *testing.T, q *Query) {
				cmp := q.Select.Where.(*ComparisonExpr)
				if cmp.Op != OpNe {
					t.Fatalf("expected OpNe, got %s", cmp.Op)
				}
			},
		},
		{
			name:  "less than",
			input: "SELECT * FROM nodes WHERE data.size < 1000",
			check: func(t *testing.T, q *Query) {
				cmp := q.Select.Where.(*ComparisonExpr)
				if cmp.Op != OpLt {
					t.Fatalf("expected OpLt, got %s", cmp.Op)
				}
				num := cmp.Right.(*NumberLit)
				if num.Value != 1000 {
					t.Fatalf("expected 1000, got %f", num.Value)
				}
			},
		},
		{
			name:  "LIKE",
			input: "SELECT * FROM nodes WHERE name LIKE '%.go'",
			check: func(t *testing.T, q *Query) {
				cmp := q.Select.Where.(*ComparisonExpr)
				if cmp.Op != OpLike {
					t.Fatalf("expected OpLike, got %s", cmp.Op)
				}
			},
		},
		{
			name:  "GLOB",
			input: "SELECT * FROM nodes WHERE name GLOB '*.go'",
			check: func(t *testing.T, q *Query) {
				cmp := q.Select.Where.(*ComparisonExpr)
				if cmp.Op != OpGlob {
					t.Fatalf("expected OpGlob, got %s", cmp.Op)
				}
			},
		},
		{
			name:  "AND",
			input: "SELECT * FROM nodes WHERE type = 'fs:file' AND name = 'foo'",
			check: func(t *testing.T, q *Query) {
				bin, ok := q.Select.Where.(*BinaryExpr)
				if !ok {
					t.Fatalf("expected BinaryExpr, got %T", q.Select.Where)
				}
				if bin.Op != OpAnd {
					t.Fatalf("expected OpAnd, got %s", bin.Op)
				}
			},
		},
		{
			name:  "OR",
			input: "SELECT * FROM nodes WHERE type = 'fs:file' OR type = 'fs:dir'",
			check: func(t *testing.T, q *Query) {
				bin := q.Select.Where.(*BinaryExpr)
				if bin.Op != OpOr {
					t.Fatalf("expected OpOr, got %s", bin.Op)
				}
			},
		},
		{
			name:  "NOT",
			input: "SELECT * FROM nodes WHERE NOT type = 'fs:file'",
			check: func(t *testing.T, q *Query) {
				un, ok := q.Select.Where.(*UnaryExpr)
				if !ok {
					t.Fatalf("expected UnaryExpr, got %T", q.Select.Where)
				}
				if un.Op != OpNot {
					t.Fatalf("expected OpNot, got %s", un.Op)
				}
			},
		},
		{
			name:  "parentheses",
			input: "SELECT * FROM nodes WHERE (type = 'fs:file' OR type = 'fs:dir') AND name = 'foo'",
			check: func(t *testing.T, q *Query) {
				bin := q.Select.Where.(*BinaryExpr)
				if bin.Op != OpAnd {
					t.Fatalf("expected top-level AND, got %s", bin.Op)
				}
				paren, ok := bin.Left.(*ParenExpr)
				if !ok {
					t.Fatalf("expected ParenExpr, got %T", bin.Left)
				}
				inner := paren.Inner.(*BinaryExpr)
				if inner.Op != OpOr {
					t.Fatalf("expected inner OR, got %s", inner.Op)
				}
			},
		},
		{
			name:  "IS NULL",
			input: "SELECT * FROM nodes WHERE data.ext IS NULL",
			check: func(t *testing.T, q *Query) {
				isn, ok := q.Select.Where.(*IsNullExpr)
				if !ok {
					t.Fatalf("expected IsNullExpr, got %T", q.Select.Where)
				}
				if isn.Not {
					t.Fatal("expected IS NULL, not IS NOT NULL")
				}
			},
		},
		{
			name:  "IS NOT NULL",
			input: "SELECT * FROM nodes WHERE data.ext IS NOT NULL",
			check: func(t *testing.T, q *Query) {
				isn := q.Select.Where.(*IsNullExpr)
				if !isn.Not {
					t.Fatal("expected IS NOT NULL")
				}
			},
		},
		{
			name:  "IN",
			input: "SELECT * FROM nodes WHERE type IN ('fs:file', 'fs:dir')",
			check: func(t *testing.T, q *Query) {
				in, ok := q.Select.Where.(*InExpr)
				if !ok {
					t.Fatalf("expected InExpr, got %T", q.Select.Where)
				}
				if len(in.Values) != 2 {
					t.Fatalf("expected 2 values, got %d", len(in.Values))
				}
			},
		},
		{
			name:  "BETWEEN",
			input: "SELECT * FROM nodes WHERE data.size BETWEEN 100 AND 1000",
			check: func(t *testing.T, q *Query) {
				btw, ok := q.Select.Where.(*BetweenExpr)
				if !ok {
					t.Fatalf("expected BetweenExpr, got %T", q.Select.Where)
				}
				low := btw.Low.(*NumberLit)
				high := btw.High.(*NumberLit)
				if low.Value != 100 || high.Value != 1000 {
					t.Fatalf("expected 100..1000, got %f..%f", low.Value, high.Value)
				}
			},
		},
		{
			name:  "CONTAINS ANY",
			input: "SELECT * FROM nodes WHERE labels CONTAINS ANY ('a', 'b', 'c')",
			check: func(t *testing.T, q *Query) {
				lbl, ok := q.Select.Where.(*LabelExpr)
				if !ok {
					t.Fatalf("expected LabelExpr, got %T", q.Select.Where)
				}
				if lbl.Op != OpContainsAny {
					t.Fatalf("expected OpContainsAny, got %s", lbl.Op)
				}
				if len(lbl.Labels) != 3 {
					t.Fatalf("expected 3 labels, got %d", len(lbl.Labels))
				}
			},
		},
		{
			name:  "CONTAINS ALL",
			input: "SELECT * FROM nodes WHERE labels CONTAINS ALL ('a', 'b')",
			check: func(t *testing.T, q *Query) {
				lbl := q.Select.Where.(*LabelExpr)
				if lbl.Op != OpContainsAll {
					t.Fatalf("expected OpContainsAll, got %s", lbl.Op)
				}
			},
		},
		{
			name:  "NOT CONTAINS",
			input: "SELECT * FROM nodes WHERE labels NOT CONTAINS ('deprecated')",
			check: func(t *testing.T, q *Query) {
				lbl := q.Select.Where.(*LabelExpr)
				if lbl.Op != OpNotContains {
					t.Fatalf("expected OpNotContains, got %s", lbl.Op)
				}
			},
		},
		{
			name:  "complex boolean",
			input: "SELECT * FROM nodes WHERE (type = 'fs:file' OR type = 'fs:dir') AND labels CONTAINS ANY ('a', 'b') AND labels NOT CONTAINS ('x')",
			check: func(t *testing.T, q *Query) {
				// Just check it parses - detailed structure is complex
				if q.Select.Where == nil {
					t.Fatal("expected WHERE clause")
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			q, err := p.Parse(tt.input)
			if err != nil {
				t.Fatalf("Parse() error = %v", err)
			}
			tt.check(t, q)
		})
	}
}

func TestParser_Parameters(t *testing.T) {
	p := NewParser()

	tests := []struct {
		name  string
		input string
		check func(t *testing.T, q *Query)
	}{
		{
			name:  "named parameter",
			input: "SELECT * FROM nodes WHERE type = $type",
			check: func(t *testing.T, q *Query) {
				cmp := q.Select.Where.(*ComparisonExpr)
				param, ok := cmp.Right.(*Parameter)
				if !ok {
					t.Fatalf("expected Parameter, got %T", cmp.Right)
				}
				if !param.IsNamed() {
					t.Fatal("expected named parameter")
				}
				if param.Name != "type" {
					t.Fatalf("expected type, got %s", param.Name)
				}
			},
		},
		{
			name:  "positional parameter",
			input: "SELECT * FROM nodes WHERE type = $1",
			check: func(t *testing.T, q *Query) {
				cmp := q.Select.Where.(*ComparisonExpr)
				param := cmp.Right.(*Parameter)
				if !param.IsPositional() {
					t.Fatal("expected positional parameter")
				}
				if param.Index != 1 {
					t.Fatalf("expected 1, got %d", param.Index)
				}
			},
		},
		{
			name:  "multiple parameters",
			input: "SELECT * FROM nodes WHERE type = $type AND name LIKE $1",
			check: func(t *testing.T, q *Query) {
				bin := q.Select.Where.(*BinaryExpr)
				left := bin.Left.(*ComparisonExpr)
				right := bin.Right.(*ComparisonExpr)

				p1 := left.Right.(*Parameter)
				p2 := right.Right.(*Parameter)

				if p1.Name != "type" {
					t.Fatalf("expected $type, got %s", p1.String())
				}
				if p2.Index != 1 {
					t.Fatalf("expected $1, got %s", p2.String())
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			q, err := p.Parse(tt.input)
			if err != nil {
				t.Fatalf("Parse() error = %v", err)
			}
			tt.check(t, q)
		})
	}
}

func TestParser_GroupBy(t *testing.T) {
	p := NewParser()

	tests := []struct {
		name  string
		input string
		check func(t *testing.T, q *Query)
	}{
		{
			name:  "GROUP BY single",
			input: "SELECT type, COUNT(*) FROM nodes GROUP BY type",
			check: func(t *testing.T, q *Query) {
				if len(q.Select.GroupBy) != 1 {
					t.Fatalf("expected 1 GROUP BY, got %d", len(q.Select.GroupBy))
				}
				if q.Select.GroupBy[0].String() != "type" {
					t.Fatalf("expected type, got %s", q.Select.GroupBy[0].String())
				}
			},
		},
		{
			name:  "GROUP BY multiple",
			input: "SELECT type, data.ext, COUNT(*) FROM nodes GROUP BY type, data.ext",
			check: func(t *testing.T, q *Query) {
				if len(q.Select.GroupBy) != 2 {
					t.Fatalf("expected 2 GROUP BY, got %d", len(q.Select.GroupBy))
				}
			},
		},
		{
			name:  "GROUP BY with HAVING",
			input: "SELECT type, COUNT(*) FROM nodes GROUP BY type HAVING COUNT(*) > 10",
			check: func(t *testing.T, q *Query) {
				if q.Select.Having == nil {
					t.Fatal("expected HAVING")
				}
				cmp := q.Select.Having.(*ComparisonExpr)
				if cmp.Op != OpGt {
					t.Fatalf("expected >, got %s", cmp.Op)
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			q, err := p.Parse(tt.input)
			if err != nil {
				t.Fatalf("Parse() error = %v", err)
			}
			tt.check(t, q)
		})
	}
}

func TestParser_OrderBy(t *testing.T) {
	p := NewParser()

	tests := []struct {
		name  string
		input string
		check func(t *testing.T, q *Query)
	}{
		{
			name:  "ORDER BY single",
			input: "SELECT * FROM nodes ORDER BY name",
			check: func(t *testing.T, q *Query) {
				if len(q.Select.OrderBy) != 1 {
					t.Fatalf("expected 1 ORDER BY, got %d", len(q.Select.OrderBy))
				}
				if q.Select.OrderBy[0].Descending {
					t.Fatal("expected ASC (default)")
				}
			},
		},
		{
			name:  "ORDER BY DESC",
			input: "SELECT * FROM nodes ORDER BY name DESC",
			check: func(t *testing.T, q *Query) {
				if !q.Select.OrderBy[0].Descending {
					t.Fatal("expected DESC")
				}
			},
		},
		{
			name:  "ORDER BY ASC explicit",
			input: "SELECT * FROM nodes ORDER BY name ASC",
			check: func(t *testing.T, q *Query) {
				if q.Select.OrderBy[0].Descending {
					t.Fatal("expected ASC")
				}
			},
		},
		{
			name:  "ORDER BY COUNT(*)",
			input: "SELECT type, COUNT(*) FROM nodes GROUP BY type ORDER BY COUNT(*) DESC",
			check: func(t *testing.T, q *Query) {
				_, ok := q.Select.OrderBy[0].Expr.(*CountCall)
				if !ok {
					t.Fatal("expected CountCall in ORDER BY")
				}
			},
		},
		{
			name:  "ORDER BY multiple",
			input: "SELECT * FROM nodes ORDER BY type ASC, name DESC",
			check: func(t *testing.T, q *Query) {
				if len(q.Select.OrderBy) != 2 {
					t.Fatalf("expected 2 ORDER BY, got %d", len(q.Select.OrderBy))
				}
				if q.Select.OrderBy[0].Descending {
					t.Fatal("expected first ASC")
				}
				if !q.Select.OrderBy[1].Descending {
					t.Fatal("expected second DESC")
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			q, err := p.Parse(tt.input)
			if err != nil {
				t.Fatalf("Parse() error = %v", err)
			}
			tt.check(t, q)
		})
	}
}

func TestParser_Pattern(t *testing.T) {
	p := NewParser()

	tests := []struct {
		name  string
		input string
		check func(t *testing.T, q *Query)
	}{
		{
			name:  "simple node pattern",
			input: "SELECT n FROM (n:fs:file)",
			check: func(t *testing.T, q *Query) {
				ps, ok := q.Select.From.(*PatternSource)
				if !ok {
					t.Fatalf("expected PatternSource, got %T", q.Select.From)
				}
				if len(ps.Patterns) != 1 {
					t.Fatalf("expected 1 pattern, got %d", len(ps.Patterns))
				}
				pat := ps.Patterns[0]
				if len(pat.Elements) != 1 {
					t.Fatalf("expected 1 element, got %d", len(pat.Elements))
				}
				node := pat.Elements[0].(*NodePattern)
				if node.Variable != "n" {
					t.Fatalf("expected n, got %s", node.Variable)
				}
				if node.Type != "fs:file" {
					t.Fatalf("expected fs:file, got %s", node.Type)
				}
			},
		},
		{
			name:  "node with type only",
			input: "SELECT n FROM (n:fs:file)",
			check: func(t *testing.T, q *Query) {
				ps := q.Select.From.(*PatternSource)
				node := ps.Patterns[0].Elements[0].(*NodePattern)
				if node.Variable != "n" || node.Type != "fs:file" {
					t.Fatalf("expected n:fs:file, got %s:%s", node.Variable, node.Type)
				}
			},
		},
		{
			name:  "node type with glob",
			input: "SELECT n FROM (n:fs:*)",
			check: func(t *testing.T, q *Query) {
				ps := q.Select.From.(*PatternSource)
				node := ps.Patterns[0].Elements[0].(*NodePattern)
				if node.Type != "fs:*" {
					t.Fatalf("expected fs:*, got %s", node.Type)
				}
			},
		},
		{
			name:  "two nodes with outgoing edge",
			input: "SELECT file FROM (dir:fs:dir)-[:contains]->(file:fs:file)",
			check: func(t *testing.T, q *Query) {
				ps := q.Select.From.(*PatternSource)
				pat := ps.Patterns[0]
				if len(pat.Elements) != 3 {
					t.Fatalf("expected 3 elements, got %d", len(pat.Elements))
				}

				node1 := pat.Elements[0].(*NodePattern)
				edge := pat.Elements[1].(*EdgePattern)
				node2 := pat.Elements[2].(*NodePattern)

				if node1.Variable != "dir" {
					t.Fatalf("expected dir, got %s", node1.Variable)
				}
				if edge.Type != "contains" {
					t.Fatalf("expected contains, got %s", edge.Type)
				}
				if edge.Direction != Outgoing {
					t.Fatalf("expected Outgoing, got %s", edge.Direction)
				}
				if node2.Variable != "file" {
					t.Fatalf("expected file, got %s", node2.Variable)
				}
			},
		},
		{
			name:  "incoming edge",
			input: "SELECT repo FROM (branch:vcs:branch)<-[:has]-(repo:vcs:repo)",
			check: func(t *testing.T, q *Query) {
				ps := q.Select.From.(*PatternSource)
				edge := ps.Patterns[0].Elements[1].(*EdgePattern)
				if edge.Direction != Incoming {
					t.Fatalf("expected Incoming, got %s", edge.Direction)
				}
			},
		},
		{
			name:  "undirected edge",
			input: "SELECT other FROM (node:fs:file)-[:references]-(other:fs:file)",
			check: func(t *testing.T, q *Query) {
				ps := q.Select.From.(*PatternSource)
				edge := ps.Patterns[0].Elements[1].(*EdgePattern)
				if edge.Direction != Undirected {
					t.Fatalf("expected Undirected, got %s", edge.Direction)
				}
			},
		},
		{
			name:  "edge with variable",
			input: "SELECT e FROM (a)-[e:contains]->(b)",
			check: func(t *testing.T, q *Query) {
				ps := q.Select.From.(*PatternSource)
				edge := ps.Patterns[0].Elements[1].(*EdgePattern)
				if edge.Variable != "e" {
					t.Fatalf("expected e, got %s", edge.Variable)
				}
			},
		},
		{
			name:  "edge any type",
			input: "SELECT b FROM (a)-[:*]->(b)",
			check: func(t *testing.T, q *Query) {
				ps := q.Select.From.(*PatternSource)
				edge := ps.Patterns[0].Elements[1].(*EdgePattern)
				// [:*] parses as empty type with Star quantifier
				// For "any edge type", use empty type
				if edge.Type != "" {
					t.Fatalf("expected empty type for any edge, got %s", edge.Type)
				}
			},
		},
		{
			name:  "transitive edge (star)",
			input: "SELECT file FROM (root:fs:dir)-[:contains*]->(file:fs:file)",
			check: func(t *testing.T, q *Query) {
				ps := q.Select.From.(*PatternSource)
				edge := ps.Patterns[0].Elements[1].(*EdgePattern)
				if !edge.IsVariableLength() {
					t.Fatal("expected variable length edge")
				}
				if edge.MinHops == nil || *edge.MinHops != 1 {
					t.Fatal("expected MinHops = 1")
				}
				if edge.MaxHops != nil {
					t.Fatal("expected MaxHops = nil (unbounded)")
				}
			},
		},
		{
			name:  "bounded hops exact",
			input: "SELECT child FROM (parent:fs:dir)-[:contains*3]->(child)",
			check: func(t *testing.T, q *Query) {
				ps := q.Select.From.(*PatternSource)
				edge := ps.Patterns[0].Elements[1].(*EdgePattern)
				if edge.MinHops == nil || *edge.MinHops != 3 {
					t.Fatalf("expected MinHops = 3, got %v", edge.MinHops)
				}
				if edge.MaxHops == nil || *edge.MaxHops != 3 {
					t.Fatalf("expected MaxHops = 3, got %v", edge.MaxHops)
				}
			},
		},
		{
			name:  "bounded hops range",
			input: "SELECT child FROM (parent:fs:dir)-[:contains*1..3]->(child)",
			check: func(t *testing.T, q *Query) {
				ps := q.Select.From.(*PatternSource)
				edge := ps.Patterns[0].Elements[1].(*EdgePattern)
				if edge.MinHops == nil || *edge.MinHops != 1 {
					t.Fatalf("expected MinHops = 1, got %v", edge.MinHops)
				}
				if edge.MaxHops == nil || *edge.MaxHops != 3 {
					t.Fatalf("expected MaxHops = 3, got %v", edge.MaxHops)
				}
			},
		},
		{
			name:  "multiple patterns",
			input: "SELECT foo, doc FROM (foo:fs:dir)-[:contains]->(bar:fs:dir), (bar)-[:contains]->(doc:md:document)",
			check: func(t *testing.T, q *Query) {
				ps := q.Select.From.(*PatternSource)
				if len(ps.Patterns) != 2 {
					t.Fatalf("expected 2 patterns, got %d", len(ps.Patterns))
				}
			},
		},
		{
			name:  "inline WHERE in node",
			input: "SELECT file FROM (dir:fs:dir WHERE dir.name = 'cmd')-[:contains]->(file:fs:file)",
			check: func(t *testing.T, q *Query) {
				ps := q.Select.From.(*PatternSource)
				node := ps.Patterns[0].Elements[0].(*NodePattern)
				if node.Where == nil {
					t.Fatal("expected inline WHERE")
				}
				cmp := node.Where.(*ComparisonExpr)
				if cmp.Left.String() != "dir.name" {
					t.Fatalf("expected dir.name, got %s", cmp.Left.String())
				}
			},
		},
		{
			name:  "chain of three nodes",
			input: "SELECT c FROM (a:fs:dir)-[:contains]->(b:fs:dir)-[:contains]->(c:fs:file)",
			check: func(t *testing.T, q *Query) {
				ps := q.Select.From.(*PatternSource)
				pat := ps.Patterns[0]
				if len(pat.Elements) != 5 {
					t.Fatalf("expected 5 elements (n-e-n-e-n), got %d", len(pat.Elements))
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			q, err := p.Parse(tt.input)
			if err != nil {
				t.Fatalf("Parse() error = %v", err)
			}
			tt.check(t, q)
		})
	}
}

func TestParser_Exists(t *testing.T) {
	p := NewParser()

	tests := []struct {
		name  string
		input string
		check func(t *testing.T, q *Query)
	}{
		{
			name:  "EXISTS",
			input: "SELECT dir FROM (dir:fs:dir) WHERE EXISTS (dir)-[:contains]->(:fs:file)",
			check: func(t *testing.T, q *Query) {
				ex, ok := q.Select.Where.(*ExistsExpr)
				if !ok {
					t.Fatalf("expected ExistsExpr, got %T", q.Select.Where)
				}
				if ex.Not {
					t.Fatal("expected EXISTS, not NOT EXISTS")
				}
				if len(ex.Pattern.Elements) != 3 {
					t.Fatalf("expected 3 elements, got %d", len(ex.Pattern.Elements))
				}
			},
		},
		{
			name:  "NOT EXISTS",
			input: "SELECT dir FROM (dir:fs:dir) WHERE NOT EXISTS (dir)-[:contains]->(:fs:file WHERE name LIKE '*_test.go')",
			check: func(t *testing.T, q *Query) {
				ex := q.Select.Where.(*ExistsExpr)
				if !ex.Not {
					t.Fatal("expected NOT EXISTS")
				}
				// Check inline WHERE in the EXISTS pattern
				node := ex.Pattern.Elements[2].(*NodePattern)
				if node.Where == nil {
					t.Fatal("expected inline WHERE in EXISTS pattern")
				}
			},
		},
		{
			name:  "EXISTS with AND",
			input: "SELECT dir FROM (dir:fs:dir) WHERE dir.name = 'src' AND EXISTS (dir)-[:contains]->(:fs:file)",
			check: func(t *testing.T, q *Query) {
				bin := q.Select.Where.(*BinaryExpr)
				if bin.Op != OpAnd {
					t.Fatalf("expected AND, got %s", bin.Op)
				}
				_, ok := bin.Right.(*ExistsExpr)
				if !ok {
					t.Fatalf("expected ExistsExpr on right, got %T", bin.Right)
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			q, err := p.Parse(tt.input)
			if err != nil {
				t.Fatalf("Parse() error = %v", err)
			}
			tt.check(t, q)
		})
	}
}

func TestParser_ComplexQueries(t *testing.T) {
	p := NewParser()

	// These should all parse without error
	queries := []string{
		// From grammar.md examples
		`SELECT * FROM nodes WHERE type = 'fs:file' AND data.ext = 'go'`,
		`SELECT type, COUNT(*) FROM nodes GROUP BY type ORDER BY COUNT(*) DESC`,
		`SELECT * FROM nodes WHERE (type = 'fs:file' OR type = 'fs:dir') AND labels CONTAINS ANY ('important', 'reviewed') AND labels NOT CONTAINS ('archived') AND data.size > 1000`,
		`SELECT * FROM nodes WHERE type = $type AND name LIKE $1`,
		`SELECT file FROM (dir:fs:dir)-[:contains]->(file:fs:file) WHERE file.data.ext = 'go'`,
		`SELECT repo.name, branch.name FROM (repo:vcs:repo)-[:has]->(branch:vcs:branch) WHERE branch.name LIKE 'feature%'`,
		`SELECT file FROM (root:fs:dir)-[:contains*]->(file:fs:file) WHERE root.name = 'src'`,
		`SELECT repo, branch, doc FROM (repo:vcs:repo)-[:has]->(branch:vcs:branch), (repo)-[:located_at]->(dir:fs:dir)-[:contains]->(doc:md:document) WHERE branch.name = 'main' AND doc.name = 'README.md'`,
		`SELECT dir FROM (dir:fs:dir) WHERE EXISTS (dir)-[:contains]->(:fs:file WHERE name LIKE '%.go')`,
		`SELECT dir FROM (dir:fs:dir)-[:contains]->(file:fs:file) WHERE file.data.ext = 'go' AND NOT EXISTS (dir)-[:contains]->(:fs:file WHERE name LIKE '*_test.go')`,
		`SELECT repo.name, COUNT(*) FROM (repo:vcs:repo)-[:has]->(branch:vcs:branch) GROUP BY repo.name HAVING COUNT(*) > 5 ORDER BY COUNT(*) DESC LIMIT 10`,
	}

	for _, query := range queries {
		t.Run(query[:min(len(query), 50)], func(t *testing.T) {
			_, err := p.Parse(query)
			if err != nil {
				t.Fatalf("Parse() error = %v", err)
			}
		})
	}
}

func TestParser_Errors(t *testing.T) {
	p := NewParser()

	tests := []struct {
		name  string
		input string
	}{
		{"missing FROM", "SELECT *"},
		{"invalid FROM", "SELECT * FROM foo"},
		{"missing SELECT", "FROM nodes"},
		{"unclosed paren", "SELECT * FROM nodes WHERE (type = 'foo'"},
		{"unclosed string", "SELECT * FROM nodes WHERE type = 'foo"},
		{"invalid operator", "SELECT * FROM nodes WHERE type == 'foo'"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := p.Parse(tt.input)
			if err == nil {
				t.Fatal("expected error")
			}
		})
	}
}

func TestParser_Values(t *testing.T) {
	p := NewParser()

	tests := []struct {
		name  string
		input string
		check func(t *testing.T, v Value)
	}{
		{
			name:  "string",
			input: "SELECT * FROM nodes WHERE name = 'hello'",
			check: func(t *testing.T, v Value) {
				s := v.(*StringLit)
				if s.Value != "hello" {
					t.Fatalf("expected hello, got %s", s.Value)
				}
			},
		},
		{
			name:  "string with escaped quote",
			input: "SELECT * FROM nodes WHERE name = 'it''s'",
			check: func(t *testing.T, v Value) {
				s := v.(*StringLit)
				if s.Value != "it's" {
					t.Fatalf("expected it's, got %s", s.Value)
				}
			},
		},
		{
			name:  "integer",
			input: "SELECT * FROM nodes WHERE data.size = 1000",
			check: func(t *testing.T, v Value) {
				n := v.(*NumberLit)
				if n.Value != 1000 || !n.IsInt {
					t.Fatalf("expected int 1000, got %f (isInt=%v)", n.Value, n.IsInt)
				}
			},
		},
		{
			name:  "float",
			input: "SELECT * FROM nodes WHERE data.score = 3.14",
			check: func(t *testing.T, v Value) {
				n := v.(*NumberLit)
				if n.Value != 3.14 || n.IsInt {
					t.Fatalf("expected float 3.14, got %f (isInt=%v)", n.Value, n.IsInt)
				}
			},
		},
		{
			name:  "true",
			input: "SELECT * FROM nodes WHERE data.active = true",
			check: func(t *testing.T, v Value) {
				b := v.(*BoolLit)
				if !b.Value {
					t.Fatal("expected true")
				}
			},
		},
		{
			name:  "false",
			input: "SELECT * FROM nodes WHERE data.active = FALSE",
			check: func(t *testing.T, v Value) {
				b := v.(*BoolLit)
				if b.Value {
					t.Fatal("expected false")
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			q, err := p.Parse(tt.input)
			if err != nil {
				t.Fatalf("Parse() error = %v", err)
			}
			cmp := q.Select.Where.(*ComparisonExpr)
			tt.check(t, cmp.Right)
		})
	}
}
