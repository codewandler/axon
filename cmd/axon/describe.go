package main

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"github.com/codewandler/axon/graph"
)

var (
	describeFields bool
	describeOutput string
	describeGlobal bool
)

var describeCmd = &cobra.Command{
	Use:   "describe",
	Short: "Show graph schema: node types, edge types, and data fields",
	Long: `Display the schema of the graph.

Reports all node types with counts, all edge types with their from/to
node-type connection patterns, and (with --fields) the JSON data field
names stored on each node type.

Note: describe always reports the entire graph (equivalent to --global).
Scoped output is not yet supported.

Examples:
  axon describe              # schema overview (text)
  axon describe -o json      # machine-readable JSON
  axon describe --fields     # include data field names per node type`,
	RunE: runDescribe,
}

func init() {
	describeCmd.Flags().BoolVarP(&describeFields, "fields", "f", false, "Include data field names per node type (slightly slower)")
	describeCmd.Flags().StringVarP(&describeOutput, "output", "o", "text", "Output format: text, json")
	// --global is accepted for CLI consistency with other commands; describe is
	// always global (scoped output is not yet implemented).
	describeCmd.Flags().BoolVarP(&describeGlobal, "global", "g", false, "Search entire graph (default behaviour; flag accepted for consistency)")
}

func runDescribe(cmd *cobra.Command, args []string) error {
	cmdCtx, err := openDB(false)
	if err != nil {
		return err
	}
	defer cmdCtx.Close()

	ax, err := cmdCtx.Axon()
	if err != nil {
		return err
	}

	desc, err := ax.Describe(cmdCtx.Ctx, describeFields)
	if err != nil {
		return fmt.Errorf("describe: %w", err)
	}

	switch describeOutput {
	case "json":
		return renderDescribeJSON(desc)
	default:
		return renderDescribeText(desc, describeFields)
	}
}

func renderDescribeJSON(desc *graph.SchemaDescription) error {
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	return enc.Encode(desc)
}

func renderDescribeText(desc *graph.SchemaDescription, showFields bool) error {
	// --- Node types ---
	totalNodes := 0
	for _, nt := range desc.NodeTypes {
		totalNodes += nt.Count
	}
	fmt.Printf("Node Types (%d types, %d nodes total):\n", len(desc.NodeTypes), totalNodes)

	if len(desc.NodeTypes) == 0 {
		fmt.Println("  (none)")
	} else {
		// Find max type width for alignment.
		maxTypeLen := 0
		for _, nt := range desc.NodeTypes {
			if len(nt.Type) > maxTypeLen {
				maxTypeLen = len(nt.Type)
			}
		}
		for _, nt := range desc.NodeTypes {
			fmt.Printf("  %-*s  %d\n", maxTypeLen, nt.Type, nt.Count)
		}
	}

	// --- Edge types ---
	totalEdges := 0
	for _, et := range desc.EdgeTypes {
		totalEdges += et.Count
	}
	fmt.Printf("\nEdge Types (%d types, %d edges total):\n", len(desc.EdgeTypes), totalEdges)

	if len(desc.EdgeTypes) == 0 {
		fmt.Println("  (none)")
	} else {
		maxTypeLen := 0
		for _, et := range desc.EdgeTypes {
			if len(et.Type) > maxTypeLen {
				maxTypeLen = len(et.Type)
			}
		}
		for _, et := range desc.EdgeTypes {
			fmt.Printf("  %-*s  %d\n", maxTypeLen, et.Type, et.Count)
			for _, conn := range et.Connections {
				fmt.Printf("    %s → %s  (%d)\n", conn.From, conn.To, conn.Count)
			}
		}
	}

	// --- Fields (only when --fields) ---
	if showFields && len(desc.NodeTypes) > 0 {
		fmt.Println("\nFields by node type:")
		maxTypeLen := 0
		for _, nt := range desc.NodeTypes {
			if len(nt.Type) > maxTypeLen {
				maxTypeLen = len(nt.Type)
			}
		}
		for _, nt := range desc.NodeTypes {
			if len(nt.Fields) == 0 {
				fmt.Printf("  %-*s  (no data fields)\n", maxTypeLen, nt.Type)
			} else {
				fmt.Printf("  %-*s  %s\n", maxTypeLen, nt.Type, strings.Join(nt.Fields, ", "))
			}
		}
	}

	return nil
}
