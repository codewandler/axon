package main

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/codewandler/axon/graph"
	"github.com/spf13/cobra"
)

var (
	flagWriteNodeURI    string
	flagWriteNodeType   string
	flagWriteNodeName   string
	flagWriteNodeData   string
	flagWriteNodeTTL    string
	flagWriteNodeLabels []string
)

var writeNodeCmd = &cobra.Command{
	Use:   "write-node",
	Short: "Persist a custom node to the graph (upsert by URI)",
	Long: `Persist a custom node to the graph.

Nodes are upserted by URI — writing a node with an existing URI updates it in
place. An optional --ttl flag sets a time-to-live so the node is treated as
non-existent after the duration elapses. Run "axon gc" to physically remove
expired nodes.

Examples:
  axon write-node --uri memory://s1/task --type memory:note --name "current task"
  axon write-node --uri memory://s1/task --type memory:note --ttl 2h
  axon write-node --uri memory://obs1   --type memory:obs  --data '{"text":"file looks broken"}' --label attention`,
	RunE: runWriteNode,
}

func init() {
	writeNodeCmd.Flags().StringVar(&flagWriteNodeURI, "uri", "", "node URI — same URI upserts the existing node (required)")
	writeNodeCmd.Flags().StringVar(&flagWriteNodeType, "type", "", "node type, e.g. memory:note (required)")
	writeNodeCmd.Flags().StringVar(&flagWriteNodeName, "name", "", "human-readable name")
	writeNodeCmd.Flags().StringVar(&flagWriteNodeData, "data", "", "JSON data payload, e.g. '{\"text\":\"hello\"}'")
	writeNodeCmd.Flags().StringVar(&flagWriteNodeTTL, "ttl", "", `time-to-live duration, e.g. "30m", "2h", "24h" (default: immortal)`)
	writeNodeCmd.Flags().StringArrayVar(&flagWriteNodeLabels, "label", nil, "label to add (may be repeated)")
	_ = writeNodeCmd.MarkFlagRequired("uri")
	_ = writeNodeCmd.MarkFlagRequired("type")
}

func runWriteNode(cmd *cobra.Command, args []string) error {
	cmdCtx, err := openDB(false)
	if err != nil {
		return err
	}
	defer cmdCtx.Close()

	node := graph.NewNode(flagWriteNodeType).
		WithURI(flagWriteNodeURI).
		WithName(flagWriteNodeName).
		WithLabels(flagWriteNodeLabels...)

	if flagWriteNodeData != "" {
		var data any
		if err := json.Unmarshal([]byte(flagWriteNodeData), &data); err != nil {
			return fmt.Errorf("invalid --data JSON: %w", err)
		}
		node = node.WithData(data)
	}

	if flagWriteNodeTTL != "" {
		d, err := time.ParseDuration(flagWriteNodeTTL)
		if err != nil {
			return fmt.Errorf("invalid --ttl duration %q: %w (use e.g. 30m, 2h, 24h)", flagWriteNodeTTL, err)
		}
		node = node.WithTTL(d)
	}

	ax, err := cmdCtx.Axon()
	if err != nil {
		return fmt.Errorf("create axon: %w", err)
	}
	if err := ax.WriteNode(cmdCtx.Ctx, node); err != nil {
		return fmt.Errorf("write node: %w", err)
	}

	fmt.Println(node.ID)
	return nil
}
