package main

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/codewandler/axon/graph"
)

// renderBreadcrumb renders the path bar for the center node.
// For filesystem nodes, shows the file/dir path extracted from the URI.
// For other nodes, shows the node name and type.
func renderBreadcrumb(node *graph.Node, width int, depth int) string {
	if node == nil {
		return ""
	}

	var b strings.Builder

	path := extractPath(node)
	b.WriteString(breadcrumbActiveStyle.Render(path))

	// Zoom indicator on the right
	if depth > 1 {
		zoomStr := zoomStyle.Render(strings.Repeat("+", depth-1))
		padding := width - lipgloss.Width(b.String()) - lipgloss.Width(zoomStr) - 2
		if padding > 0 {
			b.WriteString(strings.Repeat(" ", padding))
		}
		b.WriteString(zoomStr)
	}

	return b.String()
}

// extractPath returns a human-readable path for the node.
// Strips file:// and git+file:// URI prefixes to show the filesystem path.
// For non-URI nodes, returns the display name with type.
func extractPath(node *graph.Node) string {
	uri := node.URI
	if uri != "" {
		if strings.HasPrefix(uri, "file://") {
			return strings.TrimPrefix(uri, "file://")
		}
		if strings.HasPrefix(uri, "git+file://") {
			return strings.TrimPrefix(uri, "git+file://")
		}
		return uri
	}
	// No URI — use name
	name := displayName(node)
	return name
}
