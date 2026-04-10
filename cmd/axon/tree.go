package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/codewandler/axon"
	"github.com/codewandler/axon/adapters/sqlite"
	"github.com/codewandler/axon/render"
	"github.com/codewandler/axon/types"
	"github.com/mattn/go-isatty"
	"github.com/spf13/cobra"
)

var (
	treeDepth      int
	treeShowIDs    bool
	treeShowTypes  bool
	treeNoColor    bool
	treeNoEmoji    bool
	treeColor      bool
	treeEmoji      bool
	treeTypeFilter []string
)

var treeCmd = &cobra.Command{
	Use:   "tree [node-id|path]",
	Short: "Display graph as a tree",
	Long: `Display the indexed graph as a tree structure.

If no argument is provided, shows the tree from the current directory.
You can specify a node ID or a path to start from.

The output includes node IDs that can be used for further queries.`,
	Args: cobra.MaximumNArgs(1),
	RunE: runTree,
}

func init() {
	treeCmd.Flags().IntVarP(&treeDepth, "depth", "d", 3, "Maximum depth to display (0 for unlimited)")
	treeCmd.Flags().BoolVar(&treeShowIDs, "ids", true, "Show node IDs")
	treeCmd.Flags().BoolVar(&treeShowTypes, "types", true, "Show node types")
	treeCmd.Flags().BoolVar(&treeNoColor, "no-color", false, "Disable colored output")
	treeCmd.Flags().BoolVar(&treeNoEmoji, "no-emoji", false, "Disable emoji icons")
	treeCmd.Flags().BoolVar(&treeColor, "color", false, "Force colored output")
	treeCmd.Flags().BoolVar(&treeEmoji, "emoji", false, "Force emoji icons")
	treeCmd.Flags().StringArrayVarP(&treeTypeFilter, "type", "t", nil, "Filter by node type (glob: 'fs:*', 'md:*'). Can be repeated.")
}

func runTree(cmd *cobra.Command, args []string) error {
	ctx := context.Background()

	// Determine starting point
	startPath := "."
	if len(args) > 0 {
		startPath = args[0]
	}

	// Resolve to absolute path
	absPath, err := filepath.Abs(startPath)
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

	// Resolve database location (read-only, so forWrite=false)
	cwd, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("failed to get current directory: %w", err)
	}
	dbLoc, err := resolveDB(flagDBDir, flagGlobal, cwd, false)
	if err != nil {
		return err
	}

	// Print database location
	fmt.Printf("Using database: %s\n", dbLoc.Path)

	// Open SQLite storage
	storage, err := sqlite.New(dbLoc.Path)
	if err != nil {
		return fmt.Errorf("failed to open database: %w", err)
	}
	defer storage.Close()

	// Create axon instance with existing storage
	ax, err := axon.New(axon.Config{
		Dir:     absPath,
		Storage: storage,
	})
	if err != nil {
		return fmt.Errorf("failed to create axon: %w", err)
	}

	// Find root node
	uri := types.PathToURI(absPath)
	rootNode, err := ax.Graph().GetNodeByURI(ctx, uri)
	if err != nil {
		return fmt.Errorf("failed to find root node: %w", err)
	}

	// Detect TTY for color/emoji support
	isTTY := isatty.IsTerminal(os.Stdout.Fd()) || isatty.IsCygwinTerminal(os.Stdout.Fd())

	// Determine color/emoji settings (flags override TTY detection)
	useColor := (isTTY || treeColor) && !treeNoColor
	useEmoji := (isTTY || treeEmoji) && !treeNoEmoji

	// Render tree
	opts := render.Options{
		MaxDepth:   treeDepth,
		ShowIDs:    treeShowIDs,
		ShowTypes:  treeShowTypes,
		UseColor:   useColor,
		UseEmoji:   useEmoji,
		TypeFilter: treeTypeFilter,
	}

	output, err := render.Tree(ctx, ax.Graph(), rootNode.ID, opts)
	if err != nil {
		return fmt.Errorf("failed to render tree: %w", err)
	}

	fmt.Print(output)
	return nil
}
