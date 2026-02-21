package main

import (
	"fmt"
	"os"

	"github.com/codewandler/axon/graph"
	"github.com/spf13/cobra"
)

var (
	labelsGlobal   bool
	labelsCategory string
	labelsOutput   string
)

var labelsCmd = &cobra.Command{
	Use:   "labels",
	Short: "List all labels with counts",
	Long: `List all unique labels in the graph with their occurrence counts.

By default, scoped to the current directory. Use --global for entire graph.

Examples:
  axon labels              # Labels in current directory
  axon labels --global     # All labels in graph
  axon labels -c ci        # Only ci:* labels
  axon labels -o json      # JSON output`,
	RunE: runLabels,
}

func init() {
	labelsCmd.Flags().BoolVarP(&labelsGlobal, "global", "g", false, "Search entire graph")
	labelsCmd.Flags().StringVarP(&labelsCategory, "category", "c", "", "Filter by category prefix (e.g., 'ci' for ci:*)")
	labelsCmd.Flags().StringVarP(&labelsOutput, "output", "o", "text", "Output format: text, json")
}

func runLabels(cmd *cobra.Command, args []string) error {
	cmdCtx, err := openDB(false)
	if err != nil {
		return err
	}
	defer cmdCtx.Close()

	// Build scoped filter
	scope := resolveScope(labelsGlobal, cmdCtx.Cwd)
	filter := buildScopedNodeFilter(scope)

	// Query label counts
	counts, err := cmdCtx.Storage.CountNodes(cmdCtx.Ctx, filter, graph.QueryOptions{
		GroupBy: "label",
		OrderBy: "count",
		Desc:    true,
	})
	if err != nil {
		return fmt.Errorf("failed to count labels: %w", err)
	}

	// Build result
	var result CountResult
	result.FromMap(counts)

	// Filter by category if specified
	if labelsCategory != "" {
		result.FilterByPrefix(labelsCategory + ":")
	}

	// Sort by count (already sorted from query, but ensure consistency)
	result.SortByCount()

	// Render output
	renderer := NewRenderer(labelsOutput, os.Stdout)
	return renderer.RenderCounts(result)
}
