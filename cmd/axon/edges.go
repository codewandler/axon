package main

import (
	"context"
	"fmt"
	"os"

	"github.com/codewandler/axon/adapters/sqlite"
	"github.com/codewandler/axon/graph"
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

	// Build scoped filter
	scope := resolveScope(edgesGlobal, cwd)
	filter := buildScopedEdgeFilter(scope)

	// Query edge type counts
	counts, err := storage.CountEdges(ctx, filter, graph.QueryOptions{
		GroupBy: "type",
		OrderBy: "count",
		Desc:    true,
	})
	if err != nil {
		return fmt.Errorf("failed to count edges: %w", err)
	}

	// Build result
	var result CountResult
	result.FromMap(counts)
	result.SortByCount()

	// Render output
	renderer := NewRenderer(edgesOutput, os.Stdout)
	return renderer.RenderCounts(result)
}
