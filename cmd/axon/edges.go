package main

import (
	"fmt"
	"os"

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

	// Resolve scope using graph traversal
	traverseOpts, err := resolveScopeTraversal(cmdCtx.Ctx, cmdCtx.Storage, ax, edgesGlobal, cmdCtx.Cwd, 0)
	if err != nil {
		return err
	}

	// Traverse and count edge types
	results, err := cmdCtx.Storage.Traverse(cmdCtx.Ctx, traverseOpts)
	if err != nil {
		return fmt.Errorf("failed to traverse graph: %w", err)
	}

	counts, err := countTraversalEdgeTypes(results)
	if err != nil {
		return fmt.Errorf("failed to count edge types: %w", err)
	}

	// Build result
	var result CountResult
	result.FromMap(counts)
	result.SortByCount()

	// Render output
	renderer := NewRenderer(edgesOutput, os.Stdout)
	return renderer.RenderCounts(result)
}
