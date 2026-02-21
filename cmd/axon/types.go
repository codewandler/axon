package main

import (
	"fmt"
	"os"

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

	// Resolve scope using graph traversal
	traverseOpts, err := resolveScopeTraversal(cmdCtx.Ctx, cmdCtx.Storage, ax, typesGlobal, cmdCtx.Cwd, 0)
	if err != nil {
		return err
	}

	// Traverse and count types
	results, err := cmdCtx.Storage.Traverse(cmdCtx.Ctx, traverseOpts)
	if err != nil {
		return fmt.Errorf("failed to traverse graph: %w", err)
	}

	counts, err := countTraversalTypes(results)
	if err != nil {
		return fmt.Errorf("failed to count types: %w", err)
	}

	// Build result
	var result CountResult
	result.FromMap(counts)
	result.SortByCount()

	// Render output
	renderer := NewRenderer(typesOutput, os.Stdout)
	return renderer.RenderCounts(result)
}
