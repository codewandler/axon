package main

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/codewandler/axon/graph"
	"github.com/codewandler/axon/types"
)

// renderDetail renders the detail panel for the focused node.
func renderDetail(node *graph.Node, width int, expanded bool) string {
	if node == nil {
		return detailStyle.Width(width).Render("No node selected")
	}

	var b strings.Builder

	// Line 1: emoji + name + type
	name := displayName(node)
	e := emoji(node.Type)
	typeBadge := lipgloss.NewStyle().Foreground(typeColor(node.Type)).Render(node.Type)
	b.WriteString(fmt.Sprintf("%s %s  %s", e, nodeNameStyle.Render(name), typeBadge))
	b.WriteString("\n")

	// Line 2: ID + URI (compact)
	idStr := nodeIDStyle.Render(shortID(node.ID))
	if node.URI != "" {
		uri := node.URI
		maxURI := width - 20
		if maxURI > 0 && len(uri) > maxURI {
			uri = "..." + uri[len(uri)-maxURI:]
		}
		b.WriteString(fmt.Sprintf("%s  %s", idStr, uriStyle.Render(uri)))
	} else {
		b.WriteString(idStr)
	}

	// Line 3: Labels (if any)
	if len(node.Labels) > 0 {
		b.WriteString("\n")
		labelParts := make([]string, len(node.Labels))
		for i, l := range node.Labels {
			labelParts[i] = labelStyle.Render(l)
		}
		b.WriteString(strings.Join(labelParts, " "))
	}

	// Expanded mode: show data fields
	if expanded {
		b.WriteString("\n")
		b.WriteString(renderNodeData(node))
	}

	return detailStyle.Width(width).Render(b.String())
}

// renderNodeData renders the data fields of a node.
func renderNodeData(node *graph.Node) string {
	var fields []string

	switch data := node.Data.(type) {
	case types.DirData:
		fields = append(fields, fmt.Sprintf("Mode: %s", data.Mode.String()))
	case types.FileData:
		fields = append(fields, fmt.Sprintf("Size: %s", formatSize(data.Size)))
		if data.Ext != "" {
			fields = append(fields, fmt.Sprintf("Ext: %s", data.Ext))
		}
		fields = append(fields, fmt.Sprintf("Modified: %s", data.Modified.Format("2006-01-02 15:04")))
	case types.LinkData:
		fields = append(fields, fmt.Sprintf("Target: %s", data.Target))
	case types.RepoData:
		if data.HeadBranch != "" {
			fields = append(fields, fmt.Sprintf("Branch: %s", data.HeadBranch))
		}
		if data.HeadCommit != "" {
			fields = append(fields, fmt.Sprintf("Commit: %s", shortID(data.HeadCommit)))
		}
	case types.BranchData:
		if data.IsHead {
			fields = append(fields, "HEAD")
		}
		if data.IsRemote {
			fields = append(fields, "remote")
		}
		if data.Commit != "" {
			fields = append(fields, fmt.Sprintf("Commit: %s", shortID(data.Commit)))
		}
	case types.TagData:
		if data.Commit != "" {
			fields = append(fields, fmt.Sprintf("Commit: %s", shortID(data.Commit)))
		}
	case types.RemoteData:
		for _, url := range data.URLs {
			fields = append(fields, fmt.Sprintf("URL: %s", url))
		}
	case types.DocumentData:
		// title is already shown as the name
	case types.SectionData:
		fields = append(fields, fmt.Sprintf("Level: %d", data.Level))
	case types.CodeBlockData:
		if data.Language != "" {
			fields = append(fields, fmt.Sprintf("Lang: %s", data.Language))
		}
		fields = append(fields, fmt.Sprintf("Lines: %d", data.Lines))
	case map[string]any:
		// Generic data from JSON
		for k, v := range data {
			if k == "name" || k == "title" {
				continue // already shown
			}
			s := fmt.Sprintf("%v", v)
			if len(s) > 60 {
				s = s[:57] + "..."
			}
			fields = append(fields, fmt.Sprintf("%s: %s", k, s))
		}
	}

	if len(fields) == 0 {
		return ""
	}

	dimFields := make([]string, len(fields))
	for i, f := range fields {
		dimFields[i] = lipgloss.NewStyle().Foreground(colorMuted).Render(f)
	}
	return strings.Join(dimFields, "  ")
}
