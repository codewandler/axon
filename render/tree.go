package render

import (
	"context"
	"fmt"
	"path/filepath"
	"sort"
	"strings"

	"github.com/codewandler/axon/graph"
	"github.com/codewandler/axon/types"
)

// Type emoji mappings
var typeEmojis = map[string]string{
	types.TypeDir:               "📁",
	types.TypeFile:              "📄",
	types.TypeLink:              "🔗",
	types.TypeRepo:              "📦",
	types.TypeRemote:            "🌐",
	types.TypeBranch:            "🌿",
	types.TypeTag:               "🏷️",
	types.TypeMarkdownDoc:       "📝",
	types.TypeMarkdownSection:   "📑",
	types.TypeMarkdownCodeBlock: "💻",
	types.TypeMarkdownLink:      "🔗",
	types.TypeMarkdownImage:     "🖼️",
}

// ANSI color codes for colored output
const (
	colorReset = "\033[0m"
	colorDim   = "\033[90m" // Gray/dim color for node IDs
)

// Options configures tree rendering.
type Options struct {
	// MaxDepth limits how deep the tree renders. 0 means unlimited.
	MaxDepth int

	// ShowIDs includes node IDs in the output.
	ShowIDs bool

	// ShowTypes includes node types in the output.
	ShowTypes bool

	// Compact uses shorter output format.
	Compact bool

	// UseEmoji replaces type names with emojis.
	UseEmoji bool

	// UseColor enables ANSI color output.
	UseColor bool

	// TypeFilter restricts which node types are shown.
	// Supports glob patterns (e.g., "fs:*", "md:*").
	// If empty, all types are shown.
	TypeFilter []string
}

// DefaultOptions returns sensible default rendering options.
func DefaultOptions() Options {
	return Options{
		MaxDepth:  3,
		ShowIDs:   true,
		ShowTypes: true,
		Compact:   false,
	}
}

// Tree renders a tree starting from the given node.
func Tree(ctx context.Context, g *graph.Graph, rootID string, opts Options) (string, error) {
	root, err := g.GetNode(ctx, rootID)
	if err != nil {
		return "", err
	}

	var sb strings.Builder
	err = renderNode(ctx, g, root, &sb, "", true, 0, opts)
	if err != nil {
		return "", err
	}

	return sb.String(), nil
}

// TreeFromURI renders a tree starting from a node with the given URI.
func TreeFromURI(ctx context.Context, g *graph.Graph, uri string, opts Options) (string, error) {
	node, err := g.GetNodeByURI(ctx, uri)
	if err != nil {
		return "", err
	}
	return Tree(ctx, g, node.ID, opts)
}

func renderNode(ctx context.Context, g *graph.Graph, node *graph.Node, sb *strings.Builder, prefix string, isLast bool, depth int, opts Options) error {
	// Check if this node matches the type filter
	showNode := matchesTypeFilter(node.Type, opts.TypeFilter)

	// Get children first to determine if we should render this subtree at all
	children, err := g.Children(ctx, node.ID)
	if err != nil {
		return err
	}

	// Sort children by name
	sort.Slice(children, func(i, j int) bool {
		return GetDisplayName(children[i]) < GetDisplayName(children[j])
	})

	// If type filter is set and this node doesn't match,
	// we still need to check if any descendants match
	if len(opts.TypeFilter) > 0 && !showNode {
		// Check if any children would be rendered (recursively)
		hasMatchingDescendants := false
		for _, child := range children {
			if hasDescendantsMatching(ctx, g, child, opts.TypeFilter, opts.MaxDepth, depth+1) {
				hasMatchingDescendants = true
				break
			}
		}
		if !hasMatchingDescendants {
			return nil // Skip this entire subtree
		}
	}

	// Only render this node if it matches the filter (or no filter is set)
	if showNode {
		// Get display name
		name := GetDisplayName(node)

		// Build the line
		var line strings.Builder

		// Node ID first (on the left)
		if opts.ShowIDs {
			if opts.UseColor {
				line.WriteString(fmt.Sprintf("%s[%s]%s ", colorDim, shortID(node.ID), colorReset))
			} else {
				line.WriteString(fmt.Sprintf("[%s] ", shortID(node.ID)))
			}
		}

		// Tree branch characters
		if depth > 0 {
			if isLast {
				line.WriteString(prefix + "└── ")
			} else {
				line.WriteString(prefix + "├── ")
			}
		}

		// Type indicator (emoji or text) - before name
		if opts.UseEmoji {
			if emoji, ok := typeEmojis[node.Type]; ok {
				line.WriteString(emoji + " ")
			}
		} else if opts.ShowTypes {
			line.WriteString(fmt.Sprintf("(%s) ", node.Type))
		}

		// Name
		line.WriteString(name)

		// Check if we hit depth limit
		if opts.MaxDepth > 0 && depth >= opts.MaxDepth && len(children) > 0 {
			line.WriteString(fmt.Sprintf(" ... +%d items", len(children)))
		}

		sb.WriteString(line.String())
		sb.WriteString("\n")
	}

	// Don't recurse if at depth limit
	if opts.MaxDepth > 0 && depth >= opts.MaxDepth {
		return nil
	}

	// Render children
	childPrefix := prefix
	if depth > 0 && showNode {
		if isLast {
			childPrefix += "    "
		} else {
			childPrefix += "│   "
		}
	}

	for i, child := range children {
		isLastChild := i == len(children)-1
		if err := renderNode(ctx, g, child, sb, childPrefix, isLastChild, depth+1, opts); err != nil {
			return err
		}
	}

	return nil
}

