package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/codewandler/axon"
	"github.com/codewandler/axon/adapters/sqlite"
	"github.com/codewandler/axon/graph"
	"github.com/codewandler/axon/types"
	"github.com/spf13/cobra"
)

var showCmd = &cobra.Command{
	Use:   "show <node-id>",
	Short: "Show details of a node",
	Long: `Display detailed information about a specific node in the graph.

You can provide a full node ID or a prefix (minimum 4 characters).
If multiple nodes match the prefix, all matches will be listed.

The output includes:
- Node metadata (type, URI, key)
- Node data (name, size, mode, etc.)
- Incoming and outgoing edges`,
	Args: cobra.ExactArgs(1),
	RunE: runShow,
}

func runShow(cmd *cobra.Command, args []string) error {
	ctx := context.Background()
	nodeID := args[0]

	if len(nodeID) < 4 {
		return fmt.Errorf("node ID must be at least 4 characters")
	}

	// Get current directory for auto-lookup
	cwd, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("failed to get current directory: %w", err)
	}

	// Resolve database location (read-only, so forWrite=false)
	dbLoc, err := resolveDB(flagDBDir, flagLocal, cwd, false)
	if err != nil {
		return err
	}

	// Print database location
	fmt.Printf("Using database: %s\n", dbLoc.Path)

	// Open SQLite storage
	storage, err := sqlite.New(dbLoc.Path)
	if err != nil {
		return fmt.Errorf("failed to open database: %w", err)
	}
	defer storage.Close()

	// Create axon instance
	ax, err := axon.New(axon.Config{
		Dir:     filepath.Dir(dbLoc.Dir),
		Storage: storage,
	})
	if err != nil {
		return fmt.Errorf("failed to create axon: %w", err)
	}

	// Find node(s) matching the ID prefix
	nodes, err := findNodesByPrefix(ctx, ax.Graph(), nodeID)
	if err != nil {
		return err
	}

	if len(nodes) == 0 {
		return fmt.Errorf("no node found matching '%s'", nodeID)
	}

	if len(nodes) > 1 {
		fmt.Printf("Multiple nodes match '%s':\n\n", nodeID)
		for _, n := range nodes {
			fmt.Printf("  %s  %s\n", n.ID, getNodeSummary(n))
		}
		return nil
	}

	// Single match - show full details
	node := nodes[0]
	return showNodeDetails(ctx, ax.Graph(), node)
}

// findNodesByPrefix finds all nodes whose ID starts with the given prefix
func findNodesByPrefix(ctx context.Context, g *graph.Graph, prefix string) ([]*graph.Node, error) {
	// Get all nodes and filter by prefix
	// TODO: Add a more efficient prefix search to storage
	allNodes, err := g.FindNodes(ctx, graph.NodeFilter{})
	if err != nil {
		return nil, err
	}

	var matches []*graph.Node
	for _, n := range allNodes {
		if strings.HasPrefix(n.ID, prefix) {
			matches = append(matches, n)
		}
	}
	return matches, nil
}

// getNodeSummary returns a brief summary of a node for listing
func getNodeSummary(n *graph.Node) string {
	var name string
	switch data := n.Data.(type) {
	case types.DirData:
		name = data.Name + "/"
	case types.FileData:
		name = data.Name
	case types.LinkData:
		name = data.Name
	case types.RepoData:
		name = data.Name
	case types.RemoteData:
		name = data.Name
	case types.BranchData:
		name = data.Name
	case types.TagData:
		name = data.Name
	case map[string]any:
		if n, ok := data["name"].(string); ok {
			name = n
		}
	}

	if name != "" {
		return fmt.Sprintf("%s (%s)", name, n.Type)
	}
	return fmt.Sprintf("(%s)", n.Type)
}

// showNodeDetails displays full details of a node
func showNodeDetails(ctx context.Context, g *graph.Graph, node *graph.Node) error {
	fmt.Printf("Node: %s\n", node.ID)
	fmt.Printf("Type: %s\n", node.Type)

	if node.URI != "" {
		fmt.Printf("URI:  %s\n", node.URI)
	}
	if node.Key != "" {
		fmt.Printf("Key:  %s\n", node.Key)
	}

	// Print data fields
	fmt.Println("\nData:")
	printNodeData(node)

	// Get and print edges
	edgesFrom, err := g.GetEdgesFrom(ctx, node.ID)
	if err != nil {
		return err
	}

	edgesTo, err := g.GetEdgesTo(ctx, node.ID)
	if err != nil {
		return err
	}

	if len(edgesTo) > 0 {
		fmt.Println("\nEdges (in):")
		for _, e := range edgesTo {
			fromNode, err := g.GetNode(ctx, e.From)
			if err != nil {
				continue
			}
			fmt.Printf("  <- %s [%s] %s\n", e.Type, shortID(fromNode.ID), getNodeSummary(fromNode))
		}
	}

	if len(edgesFrom) > 0 {
		fmt.Println("\nEdges (out):")
		for _, e := range edgesFrom {
			toNode, err := g.GetNode(ctx, e.To)
			if err != nil {
				continue
			}
			fmt.Printf("  -> %s [%s] %s\n", e.Type, shortID(toNode.ID), getNodeSummary(toNode))
		}
	}

	return nil
}

