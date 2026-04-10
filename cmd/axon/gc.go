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
	Long: `Run garbage collection to clean up expired and orphaned records.

Expired nodes and edges are records whose TTL has elapsed. They are invisible
to all read paths already, but "axon gc" physically removes them.

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
			expNodes, expEdges, err := countExpired(cmdCtx)
			if err != nil {
				return err
			}
			orphanCount, err := cmdCtx.Storage.CountOrphanedEdges(cmdCtx.Ctx)
			if err != nil {
				return fmt.Errorf("count orphaned edges: %w", err)
			}
			if expNodes == 0 && expEdges == 0 && orphanCount == 0 {
				fmt.Println("Nothing to clean up")
			} else {
				if expNodes > 0 || expEdges > 0 {
					fmt.Printf("Would delete %d expired nodes, %d expired edges  (dry run)\n", expNodes, expEdges)
				}
				if orphanCount > 0 {
					fmt.Printf("Would delete %d orphaned edges  (dry run)\n", orphanCount)
				}
			}
			return nil
		}

		// Verbose dry-run: list orphaned edges.
		expNodes, expEdges, err := countExpired(cmdCtx)
		if err != nil {
			return err
		}
		edges, err := cmdCtx.Storage.FindOrphanedEdges(cmdCtx.Ctx)
		if err != nil {
			return fmt.Errorf("find orphaned edges: %w", err)
		}
		if expNodes == 0 && expEdges == 0 && len(edges) == 0 {
			fmt.Println("Nothing to clean up")
			return nil
		}
		if expNodes > 0 || expEdges > 0 {
			fmt.Printf("Would delete %d expired nodes, %d expired edges  (dry run)\n", expNodes, expEdges)
		}
		if len(edges) > 0 {
			printOrphanedEdges(cmdCtx, edges)
			fmt.Printf("Would delete %d orphaned edges  (dry run — no changes made)\n", len(edges))
		}
		return nil
	}

	// Normal run (actually delete).
	if flagGCQuiet {
		delN, delE, err := cmdCtx.Storage.DeleteExpired(cmdCtx.Ctx)
		if err != nil {
			return fmt.Errorf("delete expired: %w", err)
		}
		delOrphan, err := cmdCtx.Storage.DeleteOrphanedEdges(cmdCtx.Ctx)
		if err != nil {
			return fmt.Errorf("delete orphaned edges: %w", err)
		}
		if delN == 0 && delE == 0 && delOrphan == 0 {
			fmt.Println("Nothing to clean up")
		} else {
			if delN > 0 || delE > 0 {
				fmt.Printf("Deleted %d expired nodes, %d expired edges\n", delN, delE)
			}
			if delOrphan > 0 {
				fmt.Printf("Deleted %d orphaned edges\n", delOrphan)
			}
		}
		return nil
	}

	// Verbose normal run: delete expired, then list+delete orphaned edges.
	delN, delE, err := cmdCtx.Storage.DeleteExpired(cmdCtx.Ctx)
	if err != nil {
		return fmt.Errorf("delete expired: %w", err)
	}
	if delN > 0 || delE > 0 {
		fmt.Printf("Deleted %d expired nodes, %d expired edges\n", delN, delE)
	}

	edges, err := cmdCtx.Storage.FindOrphanedEdges(cmdCtx.Ctx)
	if err != nil {
		return fmt.Errorf("find orphaned edges: %w", err)
	}
	if len(edges) == 0 {
		if delN == 0 && delE == 0 {
			fmt.Println("Nothing to clean up")
		}
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

// countExpired returns the number of expired nodes and edges without deleting them.
func countExpired(cmdCtx *CommandContext) (int64, int64, error) {
	return cmdCtx.Storage.CountExpired(cmdCtx.Ctx)
}
