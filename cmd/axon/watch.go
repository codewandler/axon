package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	axon "github.com/codewandler/axon"
	"github.com/spf13/cobra"
)

var watchCmd = &cobra.Command{
	Use:   "watch [path]",
	Short: "Watch a directory and keep the graph up to date",
	Long: `Watch a directory for changes and automatically re-index affected subtrees.

On each file change, axon re-indexes the affected directory and updates the graph.
Press Ctrl+C to stop watching.`,
	Args: cobra.MaximumNArgs(1),
	RunE: runWatch,
}

var flagWatchDebounce time.Duration
var flagWatchQuiet bool

func init() {
	watchCmd.Flags().DurationVar(&flagWatchDebounce, "debounce", 150*time.Millisecond, "debounce duration for change events")
	watchCmd.Flags().BoolVar(&flagWatchQuiet, "quiet", false, "suppress per-change output")
}

func runWatch(cmd *cobra.Command, args []string) error {
	watchPath := "."
	if len(args) > 0 {
		watchPath = args[0]
	}

	absPath, err := filepath.Abs(watchPath)
	if err != nil {
		return fmt.Errorf("resolving path: %w", err)
	}

	cmdCtx, err := openDB(false)
	if err != nil {
		return err
	}
	defer cmdCtx.Close()

	ax, err := cmdCtx.Axon()
	if err != nil {
		return err
	}

	if !flagWatchQuiet {
		fmt.Fprintf(os.Stderr, "Watching %s ...\n", absPath)
		fmt.Fprintf(os.Stderr, "Press Ctrl+C to stop.\n")
	}

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	opts := axon.WatchOptions{
		Debounce: flagWatchDebounce,
		OnReady: func(result *axon.IndexResult, err error) {
			if err != nil {
				fmt.Fprintf(os.Stderr, "✗  Initial index error: %v\n", err)
				return
			}
			if !flagWatchQuiet {
				fmt.Fprintf(os.Stderr, "✓  Ready — %d files, %d dirs indexed\n",
					result.Files, result.Directories)
			}
		},
		OnReindex: func(path string, result *axon.IndexResult, err error) {
			if err != nil {
				fmt.Fprintf(os.Stderr, "✗  Re-index error: %v\n", err)
				return
			}
			if !flagWatchQuiet {
				rel, _ := filepath.Rel(absPath, path)
				if rel == "" || rel == "." {
					rel = "."
				}
				fmt.Printf("↻  Re-indexed ./%s — %d files, %d dirs\n",
					rel, result.Files, result.Directories)
			}
		},
	}

	return ax.Watch(ctx, absPath, opts)
}