// printNodeData prints the data fields of a node
func printNodeData(node *graph.Node) {
	switch data := node.Data.(type) {
	case types.DirData:
		fmt.Printf("  Name: %s\n", data.Name)
		fmt.Printf("  Mode: %s\n", data.Mode.String())

	case types.FileData:
		fmt.Printf("  Name:     %s\n", data.Name)
		fmt.Printf("  Size:     %s\n", formatSize(data.Size))
		fmt.Printf("  Modified: %s\n", data.Modified.Format(time.RFC3339))
		fmt.Printf("  Mode:     %s\n", data.Mode.String())

	case types.LinkData:
		fmt.Printf("  Name:   %s\n", data.Name)
		fmt.Printf("  Target: %s\n", data.Target)

	case types.RepoData:
		fmt.Printf("  Name:       %s\n", data.Name)
		fmt.Printf("  IsBare:     %v\n", data.IsBare)
		if data.HeadBranch != "" {
			fmt.Printf("  HeadBranch: %s\n", data.HeadBranch)
		}
		if data.HeadCommit != "" {
			fmt.Printf("  HeadCommit: %s\n", data.HeadCommit)
		}

	case types.RemoteData:
		fmt.Printf("  Name: %s\n", data.Name)
		if len(data.URLs) > 0 {
			fmt.Printf("  URLs:\n")
			for _, url := range data.URLs {
				fmt.Printf("    - %s\n", url)
			}
		}

	case types.BranchData:
		fmt.Printf("  Name:     %s\n", data.Name)
		fmt.Printf("  IsHead:   %v\n", data.IsHead)
		fmt.Printf("  IsRemote: %v\n", data.IsRemote)
		if data.Commit != "" {
			fmt.Printf("  Commit:   %s\n", data.Commit)
		}

	case types.TagData:
		fmt.Printf("  Name: %s\n", data.Name)
		if data.Commit != "" {
			fmt.Printf("  Commit: %s\n", data.Commit)
		}

	case map[string]any:
		// Data loaded from JSON - format based on node type
		printMapData(node.Type, data)

	default:
		if node.Data != nil {
			fmt.Printf("  %+v\n", node.Data)
		} else {
			fmt.Printf("  (no data)\n")
		}
	}
}

// printMapData formats map data based on node type
func printMapData(nodeType string, data map[string]any) {
	switch nodeType {
	case types.TypeDir:
		if name, ok := data["name"].(string); ok {
			fmt.Printf("  Name: %s\n", name)
		}
		if mode, ok := data["mode"].(float64); ok {
			fmt.Printf("  Mode: %s\n", os.FileMode(uint32(mode)).String())
		}

	case types.TypeFile:
		if name, ok := data["name"].(string); ok {
			fmt.Printf("  Name:     %s\n", name)
		}
		if size, ok := data["size"].(float64); ok {
			fmt.Printf("  Size:     %s\n", formatSize(int64(size)))
		}
		if modified, ok := data["modified"].(string); ok {
			fmt.Printf("  Modified: %s\n", modified)
		}
		if mode, ok := data["mode"].(float64); ok {
			fmt.Printf("  Mode:     %s\n", os.FileMode(uint32(mode)).String())
		}

	case types.TypeLink:
		if name, ok := data["name"].(string); ok {
			fmt.Printf("  Name:   %s\n", name)
		}
		if target, ok := data["target"].(string); ok {
			fmt.Printf("  Target: %s\n", target)
		}

	case types.TypeRepo:
		if name, ok := data["name"].(string); ok {
			fmt.Printf("  Name:       %s\n", name)
		}
		if isBare, ok := data["is_bare"].(bool); ok {
			fmt.Printf("  IsBare:     %v\n", isBare)
		}
		if headBranch, ok := data["head_branch"].(string); ok && headBranch != "" {
			fmt.Printf("  HeadBranch: %s\n", headBranch)
		}
		if headCommit, ok := data["head_commit"].(string); ok && headCommit != "" {
			fmt.Printf("  HeadCommit: %s\n", headCommit)
		}

	case types.TypeRemote:
		if name, ok := data["name"].(string); ok {
			fmt.Printf("  Name: %s\n", name)
		}
		if urls, ok := data["urls"].([]any); ok && len(urls) > 0 {
			fmt.Printf("  URLs:\n")
			for _, url := range urls {
				if urlStr, ok := url.(string); ok {
					fmt.Printf("    - %s\n", urlStr)
				}
			}
		}

	case types.TypeBranch:
		if name, ok := data["name"].(string); ok {
			fmt.Printf("  Name:     %s\n", name)
		}
		if isHead, ok := data["is_head"].(bool); ok {
			fmt.Printf("  IsHead:   %v\n", isHead)
		}
		if isRemote, ok := data["is_remote"].(bool); ok {
			fmt.Printf("  IsRemote: %v\n", isRemote)
		}
		if commit, ok := data["commit"].(string); ok && commit != "" {
			fmt.Printf("  Commit:   %s\n", commit)
		}

	case types.TypeTag:
		if name, ok := data["name"].(string); ok {
			fmt.Printf("  Name: %s\n", name)
		}
		if commit, ok := data["commit"].(string); ok && commit != "" {
			fmt.Printf("  Commit: %s\n", commit)
		}

	default:
		// Generic fallback
		for k, v := range data {
			fmt.Printf("  %s: %v\n", k, v)
		}
	}
}

// formatSize formats a byte count as a human-readable string
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

// shortID returns a shortened version of the node ID
func shortID(id string) string {
	if len(id) > 7 {
		return id[:7]
	}
	return id
}
