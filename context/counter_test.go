package context

import (
	"strings"
	"testing"
)

func TestTokenCounter(t *testing.T) {
	tc, err := NewTokenCounter()
	if err != nil {
		t.Fatalf("NewTokenCounter failed: %v", err)
	}

	tests := []struct {
		name     string
		text     string
		minCount int // minimum expected tokens
		maxCount int // maximum expected tokens
	}{
		{
			name:     "empty",
			text:     "",
			minCount: 0,
			maxCount: 0,
		},
		{
			name:     "hello world",
			text:     "Hello, world!",
			minCount: 3,
			maxCount: 5,
		},
		{
			name:     "go code",
			text:     "func main() {\n\tfmt.Println(\"Hello\")\n}",
			minCount: 10,
			maxCount: 20,
		},
		{
			name:     "longer text",
			text:     "The quick brown fox jumps over the lazy dog.",
			minCount: 8,
			maxCount: 12,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			count := tc.Count(tt.text)
			if count < tt.minCount || count > tt.maxCount {
				t.Errorf("Count(%q) = %d, expected between %d and %d",
					tt.text, count, tt.minCount, tt.maxCount)
			}
		})
	}
}

func TestTokenCounterLines(t *testing.T) {
	tc, err := NewTokenCounter()
	if err != nil {
		t.Fatalf("NewTokenCounter failed: %v", err)
	}

	lines := []string{
		"package main",
		"",
		"import \"fmt\"",
		"",
		"func main() {",
		"\tfmt.Println(\"Hello\")",
		"}",
	}

	count := tc.CountLines(lines)
	if count < 15 || count > 30 {
		t.Errorf("CountLines() = %d, expected between 15 and 30", count)
	}
}

func TestEstimateFromChars(t *testing.T) {
	tests := []struct {
		chars    int
		expected int
	}{
		{0, 0},
		{1, 1},
		{4, 1},
		{5, 2},
		{8, 2},
		{100, 25},
		{1000, 250},
	}

	for _, tt := range tests {
		got := EstimateFromChars(tt.chars)
		if got != tt.expected {
			t.Errorf("EstimateFromChars(%d) = %d, expected %d", tt.chars, got, tt.expected)
		}
	}
}

func TestTokenCounterConsistency(t *testing.T) {
	tc, err := NewTokenCounter()
	if err != nil {
		t.Fatalf("NewTokenCounter failed: %v", err)
	}

	// Counting the same text should always give the same result
	text := "func Storage() graph.Storage { return g.storage }"
	count1 := tc.Count(text)
	count2 := tc.Count(text)
	if count1 != count2 {
		t.Errorf("Count() not consistent: %d != %d", count1, count2)
	}
}

func TestTokenCounterLargeText(t *testing.T) {
	tc, err := NewTokenCounter()
	if err != nil {
		t.Fatalf("NewTokenCounter failed: %v", err)
	}

	// Generate a larger text (simulating a source file)
	var sb strings.Builder
	sb.WriteString("package example\n\n")
	for i := 0; i < 100; i++ {
		sb.WriteString("// Comment line\n")
		sb.WriteString("func Function")
		sb.WriteString(string(rune('A' + i%26)))
		sb.WriteString("() error {\n")
		sb.WriteString("\treturn nil\n")
		sb.WriteString("}\n\n")
	}

	text := sb.String()
	count := tc.Count(text)

	// Should be roughly chars/4 but tokenization is more efficient for code
	charEstimate := EstimateFromChars(len(text))

	// Token count should be less than char estimate for code (code tokenizes efficiently)
	// but within reasonable bounds
	if count > charEstimate*2 {
		t.Errorf("Token count %d seems too high for %d chars (estimate: %d)", count, len(text), charEstimate)
	}
	if count < 100 {
		t.Errorf("Token count %d seems too low for %d chars", count, len(text))
	}

	t.Logf("Large text: %d chars, %d tokens, estimate: %d", len(text), count, charEstimate)
}
