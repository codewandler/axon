package main

import (
	"fmt"
	"strings"

	"github.com/codewandler/axon/graph"
	"github.com/codewandler/axon/render"
)

// shortID returns a shortened version of the node ID.
func shortID(id string) string {
	if len(id) > 7 {
		return id[:7]
	}
	return id
}

// formatSize formats a byte count as a human-readable string.
func formatSize(bytes int64) string {
	const unit = 1024
	if bytes < unit {
		return fmt.Sprintf("%d B", bytes)
	}
	div, exp := int64(unit), 0
	for n := bytes / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %cB", float64(bytes)/float64(div), "KMGTPE"[exp])
}

// displayName returns the display name for a node, using the render package.
func displayName(n *graph.Node) string {
	return render.GetDisplayName(n)
}

// emoji returns the emoji for a node type, or empty string if none.
func emoji(nodeType string) string {
	if e, ok := typeEmojis[nodeType]; ok {
		return e
	}
	return "  "
}

// truncate truncates a string to maxLen, appending "..." if needed.
func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	if maxLen <= 3 {
		return s[:maxLen]
	}
	return s[:maxLen-3] + "..."
}

// sectionDivider creates a divider line: "── label (count) ──────"
func sectionDivider(label string, count int, width int) string {
	content := fmt.Sprintf("── %s (%d) ", label, count)
	remaining := width - len(content)
	if remaining < 2 {
		remaining = 2
	}
	return content + strings.Repeat("─", remaining)
}
