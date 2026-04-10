package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/codewandler/axon/aql"
	"github.com/codewandler/axon/graph"
	"github.com/codewandler/axon/render"
	"github.com/codewandler/axon/types"
	"github.com/mattn/go-isatty"
	"github.com/spf13/cobra"
)

var (
	findType        []string
	findExcludeType []string
	findName        string
	findQuery      string
	findData       []string
	findLabels     []string
	findExt        []string
	findCount      bool
	findLimit      int
	findMinScore   float64
	findOutput     string
	findShowParent bool
	findCompact    bool
	findGlobal     bool
	findShowQuery  bool
)

var findCmd = &cobra.Command{
	Use:   "find [query] [flags]",
	Short: "Search for nodes in the graph",
	Long: `Search for nodes in the graph.

With a text argument, performs semantic vector similarity search (requires
embeddings — run 'axon index --embed' first). Any flags are applied as
post-filters on the semantic results.

With no text argument, filters nodes by flags only (existing behaviour).

Examples:
  # Semantic search
  axon find "error handling"
  axon find "concurrency and goroutines" --type go:func
  axon find "recent logo commits"        --type vcs:commit --limit 5
  axon find "storage interface design"   --type go:interface --global

  # Flag-only (unchanged)
  axon find --type "md:*"
  axon find --name README.md
  axon find --type vcs:branch --global --query "feature*"
  axon find --type vcs:repo --global --count
  axon find --type vcs:branch --global --show-parent
  axon find --label ci:config
  axon find --label build:config --label lang:go
  axon find --label test:file --ext go
  axon find --ext go --ext py
  axon find --type fs:file --ext go --show-query`,
	Args: cobra.MaximumNArgs(1),
	RunE: runFind,
}

func init() {
	findCmd.Flags().StringArrayVarP(&findType, "type", "t", nil, "Node type pattern (glob: 'fs:*', 'md:*'). Can be repeated.")
	findCmd.Flags().StringArrayVar(&findExcludeType, "exclude-type", nil, "Exclude node type from results (e.g. vcs:commit). Can be repeated.")
	findCmd.Flags().StringVar(&findName, "name", "", "Exact name match")
	findCmd.Flags().StringVarP(&findQuery, "query", "q", "", "Name pattern with wildcards ('README*', '*test*')")
	findCmd.Flags().StringArrayVar(&findData, "data", nil, "Match on data field (key=value). Can be repeated.")
	findCmd.Flags().StringArrayVar(&findLabels, "label", nil, "Filter by label (OR logic). Can be repeated.")
	findCmd.Flags().StringArrayVarP(&findExt, "ext", "e", nil, "Filter by file extension without dot (e.g., 'go', 'py'). Can be repeated.")
	findCmd.Flags().BoolVarP(&findCount, "count", "c", false, "Just show count")
	findCmd.Flags().IntVarP(&findLimit, "limit", "l", 0, "Limit results (0 for unlimited)")
	findCmd.Flags().Float64Var(&findMinScore, "min-score", 0.5, "Minimum similarity score for semantic results (0 to disable)")
	findCmd.Flags().StringVarP(&findOutput, "output", "o", "path", "Output format: path, uri, json, table")
	findCmd.Flags().BoolVar(&findShowParent, "show-parent", false, "Show parent chain to CWD or root")
	findCmd.Flags().BoolVar(&findCompact, "compact", false, "Hide parent chain details (with --show-parent)")
	findCmd.Flags().BoolVarP(&findGlobal, "global", "g", false, "Search entire graph, not just CWD subtree")
	findCmd.Flags().BoolVar(&findShowQuery, "show-query", false, "Print the generated AQL query")
}

