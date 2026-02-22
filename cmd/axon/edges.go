package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/codewandler/axon/aql"
	"github.com/codewandler/axon/types"
	"github.com/spf13/cobra"
)

var (
	edgesGlobal bool
	edgesOutput string
)

var edgesCmd = &cobra.Command{
	Use:   "edges",
	Short: "List all edge types with counts",
	Long: `List all unique edge types in the graph with their occurrence counts.

By default, scoped to current directory (edges originating from nodes in scope).
Use --global for entire graph.

Examples:
  axon edges               # Edges in current directory
  axon edges --global      # All edges in graph
  axon edges -o json       # JSON output`,
	RunE: runEdges,
}

func init() {
	edgesCmd.Flags().BoolVarP(&edgesGlobal, "global", "g", false, "Search entire graph")
	edgesCmd.Flags().StringVarP(&edgesOutput, "output", "o", "text", "Output format: text, json")
}

func runEdges(cmd *cobra.Command, args []string) error {
	cmdCtx, err := openDB(false)
	if err != nil {
		return err
	}
	defer cmdCtx.Close()

	// Get Axon instance for potential auto-indexing
	ax, err := cmdCtx.Axon()
	if err != nil {
		return err
	}

	var counts map[string]int

	if edgesGlobal {
		// Global: Use AQL for efficient counting
		// SELECT type, COUNT(*) FROM edges GROUP BY type ORDER BY COUNT(*) DESC
		g := ax.Graph()
		q := aql.Select(aql.Col("type"), aql.Count()).
			From("edges").
			GroupByCol("type").
			OrderByExpr(aql.Count(), true).
			Build()

		result, err := g.Storage().Query(cmdCtx.Ctx, q)
		if err != nil {
			return fmt.Errorf("failed to execute query: %w", err)
		}
		counts = result.Counts
	} else {
		// Scoped: Use AQL with EXISTS pattern for edges from scoped nodes
		absPath, err := filepath.Abs(cmdCtx.Cwd)
		if err != nil {
			return fmt.Errorf("failed to resolve path: %w", err)
		}

		uri := types.PathToURI(absPath)
		cwdNode, err := cmdCtx.Storage.GetNodeByURI(cmdCtx.Ctx, uri)
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
			if _, err := ax.Index(cmdCtx.Ctx, absPath); err != nil {
				return fmt.Errorf("indexing failed: %w", err)
			}
			fmt.Println("Done.")

			cwdNode, err = cmdCtx.Storage.GetNodeByURI(cmdCtx.Ctx, uri)
			if err != nil {
				return fmt.Errorf("failed to find directory after indexing: %w", err)
			}
		}

		// Build pattern: (cwd WHERE id = cwdID)-[:contains*0..]->(edges)
		// This matches edges where from_id is in the descendant set
		g := ax.Graph()
		cwdPattern := aql.N("cwd").WithWhere(aql.Eq("id", aql.String(cwdNode.ID)))
		containsEdge := aql.AnyEdgeOfType("contains").WithMinHops(0)
		pattern := aql.Pat(cwdPattern).To(containsEdge, aql.N("edges")).Build()

		// Query: SELECT type, COUNT(*) FROM edges WHERE EXISTS pattern GROUP BY type
		q := aql.Select(aql.Col("type"), aql.Count()).
			From("edges").
			Where(aql.Exists(pattern)).
			GroupByCol("type").
			OrderByExpr(aql.Count(), true).
			Build()

		result, err := g.Storage().Query(cmdCtx.Ctx, q)
		if err != nil {
			return fmt.Errorf("failed to count edge types: %w", err)
		}
		counts = result.Counts
	}

	// Build result
	var result CountResult
	result.FromMap(counts)
	result.SortByCount()

	// Render output
	renderer := NewRenderer(edgesOutput, os.Stdout)
	return renderer.RenderCounts(result)
}
