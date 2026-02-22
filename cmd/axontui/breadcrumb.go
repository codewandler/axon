package main

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// renderBreadcrumb renders the navigation breadcrumb trail.
func renderBreadcrumb(crumbs []string, width int, depth int) string {
	var b strings.Builder

	if len(crumbs) == 0 {
		return ""
	}

	// Render crumbs: dim for history, bright for current
	maxCrumbs := 6
	start := 0
	if len(crumbs) > maxCrumbs {
		start = len(crumbs) - maxCrumbs
		b.WriteString(breadcrumbStyle.Render("... "))
	}

	for i := start; i < len(crumbs); i++ {
		if i > start {
			b.WriteString(breadcrumbSepStyle.Render(" > "))
		}
		name := crumbs[i]
		if len(name) > 20 {
			name = name[:17] + "..."
		}
		if i == len(crumbs)-1 {
			b.WriteString(breadcrumbActiveStyle.Render(name))
		} else {
			b.WriteString(breadcrumbStyle.Render(name))
		}
	}

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
