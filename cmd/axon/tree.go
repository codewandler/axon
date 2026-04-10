package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

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
You can specify a filesystem path or a node ID to start from.

Node IDs are shown in brackets by axon tree --ids and axon find output.
A prefix of at least 4 characters is sufficient.

Examples:
  axon tree                    # Current directory subtree
  axon tree /path/to/dir       # Subtree rooted at directory
  axon tree nI3NDos            # Subtree rooted at node by ID prefix
  axon tree --depth 2          # Limit depth
  axon tree --type fs:file     # Show only matching node types`,
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

	cwd, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("failed to get current directory: %w", err)
	}

	dbLoc, err := resolveDB(flagDBDir, flagGlobal, cwd, false)
	if err != nil {
		return err
	}

	fmt.Printf("Using database: %s\n", dbLoc.Path)

	storage, err := sqlite.New(dbLoc.Path)
	if err != nil {
		return fmt.Errorf("failed to open database: %w", err)
	}
	defer storage.Close()

	ax, err := axon.New(axon.Config{
		Dir:     cwd,
		Storage: storage,
	})
	if err != nil {
		return fmt.Errorf("failed to create axon: %w", err)
	}

	var rootNodeID string

	// If the argument looks like a node ID (no path separators, not starting with '.'),
	// try to resolve it in the graph first.
	if len(args) > 0 && !strings.ContainsAny(args[0], "/\\") && !strings.HasPrefix(args[0], ".") {
		if len(args[0]) >= 4 {
			nodes, err := findNodesByPrefix(ctx, ax.Graph(), args[0])
			if err == nil && len(nodes) == 1 {
				rootNodeID = nodes[0].ID
			} else if err == nil && len(nodes) > 1 {
				fmt.Printf("Multiple nodes match '%s':\n", args[0])
				for _, n := range nodes {
					fmt.Printf("  %s  %s (%s)\n", n.ID[:7], n.Name, n.Type)
				}
				return nil
			}
		}
	}

	// If no node ID was resolved, fall back to path resolution.
	if rootNodeID == "" {
		startPath := "."
		if len(args) > 0 {
			startPath = args[0]
		}
		absPath, err := filepath.Abs(startPath)
		if err != nil {
			return fmt.Errorf("failed to resolve path: %w", err)
		}
		info, err := os.Stat(absPath)
		if err != nil {
			return fmt.Errorf("path does not exist: %w", err)
		}
		if !info.IsDir() {
			return fmt.Errorf("path is not a directory: %s", absPath)
		}
		uri := types.PathToURI(absPath)
		rootNode, err := ax.Graph().GetNodeByURI(ctx, uri)
		if err != nil {
			return fmt.Errorf("failed to find root node for path %s: %w", absPath, err)
		}
		rootNodeID = rootNode.ID
	}

	// Detect TTY for color/emoji support
	isTTY := isatty.IsTerminal(os.Stdout.Fd()) || isatty.IsCygwinTerminal(os.Stdout.Fd())

	// Determine color/emoji settings (flags override TTY detection)
	useColor := (isTTY || treeColor) && !treeNoColor
	useEmoji := (isTTY || treeEmoji) && !treeNoEmoji

	opts := render.Options{
		MaxDepth:   treeDepth,
		ShowIDs:    treeShowIDs,
		ShowTypes:  treeShowTypes,
		UseColor:   useColor,
		UseEmoji:   useEmoji,
		TypeFilter: treeTypeFilter,
	}

	output, err := render.Tree(ctx, ax.Graph(), rootNodeID, opts)
	if err != nil {
		return fmt.Errorf("failed to render tree: %w", err)
	}

	fmt.Print(output)
	return nil
}
