package context

import (
	"strings"
	"testing"

	"github.com/codewandler/axon/graph"
)

func TestFitBudget_AllFit(t *testing.T) {
	counter, _ := NewTokenCounter()

	blocks := []SourceBlock{
		{
			File:      "/test/a.go",
			StartLine: 1,
			EndLine:   5,
			Content:   "func A() {}",
			Tokens:    10,
			MaxScore:  100,
			Items:     []RelevanceItem{{Node: &graph.Node{Name: "A"}}},
			Reason:    "definition of A",
		},
		{
			File:      "/test/b.go",
			StartLine: 1,
			EndLine:   5,
			Content:   "func B() {}",
			Tokens:    10,
			MaxScore:  80,
			Items:     []RelevanceItem{{Node: &graph.Node{Name: "B"}}},
			Reason:    "definition of B",
		},
	}

	result := FitBudget(blocks, 1000, "test task", counter)

	if len(result.Blocks) != 2 {
		t.Errorf("expected 2 blocks, got %d", len(result.Blocks))
	}
	if len(result.Excluded) != 0 {
		t.Errorf("expected 0 excluded, got %d", len(result.Excluded))
	}
	if result.UsedTokens != 20 {
		t.Errorf("expected 20 used tokens, got %d", result.UsedTokens)
	}
}

func TestFitBudget_SomeExcluded(t *testing.T) {
	counter, _ := NewTokenCounter()

	blocks := []SourceBlock{
		{
			File:     "/test/a.go",
			Content:  strings.Repeat("x", 1000), // ~250 tokens
			Tokens:   250,
			MaxScore: 100,
			Items:    []RelevanceItem{{Node: &graph.Node{Name: "A"}}},
		},
		{
			File:     "/test/b.go",
			Content:  strings.Repeat("x", 1000),
			Tokens:   250,
			MaxScore: 80,
			Items:    []RelevanceItem{{Node: &graph.Node{Name: "B"}}},
		},
		{
			File:     "/test/c.go",
			Content:  strings.Repeat("x", 1000),
			Tokens:   250,
			MaxScore: 40, // Low priority, should be excluded
			Items:    []RelevanceItem{{Node: &graph.Node{Name: "C"}}},
		},
	}

	// Budget only fits 2 blocks (500 tokens available after header reserve)
	result := FitBudget(blocks, 700, "test task", counter)

	if len(result.Blocks) != 2 {
		t.Errorf("expected 2 blocks, got %d", len(result.Blocks))
	}
	if len(result.Excluded) != 1 {
		t.Errorf("expected 1 excluded, got %d", len(result.Excluded))
	}
	if result.Excluded[0].File != "/test/c.go" {
		t.Errorf("expected c.go to be excluded, got %s", result.Excluded[0].File)
	}
}

func TestFitBudget_HighPriorityTruncated(t *testing.T) {
	counter, _ := NewTokenCounter()

	largeContent := strings.Repeat("line\n", 100) // ~100 lines

	blocks := []SourceBlock{
		{
			File:      "/test/a.go",
			StartLine: 1,
			EndLine:   100,
			Content:   largeContent,
			Tokens:    counter.Count(largeContent),
			MaxScore:  100, // High priority - will be truncated
			Items:     []RelevanceItem{{Node: &graph.Node{Name: "A"}}},
			Reason:    "definition of A",
		},
	}

	// Very small budget - should truncate
	result := FitBudget(blocks, 350, "test task", counter)

	if len(result.Blocks) != 1 {
		t.Fatalf("expected 1 block (truncated), got %d", len(result.Blocks))
	}
	if len(result.Truncated) != 1 {
		t.Errorf("expected 1 truncated, got %d", len(result.Truncated))
	}
	if result.Blocks[0].Tokens >= blocks[0].Tokens {
		t.Errorf("expected truncated block to have fewer tokens")
	}
}

func TestFitBudget_SortsByFilePath(t *testing.T) {
	counter, _ := NewTokenCounter()

	blocks := []SourceBlock{
		{File: "/z/file.go", Content: "a", Tokens: 1, MaxScore: 80},
		{File: "/a/file.go", Content: "b", Tokens: 1, MaxScore: 100},
		{File: "/m/file.go", Content: "c", Tokens: 1, MaxScore: 90},
	}

	result := FitBudget(blocks, 1000, "test", counter)

	// Should be sorted by file path in output
	if result.Blocks[0].File != "/a/file.go" {
		t.Errorf("expected first block to be /a/file.go, got %s", result.Blocks[0].File)
	}
	if result.Blocks[1].File != "/m/file.go" {
		t.Errorf("expected second block to be /m/file.go, got %s", result.Blocks[1].File)
	}
	if result.Blocks[2].File != "/z/file.go" {
		t.Errorf("expected third block to be /z/file.go, got %s", result.Blocks[2].File)
	}
}

func TestFitBudget_Summary(t *testing.T) {
	counter, _ := NewTokenCounter()

	blocks := []SourceBlock{
		{
			File:     "/test/a.go",
			Content:  "func A() {}",
			Tokens:   10,
			MaxScore: 100,
			Items: []RelevanceItem{
				{Node: &graph.Node{Name: "A"}},
				{Node: &graph.Node{Name: "B"}},
			},
		},
		{
			File:     "/test/c.go",
			Content:  "func C() {}",
			Tokens:   10,
			MaxScore: 80,
			Items: []RelevanceItem{
				{Node: &graph.Node{Name: "C"}},
			},
		},
	}

	result := FitBudget(blocks, 1000, "test task", counter)

	if result.Summary.FileCount != 2 {
		t.Errorf("expected 2 files, got %d", result.Summary.FileCount)
	}
	if result.Summary.SymbolCount != 3 {
		t.Errorf("expected 3 symbols, got %d", result.Summary.SymbolCount)
	}
	if len(result.Summary.Symbols) != 3 {
		t.Errorf("expected 3 symbol names, got %d", len(result.Summary.Symbols))
	}
}

func TestFitBudget_Empty(t *testing.T) {
	counter, _ := NewTokenCounter()

	result := FitBudget(nil, 1000, "test", counter)

	if result == nil {
		t.Fatal("expected non-nil result")
	}
	if len(result.Blocks) != 0 {
		t.Errorf("expected 0 blocks, got %d", len(result.Blocks))
	}
	if result.TotalBudget != 1000 {
		t.Errorf("expected budget 1000, got %d", result.TotalBudget)
	}
}

func TestTruncateToSignature(t *testing.T) {
	counter, _ := NewTokenCounter()

	content := `// Hello is a greeting function.
func Hello(name string) error {
	fmt.Println("Hello,", name)
	return nil
}
`
	block := SourceBlock{
		File:      "/test.go",
		StartLine: 1,
		EndLine:   5,
		Content:   content,
		Tokens:    counter.Count(content),
		MaxScore:  60,
	}

	truncated := truncateToSignature(block, counter)

	// Should keep signature and indicate truncation
	if !strings.Contains(truncated.Content, "func Hello") {
		t.Error("expected truncated content to contain function signature")
	}
	if !strings.Contains(truncated.Content, "lines)") {
		t.Error("expected truncated content to indicate omitted lines")
	}
	if truncated.Tokens >= block.Tokens {
		t.Errorf("expected truncated to have fewer tokens: %d >= %d",
			truncated.Tokens, block.Tokens)
	}
}