// matchesTypeFilter checks if a node type matches any of the filter patterns.
// If filter is empty, all types match.
func matchesTypeFilter(nodeType string, filter []string) bool {
	if len(filter) == 0 {
		return true
	}
	for _, pattern := range filter {
		if matchGlob(pattern, nodeType) {
			return true
		}
	}
	return false
}

// matchGlob performs simple glob matching with * and ? wildcards.
func matchGlob(pattern, s string) bool {
	// Simple glob matching
	if pattern == s {
		return true
	}
	if pattern == "*" {
		return true
	}

	// Use filepath.Match for glob patterns
	matched, _ := filepath.Match(pattern, s)
	return matched
}

// hasDescendantsMatching checks if any descendants of a node match the type filter.
func hasDescendantsMatching(ctx context.Context, g *graph.Graph, node *graph.Node, filter []string, maxDepth, currentDepth int) bool {
	if matchesTypeFilter(node.Type, filter) {
		return true
	}

	if maxDepth > 0 && currentDepth >= maxDepth {
		return false
	}

	children, err := g.Children(ctx, node.ID)
	if err != nil {
		return false
	}

	for _, child := range children {
		if hasDescendantsMatching(ctx, g, child, filter, maxDepth, currentDepth+1) {
			return true
		}
	}
	return false
}

// GetDisplayName returns a human-readable name for a node.
func GetDisplayName(node *graph.Node) string {
	// Try to extract name from Data
	if node.Data != nil {
		switch data := node.Data.(type) {
		case types.DirData:
			return data.Name + "/"
		case types.FileData:
			return data.Name
		case types.LinkData:
			return data.Name + " -> " + data.Target
		case types.RepoData:
			return data.Name
		case types.RemoteData:
			if len(data.URLs) > 0 {
				return data.Name + " (" + data.URLs[0] + ")"
			}
			return data.Name
		case types.BranchData:
			if data.IsHead {
				return data.Name + " *"
			}
			return data.Name
		case types.TagData:
			return data.Name
		case types.DocumentData:
			return data.Title
		case types.SectionData:
			return fmt.Sprintf("%s (h%d)", data.Title, data.Level)
		case types.CodeBlockData:
			lang := data.Language
			if lang == "" {
				lang = "text"
			}
			return fmt.Sprintf("%s (%d lines)", lang, data.Lines)
		case types.MarkdownLinkData:
			if data.Text != "" {
				return fmt.Sprintf("[%s](%s)", data.Text, truncateURL(data.URL))
			}
			return truncateURL(data.URL)
		case types.ImageData:
			if data.Alt != "" {
				return fmt.Sprintf("![%s]", data.Alt)
			}
			return truncateURL(data.URL)
		case map[string]any:
			// Handle different node types from JSON data
			switch node.Type {
			case types.TypeDir:
				if name, ok := data["name"].(string); ok {
					return name + "/"
				}
			case types.TypeFile:
				if name, ok := data["name"].(string); ok {
					return name
				}
			case types.TypeMarkdownDoc:
				if title, ok := data["title"].(string); ok {
					return title
				}
			case types.TypeMarkdownSection:
				title, _ := data["title"].(string)
				level, _ := data["level"].(float64) // JSON numbers are float64
				return fmt.Sprintf("%s (h%d)", title, int(level))
			case types.TypeMarkdownCodeBlock:
				lang, _ := data["language"].(string)
				lines, _ := data["lines"].(float64)
				if lang == "" {
					lang = "text"
				}
				return fmt.Sprintf("%s (%d lines)", lang, int(lines))
			case types.TypeMarkdownLink:
				text, _ := data["text"].(string)
				url, _ := data["url"].(string)
				if text != "" {
					return fmt.Sprintf("[%s](%s)", text, truncateURL(url))
				}
				return truncateURL(url)
			case types.TypeMarkdownImage:
				alt, _ := data["alt"].(string)
				url, _ := data["url"].(string)
				if alt != "" {
					return fmt.Sprintf("![%s]", alt)
				}
				return truncateURL(url)
			default:
				if name, ok := data["name"].(string); ok {
					return name
				}
				if title, ok := data["title"].(string); ok {
					return title
				}
			}
		}
	}

	// Fall back to extracting from URI or Key
	if node.Key != "" {
		return filepath.Base(node.Key)
	}
	if node.URI != "" {
		return filepath.Base(types.URIToPath(node.URI))
	}

	return node.ID
}

func truncateURL(url string) string {
	if len(url) > 40 {
		return url[:37] + "..."
	}
	return url
}

func shortID(id string) string {
	if len(id) > 7 {
		return id[:7]
	}
	return id
}

// RenderChain renders a linear chain of nodes (root to leaf) as a tree.
// This is useful for showing parent paths in find results.
func RenderChain(nodes []*graph.Node, opts Options) string {
	var sb strings.Builder
	for i, node := range nodes {
		// Build prefix for tree structure
		prefix := ""
		if i > 0 {
			prefix = strings.Repeat("    ", i-1) + "└── "
		}

		// ID
		if opts.ShowIDs {
			if opts.UseColor {
				sb.WriteString(fmt.Sprintf("%s[%s]%s ", colorDim, shortID(node.ID), colorReset))
			} else {
				sb.WriteString(fmt.Sprintf("[%s] ", shortID(node.ID)))
			}
		}

		// Tree branch
		sb.WriteString(prefix)

		// Type indicator (emoji or text) - before name
		if opts.UseEmoji {
			if emoji, ok := typeEmojis[node.Type]; ok {
				sb.WriteString(emoji + " ")
			}
		} else if opts.ShowTypes {
			sb.WriteString(fmt.Sprintf("(%s) ", node.Type))
		}

		// Name
		sb.WriteString(GetDisplayName(node))
		sb.WriteString("\n")
	}
	return sb.String()
}
