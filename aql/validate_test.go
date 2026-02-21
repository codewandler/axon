package aql

import (
	"strings"
	"testing"
)

func TestValidate_Valid(t *testing.T) {
	p := NewParser()

	// All these queries should be valid
	queries := []string{
		"SELECT * FROM nodes",
		"SELECT * FROM edges",
		"SELECT name, type FROM nodes WHERE type = 'fs:file'",
		"SELECT type, COUNT(*) FROM nodes GROUP BY type",
		"SELECT type, COUNT(*) FROM nodes GROUP BY type HAVING COUNT(*) > 10",
		"SELECT n FROM (n:fs:file)",
		"SELECT file FROM (dir:fs:dir)-[:contains]->(file:fs:file)",
		"SELECT repo.name, branch.name FROM (repo:vcs:repo)-[:has]->(branch:vcs:branch)",
		"SELECT a, b FROM (a:fs:dir)-[:contains]->(b:fs:dir), (b)-[:contains]->(c:fs:file)",
		"SELECT dir FROM (dir:fs:dir) WHERE EXISTS (dir)-[:contains]->(:fs:file)",
	}

	for _, query := range queries {
		t.Run(query[:minLen(len(query), 40)], func(t *testing.T) {
			q, err := p.Parse(query)
			if err != nil {
				t.Fatalf("Parse error: %v", err)
			}

			errs := Validate(q)
			if len(errs) > 0 {
				t.Fatalf("Unexpected validation errors: %v", errs)
			}
		})
	}
}

func TestValidate_Errors(t *testing.T) {
	p := NewParser()

	tests := []struct {
		name    string
		query   string
		wantErr string
	}{
		{
			name:    "HAVING without GROUP BY",
			query:   "SELECT * FROM nodes HAVING COUNT(*) > 10",
			wantErr: "HAVING requires GROUP BY",
		},
		{
			name:    "no variable in pattern",
			query:   "SELECT * FROM (:fs:file)",
			wantErr: "at least one variable",
		},
		{
			name:    "undefined variable in SELECT",
			query:   "SELECT x FROM (n:fs:file)",
			wantErr: "undefined variable 'x'",
		},
		{
			name:    "duplicate variable",
			query:   "SELECT n FROM (n:fs:file)-[:contains]->(n:fs:dir)",
			wantErr: "already defined",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			q, err := p.Parse(tt.query)
			if err != nil {
				t.Fatalf("Parse error: %v", err)
			}

			errs := Validate(q)
			if len(errs) == 0 {
				t.Fatal("Expected validation error, got none")
			}

			found := false
			for _, e := range errs {
				if strings.Contains(e.Message, tt.wantErr) {
					found = true
					break
				}
			}
			if !found {
				t.Fatalf("Expected error containing %q, got: %v", tt.wantErr, errs)
			}
		})
	}
}

func TestValidate_VariableScoping(t *testing.T) {
	p := NewParser()

	// Variables from outer scope can be used in EXISTS
	query := "SELECT dir FROM (dir:fs:dir) WHERE EXISTS (dir)-[:contains]->(:fs:file)"
	q, err := p.Parse(query)
	if err != nil {
		t.Fatalf("Parse error: %v", err)
	}

	errs := Validate(q)
	if len(errs) > 0 {
		t.Fatalf("Unexpected validation errors: %v", errs)
	}
}

func minLen(a, b int) int {
	if a < b {
		return a
	}
	return b
}
