package main

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

var version = "dev"

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
	rootCmd.AddCommand(initCmd)
	rootCmd.AddCommand(treeCmd)

	rootCmd.Version = version
}
