package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/codewandler/axon/aql"
	"github.com/codewandler/axon/graph"
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

	g := ax.Graph()
	var counts []graph.CountItem

	if edgesGlobal {
		// Global: SELECT type, COUNT(*) FROM edges GROUP BY type ORDER BY COUNT(*) DESC
		query := aql.Edges.
			Select(aql.Type, aql.Count()).
			GroupBy(aql.Type).
			OrderByCount(true).
			Build()

		result, err := g.Storage().Query(cmdCtx.Ctx, query)
		if err != nil {
			return fmt.Errorf("failed to execute query: %w", err)
		}
		counts = result.Counts
	} else {
		// Scoped: Use DescendantsOf helper
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

		query := aql.Edges.
			Select(aql.Type, aql.Count()).
			Where(aql.Edges.ScopedTo(cwdNode.ID)).
			GroupBy(aql.Type).
			OrderByCount(true).
			Build()

		result, err := g.Storage().Query(cmdCtx.Ctx, query)
		if err != nil {
			return fmt.Errorf("failed to count edge types: %w", err)
		}
		counts = result.Counts
	}

	// Build result
	var result CountResult
	result.FromSlice(counts)
	result.SortByCount()

	// Render output
	renderer := NewRenderer(edgesOutput, os.Stdout)
	return renderer.RenderCounts(result)
}
