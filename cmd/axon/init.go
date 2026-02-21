package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/codewandler/axon"
	"github.com/spf13/cobra"
)

var initCmd = &cobra.Command{
	Use:   "init [path]",
	Short: "Initialize and index a directory",
	Long: `Initialize axon in a directory and index its contents.
If no path is provided, the current directory is used.

This command indexes all files and directories, creating a graph
structure that can be queried with other axon commands.`,
	Args: cobra.MaximumNArgs(1),
	RunE: runInit,
}

func runInit(cmd *cobra.Command, args []string) error {
	ctx := context.Background()

	// Determine path
	path := "."
	if len(args) > 0 {
		path = args[0]
	}

	// Resolve to absolute path
	absPath, err := filepath.Abs(path)
	if err != nil {
		return fmt.Errorf("failed to resolve path: %w", err)
	}

	// Check path exists
	info, err := os.Stat(absPath)
	if err != nil {
		return fmt.Errorf("path does not exist: %w", err)
	}
	if !info.IsDir() {
		return fmt.Errorf("path is not a directory: %s", absPath)
	}

	// Create axon instance
	ax, err := axon.New(axon.Config{Dir: absPath})
	if err != nil {
		return fmt.Errorf("failed to create axon: %w", err)
	}

	// Run indexing
	result, err := ax.Index(ctx, "")
	if err != nil {
		return fmt.Errorf("indexing failed: %w", err)
	}

	// Print results
	fmt.Printf("Indexed %d files, %d directories", result.Files, result.Directories)
	if result.Repos > 0 {
		fmt.Printf(", %d git repos", result.Repos)
	}
	fmt.Println()
	if result.StaleRemoved > 0 {
		fmt.Printf("Removed %d stale entries\n", result.StaleRemoved)
	}

	return nil
}
