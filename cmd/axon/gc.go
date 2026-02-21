package main

import (
	"fmt"

	"github.com/spf13/cobra"
)

var (
	flagGCDryRun bool
)

var gcCmd = &cobra.Command{
	Use:   "gc",
	Short: "Run garbage collection on the graph database",
	Long: `Run garbage collection to clean up orphaned edges.

Orphaned edges are edges that point to nodes that no longer exist.
These can accumulate when files are deleted and indexing is run with --no-gc,
or when the optimization skips GC because no nodes were deleted.

Use --dry-run to see what would be cleaned without making changes.`,
	RunE: runGC,
}

func init() {
	gcCmd.Flags().BoolVar(&flagGCDryRun, "dry-run", false, "Show what would be cleaned without making changes")
}

func runGC(cmd *cobra.Command, args []string) error {
	cmdCtx, err := openDB(false)
	if err != nil {
		return err
	}
	defer cmdCtx.Close()

	fmt.Printf("Using database: %s\n", cmdCtx.DBLoc.Path)

	if flagGCDryRun {
		// Count orphaned edges without deleting
		count, err := cmdCtx.Storage.CountOrphanedEdges(cmdCtx.Ctx)
		if err != nil {
			return fmt.Errorf("failed to count orphaned edges: %w", err)
		}
		if count == 0 {
			fmt.Println("No orphaned edges found")
		} else {
			fmt.Printf("Would delete %d orphaned edges\n", count)
		}
	} else {
		// Actually delete orphaned edges
		deleted, err := cmdCtx.Storage.DeleteOrphanedEdges(cmdCtx.Ctx)
		if err != nil {
			return fmt.Errorf("failed to delete orphaned edges: %w", err)
		}
		if deleted == 0 {
			fmt.Println("No orphaned edges found")
		} else {
			fmt.Printf("Deleted %d orphaned edges\n", deleted)
		}
	}

	return nil
}
