// Package context provides the budget fitting algorithm for the Context Window Optimizer.
//
// The budget fitter is responsible for selecting which source code blocks to include
// in the final context output, given a token budget constraint. It uses a greedy
// algorithm with priority-based truncation to maximize the usefulness of the context.
//
// # Algorithm Overview
//
// The fitting process works in three phases:
//
//  1. Sort blocks by relevance score (highest first)
//  2. Greedily include blocks until budget is exhausted
//  3. Apply smart truncation for overflow situations
//
// # Priority Tiers
//
// Blocks are handled differently based on their relevance score:
//
//   - Score >= 100 (Ring 0 definitions): Always included, truncated if needed
//   - Score >= 60 (Ring 1-2 dependencies): Signature-only if space is tight
//   - Score < 60 (Ring 3+ context): Excluded when budget is constrained
//
// # Truncation Strategies
//
// When a block doesn't fit completely, two strategies are available:
//
//   - truncateBlock: Binary search for maximum lines that fit, adds "... omitted" marker
//   - truncateToSignature: Extracts just the function/type signature with body placeholder
package context

import (
	"fmt"
	"sort"
	"strings"
)

// FitResult holds the complete result of the budget fitting process.
// It contains both the selected blocks and metadata about what was
// truncated or excluded, useful for diagnostics and manifest output.
type FitResult struct {
	Task        string          // Original task description from user input
	Blocks      []SourceBlock   // Included blocks, sorted by file path for readability
	Summary     Summary         // Aggregated statistics about the fit
	Truncated   []TruncatedInfo // Blocks that were shortened to fit
	Excluded    []ExcludedInfo  // Blocks that didn't fit at all
	UsedTokens  int             // Actual tokens consumed by included blocks
	TotalBudget int             // Original token budget (includes header reserve)
}

// Summary provides aggregated statistics about the context fit.
// This is displayed in the output header to give users a quick
// overview of what's included.
type Summary struct {
	FileCount     int      // Number of unique files with included blocks
	SymbolCount   int      // Total symbols found across all blocks
	Symbols       []string // Top symbol names (capped at 10 for display)
	TruncateCount int      // Number of blocks that were truncated
	ExcludeCount  int      // Number of blocks excluded entirely
}

// TruncatedInfo records details about a block that was shortened.
// This helps users understand what context might be missing.
type TruncatedInfo struct {
	File       string // Source file path
	FullTokens int    // Original token count before truncation
	KeptTokens int    // Token count after truncation
	Reason     string // Why truncation was applied (e.g., "definition priority", "signature only")
}

// ExcludedInfo records details about a block that was excluded entirely.
// Low-priority blocks are excluded when the budget is tight.
type ExcludedInfo struct {
	File   string  // Source file path
	Tokens int     // Token count of the excluded block
	Score  float64 // Relevance score (lower scores are excluded first)
	Reason string  // Why this block was relevant (for diagnostics)
}

// FitBudget selects source blocks that fit within the given token budget.
//
// The algorithm processes blocks in descending score order (greedy approach),
// applying different strategies based on priority:
//
//   - High priority (score >= 100): Truncate to fit if needed
//   - Medium priority (score >= 60): Include signature only if tight on space
//   - Low priority (score < 60): Exclude when budget is constrained
//
// The function reserves 200 tokens for the output header/summary, so the
// actual content budget is maxTokens - 200.
//
// Parameters:
//   - blocks: Source blocks to consider, typically from ReadSources
//   - maxTokens: Total token budget (including header reserve)
//   - task: Original task description for the result
//   - counter: Token counter for measuring block sizes
//
// Returns a FitResult containing the selected blocks and metadata about
// what was truncated or excluded.
func FitBudget(blocks []SourceBlock, maxTokens int, task string, counter *TokenCounter) *FitResult {
	result := &FitResult{
		Task:        task,
		TotalBudget: maxTokens,
	}

	if len(blocks) == 0 {
		return result
	}

	// Reserve tokens for the markdown summary header in the output.
	// This ensures we don't overflow when Format() adds the header text.
	headerReserve := 200
	available := maxTokens - headerReserve

	// Sort blocks by score descending to prioritize most relevant content.
	// We copy to avoid mutating the input slice.
	sorted := make([]SourceBlock, len(blocks))
	copy(sorted, blocks)
	sort.Slice(sorted, func(i, j int) bool {
		return sorted[i].MaxScore > sorted[j].MaxScore
	})

	used := 0
	var included []SourceBlock
	symbolSet := make(map[string]bool) // Track unique symbols for summary

	for _, block := range sorted {
		// Collect symbol names from this block for the summary.
		// We do this even for excluded blocks to show what was considered.
		for _, item := range block.Items {
			if item.Node != nil && item.Node.Name != "" {
				symbolSet[item.Node.Name] = true
			}
		}

		if used+block.Tokens <= available {
			// Block fits completely - include it as-is
			included = append(included, block)
			used += block.Tokens
		} else if block.MaxScore >= 100 {
			// High priority block (Ring 0 definition) - try to truncate to fit.
			// These are the symbols the user explicitly asked about, so we
			// always want to include at least part of them.
			remaining := available - used
			if remaining > 100 {
				// Enough space for a meaningful truncation
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
				// Not enough space even for truncation - must exclude
				result.Excluded = append(result.Excluded, ExcludedInfo{
					File:   block.File,
					Tokens: block.Tokens,
					Score:  block.MaxScore,
					Reason: block.Reason,
				})
			}
		} else if block.MaxScore >= 60 && used+100 <= available {
			// Medium priority block (Ring 1-2 dependencies) - try signature only.
			// Including just the signature gives the AI agent enough context
			// to understand the API without consuming too many tokens.
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
					// Signature itself doesn't fit
					result.Excluded = append(result.Excluded, ExcludedInfo{
						File:   block.File,
						Tokens: block.Tokens,
						Score:  block.MaxScore,
						Reason: block.Reason,
					})
				}
			}
		} else {
			// Low priority block (Ring 3+ context) - exclude to save space.
			// These provide helpful but non-essential context.
			result.Excluded = append(result.Excluded, ExcludedInfo{
				File:   block.File,
				Tokens: block.Tokens,
				Score:  block.MaxScore,
				Reason: block.Reason,
			})
		}
	}

	// Re-sort included blocks by file path and line number for readability.
	// This groups related code together in the output.
	sort.Slice(included, func(i, j int) bool {
		if included[i].File != included[j].File {
			return included[i].File < included[j].File
		}
		return included[i].StartLine < included[j].StartLine
	})

	result.Blocks = included
	result.UsedTokens = used

	// Build the summary with top symbols for display.
	// Cap at 10 symbols to keep the header concise.
	var symbols []string
	for s := range symbolSet {
		symbols = append(symbols, s)
	}
	sort.Strings(symbols)
	if len(symbols) > 10 {
		symbols = symbols[:10]
	}

	// Count unique files for the summary
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

