package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"

	"github.com/codewandler/axon"
	"github.com/codewandler/axon/aql"
	"github.com/spf13/cobra"
)

var (
	pathMaxDepth  int
	pathMaxPaths  int
	pathEdgeTypes []string
	pathOutput    string
)

var pathCmd = &cobra.Command{
	Use:   "path <from> <to>",
	Short: "Find paths between two nodes in the knowledge graph",
	Long: `Find the shortest path(s) between two nodes in the knowledge graph.

<from> and <to> are resolved in order: URI, exact name, then node ID.
When a name matches multiple nodes the first result is used.

The search traverses both outgoing and incoming edges (bidirectional BFS),
bounded by --max-depth. Up to --max-paths shortest paths are returned.

Examples:
  axon path Storage FindNodes
  axon path Call Connector --max-depth 4
  axon path Storage NewNode --edge-type calls --edge-type uses
  axon path "go:func:New" "go:interface:Storage" --output json`,
	Args: cobra.ExactArgs(2),
	RunE: runPath,
}

func init() {
	pathCmd.Flags().IntVar(&pathMaxDepth, "max-depth", 6, "Maximum path depth in number of edges")
	pathCmd.Flags().IntVar(&pathMaxPaths, "max-paths", 3, "Maximum number of paths to return")
	pathCmd.Flags().StringArrayVar(&pathEdgeTypes, "edge-type", nil, "Restrict traversal to this edge type (repeatable)")
	pathCmd.Flags().StringVarP(&pathOutput, "output", "o", "text", "Output format: text, json")
}

func runPath(cmd *cobra.Command, args []string) error {
	fromArg, toArg := args[0], args[1]
	ctx := context.Background()

	cmdCtx, err := openDB(false)
	if err != nil {
		return err
	}
	defer cmdCtx.Close()

	ax, err := cmdCtx.Axon()
	if err != nil {
		return err
	}

	fromID, err := resolvePathNodeID(ctx, cmdCtx, fromArg)
	if err != nil {
		return fmt.Errorf("resolve 'from' node %q: %w", fromArg, err)
	}
	toID, err := resolvePathNodeID(ctx, cmdCtx, toArg)
	if err != nil {
		return fmt.Errorf("resolve 'to' node %q: %w", toArg, err)
	}

	opts := axon.PathOptions{
		MaxDepth:  pathMaxDepth,
		MaxPaths:  pathMaxPaths,
		EdgeTypes: pathEdgeTypes,
	}

	paths, err := ax.FindPath(ctx, fromID, toID, opts)
	if err != nil {
		return fmt.Errorf("find path: %w", err)
	}

	if len(paths) == 0 {
		fmt.Printf("No path found between %q and %q within %d hops.\n", fromArg, toArg, pathMaxDepth)
		return nil
	}

	switch pathOutput {
	case "json":
		return printPathJSON(paths)
	default:
		printPathText(paths, fromArg, toArg)
		return nil
	}
}

// resolvePathNodeID resolves a name, URI, or node ID string to an internal node ID.
// Resolution order: URI lookup → exact name search → direct ID lookup.
func resolvePathNodeID(ctx context.Context, cmdCtx *CommandContext, ref string) (string, error) {
	// 1. Try URI.
	if n, err := cmdCtx.Storage.GetNodeByURI(ctx, ref); err == nil {
		return n.ID, nil
	}

	// 2. Try exact name (AQL).
	q, err := aql.Parse(fmt.Sprintf(`SELECT * FROM nodes WHERE name = '%s' LIMIT 1`, escapeSQL(ref)))
	if err != nil {
		return "", fmt.Errorf("building name query: %w", err)
	}
	result, err := cmdCtx.Storage.Query(ctx, q)
	if err != nil {
		return "", fmt.Errorf("querying by name: %w", err)
	}
	if len(result.Nodes) > 0 {
		return result.Nodes[0].ID, nil
	}

	// 3. Try direct node ID.
	if n, err := cmdCtx.Storage.GetNode(ctx, ref); err == nil {
		return n.ID, nil
	}

	return "", fmt.Errorf("no node found for %q — try a node name, URI, or node ID", ref)
}

// printPathText renders paths as human-readable ASCII to stdout.
func printPathText(paths []*axon.Path, fromArg, toArg string) {
	fmt.Printf("Found %d path(s) from %q to %q:\n\n", len(paths), fromArg, toArg)
	for i, p := range paths {
		fmt.Printf("Path %d  (%d hop(s))\n", i+1, p.Length())
		for j, step := range p.Steps {
			prefix := ""
			if j > 0 {
				arrow := fmt.Sprintf("-[%s]->", step.EdgeType)
				if step.Incoming {
					arrow = fmt.Sprintf("<-[%s]-", step.EdgeType)
				}
				prefix = fmt.Sprintf("  %s ", arrow)
			}
			name := step.Node.Name
			if name == "" {
				name = step.Node.ID
			}
			fmt.Printf("%s[%s] %s  (%s)\n", prefix, shortID(step.Node.ID), name, step.Node.Type)
		}
		fmt.Println()
	}
}

// pathJSONStep is the JSON-serialisable representation of one path step.
type pathJSONStep struct {
	NodeID   string `json:"node_id"`
	Type     string `json:"type"`
	Name     string `json:"name,omitempty"`
	URI      string `json:"uri,omitempty"`
	EdgeType string `json:"edge_type,omitempty"`
	Incoming bool   `json:"incoming,omitempty"`
	Data     any    `json:"data,omitempty"`
}

type pathJSONEntry struct {
	Length int            `json:"length"`
	Steps  []pathJSONStep `json:"steps"`
}

// printPathJSON renders paths as a JSON array to stdout.
func printPathJSON(paths []*axon.Path) error {
	out := make([]pathJSONEntry, 0, len(paths))
	for _, p := range paths {
		steps := make([]pathJSONStep, len(p.Steps))
		for i, s := range p.Steps {
			steps[i] = pathJSONStep{
				NodeID:   s.Node.ID,
				Type:     s.Node.Type,
				Name:     s.Node.Name,
				URI:      s.Node.URI,
				EdgeType: s.EdgeType,
				Incoming: s.Incoming,
				Data:     s.Node.Data,
			}
		}
		out = append(out, pathJSONEntry{Length: p.Length(), Steps: steps})
	}
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	return enc.Encode(out)
}
