package context

import (
	"bufio"
	"os"
	"sort"
	"strings"
)

// SourceBlock represents a contiguous block of source code.
type SourceBlock struct {
	File      string          // File path
	StartLine int             // Start line (1-indexed, inclusive)
	EndLine   int             // End line (1-indexed, inclusive)
	Content   string          // The actual source code
	Tokens    int             // Token count for this block
	Items     []RelevanceItem // Symbols contributing to this block
	MaxScore  float64         // Highest relevance score among items
	Reason    string          // Aggregated reason for this block
}

// ReadSourcesOptions configures source reading behavior.
type ReadSourcesOptions struct {
	ContextLines   int // Lines of context to add above each range (default: 2)
	MergeThreshold int // Merge ranges within this many lines (default: 3)
}

// DefaultReadSourcesOptions returns the default options.
func DefaultReadSourcesOptions() ReadSourcesOptions {
	return ReadSourcesOptions{
		ContextLines:   2,
		MergeThreshold: 3,
	}
}

// ReadSources reads source code for relevance items, merging overlapping ranges.
func ReadSources(items []RelevanceItem, counter *TokenCounter, opts ReadSourcesOptions) ([]SourceBlock, error) {
	if len(items) == 0 {
		return nil, nil
	}

	// Group items by file
	byFile := make(map[string][]RelevanceItem)
	for _, item := range items {
		if item.File == "" {
			continue
		}
		byFile[item.File] = append(byFile[item.File], item)
	}

	var blocks []SourceBlock

	for file, fileItems := range byFile {
		// Read the file once
		lines, err := readFileLines(file)
		if err != nil {
			// Skip files that can't be read
			continue
		}

		// Build merged ranges for this file
		ranges := buildMergedRanges(fileItems, opts, len(lines))

		// Create source blocks from ranges
		for _, r := range ranges {
			content := extractLines(lines, r.start, r.end)
			tokens := 0
			if counter != nil {
				tokens = counter.Count(content)
			}

			block := SourceBlock{
				File:      file,
				StartLine: r.start,
				EndLine:   r.end,
				Content:   content,
				Tokens:    tokens,
				Items:     r.items,
				MaxScore:  r.maxScore,
				Reason:    buildReason(r.items),
			}
			blocks = append(blocks, block)
		}
	}

	// Sort blocks by max score descending
	sort.Slice(blocks, func(i, j int) bool {
		return blocks[i].MaxScore > blocks[j].MaxScore
	})

	return blocks, nil
}

// lineRange represents a range of lines with associated items.
type lineRange struct {
	start    int
	end      int
	items    []RelevanceItem
	maxScore float64
}

// buildMergedRanges builds non-overlapping ranges from items, merging nearby ones.
func buildMergedRanges(items []RelevanceItem, opts ReadSourcesOptions, fileLines int) []lineRange {
	if len(items) == 0 {
		return nil
	}

	// Build initial ranges with context
	var ranges []lineRange
	for _, item := range items {
		start := item.StartLine - opts.ContextLines
		if start < 1 {
			start = 1
		}
		end := item.EndLine
		if end < item.StartLine {
			end = item.StartLine // Fallback if EndLine not set
		}
		if end > fileLines {
			end = fileLines
		}

		ranges = append(ranges, lineRange{
			start:    start,
			end:      end,
			items:    []RelevanceItem{item},
			maxScore: item.Score,
		})
	}

	// Sort by start line
	sort.Slice(ranges, func(i, j int) bool {
		return ranges[i].start < ranges[j].start
	})

	// Merge overlapping or close ranges
	merged := []lineRange{ranges[0]}
	for i := 1; i < len(ranges); i++ {
		last := &merged[len(merged)-1]
		curr := ranges[i]

		// Merge if overlapping or within threshold
		if curr.start <= last.end+opts.MergeThreshold {
			// Extend the range
			if curr.end > last.end {
				last.end = curr.end
			}
			// Combine items
			last.items = append(last.items, curr.items...)
			// Update max score
			if curr.maxScore > last.maxScore {
				last.maxScore = curr.maxScore
			}
		} else {
			merged = append(merged, curr)
		}
	}

	return merged
}

// readFileLines reads all lines from a file.
func readFileLines(path string) ([]string, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	var lines []string
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		lines = append(lines, scanner.Text())
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}

	return lines, nil
}

// extractLines extracts lines from start to end (1-indexed, inclusive).
func extractLines(lines []string, start, end int) string {
	if start < 1 {
		start = 1
	}
	if end > len(lines) {
		end = len(lines)
	}
	if start > end || start > len(lines) {
		return ""
	}

	// Convert to 0-indexed
	extracted := lines[start-1 : end]
	return strings.Join(extracted, "\n")
}

// buildReason creates a human-readable reason from multiple items.
func buildReason(items []RelevanceItem) string {
	if len(items) == 0 {
		return ""
	}
	if len(items) == 1 {
		return items[0].Reason
	}

	// Collect unique reasons, preserving first-seen insertion order.
	seen := make(map[string]bool)
	var parts []string
	for _, item := range items {
		if item.Reason != "" && !seen[item.Reason] {
			seen[item.Reason] = true
			parts = append(parts, item.Reason)
			if len(parts) >= 3 {
				break
			}
		}
	}

	if len(seen) > 3 {
		return strings.Join(parts, "; ") + " (+ more)"
	}
	return strings.Join(parts, "; ")
}

// CountFileLines returns the total line count of a file.
func CountFileLines(path string) (int, error) {
	lines, err := readFileLines(path)
	if err != nil {
		return 0, err
	}
	return len(lines), nil
}
