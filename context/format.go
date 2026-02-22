package context

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
)

// OutputFormat represents the output format.
type OutputFormat int

const (
	FormatText OutputFormat = iota
	FormatJSON
)

// Format formats a FitResult for output.
func Format(result *FitResult, format OutputFormat) string {
	switch format {
	case FormatJSON:
		return formatJSON(result)
	default:
		return formatText(result)
	}
}

// formatText produces a markdown-style text output optimized for AI agents.
func formatText(result *FitResult) string {
	var sb strings.Builder

	// Header
	sb.WriteString(fmt.Sprintf("## Context for: %q\n\n", result.Task))

	// Summary
	sb.WriteString("### Summary\n\n")
	sb.WriteString(fmt.Sprintf("- **%d files**, **%d tokens** (budget: %d)\n",
		result.Summary.FileCount, result.UsedTokens, result.TotalBudget))

	if len(result.Summary.Symbols) > 0 {
		sb.WriteString(fmt.Sprintf("- Symbols: %s\n", strings.Join(result.Summary.Symbols, ", ")))
	}

	if result.Summary.TruncateCount > 0 || result.Summary.ExcludeCount > 0 {
		sb.WriteString(fmt.Sprintf("- %d truncated, %d excluded\n",
			result.Summary.TruncateCount, result.Summary.ExcludeCount))
	}

	sb.WriteString("\n---\n\n")

	// Source blocks
	if len(result.Blocks) > 0 {
		sb.WriteString("### Source\n\n")

		for _, block := range result.Blocks {
			shortPath := shortenPath(block.File)
			sb.WriteString(fmt.Sprintf("#### %s (lines %d-%d)\n",
				shortPath, block.StartLine, block.EndLine))

			if block.Reason != "" {
				sb.WriteString(fmt.Sprintf("> %s\n", block.Reason))
			}

			sb.WriteString("\n```go\n")
			sb.WriteString(block.Content)
			if !strings.HasSuffix(block.Content, "\n") {
				sb.WriteString("\n")
			}
			sb.WriteString("```\n\n")
		}
	}

	// Excluded section
	if len(result.Excluded) > 0 {
		sb.WriteString("---\n\n")
		sb.WriteString("### Excluded (didn't fit)\n\n")

		for _, ex := range result.Excluded {
			shortPath := shortenPath(ex.File)
			sb.WriteString(fmt.Sprintf("- **%s** - %d tokens - %s\n",
				shortPath, ex.Tokens, ex.Reason))
		}
	}

	return sb.String()
}

// JSONOutput is the JSON output structure.
type JSONOutput struct {
	Task     string        `json:"task"`
	Budget   int           `json:"budget"`
	Used     int           `json:"used"`
	Summary  JSONSummary   `json:"summary"`
	Blocks   []JSONBlock   `json:"blocks"`
	Excluded []JSONExclude `json:"excluded,omitempty"`
}

// JSONSummary is the JSON summary structure.
type JSONSummary struct {
	FileCount     int      `json:"file_count"`
	SymbolCount   int      `json:"symbol_count"`
	Symbols       []string `json:"symbols"`
	Truncated     int      `json:"truncated"`
	ExcludedCount int      `json:"excluded"`
}

// JSONBlock is a source block in JSON format.
type JSONBlock struct {
	File      string   `json:"file"`
	StartLine int      `json:"start_line"`
	EndLine   int      `json:"end_line"`
	Content   string   `json:"content"`
	Tokens    int      `json:"tokens"`
	Score     float64  `json:"score"`
	Reason    string   `json:"reason,omitempty"`
	Symbols   []string `json:"symbols,omitempty"`
}

// JSONExclude is an excluded block in JSON format.
type JSONExclude struct {
	File   string  `json:"file"`
	Tokens int     `json:"tokens"`
	Score  float64 `json:"score"`
	Reason string  `json:"reason,omitempty"`
}

