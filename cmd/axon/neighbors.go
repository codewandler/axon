package main

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"text/tabwriter"

	"github.com/spf13/cobra"

	axon "github.com/codewandler/axon"
)

var (
	neighborsDirection string
	neighborsEdgeTypes []string
	neighborsMax       int
	neighborsOutput    string
)

var neighborsCmd = &cobra.Command{
	Use:   "neighbors <uri>",
	Short: "Show immediate neighbors of a node via its edges",
	Long: `Return all nodes directly connected to the given node.

<uri> is the node URI (e.g. "file:///path/to/file.go" or "go:func:pkg.Func").
You can also supply a plain node name or node ID and the command will attempt
to resolve it the same way 'axon path' does.

Use --direction to restrict traversal to outgoing (out), incoming (in), or
both directions (default). Use --edge-type to filter to a specific relationship.

Examples:
  # All neighbors of a function (both directions)
  axon neighbors "go:func:github.com/myorg/mymod/pkg.MyFunc"

  # Who calls this function? (incoming edges)
  axon neighbors "go:func:pkg.Handle" --direction in --edge-type calls

  # What does this struct embed / implement?
  axon neighbors "go:struct:pkg.Server" --direction out

  # Resolve by name instead of URI
  axon neighbors Storage --direction in --edge-type implements

  # JSON output for scripting
  axon neighbors "go:interface:Storage" --direction in --output json`,
	Args: cobra.ExactArgs(1),
	RunE: runNeighbors,
}

func init() {
	neighborsCmd.Flags().StringVarP(&neighborsDirection, "direction", "d", "both",
		"Edge direction: in, out, or both (default: both)")
	neighborsCmd.Flags().StringArrayVar(&neighborsEdgeTypes, "edge-type", nil,
		"Restrict to this edge type (repeatable, e.g. --edge-type calls --edge-type uses)")
	neighborsCmd.Flags().IntVarP(&neighborsMax, "max", "n", 50,
		"Maximum number of results (0 for unlimited)")
	neighborsCmd.Flags().StringVarP(&neighborsOutput, "output", "o", "text",
		"Output format: text, table, json")
}

func runNeighbors(cmd *cobra.Command, args []string) error {
	ref := args[0]

	// Validate direction flag early.
	switch neighborsDirection {
	case "in", "out", "both":
	default:
		return fmt.Errorf("invalid --direction %q: must be 'in', 'out', or 'both'", neighborsDirection)
	}

	cmdCtx, err := openDB(false)
	if err != nil {
		return err
	}
	defer cmdCtx.Close()

	fmt.Printf("Using database: %s\n", cmdCtx.DBLoc.Path)

	ax, err := cmdCtx.Axon()
	if err != nil {
		return err
	}

	// Resolve the URI — same three-step resolution used by 'axon path'.
	uri, err := resolveNeighborURI(cmdCtx, ref)
	if err != nil {
		return fmt.Errorf("resolve node %q: %w", ref, err)
	}

	opts := axon.NeighborsOptions{
		Direction: neighborsDirection,
		EdgeTypes: neighborsEdgeTypes,
		Max:       neighborsMax,
	}

	results, err := ax.Neighbors(cmdCtx.Ctx, uri, opts)
	if err != nil {
		return fmt.Errorf("neighbors: %w", err)
	}

	if len(results) == 0 {
		fmt.Printf("No neighbors found for %q (direction=%s", ref, neighborsDirection)
		if len(neighborsEdgeTypes) > 0 {
			fmt.Printf(", edge-type=%s", strings.Join(neighborsEdgeTypes, ","))
		}
		fmt.Println(")")
		return nil
	}

	switch neighborsOutput {
	case "json":
		return renderNeighborsJSON(results)
	case "table":
		return renderNeighborsTable(results)
	default:
		renderNeighborsText(results, ref)
		return nil
	}
}

// resolveNeighborURI resolves a name, URI, or node ID to a URI string.
// Resolution order: direct URI lookup → exact name search → direct ID lookup.
func resolveNeighborURI(cmdCtx *CommandContext, ref string) (string, error) {
	ctx := cmdCtx.Ctx

	// 1. Try as a direct URI.
	if n, err := cmdCtx.Storage.GetNodeByURI(ctx, ref); err == nil {
		return n.URI, nil
	}

	// 2. Try exact name (AQL).
	q, err := resolveByName(cmdCtx, ref)
	if err == nil && q != "" {
		return q, nil
	}

	// 3. Try direct node ID.
	if n, err := cmdCtx.Storage.GetNode(ctx, ref); err == nil {
		return n.URI, nil
	}

	return "", fmt.Errorf("no node found for %q — try a node URI, name, or node ID", ref)
}

// resolveByName looks up a node by exact name and returns its URI.
func resolveByName(cmdCtx *CommandContext, name string) (string, error) {
	c := cmdCtx.Ctx
	nodeID, err := resolvePathNodeID(c, cmdCtx, name)
	if err != nil {
		return "", err
	}
	n, err := cmdCtx.Storage.GetNode(c, nodeID)
	if err != nil {
		return "", err
	}
	return n.URI, nil
}

// renderNeighborsText prints results as human-readable text.
func renderNeighborsText(results []*axon.NeighborResult, origin string) {
	fmt.Printf("%d neighbor(s) of %q:\n\n", len(results), origin)
	for _, r := range results {
		arrow := "->"
		if r.Direction == "in" {
			arrow = "<-"
		}
		name := r.Node.Name
		if name == "" {
			name = r.Node.URI
		}
		if name == "" {
			name = r.Node.ID
		}
		fmt.Printf("  %s [%s] [%s] %s  (%s)\n",
			arrow, r.EdgeType, shortID(r.Node.ID), name, r.Node.Type)
	}
}

// renderNeighborsTable prints results as a tab-aligned table.
func renderNeighborsTable(results []*axon.NeighborResult) error {
	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "DIR\tEDGE\tID\tTYPE\tNAME\tURI")
	for _, r := range results {
		name := r.Node.Name
		if name == "" {
			name = "-"
		}
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\t%s\n",
			r.Direction, r.EdgeType, shortID(r.Node.ID), r.Node.Type, name, r.Node.URI)
	}
	return w.Flush()
}

// neighborJSONEntry is the JSON-serialisable form of one NeighborResult.
type neighborJSONEntry struct {
	Direction string      `json:"direction"`
	EdgeType  string      `json:"edge_type"`
	EdgeID    string      `json:"edge_id"`
	NodeID    string      `json:"node_id"`
	NodeType  string      `json:"node_type"`
	NodeName  string      `json:"node_name,omitempty"`
	NodeURI   string      `json:"node_uri,omitempty"`
	NodeData  interface{} `json:"node_data,omitempty"`
}

// renderNeighborsJSON emits results as a JSON array.
func renderNeighborsJSON(results []*axon.NeighborResult) error {
	out := make([]neighborJSONEntry, len(results))
	for i, r := range results {
		out[i] = neighborJSONEntry{
			Direction: r.Direction,
			EdgeType:  r.EdgeType,
			EdgeID:    r.EdgeID,
			NodeID:    r.Node.ID,
			NodeType:  r.Node.Type,
			NodeName:  r.Node.Name,
			NodeURI:   r.Node.URI,
			NodeData:  r.Node.Data,
		}
	}
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	return enc.Encode(out)
}
