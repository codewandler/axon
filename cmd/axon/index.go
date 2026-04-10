package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/codewandler/axon"
	"github.com/codewandler/axon/adapters/sqlite"
	"github.com/codewandler/axon/progress"
	"github.com/mattn/go-isatty"
	"github.com/spf13/cobra"
)

var (
	flagNoGC           bool
	flagEmbed          bool
	flagEmbedProvider  string
	flagEmbedModelPath string
	flagWatch          bool
	flagWatchDebounce  time.Duration
	flagWatchQuiet     bool
)

var indexCmd = &cobra.Command{
	Use:     "index [path]",
	Aliases: []string{"init"},
	Short:   "Index a directory into the knowledge graph",
	Long: `Index a directory into the knowledge graph.
If no path is provided, the current directory is used.

This command indexes all files and directories, creating a graph
structure that can be queried with other axon commands.`,
	Args: cobra.MaximumNArgs(1),
	RunE: runIndex,
}

func init() {
	indexCmd.Flags().BoolVar(&flagNoGC, "no-gc", false, "Skip garbage collection (orphaned edge cleanup)")
	indexCmd.Flags().BoolVar(&flagEmbed, "embed", false, "Generate embeddings after indexing (use --embed-provider to select provider)")
	indexCmd.Flags().StringVar(&flagEmbedProvider, "embed-provider", "", "Embedding provider: ollama|hugot (overrides AXON_EMBED_PROVIDER)")
	indexCmd.Flags().StringVar(&flagEmbedModelPath, "embed-model-path", "", "Local model directory for hugot provider (default: ~/.axon/models/<model>)")
	indexCmd.Flags().BoolVar(&flagWatch, "watch", false, "watch for changes and re-index automatically (Ctrl+C to stop)")
	indexCmd.Flags().DurationVar(&flagWatchDebounce, "watch-debounce", 150*time.Millisecond, "debounce window for change events (only with --watch)")
	indexCmd.Flags().BoolVar(&flagWatchQuiet, "watch-quiet", false, "suppress per-change output (only with --watch)")
}

func runIndex(cmd *cobra.Command, args []string) error {
	ctx := context.Background()

	// Determine path to index
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

	// Resolve database location
	cwd, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("failed to get current directory: %w", err)
	}
	dbLoc, err := resolveDB(flagDBDir, flagGlobal, cwd, true)
	if err != nil {
		return fmt.Errorf("failed to resolve database location: %w", err)
	}

	// Print database location
	fmt.Printf("Using database: %s\n", dbLoc.Path)

	// Open SQLite storage
	storage, err := sqlite.New(dbLoc.Path)
	if err != nil {
		return fmt.Errorf("failed to open database: %w", err)
	}
	defer storage.Close()

	// Create axon instance with SQLite storage
	axCfg := axon.Config{
		Dir:     absPath,
		Storage: storage,
	}

	// Optionally enable embedding generation
	if flagEmbed {
		provider, err := resolveEmbeddingProvider(flagEmbedProvider, flagEmbedModelPath)
		if err != nil {
			return err
		}
		defer func() { _ = provider.Close() }()
		axCfg.EmbeddingProvider = provider
		fmt.Printf("Embedding provider: %s\n", provider.Name())
	}

	ax, err := axon.New(axCfg)
	if err != nil {
		return fmt.Errorf("failed to create axon: %w", err)
	}

	// Check if we have a TTY for progress display
	isTTY := isatty.IsTerminal(os.Stdout.Fd()) || isatty.IsCygwinTerminal(os.Stdout.Fd())

	// Build index options
	opts := axon.IndexOptions{
		SkipGC: flagNoGC,
	}

	var result *axon.IndexResult

	if isTTY {
		// Use bubbletea progress UI
		result, err = runIndexWithProgress(ctx, ax, absPath, opts)
	} else {
		// Simple text output
		result, err = ax.IndexWithOptions(ctx, opts)
	}

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

	if !flagWatch {
		return nil
	}

	// Enter watch mode
	if !flagWatchQuiet {
		fmt.Fprintf(os.Stderr, "Watching %s — press Ctrl+C to stop.\n", absPath)
	}
	watchCtx, watchCancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer watchCancel()

	err = ax.Watch(watchCtx, absPath, axon.WatchOptions{
		IndexOptions: axon.IndexOptions{SkipGC: flagNoGC},
		Debounce:     flagWatchDebounce,
		OnReindex: func(path string, result *axon.IndexResult, err error) {
			if err != nil {
				fmt.Fprintf(os.Stderr, "✗  re-index error: %v\n", err)
				return
			}
			if !flagWatchQuiet {
				rel, _ := filepath.Rel(absPath, path)
				if rel == "" || rel == "." {
					rel = "."
				}
				fmt.Fprintf(os.Stderr, "↻  ./%s — %d files\n", rel, result.Files)
			}
		},
	})
	if errors.Is(err, context.Canceled) {
		return nil
	}
	return err
}

// runIndexWithProgress runs indexing with a bubbletea progress UI.
func runIndexWithProgress(ctx context.Context, ax *axon.Axon, absPath string, opts axon.IndexOptions) (*axon.IndexResult, error) {
	// Create progress coordinator
	coord := progress.NewCoordinator()
	startTime := time.Now()

	// Channel for result
	resultCh := make(chan *axon.IndexResult, 1)
	errCh := make(chan error, 1)

	// Run indexing in background
	go func() {
		opts.Progress = coord.Events()
		result, err := ax.IndexWithOptions(ctx, opts)
		if err != nil {
			errCh <- err
		} else {
			resultCh <- result
		}
		// Signal coordinator that indexing is complete
		coord.Close()
	}()

	// Create and run bubbletea program
	p := tea.NewProgram(progress.NewModel(coord))

	// Run the TUI (blocks until coordinator signals done via IsRunning() returning false)
	if _, err := p.Run(); err != nil {
		return nil, fmt.Errorf("progress UI error: %w", err)
	}

	// Print git-style summary after TUI clears
	totalDuration := time.Since(startTime)
	fmt.Print(progress.FormatSummary(coord.Summary(), totalDuration))

	// Result must be available now since indexing goroutine writes before coord.Close()
	select {
	case err := <-errCh:
		return nil, err
	case result := <-resultCh:
		return result, nil
	}
}
