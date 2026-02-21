package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"text/tabwriter"

	"github.com/codewandler/axon"
	"github.com/codewandler/axon/adapters/sqlite"
	"github.com/codewandler/axon/graph"
	"github.com/codewandler/axon/render"
	"github.com/codewandler/axon/types"
	"github.com/mattn/go-isatty"
	"github.com/spf13/cobra"
)

var (
	findType       []string
	findName       string
	findQuery      string
	findData       []string
	findCount      bool
	findLimit      int
	findOutput     string
	findShowParent bool
	findCompact    bool
	findGlobal     bool
)

var findCmd = &cobra.Command{
	Use:   "find [flags]",
	Short: "Search for nodes in the graph",
	Long: `Search for nodes in the graph using various filters.

By default, searches within the current directory subtree. Use --global to search
the entire indexed graph.

Examples:
  # Find all markdown documents
  axon find --type "md:*"

  # Find files named README.md
  axon find --name README.md

  # Find all branches (with wildcard query)
  axon find --type vcs:branch --query "feature*"

  # Count git repositories
  axon find --type vcs:repo --count

  # Show nodes with parent chain
  axon find --type vcs:branch --show-parent`,
	RunE: runFind,
}

func init() {
	findCmd.Flags().StringArrayVarP(&findType, "type", "t", nil, "Node type pattern (glob: 'fs:*', 'md:*'). Can be repeated.")
	findCmd.Flags().StringVar(&findName, "name", "", "Exact name match")
	findCmd.Flags().StringVarP(&findQuery, "query", "q", "", "Name pattern with wildcards ('README*', '*test*')")
	findCmd.Flags().StringArrayVar(&findData, "data", nil, "Match on data field (key=value). Can be repeated.")
	findCmd.Flags().BoolVarP(&findCount, "count", "c", false, "Just show count")
	findCmd.Flags().IntVarP(&findLimit, "limit", "n", 0, "Limit results (0 for unlimited)")
	findCmd.Flags().StringVarP(&findOutput, "output", "o", "path", "Output format: path, uri, json, table")
	findCmd.Flags().BoolVar(&findShowParent, "show-parent", false, "Show parent chain to CWD or root")
	findCmd.Flags().BoolVar(&findCompact, "compact", false, "Hide parent chain details (with --show-parent)")
	findCmd.Flags().BoolVarP(&findGlobal, "global", "g", false, "Search entire graph, not just CWD subtree")
}

