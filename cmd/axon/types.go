package main

import (
	"context"
	"fmt"
	"os"

	"github.com/codewandler/axon"
	"github.com/codewandler/axon/adapters/sqlite"
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
	ctx := context.Background()

	// Get current directory
	cwd, err := os.Getwd()
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

	// Create Axon instance for potential auto-indexing
	ax, err := axon.New(axon.Config{
		Dir:     cwd,
		Storage: storage,
	})
	if err != nil {
		return fmt.Errorf("failed to create axon instance: %w", err)
	}

	// Resolve scope using graph traversal
	traverseOpts, err := resolveScopeTraversal(ctx, storage, ax, typesGlobal, cwd, 0)
	if err != nil {
		return err
	}

	// Traverse and count types
	results, err := storage.Traverse(ctx, traverseOpts)
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
