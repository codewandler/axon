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
	typesGlobal bool
	typesOutput string
)

var typesCmd = &cobra.Command{
	Use:   "types",
	Short: "List all node types with counts",
	Long: `List all unique node types in the graph with their occurrence counts.

By default, scoped to the current directory. Use --global for entire graph.

Examples:
  axon types               # Types in current directory
  axon types --global      # All types in graph
  axon types -o json       # JSON output`,
	RunE: runTypes,
}

func init() {
	typesCmd.Flags().BoolVarP(&typesGlobal, "global", "g", false, "Search entire graph")
	typesCmd.Flags().StringVarP(&typesOutput, "output", "o", "text", "Output format: text, json")
}

func runTypes(cmd *cobra.Command, args []string) error {
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
	ctx := cmdCtx.Ctx
	cwd := cmdCtx.Cwd

	var query *aql.Query

	if typesGlobal {
		// Global: SELECT type, COUNT(*) FROM nodes GROUP BY type ORDER BY COUNT(*) DESC
		query = aql.Nodes.
			Select(aql.Type, aql.Count()).
			GroupBy(aql.Type).
			OrderByCount(true).
			Build()
	} else {
		// Scoped: Add WHERE with DescendantsOf
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

			cwdNode, err = g.Storage().GetNodeByURI(ctx, uri)
			if err != nil {
				return fmt.Errorf("failed to find directory after indexing: %w", err)
			}
		}

		query = aql.Nodes.
			Select(aql.Type, aql.Count()).
			Where(aql.Nodes.ScopedTo(cwdNode.ID)).
			GroupBy(aql.Type).
			OrderByCount(true).
			Build()
	}

	result, err := g.Storage().Query(ctx, query)
	if err != nil {
		return fmt.Errorf("failed to execute query: %w", err)
	}

	// Build result from counts
	var countResult CountResult
	countResult.FromSlice(result.Counts)
	// Already sorted by COUNT DESC from query

	// Render output
	renderer := NewRenderer(typesOutput, os.Stdout)
	return renderer.RenderCounts(countResult)
}