func runFind(cmd *cobra.Command, args []string) error {
	ctx := context.Background()

	// Get current directory for scoping
	cwd, err := filepath.Abs(".")
	if err != nil {
		return fmt.Errorf("failed to get current directory: %w", err)
	}

	// Resolve database location
	dbLoc, err := resolveDB(flagDBDir, flagLocal, cwd, false)
	if err != nil {
		return err
	}

	// Open SQLite storage
	storage, err := sqlite.New(dbLoc.Path)
	if err != nil {
		return fmt.Errorf("failed to open database: %w", err)
	}
	defer storage.Close()

	// Create axon instance
	ax, err := axon.New(axon.Config{
		Dir:     cwd,
		Storage: storage,
	})
	if err != nil {
		return fmt.Errorf("failed to create axon: %w", err)
	}

	g := ax.Graph()

	// Build filter
	filter := graph.NodeFilter{}

	// Note: We don't use URIPrefix for scoping because different node types
	// have different URI schemes (file://, file+md://, git+file://). Instead,
	// we post-filter by path below.

	// Type pattern - if multiple, we'll need to do multiple queries
	// For now, support single type pattern in filter
	typePatterns := findType
	if len(typePatterns) == 1 {
		if strings.Contains(typePatterns[0], "*") || strings.Contains(typePatterns[0], "?") {
			filter.TypePattern = typePatterns[0]
		} else {
			filter.Type = typePatterns[0]
		}
		typePatterns = nil // Handled by filter
	}

	// Name matching
	if findName != "" {
		filter.Name = findName
	} else if findQuery != "" {
		filter.NamePattern = findQuery
	}

	// Find nodes
	var allNodes []*graph.Node

	if len(typePatterns) > 1 {
		// Multiple type patterns - run separate queries
		for _, tp := range typePatterns {
			f := filter
			if strings.Contains(tp, "*") || strings.Contains(tp, "?") {
				f.TypePattern = tp
			} else {
				f.Type = tp
			}
			nodes, err := g.FindNodes(ctx, f)
			if err != nil {
				return fmt.Errorf("failed to find nodes: %w", err)
			}
			allNodes = append(allNodes, nodes...)
		}
	} else {
		nodes, err := g.FindNodes(ctx, filter)
		if err != nil {
			return fmt.Errorf("failed to find nodes: %w", err)
		}
		allNodes = nodes
	}

	// Filter by path scope (post-filter since URI schemes differ)
	if !findGlobal {
		allNodes = filterByPathPrefix(allNodes, cwd)
	}

	// Filter by data fields (post-filter since storage doesn't support this)
	if len(findData) > 0 {
		allNodes = filterByData(allNodes, findData)
	}

	// Apply limit
	if findLimit > 0 && len(allNodes) > findLimit {
		allNodes = allNodes[:findLimit]
	}

	// Output
	if findCount {
		fmt.Printf("%d\n", len(allNodes))
		return nil
	}

	if len(allNodes) == 0 {
		return nil
	}

	// Detect TTY for color support
	isTTY := isatty.IsTerminal(os.Stdout.Fd()) || isatty.IsCygwinTerminal(os.Stdout.Fd())

	switch findOutput {
	case "path":
		return outputPath(allNodes, findShowParent, findCompact, ctx, g, cwd, isTTY)
	case "uri":
		return outputURI(allNodes)
	case "json":
		return outputJSON(allNodes)
	case "table":
		return outputTable(allNodes)
	default:
		return fmt.Errorf("unknown output format: %s", findOutput)
	}
}

func filterByPathPrefix(nodes []*graph.Node, pathPrefix string) []*graph.Node {
	var result []*graph.Node
	for _, node := range nodes {
		nodePath := extractPathFromURI(node.URI)
		if nodePath != "" && strings.HasPrefix(nodePath, pathPrefix) {
			result = append(result, node)
		}
	}
	return result
}

// extractPathFromURI extracts the filesystem path from various URI schemes.
// Handles file://, file+md://, git+file://, etc.
func extractPathFromURI(uri string) string {
	// Handle file:// scheme
	if strings.HasPrefix(uri, "file://") {
		return strings.TrimPrefix(uri, "file://")
	}

	// Handle file+md:// scheme (markdown nodes)
	if strings.HasPrefix(uri, "file+md://") {
		path := strings.TrimPrefix(uri, "file+md://")
		// Remove fragment (section anchor)
		if idx := strings.Index(path, "#"); idx != -1 {
			path = path[:idx]
		}
		return path
	}

	// Handle git+file:// scheme
	if strings.HasPrefix(uri, "git+file://") {
		path := strings.TrimPrefix(uri, "git+file://")
		// Remove fragment or query
		if idx := strings.Index(path, "#"); idx != -1 {
			path = path[:idx]
		}
		if idx := strings.Index(path, "?"); idx != -1 {
			path = path[:idx]
		}
		return path
	}

	return ""
}

func filterByData(nodes []*graph.Node, dataFilters []string) []*graph.Node {
	var result []*graph.Node
	for _, node := range nodes {
		if matchesDataFilters(node, dataFilters) {
			result = append(result, node)
		}
	}
	return result
}

func matchesDataFilters(node *graph.Node, dataFilters []string) bool {
	if node.Data == nil {
		return false
	}

	dataMap, ok := toStringMap(node.Data)
	if !ok {
		return false
	}

	for _, df := range dataFilters {
		parts := strings.SplitN(df, "=", 2)
		if len(parts) != 2 {
			continue
		}
		key, value := parts[0], parts[1]
		if dataMap[key] != value {
			return false
		}
	}
	return true
}

