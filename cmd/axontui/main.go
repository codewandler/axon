package main

import (
	"fmt"
	"os"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/spf13/cobra"
)

var (
	version      = "dev"
	flagDBDir    string
	startNodeArg string
)

var rootCmd = &cobra.Command{
	Use:   "axontui [path-or-id]",
	Short: "Interactive graph explorer for Axon",
	Long: `axontui is an interactive TUI for exploring Axon's knowledge graph.

Navigate the graph by drilling into nodes, viewing incoming and outgoing
edges, and using AQL queries to filter the view.

Start from the current directory (default) or provide a path or node ID.`,
	Args: cobra.MaximumNArgs(1),
	RunE: runExplore,
}

func init() {
	rootCmd.PersistentFlags().StringVar(&flagDBDir, "db-dir", "", "directory containing the database (default: auto-lookup)")
	rootCmd.Version = version
}

func runExplore(cmd *cobra.Command, args []string) error {
	if len(args) > 0 {
		startNodeArg = args[0]
	}

	// Open database
	app, err := newAppContext(flagDBDir)
	if err != nil {
		return err
	}
	defer app.Close()

	// Create and run the TUI
	m := newModel(app, startNodeArg)
	p := tea.NewProgram(m, tea.WithAltScreen())

	finalModel, err := p.Run()
	if err != nil {
		return fmt.Errorf("TUI error: %w", err)
	}

	// Check if the model had an error
	if fm, ok := finalModel.(model); ok && fm.err != nil {
		return fm.err
	}

	return nil
}

func main() {
	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintf(os.Stderr, "%v\n", err)
		os.Exit(1)
	}
}
