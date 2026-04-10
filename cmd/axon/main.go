package main

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

var version = "dev"

// Global flags
var (
	flagDBDir  string
	flagGlobal bool
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
	rootCmd.PersistentFlags().StringVar(&flagDBDir, "db-dir", "", "explicit directory containing the database")
	rootCmd.PersistentFlags().BoolVar(&flagGlobal, "global", false, "search parent directories for a database, falling back to ~/.axon (opposite of default local-only behaviour)")

	rootCmd.AddCommand(indexCmd)
	rootCmd.AddCommand(treeCmd)
	rootCmd.AddCommand(showCmd)
	rootCmd.AddCommand(findCmd)
	rootCmd.AddCommand(searchCmd) // deprecated alias for findCmd
	rootCmd.AddCommand(queryCmd)
	rootCmd.AddCommand(labelsCmd)
	rootCmd.AddCommand(typesCmd)
	rootCmd.AddCommand(edgesCmd)
	rootCmd.AddCommand(gcCmd)
	rootCmd.AddCommand(statsCmd)
	rootCmd.AddCommand(infoCmd)
	rootCmd.AddCommand(parseCmd)
	rootCmd.AddCommand(describeCmd)
	rootCmd.AddCommand(impactCmd)

	rootCmd.Version = version
}
