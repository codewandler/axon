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

	var counts map[string]int

	if labelsGlobal {
		// Global mode: Use AQL with json_each for fast label unpacking
		// Query: SELECT value, COUNT(*) FROM nodes, json_each(labels) WHERE value != '' GROUP BY value ORDER BY COUNT(*) DESC
		q := aql.Select(aql.Col("value"), aql.Count()).
			FromJoined("nodes", "json_each", "labels").
			Where(aql.Ne("value", aql.String(""))).
			GroupByCol("value").
			OrderByExpr(aql.Count(), true).
			Build()

		result, err := cmdCtx.Storage.Query(cmdCtx.Ctx, q)
		if err != nil {
			return fmt.Errorf("failed to execute query: %w", err)
		}
		counts = result.Counts
	} else {
		// Scoped mode: Use AQL with EXISTS pattern for efficient scoping
		ax, err := cmdCtx.Axon()
		if err != nil {
			return err
		}

		// Get the CWD node
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

		// Build pattern: (cwd WHERE id = 'cwdID')-[:contains*0..]->(nodes)
		cwdPattern := aql.N("cwd").WithWhere(aql.Eq("id", aql.String(cwdNode.ID)))
		containsEdge := aql.AnyEdgeOfType("contains").WithMinHops(0)
		pattern := aql.Pat(cwdPattern).To(containsEdge, aql.N("nodes")).Build()

		// Query with EXISTS pattern
		q := aql.Select(aql.Col("value"), aql.Count()).
			FromJoined("nodes", "json_each", "labels").
			Where(aql.And(
				aql.Ne("value", aql.String("")),
				aql.Exists(pattern),
			)).
			GroupByCol("value").
			OrderByExpr(aql.Count(), true).
			Build()

		result, err := cmdCtx.Storage.Query(cmdCtx.Ctx, q)
		if err != nil {
			return fmt.Errorf("failed to execute query: %w", err)
		}
		counts = result.Counts
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