// formatJSON produces JSON output.
func formatJSON(result *FitResult) string {
	output := JSONOutput{
		Task:   result.Task,
		Budget: result.TotalBudget,
		Used:   result.UsedTokens,
		Summary: JSONSummary{
			FileCount:     result.Summary.FileCount,
			SymbolCount:   result.Summary.SymbolCount,
			Symbols:       result.Summary.Symbols,
			Truncated:     result.Summary.TruncateCount,
			ExcludedCount: result.Summary.ExcludeCount,
		},
	}

	for _, block := range result.Blocks {
		var symbols []string
		for _, item := range block.Items {
			if item.Node != nil && item.Node.Name != "" {
				symbols = appendUnique(symbols, item.Node.Name)
			}
		}

		output.Blocks = append(output.Blocks, JSONBlock{
			File:      block.File,
			StartLine: block.StartLine,
			EndLine:   block.EndLine,
			Content:   block.Content,
			Tokens:    block.Tokens,
			Score:     block.MaxScore,
			Reason:    block.Reason,
			Symbols:   symbols,
		})
	}

	for _, ex := range result.Excluded {
		output.Excluded = append(output.Excluded, JSONExclude{
			File:   ex.File,
			Tokens: ex.Tokens,
			Score:  ex.Score,
			Reason: ex.Reason,
		})
	}

	// Pretty print JSON
	data, err := json.MarshalIndent(output, "", "  ")
	if err != nil {
		return fmt.Sprintf(`{"error": %q}`, err.Error())
	}
	return string(data)
}

// shortenPath shortens a file path for display.
func shortenPath(path string) string {
	// Try to find a common project marker
	markers := []string{"/src/", "/pkg/", "/cmd/", "/internal/"}
	for _, marker := range markers {
		if idx := strings.LastIndex(path, marker); idx != -1 {
			return path[idx+1:]
		}
	}

	// Try to shorten based on common project names
	if idx := strings.Index(path, "/github.com/"); idx != -1 {
		parts := strings.SplitN(path[idx+12:], "/", 3)
		if len(parts) >= 3 {
			return parts[1] + "/" + parts[2]
		}
	}

	// Fall back to last 3 path components
	parts := strings.Split(path, string(filepath.Separator))
	if len(parts) > 3 {
		return strings.Join(parts[len(parts)-3:], string(filepath.Separator))
	}

	return path
}

// FormatManifest produces a manifest-only output (no source content).
func FormatManifest(result *FitResult) string {
	var sb strings.Builder

	sb.WriteString(fmt.Sprintf("## Context Manifest for: %q\n\n", result.Task))
	sb.WriteString(fmt.Sprintf("Budget: %d tokens, Used: %d tokens\n\n", result.TotalBudget, result.UsedTokens))

	sb.WriteString("### Included Files\n\n")
	sb.WriteString("| File | Lines | Tokens | Reason |\n")
	sb.WriteString("|------|-------|--------|--------|\n")

	for _, block := range result.Blocks {
		shortPath := shortenPath(block.File)
		lines := fmt.Sprintf("%d-%d", block.StartLine, block.EndLine)
		reason := block.Reason
		if len(reason) > 40 {
			reason = reason[:40] + "..."
		}
		sb.WriteString(fmt.Sprintf("| %s | %s | %d | %s |\n",
			shortPath, lines, block.Tokens, reason))
	}

	if len(result.Excluded) > 0 {
		sb.WriteString("\n### Excluded Files\n\n")
		sb.WriteString("| File | Tokens | Reason |\n")
		sb.WriteString("|------|--------|--------|\n")

		for _, ex := range result.Excluded {
			shortPath := shortenPath(ex.File)
			reason := ex.Reason
			if len(reason) > 40 {
				reason = reason[:40] + "..."
			}
			sb.WriteString(fmt.Sprintf("| %s | %d | %s |\n",
				shortPath, ex.Tokens, reason))
		}
	}

	return sb.String()
}
