package context

import (
	"fmt"
	"sort"
	"strings"
)

// FitResult holds the result of budget fitting.
type FitResult struct {
	Task        string          // Original task description
	Blocks      []SourceBlock   // Included blocks, sorted by file path
	Summary     Summary         // Summary information
	Truncated   []TruncatedInfo // Blocks that were shortened
	Excluded    []ExcludedInfo  // Blocks that didn't fit
	UsedTokens  int
	TotalBudget int
}

// Summary holds summary information about the context.
type Summary struct {
	FileCount     int
	SymbolCount   int
	Symbols       []string // Key symbol names
	TruncateCount int
	ExcludeCount  int
}

// TruncatedInfo describes a truncated block.
type TruncatedInfo struct {
	File       string
	FullTokens int
	KeptTokens int
	Reason     string
}

// ExcludedInfo describes an excluded block.
type ExcludedInfo struct {
	File   string
	Tokens int
	Score  float64
	Reason string
}

// FitBudget selects blocks that fit within the token budget.
// Uses a greedy algorithm: highest score first, with smart truncation for overflow.
func FitBudget(blocks []SourceBlock, maxTokens int, task string, counter *TokenCounter) *FitResult {
	result := &FitResult{
		Task:        task,
		TotalBudget: maxTokens,
	}

	if len(blocks) == 0 {
		return result
	}

	// Reserve tokens for summary header
	headerReserve := 200
	available := maxTokens - headerReserve

	// Sort blocks by score descending (should already be sorted, but ensure)
	sorted := make([]SourceBlock, len(blocks))
	copy(sorted, blocks)
	sort.Slice(sorted, func(i, j int) bool {
		return sorted[i].MaxScore > sorted[j].MaxScore
	})

	used := 0
	var included []SourceBlock
	symbolSet := make(map[string]bool)

	for _, block := range sorted {
		// Collect symbols from block items
		for _, item := range block.Items {
			if item.Node != nil && item.Node.Name != "" {
				symbolSet[item.Node.Name] = true
			}
		}

		if used+block.Tokens <= available {
			// Fits completely
			included = append(included, block)
			used += block.Tokens
		} else if block.MaxScore >= 100 {
			// High priority (definitions) - always include, truncate if needed
			remaining := available - used
			if remaining > 100 { // Only truncate if we have meaningful space
				truncated := truncateBlock(block, remaining, counter)
				included = append(included, truncated)
				used += truncated.Tokens
				result.Truncated = append(result.Truncated, TruncatedInfo{
					File:       block.File,
					FullTokens: block.Tokens,
					KeptTokens: truncated.Tokens,
					Reason:     "definition priority",
				})
			} else {
				// Can't fit even truncated
				result.Excluded = append(result.Excluded, ExcludedInfo{
					File:   block.File,
					Tokens: block.Tokens,
					Score:  block.MaxScore,
					Reason: block.Reason,
				})
			}
		} else if block.MaxScore >= 60 && used+100 <= available {
			// Medium priority - try to fit signature only
			remaining := available - used
			if remaining > 50 {
				truncated := truncateToSignature(block, counter)
				if truncated.Tokens <= remaining {
					included = append(included, truncated)
					used += truncated.Tokens
					result.Truncated = append(result.Truncated, TruncatedInfo{
						File:       block.File,
						FullTokens: block.Tokens,
						KeptTokens: truncated.Tokens,
						Reason:     "signature only",
					})
				} else {
					result.Excluded = append(result.Excluded, ExcludedInfo{
						File:   block.File,
						Tokens: block.Tokens,
						Score:  block.MaxScore,
						Reason: block.Reason,
					})
				}
			}
		} else {
			// Low priority - exclude
			result.Excluded = append(result.Excluded, ExcludedInfo{
				File:   block.File,
				Tokens: block.Tokens,
				Score:  block.MaxScore,
				Reason: block.Reason,
			})
		}
	}

	// Sort included blocks by file path for output readability
	sort.Slice(included, func(i, j int) bool {
		if included[i].File != included[j].File {
			return included[i].File < included[j].File
		}
		return included[i].StartLine < included[j].StartLine
	})

	result.Blocks = included
	result.UsedTokens = used

	// Build summary
	var symbols []string
	for s := range symbolSet {
		symbols = append(symbols, s)
	}
	sort.Strings(symbols)
	if len(symbols) > 10 {
		symbols = symbols[:10]
	}

	fileSet := make(map[string]bool)
	for _, b := range included {
		fileSet[b.File] = true
	}

	result.Summary = Summary{
		FileCount:     len(fileSet),
		SymbolCount:   len(symbolSet),
		Symbols:       symbols,
		TruncateCount: len(result.Truncated),
		ExcludeCount:  len(result.Excluded),
	}

	return result
}

