package aql

import (
	"testing"
)

func TestQueryString(t *testing.T) {
	tests := []struct {
		name  string
		query *Query
		want  string
	}{
		{
			name: "simple SELECT *",
			query: &Query{Select: &SelectStmt{
				Columns: []Column{{Expr: &Star{}}},
				From:    &TableSource{Table: "nodes"},
			}},
			want: "SELECT * FROM nodes",
		},
		{
			name: "SELECT with WHERE",
			query: &Query{Select: &SelectStmt{
				Columns: []Column{{Expr: &Star{}}},
				From:    &TableSource{Table: "nodes"},
				Where: &ComparisonExpr{
					Left:  &Selector{Parts: []string{"type"}},
					Op:    OpEq,
					Right: &StringLit{Value: "fs:file"},
				},
			}},
			want: "SELECT * FROM nodes WHERE type = 'fs:file'",
		},
		{
			name: "SELECT with LIMIT",
			query: &Query{Select: &SelectStmt{
				Columns: []Column{{Expr: &Star{}}},
				From:    &TableSource{Table: "nodes"},
				Limit:   intPtr(10),
			}},
			want: "SELECT * FROM nodes LIMIT 10",
		},
		{
			name: "SELECT with ORDER BY",
			query: &Query{Select: &SelectStmt{
				Columns: []Column{{Expr: &Star{}}},
				From:    &TableSource{Table: "nodes"},
				OrderBy: []OrderSpec{
					{Expr: &Selector{Parts: []string{"name"}}, Descending: false},
				},
			}},
			want: "SELECT * FROM nodes ORDER BY name",
		},
		{
			name: "SELECT with ORDER BY DESC",
			query: &Query{Select: &SelectStmt{
				Columns: []Column{{Expr: &Star{}}},
				From:    &TableSource{Table: "nodes"},
				OrderBy: []OrderSpec{
					{Expr: &Selector{Parts: []string{"name"}}, Descending: true},
				},
			}},
			want: "SELECT * FROM nodes ORDER BY name DESC",
		},
		{
			name: "SELECT specific columns",
			query: &Query{Select: &SelectStmt{
				Columns: []Column{
					{Expr: &Selector{Parts: []string{"name"}}},
					{Expr: &Selector{Parts: []string{"type"}}},
				},
				From: &TableSource{Table: "nodes"},
			}},
			want: "SELECT name, type FROM nodes",
		},
		{
			name: "SELECT with column alias",
			query: &Query{Select: &SelectStmt{
				Columns: []Column{
					{Expr: &Selector{Parts: []string{"name"}}, Alias: "node_name"},
				},
				From: &TableSource{Table: "nodes"},
			}},
			want: "SELECT name AS node_name FROM nodes",
		},
		{
			name: "SELECT COUNT(*)",
			query: &Query{Select: &SelectStmt{
				Columns: []Column{{Expr: &CountCall{}}},
				From:    &TableSource{Table: "nodes"},
			}},
			want: "SELECT COUNT(*) FROM nodes",
		},
		{
			name: "SELECT DISTINCT",
			query: &Query{Select: &SelectStmt{
				Distinct: true,
				Columns:  []Column{{Expr: &Selector{Parts: []string{"type"}}}},
				From:     &TableSource{Table: "nodes"},
			}},
			want: "SELECT DISTINCT type FROM nodes",
		},
		{
			name: "SELECT with GROUP BY",
			query: &Query{Select: &SelectStmt{
				Columns: []Column{
					{Expr: &Selector{Parts: []string{"type"}}},
					{Expr: &CountCall{}},
				},
				From:    &TableSource{Table: "nodes"},
				GroupBy: []Selector{{Parts: []string{"type"}}},
			}},
			want: "SELECT type, COUNT(*) FROM nodes GROUP BY type",
		},
		{
			name: "SELECT with OFFSET",
			query: &Query{Select: &SelectStmt{
				Columns: []Column{{Expr: &Star{}}},
				From:    &TableSource{Table: "nodes"},
				Limit:   intPtr(10),
				Offset:  intPtr(20),
			}},
			want: "SELECT * FROM nodes LIMIT 10 OFFSET 20",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.query.String()
			if got != tt.want {
				t.Errorf("Query.String() =\n%q\nwant\n%q", got, tt.want)
			}
		})
	}
}