func toStringMap(data any) (map[string]string, bool) {
	result := make(map[string]string)

	switch d := data.(type) {
	case map[string]any:
		for k, v := range d {
			result[k] = fmt.Sprintf("%v", v)
		}
		return result, true
	case map[string]string:
		return d, true
	default:
		// Try JSON marshal/unmarshal for struct types
		b, err := json.Marshal(data)
		if err != nil {
			return nil, false
		}
		var m map[string]any
		if err := json.Unmarshal(b, &m); err != nil {
			return nil, false
		}
		for k, v := range m {
			result[k] = fmt.Sprintf("%v", v)
		}
		return result, true
	}
}

func outputPath(nodes []*graph.Node, showParent, compact bool, ctx context.Context, g *graph.Graph, cwd string, isTTY bool) error {
	if showParent {
		return outputWithParents(nodes, compact, ctx, g, cwd, isTTY)
	}

	for _, node := range nodes {
		path := types.URIToPath(node.URI)
		if path == "" {
			path = node.Key
		}
		fmt.Printf("[%s] %s (%s)\n", shortID(node.ID), path, node.Type)
	}
	return nil
}

func outputWithParents(nodes []*graph.Node, compact bool, ctx context.Context, g *graph.Graph, cwd string, isTTY bool) error {
	if compact {
		// Compact mode: just show [ID] /path for each match
		for _, node := range nodes {
			// Build parent path
			pathParts := []string{}
			current := node
			for current != nil {
				path := types.URIToPath(current.URI)
				if path != "" {
					pathParts = append([]string{filepath.Base(path)}, pathParts...)
				}
				parents, err := g.Parents(ctx, current.ID)
				if err != nil || len(parents) == 0 {
					break
				}
				current = parents[0]
			}
			fullPath := "/" + strings.Join(pathParts, "/")
			fmt.Printf("[%s] %s\n", shortID(node.ID), fullPath)
		}
		return nil
	}

	// Group nodes by their ancestor paths and render as trees
	// Build trees from root to each matching node
	return outputAsTree(nodes, ctx, g, isTTY)
}

// parentEdgeTypes defines which edge types to follow when traversing parents.
// This includes structural (contained_by, belongs_to) and location (located_at) edges.
var parentEdgeTypes = map[string]bool{
	"contained_by": true,
	"belongs_to":   true,
	"located_at":   true,
}

// getParentsWithLocation returns parent nodes following containment, ownership, and location edges.
func getParentsWithLocation(ctx context.Context, g *graph.Graph, nodeID string) ([]*graph.Node, error) {
	edges, err := g.Storage().GetEdgesFrom(ctx, nodeID)
	if err != nil {
		return nil, err
	}

	nodes := make([]*graph.Node, 0)
	for _, e := range edges {
		if parentEdgeTypes[e.Type] {
			node, err := g.Storage().GetNode(ctx, e.To)
			if err != nil {
				continue
			}
			nodes = append(nodes, node)
		}
	}
	return nodes, nil
}

func outputAsTree(nodes []*graph.Node, ctx context.Context, g *graph.Graph, isTTY bool) error {
	opts := render.Options{
		ShowIDs:   true,
		ShowTypes: true,
		UseColor:  isTTY,
		UseEmoji:  isTTY,
	}

	for _, node := range nodes {
		// Build ancestor chain
		chain := []*graph.Node{node}
		current := node
		for {
			parents, err := getParentsWithLocation(ctx, g, current.ID)
			if err != nil || len(parents) == 0 {
				break
			}
			chain = append([]*graph.Node{parents[0]}, chain...)
			current = parents[0]
		}

		fmt.Print(render.RenderChain(chain, opts))
		fmt.Println()
	}
	return nil
}

func outputURI(nodes []*graph.Node) error {
	for _, node := range nodes {
		fmt.Printf("[%s] %s (%s)\n", shortID(node.ID), node.URI, node.Type)
	}
	return nil
}

func outputJSON(nodes []*graph.Node) error {
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	return enc.Encode(nodes)
}

func outputTable(nodes []*graph.Node) error {
	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "ID\tTYPE\tNAME\tURI")
	for _, node := range nodes {
		name := node.Name
		if name == "" {
			name = "-"
		}
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\n", shortID(node.ID), node.Type, name, node.URI)
	}
	return w.Flush()
}