// truncateBlock shortens a block to fit within maxTokens.
func truncateBlock(block SourceBlock, maxTokens int, counter *TokenCounter) SourceBlock {
	lines := strings.Split(block.Content, "\n")
	if len(lines) <= 3 {
		return block // Too short to truncate meaningfully
	}

	// Binary search for the right number of lines
	low, high := 1, len(lines)
	var best SourceBlock

	for low <= high {
		mid := (low + high) / 2
		content := buildTruncatedContent(lines, mid)
		tokens := counter.Count(content)

		if tokens <= maxTokens {
			best = SourceBlock{
				File:      block.File,
				StartLine: block.StartLine,
				EndLine:   block.StartLine + mid - 1,
				Content:   content,
				Tokens:    tokens,
				Items:     block.Items,
				MaxScore:  block.MaxScore,
				Reason:    block.Reason + " (truncated)",
			}
			low = mid + 1 // Try to include more
		} else {
			high = mid - 1 // Need to include less
		}
	}

	if best.Content == "" {
		// Couldn't fit anything, return first line only
		content := lines[0] + "\n// ... (truncated)"
		return SourceBlock{
			File:      block.File,
			StartLine: block.StartLine,
			EndLine:   block.StartLine,
			Content:   content,
			Tokens:    counter.Count(content),
			Items:     block.Items,
			MaxScore:  block.MaxScore,
			Reason:    block.Reason + " (truncated)",
		}
	}

	return best
}

// buildTruncatedContent builds content with truncation indicator.
func buildTruncatedContent(lines []string, keepLines int) string {
	if keepLines >= len(lines) {
		return strings.Join(lines, "\n")
	}

	kept := lines[:keepLines]
	omitted := len(lines) - keepLines
	truncation := fmt.Sprintf("\n// ... (%d lines omitted)", omitted)

	return strings.Join(kept, "\n") + truncation
}

// truncateToSignature extracts just the signature/declaration from a block.
func truncateToSignature(block SourceBlock, counter *TokenCounter) SourceBlock {
	lines := strings.Split(block.Content, "\n")
	if len(lines) == 0 {
		return block
	}

	// Find the first meaningful line (skip empty/comment lines at start)
	var signatureLines []string
	inSignature := false
	braceCount := 0

	for i, line := range lines {
		trimmed := strings.TrimSpace(line)

		// Skip leading empty lines and pure comment lines
		if !inSignature && (trimmed == "" || strings.HasPrefix(trimmed, "//")) {
			continue
		}

		inSignature = true
		signatureLines = append(signatureLines, line)

		// Count braces to find the end of signature
		braceCount += strings.Count(line, "{") - strings.Count(line, "}")

		// Stop after opening brace or if we've collected enough context
		if braceCount > 0 || i >= 5 {
			break
		}
	}

	if len(signatureLines) == 0 {
		signatureLines = append(signatureLines, lines[0])
	}

	// Add truncation indicator
	if len(lines) > len(signatureLines) {
		omitted := len(lines) - len(signatureLines)
		signatureLines = append(signatureLines, fmt.Sprintf("\t// ... (%d lines)", omitted))
		signatureLines = append(signatureLines, "}")
	}

	content := strings.Join(signatureLines, "\n")
	return SourceBlock{
		File:      block.File,
		StartLine: block.StartLine,
		EndLine:   block.StartLine + len(signatureLines) - 1,
		Content:   content,
		Tokens:    counter.Count(content),
		Items:     block.Items,
		MaxScore:  block.MaxScore,
		Reason:    block.Reason + " (signature)",
	}
}