func TestExpressionString(t *testing.T) {
	tests := []struct {
		name string
		expr Expression
		want string
	}{
		{
			name: "simple comparison",
			expr: &ComparisonExpr{
				Left:  &Selector{Parts: []string{"type"}},
				Op:    OpEq,
				Right: &StringLit{Value: "fs:file"},
			},
			want: "type = 'fs:file'",
		},
		{
			name: "AND expression",
			expr: &BinaryExpr{
				Left: &ComparisonExpr{
					Left:  &Selector{Parts: []string{"type"}},
					Op:    OpEq,
					Right: &StringLit{Value: "fs:file"},
				},
				Op: OpAnd,
				Right: &ComparisonExpr{
					Left:  &Selector{Parts: []string{"data", "ext"}},
					Op:    OpEq,
					Right: &StringLit{Value: ".go"},
				},
			},
			want: "type = 'fs:file' AND data.ext = '.go'",
		},
		{
			name: "OR expression",
			expr: &BinaryExpr{
				Left: &ComparisonExpr{
					Left:  &Selector{Parts: []string{"type"}},
					Op:    OpEq,
					Right: &StringLit{Value: "fs:file"},
				},
				Op: OpOr,
				Right: &ComparisonExpr{
					Left:  &Selector{Parts: []string{"type"}},
					Op:    OpEq,
					Right: &StringLit{Value: "fs:dir"},
				},
			},
			want: "type = 'fs:file' OR type = 'fs:dir'",
		},
		{
			name: "NOT expression",
			expr: &UnaryExpr{
				Op: OpNot,
				Operand: &ComparisonExpr{
					Left:  &Selector{Parts: []string{"type"}},
					Op:    OpEq,
					Right: &StringLit{Value: "fs:file"},
				},
			},
			want: "NOT type = 'fs:file'",
		},
		{
			name: "GLOB expression",
			expr: &ComparisonExpr{
				Left:  &Selector{Parts: []string{"type"}},
				Op:    OpGlob,
				Right: &StringLit{Value: "fs:*"},
			},
			want: "type GLOB 'fs:*'",
		},
		{
			name: "LIKE expression",
			expr: &ComparisonExpr{
				Left:  &Selector{Parts: []string{"name"}},
				Op:    OpLike,
				Right: &StringLit{Value: "%test%"},
			},
			want: "name LIKE '%test%'",
		},
		{
			name: "IN expression",
			expr: &InExpr{
				Left: &Selector{Parts: []string{"type"}},
				Values: []Value{
					&StringLit{Value: "fs:file"},
					&StringLit{Value: "fs:dir"},
				},
			},
			want: "type IN ('fs:file', 'fs:dir')",
		},
		{
			name: "BETWEEN expression",
			expr: &BetweenExpr{
				Left: &Selector{Parts: []string{"data", "size"}},
				Low:  &NumberLit{Value: 100, IsInt: true},
				High: &NumberLit{Value: 1000, IsInt: true},
			},
			want: "data.size BETWEEN 100 AND 1000",
		},
		{
			name: "IS NULL expression",
			expr: &IsNullExpr{
				Selector: &Selector{Parts: []string{"name"}},
				Not:      false,
			},
			want: "name IS NULL",
		},
		{
			name: "IS NOT NULL expression",
			expr: &IsNullExpr{
				Selector: &Selector{Parts: []string{"name"}},
				Not:      true,
			},
			want: "name IS NOT NULL",
		},
		{
			name: "CONTAINS ANY expression",
			expr: &LabelExpr{
				Selector: &Selector{Parts: []string{"labels"}},
				Op:       OpContainsAny,
				Labels: []Value{
					&StringLit{Value: "test:file"},
				},
			},
			want: "labels CONTAINS ANY ('test:file')",
		},
		{
			name: "CONTAINS ALL expression",
			expr: &LabelExpr{
				Selector: &Selector{Parts: []string{"labels"}},
				Op:       OpContainsAll,
				Labels: []Value{
					&StringLit{Value: "test:file"},
					&StringLit{Value: "build:config"},
				},
			},
			want: "labels CONTAINS ALL ('test:file', 'build:config')",
		},
		{
			name: "Parenthesized expression",
			expr: &ParenExpr{
				Inner: &ComparisonExpr{
					Left:  &Selector{Parts: []string{"type"}},
					Op:    OpEq,
					Right: &StringLit{Value: "fs:file"},
				},
			},
			want: "(type = 'fs:file')",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := expressionString(tt.expr)
			if got != tt.want {
				t.Errorf("expressionString() =\n%q\nwant\n%q", got, tt.want)
			}
		})
	}
}

