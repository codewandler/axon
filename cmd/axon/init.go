package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/codewandler/axon"
	"github.com/codewandler/axon/adapters/sqlite"
	"github.com/codewandler/axon/indexer/embeddings"
	"github.com/codewandler/axon/progress"
	"github.com/mattn/go-isatty"
	"github.com/spf13/cobra"
)

var (
	flagNoGC  bool
	flagEmbed bool
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

func init() {
	initCmd.Flags().BoolVar(&flagNoGC, "no-gc", false, "Skip garbage collection (orphaned edge cleanup)")
	initCmd.Flags().BoolVar(&flagEmbed, "embed", false, "Generate embeddings after indexing (requires Ollama with nomic-embed-text or AXON_EMBED_PROVIDER=ollama)")
}

func runInit(cmd *cobra.Command, args []string) error {
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
		providerName := os.Getenv("AXON_EMBED_PROVIDER")
		if providerName == "" {
			providerName = "ollama"
		}
		switch providerName {
		case "ollama":
			baseURL := os.Getenv("AXON_OLLAMA_URL")
			model := os.Getenv("AXON_OLLAMA_MODEL")
			axCfg.EmbeddingProvider = embeddings.NewOllama(baseURL, model)
			fmt.Printf("Embedding provider: %s\n", axCfg.EmbeddingProvider.Name())
		default:
			return fmt.Errorf("unknown embedding provider %q (set AXON_EMBED_PROVIDER=ollama)", providerName)
		}
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
		result, err = runInitWithProgress(ctx, ax, absPath, opts)
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

	return nil
}

// runInitWithProgress runs indexing with a bubbletea progress UI.
func runInitWithProgress(ctx context.Context, ax *axon.Axon, absPath string, opts axon.IndexOptions) (*axon.IndexResult, error) {
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
