package main

import (
	"context"
	"fmt"

	"github.com/codewandler/axon/adapters/sqlite"
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
	ctx := context.Background()

	// Resolve database location
	dbLoc, err := resolveDB(flagDBDir, flagLocal, ".", false)
	if err != nil {
		return fmt.Errorf("failed to resolve database location: %w", err)
	}

	fmt.Printf("Using database: %s\n", dbLoc.Path)

	// Open SQLite storage
	storage, err := sqlite.New(dbLoc.Path)
	if err != nil {
		return fmt.Errorf("failed to open database: %w", err)
	}
	defer storage.Close()

	if flagGCDryRun {
		// Count orphaned edges without deleting
		count, err := storage.CountOrphanedEdges(ctx)
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
		deleted, err := storage.DeleteOrphanedEdges(ctx)
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