func TestValueString(t *testing.T) {
	tests := []struct {
		name  string
		value Value
		want  string
	}{
		{
			name:  "string literal",
			value: &StringLit{Value: "hello"},
			want:  "'hello'",
		},
		{
			name:  "string literal with quotes",
			value: &StringLit{Value: "it's"},
			want:  "'it''s'",
		},
		{
			name:  "integer",
			value: &NumberLit{Value: 42, IsInt: true},
			want:  "42",
		},
		{
			name:  "float",
			value: &NumberLit{Value: 3.14, IsInt: false},
			want:  "3.14",
		},
		{
			name:  "boolean true",
			value: &BoolLit{Value: true},
			want:  "TRUE",
		},
		{
			name:  "boolean false",
			value: &BoolLit{Value: false},
			want:  "FALSE",
		},
		{
			name:  "named parameter",
			value: &Parameter{Name: "foo"},
			want:  "$foo",
		},
		{
			name:  "positional parameter",
			value: &Parameter{Index: 1},
			want:  "$1",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := valueString(tt.value)
			if got != tt.want {
				t.Errorf("valueString() =\n%q\nwant\n%q", got, tt.want)
			}
		})
	}
}

func TestPatternString(t *testing.T) {
	tests := []struct {
		name    string
		pattern *Pattern
		want    string
	}{
		{
			name: "simple node pattern",
			pattern: &Pattern{
				Elements: []PatternElement{
					&NodePattern{Variable: "n", Type: "fs:file"},
				},
			},
			want: "(n:fs:file)",
		},
		{
			name: "node pattern with type only",
			pattern: &Pattern{
				Elements: []PatternElement{
					&NodePattern{Type: "fs:file"},
				},
			},
			want: "(:fs:file)",
		},
		{
			name: "simple edge pattern",
			pattern: &Pattern{
				Elements: []PatternElement{
					&NodePattern{Variable: "a"},
					&EdgePattern{Type: "contains", Direction: Outgoing},
					&NodePattern{Variable: "b"},
				},
			},
			want: "(a)-[:contains]->(b)",
		},
		{
			name: "edge pattern with multiple types",
			pattern: &Pattern{
				Elements: []PatternElement{
					&NodePattern{Variable: "a"},
					&EdgePattern{Types: []string{"contains", "has"}, Direction: Outgoing},
					&NodePattern{Variable: "b"},
				},
			},
			want: "(a)-[:contains|has]->(b)",
		},
		{
			name: "incoming edge",
			pattern: &Pattern{
				Elements: []PatternElement{
					&NodePattern{Variable: "a"},
					&EdgePattern{Type: "contains", Direction: Incoming},
					&NodePattern{Variable: "b"},
				},
			},
			want: "(a)<-[:contains]-(b)",
		},
		{
			name: "undirected edge",
			pattern: &Pattern{
				Elements: []PatternElement{
					&NodePattern{Variable: "a"},
					&EdgePattern{Type: "related", Direction: Undirected},
					&NodePattern{Variable: "b"},
				},
			},
			want: "(a)-[:related]-(b)",
		},
		{
			name: "variable-length edge",
			pattern: &Pattern{
				Elements: []PatternElement{
					&NodePattern{Variable: "a"},
					&EdgePattern{Type: "contains", Direction: Outgoing, MinHops: intPtr(1), MaxHops: intPtr(3)},
					&NodePattern{Variable: "b"},
				},
			},
			want: "(a)-[:contains*1..3]->(b)",
		},
		{
			name: "variable-length edge unbounded",
			pattern: &Pattern{
				Elements: []PatternElement{
					&NodePattern{Variable: "a"},
					&EdgePattern{Type: "contains", Direction: Outgoing, MinHops: intPtr(2)},
					&NodePattern{Variable: "b"},
				},
			},
			want: "(a)-[:contains*2..]->(b)",
		},
		{
			name: "edge with variable",
			pattern: &Pattern{
				Elements: []PatternElement{
					&NodePattern{Variable: "a"},
					&EdgePattern{Variable: "e", Type: "contains", Direction: Outgoing},
					&NodePattern{Variable: "b"},
				},
			},
			want: "(a)-[e:contains]->(b)",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.pattern.String()
			if got != tt.want {
				t.Errorf("Pattern.String() =\n%q\nwant\n%q", got, tt.want)
			}
		})
	}
}

