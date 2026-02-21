package main

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

var version = "dev"

// Global flags
var (
	flagDBDir string
	flagLocal bool
)

var rootCmd = &cobra.Command{
	Use:   "axon",
	Short: "Graph-based storage for AI agents",
	Long: `Axon is a graph-based storage system designed for AI agent context management,
retrieval, and exploration. It indexes codebases and projects into a queryable graph
that can be used to pre-seed AI context windows.`,
}

func main() {
	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func init() {
	// Global persistent flags
	rootCmd.PersistentFlags().StringVar(&flagDBDir, "db-dir", "", "directory containing the database (default: auto-lookup)")
	rootCmd.PersistentFlags().BoolVar(&flagLocal, "local", false, "use local .axon directory in target path")

	rootCmd.AddCommand(initCmd)
	rootCmd.AddCommand(treeCmd)
	rootCmd.AddCommand(showCmd)
	rootCmd.AddCommand(findCmd)
	rootCmd.AddCommand(labelsCmd)
	rootCmd.AddCommand(typesCmd)
	rootCmd.AddCommand(edgesCmd)
	rootCmd.AddCommand(gcCmd)
	rootCmd.AddCommand(statsCmd)

	rootCmd.Version = version
}
