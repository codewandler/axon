package aql

import "testing"

func TestPosition_String(t *testing.T) {
	tests := []struct {
		name string
		pos  Position
		want string
	}{
		{"zero", Position{}, ""},
		{"valid", Position{Line: 1, Column: 5}, "1:5"},
		{"line only", Position{Line: 10}, "10:0"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.pos.String(); got != tt.want {
				t.Errorf("Position.String() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestSelector_String(t *testing.T) {
	tests := []struct {
		name  string
		parts []string
		want  string
	}{
		{"empty", nil, ""},
		{"single", []string{"name"}, "name"},
		{"two parts", []string{"data", "ext"}, "data.ext"},
		{"three parts", []string{"file", "data", "size"}, "file.data.size"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := &Selector{Parts: tt.parts}
			if got := s.String(); got != tt.want {
				t.Errorf("Selector.String() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestDirection_String(t *testing.T) {
	tests := []struct {
		dir  Direction
		want string
	}{
		{Outgoing, "->"},
		{Incoming, "<-"},
		{Undirected, "-"},
		{Direction(99), "?"},
	}

	for _, tt := range tests {
		t.Run(tt.want, func(t *testing.T) {
			if got := tt.dir.String(); got != tt.want {
				t.Errorf("Direction.String() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestBinaryOp_String(t *testing.T) {
	tests := []struct {
		op   BinaryOp
		want string
	}{
		{OpAnd, "AND"},
		{OpOr, "OR"},
		{BinaryOp(99), "?"},
	}

	for _, tt := range tests {
		t.Run(tt.want, func(t *testing.T) {
			if got := tt.op.String(); got != tt.want {
				t.Errorf("BinaryOp.String() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestUnaryOp_String(t *testing.T) {
	tests := []struct {
		op   UnaryOp
		want string
	}{
		{OpNot, "NOT"},
		{UnaryOp(99), "?"},
	}

	for _, tt := range tests {
		t.Run(tt.want, func(t *testing.T) {
			if got := tt.op.String(); got != tt.want {
				t.Errorf("UnaryOp.String() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestComparisonOp_String(t *testing.T) {
	tests := []struct {
		op   ComparisonOp
		want string
	}{
		{OpEq, "="},
		{OpNe, "!="},
		{OpLt, "<"},
		{OpLe, "<="},
		{OpGt, ">"},
		{OpGe, ">="},
		{OpLike, "LIKE"},
		{OpGlob, "GLOB"},
		{ComparisonOp(99), "?"},
	}

	for _, tt := range tests {
		t.Run(tt.want, func(t *testing.T) {
			if got := tt.op.String(); got != tt.want {
				t.Errorf("ComparisonOp.String() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestLabelOp_String(t *testing.T) {
	tests := []struct {
		op   LabelOp
		want string
	}{
		{OpContainsAny, "CONTAINS ANY"},
		{OpContainsAll, "CONTAINS ALL"},
		{OpNotContains, "NOT CONTAINS"},
		{OpNotContainsAny, "NOT CONTAINS ANY"},
		{OpNotContainsAll, "NOT CONTAINS ALL"},
		{LabelOp(99), "?"},
	}

	for _, tt := range tests {
		t.Run(tt.want, func(t *testing.T) {
			if got := tt.op.String(); got != tt.want {
				t.Errorf("LabelOp.String() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestParameter_String(t *testing.T) {
	tests := []struct {
		name  string
		param Parameter
		want  string
	}{
		{"named", Parameter{Name: "type"}, "$type"},
		{"positional", Parameter{Index: 1}, "$1"},
		{"positional high", Parameter{Index: 42}, "$42"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.param.String(); got != tt.want {
				t.Errorf("Parameter.String() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestParameter_IsNamed(t *testing.T) {
	if !(&Parameter{Name: "foo"}).IsNamed() {
		t.Error("expected IsNamed() = true for named param")
	}
	if (&Parameter{Index: 1}).IsNamed() {
		t.Error("expected IsNamed() = false for positional param")
	}
}

func TestParameter_IsPositional(t *testing.T) {
	if !(&Parameter{Index: 1}).IsPositional() {
		t.Error("expected IsPositional() = true for positional param")
	}
	if (&Parameter{Name: "foo"}).IsPositional() {
		t.Error("expected IsPositional() = false for named param")
	}
}

func TestNumberLit_IntValue(t *testing.T) {
	tests := []struct {
		value float64
		want  int
	}{
		{42.0, 42},
		{3.14, 3},
		{0.0, 0},
		{-5.9, -5},
	}

	for _, tt := range tests {
		n := &NumberLit{Value: tt.value}
		if got := n.IntValue(); got != tt.want {
			t.Errorf("NumberLit{%v}.IntValue() = %d, want %d", tt.value, got, tt.want)
		}
	}
}

func TestEdgePattern_IsVariableLength(t *testing.T) {
	one := 1
	tests := []struct {
		name    string
		minHops *int
		want    bool
	}{
		{"nil", nil, false},
		{"set", &one, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := &EdgePattern{MinHops: tt.minHops}
			if got := e.IsVariableLength(); got != tt.want {
				t.Errorf("IsVariableLength() = %v, want %v", got, tt.want)
			}
		})
	}
}
