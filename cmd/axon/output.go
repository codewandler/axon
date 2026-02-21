package main

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/codewandler/axon/graph"
)

// OutputFormat represents supported output formats.
type OutputFormat string

const (
	OutputText OutputFormat = "text"
	OutputJSON OutputFormat = "json"
)

// Renderer handles output formatting for various result types.
type Renderer struct {
	format OutputFormat
	writer io.Writer
}

// NewRenderer creates a new renderer with the specified format.
// If format is empty or invalid, defaults to text.
func NewRenderer(format string, w io.Writer) *Renderer {
	if w == nil {
		w = os.Stdout
	}
	f := OutputFormat(format)
	if f != OutputJSON {
		f = OutputText
	}
	return &Renderer{format: f, writer: w}
}

// RenderCounts outputs CountResult in the appropriate format.
func (r *Renderer) RenderCounts(result CountResult) error {
	switch r.format {
	case OutputJSON:
		return r.renderCountsJSON(result)
	default:
		return r.renderCountsText(result)
	}
}

// renderCountsText renders counts as aligned text columns.
func (r *Renderer) renderCountsText(result CountResult) error {
	if len(result.Items) == 0 {
		return nil
	}

	// Find max name length for alignment
	maxLen := 0
	for _, item := range result.Items {
		if len(item.Name) > maxLen {
			maxLen = len(item.Name)
		}
	}

	// Render each item
	for _, item := range result.Items {
		padding := strings.Repeat(" ", maxLen-len(item.Name)+2)
		fmt.Fprintf(r.writer, "%s%s%d\n", item.Name, padding, item.Count)
	}
	return nil
}

// renderCountsJSON renders counts as JSON.
func (r *Renderer) renderCountsJSON(result CountResult) error {
	enc := json.NewEncoder(r.writer)
	enc.SetIndent("", "  ")
	return enc.Encode(result.Items)
}

// RenderNodes outputs nodes in the appropriate format.
func (r *Renderer) RenderNodes(nodes []*graph.Node) error {
	switch r.format {
	case OutputJSON:
		return r.renderNodesJSON(nodes)
	default:
		return r.renderNodesText(nodes)
	}
}

// renderNodesText renders nodes as text.
func (r *Renderer) renderNodesText(nodes []*graph.Node) error {
	for _, node := range nodes {
		labels := "-"
		if len(node.Labels) > 0 {
			labels = strings.Join(node.Labels, ", ")
		}
		fmt.Fprintf(r.writer, "[%s] %s (%s) [%s]\n", shortID(node.ID), node.Name, node.Type, labels)
	}
	return nil
}

// renderNodesJSON renders nodes as JSON.
func (r *Renderer) renderNodesJSON(nodes []*graph.Node) error {
	enc := json.NewEncoder(r.writer)
	enc.SetIndent("", "  ")
	return enc.Encode(nodes)
}
