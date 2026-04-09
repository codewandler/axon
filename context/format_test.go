package context

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/codewandler/axon/graph"
)

func TestFormatText(t *testing.T) {
	result := &FitResult{
		Task:        "add caching to Storage",
		UsedTokens:  500,
		TotalBudget: 1000,
		Summary: Summary{
			FileCount:     2,
			SymbolCount:   3,
			Symbols:       []string{"Storage", "Cache", "Flush"},
			TruncateCount: 1,
			ExcludeCount:  2,
		},
		Blocks: []SourceBlock{
			{
				File:      "/home/user/project/graph/storage.go",
				StartLine: 10,
				EndLine:   30,
				Content:   "type Storage interface {\n\tGet(id string) error\n}",
				Tokens:    20,
				MaxScore:  100,
				Reason:    "definition of Storage",
			},
		},
		Excluded: []ExcludedInfo{
			{
				File:   "/home/user/project/other.go",
				Tokens: 200,
				Score:  40,
				Reason: "low priority",
			},
		},
	}

	output := Format(result, FormatText)

	// Check essential elements
	if !strings.Contains(output, "## Context for:") {
		t.Error("expected header")
	}
	if !strings.Contains(output, "add caching to Storage") {
		t.Error("expected task description")
	}
	if !strings.Contains(output, "2 files") {
		t.Error("expected file count")
	}
	if !strings.Contains(output, "500 tokens") {
		t.Error("expected token count")
	}
	if !strings.Contains(output, "Storage, Cache, Flush") {
		t.Error("expected symbols")
	}
	if !strings.Contains(output, "```go") {
		t.Error("expected code block")
	}
	if !strings.Contains(output, "definition of Storage") {
		t.Error("expected reason")
	}
	if !strings.Contains(output, "Excluded") {
		t.Error("expected excluded section")
	}
}

func TestFormatJSON(t *testing.T) {
	result := &FitResult{
		Task:        "fix Storage bug",
		UsedTokens:  300,
		TotalBudget: 500,
		Summary: Summary{
			FileCount:   1,
			SymbolCount: 2,
			Symbols:     []string{"Storage", "Get"},
		},
		Blocks: []SourceBlock{
			{
				File:      "/test/storage.go",
				StartLine: 1,
				EndLine:   10,
				Content:   "func Get() {}",
				Tokens:    10,
				MaxScore:  100,
				Items:     []RelevanceItem{{Node: &graph.Node{Name: "Get"}}},
				Reason:    "definition",
			},
		},
	}

	output := Format(result, FormatJSON)

	// Should be valid JSON
	var parsed JSONOutput
	if err := json.Unmarshal([]byte(output), &parsed); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}

	if parsed.Task != "fix Storage bug" {
		t.Errorf("expected task 'fix Storage bug', got %q", parsed.Task)
	}
	if parsed.Budget != 500 {
		t.Errorf("expected budget 500, got %d", parsed.Budget)
	}
	if parsed.Used != 300 {
		t.Errorf("expected used 300, got %d", parsed.Used)
	}
	if len(parsed.Blocks) != 1 {
		t.Errorf("expected 1 block, got %d", len(parsed.Blocks))
	}
	if parsed.Blocks[0].Content != "func Get() {}" {
		t.Errorf("unexpected content: %q", parsed.Blocks[0].Content)
	}
}

func TestFormatManifest(t *testing.T) {
	result := &FitResult{
		Task:        "test manifest",
		UsedTokens:  100,
		TotalBudget: 200,
		Blocks: []SourceBlock{
			{
				File:      "/test/a.go",
				StartLine: 1,
				EndLine:   10,
				Tokens:    50,
				Reason:    "definition",
			},
			{
				File:      "/test/b.go",
				StartLine: 5,
				EndLine:   15,
				Tokens:    50,
				Reason:    "caller",
			},
		},
		Excluded: []ExcludedInfo{
			{File: "/test/c.go", Tokens: 100, Reason: "excluded"},
		},
	}

	output := FormatManifest(result)

	// Should contain table headers
	if !strings.Contains(output, "| File |") {
		t.Error("expected table header")
	}
	// Should contain files
	if !strings.Contains(output, "a.go") {
		t.Error("expected a.go in output")
	}
	if !strings.Contains(output, "b.go") {
		t.Error("expected b.go in output")
	}
	// Should contain excluded section
	if !strings.Contains(output, "Excluded") {
		t.Error("expected Excluded section")
	}
	if !strings.Contains(output, "c.go") {
		t.Error("expected c.go in excluded")
	}
	// Should NOT contain actual source code
	if strings.Contains(output, "func ") {
		t.Error("manifest should not contain source code")
	}
}

func TestShortenPath(t *testing.T) {
	tests := []struct {
		path string
		want string
	}{
		{
			// Path outside CWD: falls back to last 3 components
			"/home/user/go/src/myproject/pkg/storage/storage.go",
			"pkg/storage/storage.go",
		},
		{
			// Path outside CWD: last 3 components happen to be "internal/db/db.go"
			"/home/user/project/internal/db/db.go",
			"internal/db/db.go",
		},
		{
			// Path outside CWD: last 3 components
			"/home/user/go/pkg/mod/github.com/example/repo/file.go",
			"example/repo/file.go",
		},
		{
			"/short/path.go",
			"/short/path.go", // Only 2 parts, returned as-is
		},
	}

	for _, tt := range tests {
		got := shortenPath(tt.path)
		if got != tt.want {
			t.Errorf("shortenPath(%q) = %q, want %q", tt.path, got, tt.want)
		}
	}
}

func TestFormatEmpty(t *testing.T) {
	result := &FitResult{
		Task:        "empty task",
		TotalBudget: 1000,
	}

	// Should not panic on empty result
	textOutput := Format(result, FormatText)
	if textOutput == "" {
		t.Error("expected non-empty text output")
	}

	jsonOutput := Format(result, FormatJSON)
	if jsonOutput == "" {
		t.Error("expected non-empty JSON output")
	}

	// JSON should be valid
	var parsed JSONOutput
	if err := json.Unmarshal([]byte(jsonOutput), &parsed); err != nil {
		t.Errorf("invalid JSON for empty result: %v", err)
	}
}