func runFind(cmd *cobra.Command, args []string) error {
	if len(args) == 1 {
		return runSemanticFind(args[0])
	}
	cmdCtx, err := openDB(false)
	if err != nil {
		return err
	}
	defer cmdCtx.Close()

	// Get Axon instance
	ax, err := cmdCtx.Axon()
	if err != nil {
		return err
	}

	g := ax.Graph()
	ctx := cmdCtx.Ctx
	cwd := cmdCtx.Cwd

	// Build AQL query - start with table reference
	var q *aql.Builder
	if findCount {
		q = aql.Nodes.Select(aql.Count())
	} else {
		q = aql.Nodes.SelectStar()
	}

	// Build WHERE conditions
	var conditions []aql.Expression

	if !findGlobal {
		// Scoped to CWD: query descendants of current directory
		absPath, err := filepath.Abs(cwd)
		if err != nil {
			return fmt.Errorf("failed to resolve path: %w", err)
		}

		uri := types.PathToURI(absPath)
		cwdNode, err := g.Storage().GetNodeByURI(ctx, uri)
		if err != nil {
			// Directory not indexed - prompt to index
			fmt.Printf("Directory not indexed: %s\nIndex now? [Y/n] ", absPath)
			var response string
			fmt.Scanln(&response)
			response = strings.TrimSpace(strings.ToLower(response))
			if response != "" && response != "y" && response != "yes" {
				return fmt.Errorf("directory not indexed: %s", absPath)
			}

			fmt.Printf("Indexing %s...\n", absPath)
			if _, err := ax.Index(ctx, absPath); err != nil {
				return fmt.Errorf("indexing failed: %w", err)
			}
			fmt.Println("Done.")

			// Try again after indexing
			cwdNode, err = g.Storage().GetNodeByURI(ctx, uri)
			if err != nil {
				return fmt.Errorf("failed to find directory after indexing: %w", err)
			}
		}

		conditions = append(conditions, aql.Nodes.ScopedTo(cwdNode.ID))
	}

	// Type matching - handle multiple types with OR logic
	if len(findType) > 0 {
		var typeConditions []aql.Expression
		for _, t := range findType {
			if strings.Contains(t, "*") || strings.Contains(t, "?") {
				// Use GLOB for pattern matching (indexed)
				typeConditions = append(typeConditions, aql.Type.Glob(t))
			} else {
				// Exact match
				typeConditions = append(typeConditions, aql.Type.Eq(t))
			}
		}
		if len(typeConditions) == 1 {
			conditions = append(conditions, typeConditions[0])
		} else {
			conditions = append(conditions, aql.Or(typeConditions...))
		}
	}

	// Name matching
	if findName != "" {
		// Exact match
		conditions = append(conditions, aql.Name.Eq(findName))
	} else if findQuery != "" {
		// Pattern match with GLOB (indexed)
		conditions = append(conditions, aql.Name.Glob(findQuery))
	}

	// Label filtering (OR logic - node has at least one of the labels)
	if len(findLabels) > 0 {
		var labelConditions []aql.Expression
		for _, label := range findLabels {
			labelConditions = append(labelConditions, aql.Labels.ContainsAny(label))
		}
		if len(labelConditions) == 1 {
			conditions = append(conditions, labelConditions[0])
		} else {
			conditions = append(conditions, aql.Or(labelConditions...))
		}
	}

	// Extension filtering (OR logic - node has one of the extensions)
	if len(findExt) > 0 {
		var extConditions []aql.Expression
		// Extensions are stored in data.ext without a leading dot.
		// Accept both 'go' and '.go' from the user for convenience.
		for _, ext := range findExt {
			ext = strings.TrimPrefix(ext, ".")
			extConditions = append(extConditions, aql.DataExt.Eq(ext))
		}
		if len(extConditions) == 1 {
			conditions = append(conditions, extConditions[0])
		} else {
			conditions = append(conditions, aql.Or(extConditions...))
		}
	}

	// ExcludeTypes: NOT (type = a OR type = b ...)
	if len(findExcludeType) > 0 {
		var excl []aql.Expression
		for _, t := range findExcludeType {
			excl = append(excl, aql.Type.Eq(t))
		}
		if len(excl) == 1 {
			conditions = append(conditions, aql.Not(excl[0]))
		} else {
			// Wrap in Paren so NOT applies to the whole OR: NOT (a OR b)
			conditions = append(conditions, aql.Not(aql.Paren(aql.Or(excl...))))
		}
	}

	// Data field filters (AND logic - all must match)
	for _, df := range findData {
		parts := strings.SplitN(df, "=", 2)
		if len(parts) == 2 {
			key, value := parts[0], parts[1]
			conditions = append(conditions, aql.Data.Field(key).Eq(value))
		}
	}

	// Combine all conditions with AND
	if len(conditions) > 0 {
		q = q.Where(aql.And(conditions...))
	}

	// Add limit if specified (but not with --count, as COUNT ignores LIMIT)
	if findLimit > 0 {
		if findCount {
			fmt.Fprintf(os.Stderr, "Warning: --limit is ignored when using --count\n")
		} else {
			q = q.Limit(findLimit)
		}
	}

	// Build query
	query := q.Build()

	// Show query if requested
	if findShowQuery {
		fmt.Println("Generated AQL Query:")
		fmt.Println(query.String())
		fmt.Println()
	}

	// Execute query
	result, err := g.Storage().Query(ctx, query)
	if err != nil {
		return fmt.Errorf("failed to execute query: %w", err)
	}

	// Handle COUNT queries
	if findCount {
		count := result.Count()
		fmt.Printf("%d\n", count)
		return nil
	}

	allNodes := result.Nodes

	// Note: Scoping is now handled via EXISTS in the AQL query
	// Note: Data field filters are now handled in the AQL query above
	// Note: Limit is now handled in the AQL query above

	// Output

	if len(allNodes) == 0 {
		// For structured output formats emit a valid empty value.
		// For path/uri/default, silent empty output is acceptable Unix behaviour.
		switch findOutput {
		case "json":
			fmt.Println("[]")
		case "table":
			return outputTable(allNodes)
		}
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

func outputPath(nodes []*graph.Node, showParent, compact bool, ctx context.Context, g *graph.Graph, cwd string, isTTY bool) error {
	if showParent {
		return outputWithParents(nodes, compact, ctx, g, cwd, isTTY)
	}

	for _, node := range nodes {
		fmt.Printf("[%s] %s (%s)\n", shortID(node.ID), nodeDisplay(node), node.Type)
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
	fmt.Fprintln(w, "ID\tTYPE\tNAME\tLABELS\tURI")
	for _, node := range nodes {
		name := node.Name
		if name == "" {
			name = "-"
		}
		labels := "-"
		if len(node.Labels) > 0 {
			labels = strings.Join(node.Labels, ", ")
		}
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\n", shortID(node.ID), node.Type, name, labels, node.URI)
	}
	return w.Flush()
}

// runSemanticFind performs vector similarity search for the given text query,
// then applies any active flag filters as post-filters on the results.
func runSemanticFind(query string) error {
	cmdCtx, err := openDB(false)
	if err != nil {
		return err
	}
	defer cmdCtx.Close()

	ctx := cmdCtx.Ctx

	provider, err := resolveEmbeddingProvider("", "")
	if err != nil {
		return fmt.Errorf(
			"no embedding provider available — run 'axon index --embed .' to generate embeddings first: %w",
			err,
		)
	}
	fmt.Fprintf(os.Stderr, "Using embedding provider: %s\n", provider.Name())

	embedding, err := provider.Embed(ctx, query)
	if err != nil {
		return fmt.Errorf("embedding query: %w", err)
	}

	// Build NodeFilter from active flags.
	filter := &graph.NodeFilter{}
	if len(findType) == 1 {
		filter.Type = findType[0]
	} else if len(findType) > 1 {
		// FindSimilar only supports a single type; warn and use the first.
		fmt.Fprintf(os.Stderr, "Warning: semantic search supports one --type at a time; using %q\n", findType[0])
		filter.Type = findType[0]
	}
	filter.Labels = findLabels
	filter.Extensions = findExt
	filter.ExcludeTypes = findExcludeType

	// Local scope: restrict to nodes whose URI is under the CWD.
	// Only applied when --type is set, so we can pick the correct URI scheme.
	// Without --type, all embedded node types use different schemes (git+file://,
	// go+file://, file+md://) so a single prefix would exclude most results;
	// the local DB file already provides repo-level scoping in that case.
	if !findGlobal && filter.Type != "" {
		absPath, err := filepath.Abs(cmdCtx.Cwd)
		if err != nil {
			return err
		}
		filter.URIPrefix = uriPrefixForScope(absPath, filter.Type)
	}

	limit := findLimit
	if limit <= 0 {
		limit = 20 // sensible default for semantic results
	}

	results, err := cmdCtx.Storage.FindSimilar(ctx, embedding, limit, filter)
	if err != nil {
		return fmt.Errorf("similarity search: %w", err)
	}

	if len(results) == 0 {
		fmt.Println("No results found.")
		fmt.Println("Tip: run 'axon index --embed .' to generate embeddings first.")
		return nil
	}

	// Drop results below the minimum score threshold.
	if findMinScore > 0 {
		filtered := results[:0]
		for _, r := range results {
			if float64(r.Score) >= findMinScore {
				filtered = append(filtered, r)
			}
		}
		results = filtered
	}

	if len(results) == 0 {
		fmt.Printf("No results above minimum score %.2f (use --min-score 0 to see all results).\n", findMinScore)
		return nil
	}

	return outputSemanticResults(results, findOutput)
}

// outputSemanticResults renders similarity-search results in the requested format.
func outputSemanticResults(results []*graph.NodeWithScore, format string) error {
	switch format {
	case "json":
		type jsonResult struct {
			Score float32 `json:"score"`
			*graph.Node
		}
		out := make([]jsonResult, len(results))
		for i, r := range results {
			out[i] = jsonResult{Score: r.Score, Node: r.Node}
		}
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(out)

	case "table":
		w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
		fmt.Fprintln(w, "Score\tID\tType\tName\tURI")
		for _, r := range results {
			fmt.Fprintf(w, "%.3f\t%s\t%s\t%s\t%s\n",
				r.Score, shortID(r.ID), r.Type, r.Name, truncate(r.URI, 60))
		}
		return w.Flush()

	case "uri":
		for _, r := range results {
			fmt.Printf("%.3f  [%s] %s (%s)\n", r.Score, shortID(r.ID), r.URI, r.Type)
		}
		return nil

	default: // "path"
		for _, r := range results {
			fmt.Printf("%.3f  [%s] %s (%s)\n", r.Score, shortID(r.ID), nodeDisplay(r.Node), r.Type)
		}
		return nil
	}
}

// uriPrefixForScope delegates to types.URIPrefixForType.
func uriPrefixForScope(absPath, nodeType string) string {
	return types.URIPrefixForType(nodeType, absPath)
}

// escapeSQL escapes single quotes in a string for safe interpolation into AQL/SQL.
func escapeSQL(s string) string {
	return strings.ReplaceAll(s, "'", "''")
}

// nodeDisplay returns the display string for a node in find output.
// For vcs:commit nodes it shows enriched metadata via commitDisplay.
// For filesystem nodes it returns the path. For all others it falls back to Key then Name.
func nodeDisplay(node *graph.Node) string {
	if node.Type == types.TypeCommit {
		return commitDisplay(node)
	}
	path := types.URIToPath(node.URI)
	if path == "" {
		path = node.Key
	}
	if path == "" {
		path = node.Name
	}
	return path
}

// commitDisplay formats a vcs:commit node for one-line display.
// For the typed path (in-memory CommitData), delegates to CommitData.Description() --
// the single source of truth for commit formatting, accessible to all library users.
// For the map path (SQLite-loaded nodes), reconstructs CommitData and calls the same
// Description() method so output is identical in both cases.
func commitDisplay(node *graph.Node) string {
	switch d := node.Data.(type) {
	case types.CommitData:
		return d.Description()
	case map[string]any:
		t, _ := time.Parse(time.RFC3339, getMapString(d, "author_date"))
		cd := types.CommitData{
			SHA:          getMapString(d, "sha"),
			Message:      getMapString(d, "message"),
			AuthorName:   getMapString(d, "author_name"),
			AuthorDate:   t,
			FilesChanged: int(getMapFloat(d, "files_changed")),
		}
		return cd.Description()
	}
	// Last resort: use the enriched Name (includes subject after re-index).
	if node.Name != "" {
		return node.Name
	}
	return node.Key
}
