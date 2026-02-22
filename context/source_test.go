package context

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/codewandler/axon/graph"
)

func createTestFile(t *testing.T, content string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "test.go")
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatalf("failed to write test file: %v", err)
	}
	return path
}

func TestReadSources_Basic(t *testing.T) {
	content := `package main

import "fmt"

// Hello prints a greeting.
func Hello() {
	fmt.Println("Hello")
}

// Goodbye prints a farewell.
func Goodbye() {
	fmt.Println("Goodbye")
}
`
	path := createTestFile(t, content)

	counter, err := NewTokenCounter()
	if err != nil {
		t.Fatalf("NewTokenCounter failed: %v", err)
	}

	items := []RelevanceItem{
		{
			Node:      &graph.Node{Name: "Hello"},
			Score:     100,
			Ring:      RingDefinition,
			File:      path,
			StartLine: 6,
			EndLine:   8,
			Reason:    "definition of Hello",
		},
	}

	blocks, err := ReadSources(items, counter, DefaultReadSourcesOptions())
	if err != nil {
		t.Fatalf("ReadSources failed: %v", err)
	}

	if len(blocks) != 1 {
		t.Fatalf("expected 1 block, got %d", len(blocks))
	}

	block := blocks[0]
	if block.File != path {
		t.Errorf("expected file %s, got %s", path, block.File)
	}
	// StartLine should be 6 - 2 (context) = 4
	if block.StartLine != 4 {
		t.Errorf("expected StartLine 4, got %d", block.StartLine)
	}
	if block.EndLine != 8 {
		t.Errorf("expected EndLine 8, got %d", block.EndLine)
	}
	if block.Tokens == 0 {
		t.Error("expected non-zero token count")
	}
	if block.MaxScore != 100 {
		t.Errorf("expected MaxScore 100, got %f", block.MaxScore)
	}
}

func TestReadSources_MergesOverlapping(t *testing.T) {
	content := `package main

// Func1 is first.
func Func1() {}

// Func2 is second.
func Func2() {}

// Func3 is third.
func Func3() {}
`
	path := createTestFile(t, content)

	counter, _ := NewTokenCounter()

	// Two items that are close together - should merge
	items := []RelevanceItem{
		{
			Node:      &graph.Node{Name: "Func1"},
			Score:     100,
			File:      path,
			StartLine: 4,
			EndLine:   4,
			Reason:    "definition of Func1",
		},
		{
			Node:      &graph.Node{Name: "Func2"},
			Score:     80,
			File:      path,
			StartLine: 7, // Within merge threshold of Func1's end
			EndLine:   7,
			Reason:    "definition of Func2",
		},
	}

	opts := ReadSourcesOptions{
		ContextLines:   1,
		MergeThreshold: 3,
	}
	blocks, err := ReadSources(items, counter, opts)
	if err != nil {
		t.Fatalf("ReadSources failed: %v", err)
	}

	// Should be merged into 1 block
	if len(blocks) != 1 {
		t.Fatalf("expected 1 merged block, got %d", len(blocks))
	}

	block := blocks[0]
	if len(block.Items) != 2 {
		t.Errorf("expected 2 items in merged block, got %d", len(block.Items))
	}
	if block.MaxScore != 100 {
		t.Errorf("expected MaxScore 100 (highest), got %f", block.MaxScore)
	}
}

func TestReadSources_SeparateBlocks(t *testing.T) {
	content := `package main

// Func1 at the top.
func Func1() {}

// Many lines in between...
// ...
// ...
// ...
// ...
// ...
// ...
// ...
// ...
// ...

// Func2 at the bottom.
func Func2() {}
`
	path := createTestFile(t, content)

	counter, _ := NewTokenCounter()

	// Two items that are far apart - should NOT merge
	items := []RelevanceItem{
		{
			Node:      &graph.Node{Name: "Func1"},
			Score:     100,
			File:      path,
			StartLine: 4,
			EndLine:   4,
			Reason:    "definition of Func1",
		},
		{
			Node:      &graph.Node{Name: "Func2"},
			Score:     80,
			File:      path,
			StartLine: 17,
			EndLine:   17,
			Reason:    "definition of Func2",
		},
	}

	opts := ReadSourcesOptions{
		ContextLines:   1,
		MergeThreshold: 3,
	}
	blocks, err := ReadSources(items, counter, opts)
	if err != nil {
		t.Fatalf("ReadSources failed: %v", err)
	}

	// Should be 2 separate blocks
	if len(blocks) != 2 {
		t.Fatalf("expected 2 separate blocks, got %d", len(blocks))
	}
}

