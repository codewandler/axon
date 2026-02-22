package main

import (
	"bufio"
	"fmt"
	"os"
	"strings"

	"github.com/codewandler/axon/context"
	"github.com/codewandler/axon/graph"
	"github.com/spf13/cobra"
)

var (
	contextTask     string
	contextTokens   int
	contextRing     int
	contextOutput   string
	contextSymbols  []string
	contextNoSource bool
	contextGlobal   bool
)

var contextCmd = &cobra.Command{
	Use:   "context",
	Short: "Generate optimized context for AI agents",
	Long: `Generate optimized context for AI agents based on a task description.

This command analyzes the indexed codebase to find the most relevant code
for your task, fits it within a token budget, and outputs it in a format
ready for use with AI assistants.

The context optimizer:
- Extracts symbols from your task description
- Finds definitions in the graph (Ring 0)
- Expands to direct dependencies like methods and fields (Ring 1)
- Includes callers and references (Ring 2)
- Adds related symbols from the same package (Ring 3)
- Fits everything within the token budget with smart truncation

Examples:
  # Basic usage
  axon context --task "add caching to Storage interface"

  # With custom token budget
  axon context --task "refactor Query method" --tokens 8000

  # JSON output for programmatic use
  axon context --task "fix NewNode" --output json

  # Manifest only (no source code, just file list)
  axon context --task "explain Indexer" --no-source

  # Include specific symbols
  axon context --task "improve performance" --symbols Storage --symbols Query

  # Read task from stdin
  echo "add error handling to Flush" | axon context`,
	RunE: runContext,
}

func init() {
	contextCmd.Flags().StringVarP(&contextTask, "task", "t", "", "Task description (reads from stdin if not provided)")
	contextCmd.Flags().IntVar(&contextTokens, "tokens", 12000, "Token budget")
	contextCmd.Flags().IntVar(&contextRing, "ring", 3, "Maximum ring depth (0-4)")
	contextCmd.Flags().StringVarP(&contextOutput, "output", "o", "text", "Output format: text, json")
	contextCmd.Flags().StringArrayVarP(&contextSymbols, "symbols", "s", nil, "Additional symbols to include (repeatable)")
	contextCmd.Flags().BoolVar(&contextNoSource, "no-source", false, "Output manifest only (no source content)")
	contextCmd.Flags().BoolVarP(&contextGlobal, "global", "g", false, "Search entire graph (not just CWD subtree)")

	rootCmd.AddCommand(contextCmd)
}

func runContext(cmd *cobra.Command, args []string) error {
	// Get task from flag or stdin
	task := contextTask
	if task == "" {
		// Check if stdin has data
		stat, _ := os.Stdin.Stat()
		if (stat.Mode() & os.ModeCharDevice) == 0 {
			// stdin has data
			scanner := bufio.NewScanner(os.Stdin)
			var lines []string
			for scanner.Scan() {
				lines = append(lines, scanner.Text())
			}
			if err := scanner.Err(); err != nil {
				return fmt.Errorf("reading stdin: %w", err)
			}
			task = strings.Join(lines, " ")
		}
	}

	if task == "" {
		return fmt.Errorf("task description required (use --task or pipe to stdin)")
	}

	// Add extra symbols to task if provided
	if len(contextSymbols) > 0 {
		// Append symbols in backticks so they're parsed as symbols
		task = task + " `" + strings.Join(contextSymbols, "` `") + "`"
	}

	// Open database
	cmdCtx, err := openDB(false)
	if err != nil {
		return err
	}
	defer cmdCtx.Close()

	ax, err := cmdCtx.Axon()
	if err != nil {
		return err
	}

	storage := ax.Graph().Storage()

	// Determine output format
	var outputFormat context.OutputFormat
	switch strings.ToLower(contextOutput) {
	case "json":
		outputFormat = context.FormatJSON
	default:
		outputFormat = context.FormatText
	}

	// Validate ring
	if contextRing < 0 || contextRing > 4 {
		return fmt.Errorf("ring must be between 0 and 4")
	}

	// Build options
	opts := context.Options{
		Task:         task,
		MaxTokens:    contextTokens,
		MaxRing:      context.Ring(contextRing),
		IncludeTests: true,
		Output:       outputFormat,
		ManifestOnly: contextNoSource,
	}

	// If not global, scope to current directory
	if !contextGlobal {
		cwdNode, err := getCwdNode(cmdCtx)
		if err == nil && cwdNode != nil {
			opts.ScopeNodeID = cwdNode.ID
		}
	}

	// Gather context
	output, err := context.Gather(cmdCtx.Ctx, storage, opts)
	if err != nil {
		return err
	}

	fmt.Print(output)
	return nil
}

// getCwdNode attempts to find the node for the current working directory.
func getCwdNode(cmdCtx *CommandContext) (*graph.Node, error) {
	cwd, err := os.Getwd()
	if err != nil {
		return nil, err
	}

	uri := "file://" + cwd
	return cmdCtx.Storage.GetNodeByURI(cmdCtx.Ctx, uri)
}
