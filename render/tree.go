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
	types.TypeDir:    "📁",
	types.TypeFile:   "📄",
	types.TypeLink:   "🔗",
	types.TypeRepo:   "📦",
	types.TypeRemote: "🌐",
	types.TypeBranch: "🌿",
	types.TypeTag:    "🏷️",
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
	// Get display name
	name := getDisplayName(node)

	// Build the line
	var line strings.Builder

	// Tree branch characters
	if depth > 0 {
		if isLast {
			line.WriteString(prefix + "└── ")
		} else {
			line.WriteString(prefix + "├── ")
		}
	}

	// Type indicator (emoji)
	if opts.UseEmoji {
		if emoji, ok := typeEmojis[node.Type]; ok {
			line.WriteString(emoji + " ")
		}
	}

	// Name
	line.WriteString(name)

	// Type (only if not using emoji and ShowTypes is enabled)
	if opts.ShowTypes && !opts.UseEmoji {
		line.WriteString(fmt.Sprintf(" (%s)", node.Type))
	}

	// Node ID (dimmed, at the end)
	if opts.ShowIDs {
		if opts.UseColor {
			line.WriteString(fmt.Sprintf(" %s[%s]%s", colorDim, shortID(node.ID), colorReset))
		} else {
			line.WriteString(fmt.Sprintf(" [%s]", shortID(node.ID)))
		}
	}

	// Get children
	children, err := g.Children(ctx, node.ID)
	if err != nil {
		return err
	}

	// Sort children by name
	sort.Slice(children, func(i, j int) bool {
		return getDisplayName(children[i]) < getDisplayName(children[j])
	})

	// Check if we hit depth limit
	if opts.MaxDepth > 0 && depth >= opts.MaxDepth && len(children) > 0 {
		line.WriteString(fmt.Sprintf(" ... +%d items", len(children)))
	}

	sb.WriteString(line.String())
	sb.WriteString("\n")

	// Don't recurse if at depth limit
	if opts.MaxDepth > 0 && depth >= opts.MaxDepth {
		return nil
	}

	// Render children
	childPrefix := prefix
	if depth > 0 {
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

func getDisplayName(node *graph.Node) string {
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
		case map[string]any:
			if name, ok := data["name"].(string); ok {
				if node.Type == types.TypeDir {
					return name + "/"
				}
				return name
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

func shortID(id string) string {
	if len(id) > 7 {
		return id[:7]
	}
	return id
}
