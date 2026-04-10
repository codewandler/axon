package main

import (
	"fmt"

	"github.com/codewandler/axon/graph"
	"github.com/spf13/cobra"
)

var (
	flagGCDryRun bool
	flagGCQuiet  bool
)

var gcCmd = &cobra.Command{
	Use:   "gc",
	Short: "Run garbage collection on the graph database",
	Long: `Run garbage collection to clean up orphaned edges.

Orphaned edges are edges that point to nodes that no longer exist.
These can accumulate when files are deleted and indexing is run with --no-gc,
or when the optimization skips GC because no nodes were deleted.

Use --dry-run to see what would be cleaned without making changes.
Use --quiet / -q to suppress per-record listing and show only the summary line.`,
	RunE: runGC,
}

func init() {
	gcCmd.Flags().BoolVar(&flagGCDryRun, "dry-run", false, "Show what would be cleaned without making changes")
	gcCmd.Flags().BoolVarP(&flagGCQuiet, "quiet", "q", false, "Suppress per-record listing; show only the summary line")
}

// resolveNodeDesc returns a short human-readable description for a node ID.
// If the node exists: "type name" (or just "type" if name is blank).
// If the node is missing (orphaned endpoint): "<missing>".
func resolveNodeDesc(cmdCtx *CommandContext, id string) string {
	node, err := cmdCtx.Storage.GetNode(cmdCtx.Ctx, id)
	if err != nil {
		// Node is missing — expected for orphaned edges.
		return "<missing>"
	}
	if node.Name != "" {
		return node.Type + " " + node.Name
	}
	return node.Type
}

// printOrphanedEdges prints a verbose listing of the supplied orphaned edges.
func printOrphanedEdges(cmdCtx *CommandContext, edges []*graph.Edge) {
	fmt.Printf("\nOrphaned edges (%d):\n", len(edges))
	for _, e := range edges {
		from := resolveNodeDesc(cmdCtx, e.From)
		to := resolveNodeDesc(cmdCtx, e.To)
		fmt.Printf("  [%s]  (from: %s)  →  (to: %s)\n",
			e.Type, from, to)
	}
	fmt.Println()
}

func runGC(cmd *cobra.Command, args []string) error {
	cmdCtx, err := openDB(false)
	if err != nil {
		return err
	}
	defer cmdCtx.Close()

	fmt.Printf("Using database: %s\n", cmdCtx.DBLoc.Path)

	if flagGCDryRun {
		if flagGCQuiet {
			// Quiet dry-run: count only.
			count, err := cmdCtx.Storage.CountOrphanedEdges(cmdCtx.Ctx)
			if err != nil {
				return fmt.Errorf("count orphaned edges: %w", err)
			}
			if count == 0 {
				fmt.Println("No orphaned edges found")
			} else {
				fmt.Printf("Would delete %d orphaned edges  (dry run — no changes made)\n", count)
			}
			return nil
		}

		// Verbose dry-run: list every edge.
		edges, err := cmdCtx.Storage.FindOrphanedEdges(cmdCtx.Ctx)
		if err != nil {
			return fmt.Errorf("find orphaned edges: %w", err)
		}
		if len(edges) == 0 {
			fmt.Println("No orphaned edges found")
			return nil
		}
		printOrphanedEdges(cmdCtx, edges)
		fmt.Printf("Would delete %d orphaned edges  (dry run — no changes made)\n", len(edges))
		return nil
	}

	// Normal run (actually delete).
	if flagGCQuiet {
		deleted, err := cmdCtx.Storage.DeleteOrphanedEdges(cmdCtx.Ctx)
		if err != nil {
			return fmt.Errorf("delete orphaned edges: %w", err)
		}
		if deleted == 0 {
			fmt.Println("No orphaned edges found")
		} else {
			fmt.Printf("Deleted %d orphaned edges\n", deleted)
		}
		return nil
	}

	// Verbose normal run: list edges, then delete.
	edges, err := cmdCtx.Storage.FindOrphanedEdges(cmdCtx.Ctx)
	if err != nil {
		return fmt.Errorf("find orphaned edges: %w", err)
	}
	if len(edges) == 0 {
		fmt.Println("No orphaned edges found")
		return nil
	}
	printOrphanedEdges(cmdCtx, edges)

	deleted, err := cmdCtx.Storage.DeleteOrphanedEdges(cmdCtx.Ctx)
	if err != nil {
		return fmt.Errorf("delete orphaned edges: %w", err)
	}
	fmt.Printf("Deleted %d orphaned edges\n", deleted)
	return nil
}