func TestComplexQueries(t *testing.T) {
	tests := []struct {
		name  string
		query *Query
		want  string
	}{
		{
			name: "complex WHERE with AND/OR",
			query: &Query{Select: &SelectStmt{
				Columns: []Column{{Expr: &Star{}}},
				From:    &TableSource{Table: "nodes"},
				Where: &BinaryExpr{
					Left: &ComparisonExpr{
						Left:  &Selector{Parts: []string{"type"}},
						Op:    OpGlob,
						Right: &StringLit{Value: "fs:*"},
					},
					Op: OpAnd,
					Right: &BinaryExpr{
						Left: &ComparisonExpr{
							Left:  &Selector{Parts: []string{"data", "ext"}},
							Op:    OpEq,
							Right: &StringLit{Value: ".go"},
						},
						Op: OpOr,
						Right: &ComparisonExpr{
							Left:  &Selector{Parts: []string{"data", "ext"}},
							Op:    OpEq,
							Right: &StringLit{Value: ".md"},
						},
					},
				},
				Limit: intPtr(10),
			}},
			want: "SELECT * FROM nodes WHERE type GLOB 'fs:*' AND data.ext = '.go' OR data.ext = '.md' LIMIT 10",
		},
		{
			name: "query with GROUP BY and ORDER BY",
			query: &Query{Select: &SelectStmt{
				Columns: []Column{
					{Expr: &Selector{Parts: []string{"type"}}},
					{Expr: &CountCall{}, Alias: "count"},
				},
				From: &TableSource{Table: "nodes"},
				Where: &ComparisonExpr{
					Left:  &Selector{Parts: []string{"type"}},
					Op:    OpGlob,
					Right: &StringLit{Value: "fs:*"},
				},
				GroupBy: []Selector{{Parts: []string{"type"}}},
				OrderBy: []OrderSpec{
					{Expr: &CountCall{}, Descending: true},
				},
				Limit: intPtr(5),
			}},
			want: "SELECT type, COUNT(*) AS count FROM nodes WHERE type GLOB 'fs:*' GROUP BY type ORDER BY COUNT(*) DESC LIMIT 5",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.query.String()
			if got != tt.want {
				t.Errorf("Query.String() =\n%q\nwant\n%q", got, tt.want)
			}
		})
	}
}

// Helper function to create int pointers
func intPtr(i int) *int {
	return &i
}