func TestReadSources_MultipleFiles(t *testing.T) {
	dir := t.TempDir()

	file1 := filepath.Join(dir, "file1.go")
	file2 := filepath.Join(dir, "file2.go")

	os.WriteFile(file1, []byte("package main\n\nfunc A() {}\n"), 0644)
	os.WriteFile(file2, []byte("package main\n\nfunc B() {}\n"), 0644)

	counter, _ := NewTokenCounter()

	items := []RelevanceItem{
		{
			Node:      &graph.Node{Name: "A"},
			Score:     100,
			File:      file1,
			StartLine: 3,
			EndLine:   3,
			Reason:    "definition of A",
		},
		{
			Node:      &graph.Node{Name: "B"},
			Score:     80,
			File:      file2,
			StartLine: 3,
			EndLine:   3,
			Reason:    "definition of B",
		},
	}

	blocks, err := ReadSources(items, counter, DefaultReadSourcesOptions())
	if err != nil {
		t.Fatalf("ReadSources failed: %v", err)
	}

	// Should be 2 blocks (one per file)
	if len(blocks) != 2 {
		t.Fatalf("expected 2 blocks, got %d", len(blocks))
	}

	// Should be sorted by score
	if blocks[0].MaxScore < blocks[1].MaxScore {
		t.Error("blocks not sorted by score descending")
	}
}

func TestReadSources_MissingFile(t *testing.T) {
	counter, _ := NewTokenCounter()

	items := []RelevanceItem{
		{
			Node:      &graph.Node{Name: "Missing"},
			Score:     100,
			File:      "/nonexistent/path/file.go",
			StartLine: 1,
			EndLine:   10,
			Reason:    "missing file",
		},
	}

	blocks, err := ReadSources(items, counter, DefaultReadSourcesOptions())
	if err != nil {
		t.Fatalf("ReadSources should not fail for missing files: %v", err)
	}

	// Missing files should be skipped
	if len(blocks) != 0 {
		t.Errorf("expected 0 blocks for missing file, got %d", len(blocks))
	}
}

func TestReadSources_EmptyItems(t *testing.T) {
	blocks, err := ReadSources(nil, nil, DefaultReadSourcesOptions())
	if err != nil {
		t.Fatalf("ReadSources failed: %v", err)
	}
	if blocks != nil {
		t.Errorf("expected nil blocks for nil items, got %v", blocks)
	}
}

func TestReadSources_NoEndLine(t *testing.T) {
	content := `package main

func Test() {
	// body
}
`
	path := createTestFile(t, content)
	counter, _ := NewTokenCounter()

	// Item with EndLine = 0 (not set)
	items := []RelevanceItem{
		{
			Node:      &graph.Node{Name: "Test"},
			Score:     100,
			File:      path,
			StartLine: 3,
			EndLine:   0, // Not set
			Reason:    "test",
		},
	}

	blocks, err := ReadSources(items, counter, DefaultReadSourcesOptions())
	if err != nil {
		t.Fatalf("ReadSources failed: %v", err)
	}

	if len(blocks) != 1 {
		t.Fatalf("expected 1 block, got %d", len(blocks))
	}

	// Should fall back to StartLine for EndLine
	if blocks[0].EndLine != 3 {
		t.Errorf("expected EndLine 3 (fallback to StartLine), got %d", blocks[0].EndLine)
	}
}

func TestExtractLines(t *testing.T) {
	lines := []string{"line1", "line2", "line3", "line4", "line5"}

	tests := []struct {
		start int
		end   int
		want  string
	}{
		{1, 1, "line1"},
		{1, 3, "line1\nline2\nline3"},
		{2, 4, "line2\nline3\nline4"},
		{4, 5, "line4\nline5"},
		{0, 2, "line1\nline2"},  // start < 1 should clamp
		{4, 10, "line4\nline5"}, // end > len should clamp
		{6, 7, ""},              // start > len should return empty
	}

	for _, tt := range tests {
		got := extractLines(lines, tt.start, tt.end)
		if got != tt.want {
			t.Errorf("extractLines(%d, %d) = %q, want %q", tt.start, tt.end, got, tt.want)
		}
	}
}

func TestBuildReason(t *testing.T) {
	tests := []struct {
		items []RelevanceItem
		want  string
	}{
		{nil, ""},
		{[]RelevanceItem{}, ""},
		{[]RelevanceItem{{Reason: "only one"}}, "only one"},
		{[]RelevanceItem{{Reason: "first"}, {Reason: "second"}}, "first; second"},
	}

	for _, tt := range tests {
		got := buildReason(tt.items)
		if got != tt.want {
			t.Errorf("buildReason() = %q, want %q", got, tt.want)
		}
	}
}