// truncateBlock shortens a block to fit within maxTokens using binary search.
//
// The algorithm finds the maximum number of lines that fit within the budget,
// then appends a "... (N lines omitted)" indicator to show truncation occurred.
//
// Binary search is used because token count is monotonic with line count,
// making this O(log n) in the number of lines rather than O(n).
//
// If the block is 3 lines or fewer, it's returned unchanged since truncation
// wouldn't provide meaningful savings.
func truncateBlock(block SourceBlock, maxTokens int, counter *TokenCounter) SourceBlock {
	lines := strings.Split(block.Content, "\n")
	if len(lines) <= 3 {
		return block // Too short to truncate meaningfully
	}

	// Binary search for the maximum number of lines that fit.
	// We track the best valid result since not all midpoints may fit.
	low, high := 1, len(lines)
	var best SourceBlock

	for low <= high {
		mid := (low + high) / 2
		content := buildTruncatedContent(lines, mid)
		tokens := counter.Count(content)

		if tokens <= maxTokens {
			// This fits - record it and try to include more lines
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
			low = mid + 1
		} else {
			// Doesn't fit - try fewer lines
			high = mid - 1
		}
	}

	if best.Content == "" {
		// Edge case: even one line with truncation marker doesn't fit.
		// Return just the first line as a last resort.
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

// buildTruncatedContent constructs content from the first N lines with a truncation marker.
//
// If keepLines covers all lines, returns the original content unchanged.
// Otherwise, appends a comment showing how many lines were omitted,
// e.g., "// ... (42 lines omitted)"
func buildTruncatedContent(lines []string, keepLines int) string {
	if keepLines >= len(lines) {
		return strings.Join(lines, "\n")
	}

	kept := lines[:keepLines]
	omitted := len(lines) - keepLines
	truncation := fmt.Sprintf("\n// ... (%d lines omitted)", omitted)

	return strings.Join(kept, "\n") + truncation
}

// truncateToSignature extracts just the function/type signature from a block.
//
// This is useful for medium-priority blocks where we want to show the API
// without the full implementation. The AI agent gets enough context to
// understand how to call the function without consuming many tokens.
//
// The algorithm:
//  1. Skips leading empty lines and comments
//  2. Collects lines until the first opening brace (or 5 lines max)
//  3. Appends a placeholder body showing how many lines were omitted
//
// Example output:
//
//	func FitBudget(blocks []SourceBlock, maxTokens int) *FitResult {
//		// ... (127 lines)
//	}
func truncateToSignature(block SourceBlock, counter *TokenCounter) SourceBlock {
	lines := strings.Split(block.Content, "\n")
	if len(lines) == 0 {
		return block
	}

	// Extract signature lines, skipping leading whitespace/comments
	var signatureLines []string
	inSignature := false
	braceCount := 0

	for i, line := range lines {
		trimmed := strings.TrimSpace(line)

		// Skip leading empty lines and standalone comments
		if !inSignature && (trimmed == "" || strings.HasPrefix(trimmed, "//")) {
			continue
		}

		inSignature = true
		signatureLines = append(signatureLines, line)

		// Track brace depth to find where the body starts
		braceCount += strings.Count(line, "{") - strings.Count(line, "}")

		// Stop once we've entered the body (brace opened) or collected enough lines
		if braceCount > 0 || i >= 5 {
			break
		}
	}

	// Fallback: if no signature found, use the first line
	if len(signatureLines) == 0 {
		signatureLines = append(signatureLines, lines[0])
	}

	// Add a placeholder body to indicate truncation
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
