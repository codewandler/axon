package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/codewandler/axon/aql"
	"github.com/codewandler/axon/graph"
	"github.com/codewandler/axon/types"
	"github.com/spf13/cobra"
	"golang.org/x/text/language"
	"golang.org/x/text/message"
)

var (
	statsVerbose bool
	statsOutput  string
	statsGlobal  bool
)

var statsCmd = &cobra.Command{
	Use:   "stats",
	Short: "Show database statistics",
	Long: `Display database statistics including size, node/edge counts, and last index info.

Use --verbose for detailed breakdown by type, extension, and labels.

Examples:
  axon stats           # Stats for current directory
  axon stats --global  # Stats for entire database
  axon stats -v        # Verbose output with breakdowns
  axon stats -o json   # JSON output`,
	RunE: runStats,
}

func init() {
	statsCmd.Flags().BoolVarP(&statsVerbose, "verbose", "v", false, "Show detailed breakdown by type, extension, and labels")
	statsCmd.Flags().StringVarP(&statsOutput, "output", "o", "text", "Output format: text, json")
	statsCmd.Flags().BoolVarP(&statsGlobal, "global", "g", false, "Show stats for entire database")
}

// statsData holds all stats data for rendering
type statsData struct {
	Database      string           `json:"database"`
	FileSizeBytes int64            `json:"file_size_bytes"`
	FileSizeHuman string           `json:"file_size_human"`
	Nodes         int              `json:"nodes"`
	Edges         int              `json:"edges"`
	LastIndexed   *lastIndexedData `json:"last_indexed,omitempty"`
	NodeTypes     map[string]int   `json:"node_types,omitempty"`
	EdgeTypes     map[string]int   `json:"edge_types,omitempty"`
	Extensions    map[string]int   `json:"extensions,omitempty"`
	Labels        map[string]int   `json:"labels,omitempty"`
}

type lastIndexedData struct {
	Timestamp     time.Time `json:"timestamp"`
	Ago           string    `json:"ago"`
	DurationMs    int64     `json:"duration_ms"`
	DurationHuman string    `json:"duration_human"`
	FilesIndexed  int       `json:"files_indexed"`
	RootPath      string    `json:"root_path"`
}

func runStats(cmd *cobra.Command, args []string) error {
	cmdCtx, err := openDB(false)
	if err != nil {
		return err
	}
	defer cmdCtx.Close()

	// Get Axon instance for potential auto-indexing
	ax, err := cmdCtx.Axon()
	if err != nil {
		return err
	}

	// Gather stats
	data := statsData{
		Database: cmdCtx.Storage.GetDatabasePath(),
	}

	// File size
	if fileInfo, err := os.Stat(data.Database); err == nil {
		data.FileSizeBytes = fileInfo.Size()
		data.FileSizeHuman = formatFileSize(data.FileSizeBytes)
	}

	g := ax.Graph()
	ctx := cmdCtx.Ctx
	cwd := cmdCtx.Cwd

	var nodeTypes []graph.CountItem
	var edgeTypes []graph.CountItem
	var extensions []graph.CountItem
	var labels []graph.CountItem

	if statsGlobal {
		// Global: Use AQL queries for efficiency
		// Node count and types
		nodeQ := aql.Nodes.
			Select(aql.Type, aql.Count()).
			GroupBy(aql.Type).
			Build()
		nodeResult, err := g.Storage().Query(ctx, nodeQ)
		if err != nil {
			return fmt.Errorf("failed to query node types: %w", err)
		}
		nodeTypes = nodeResult.Counts
		for _, c := range nodeTypes {
			data.Nodes += c.Count
		}

		// Edge count and types
		edgeQ := aql.Edges.
			Select(aql.Type, aql.Count()).
			GroupBy(aql.Type).
			Build()
		edgeResult, err := g.Storage().Query(ctx, edgeQ)
		if err != nil {
			return fmt.Errorf("failed to query edge types: %w", err)
		}
		edgeTypes = edgeResult.Counts
		for _, c := range edgeTypes {
			data.Edges += c.Count
		}

		// Extensions and labels for verbose mode using AQL
		if statsVerbose || statsOutput == "json" {
			// Labels using json_each
			labelsQ := aql.Nodes.JsonEach(aql.Labels).
				Select(aql.Val, aql.Count()).
				Where(aql.Val.Ne("")).
				GroupBy(aql.Val).
				Build()
			labelsResult, err := g.Storage().Query(ctx, labelsQ)
			if err != nil {
				return fmt.Errorf("failed to query labels: %w", err)
			}
			labels = labelsResult.Counts

			// Extensions from data.ext field
			extQ := aql.Nodes.
				Select(aql.DataExt, aql.Count()).
				Where(aql.DataExt.IsNotNull()).
				GroupBy(aql.DataExt).
				Build()
			extResult, err := g.Storage().Query(ctx, extQ)
			if err != nil {
				return fmt.Errorf("failed to query extensions: %w", err)
			}
			extensions = extResult.Counts
		}
	} else {
		// Scoped: Use AQL with EXISTS pattern for node types
		absPath, err := filepath.Abs(cwd)
		if err != nil {
			return fmt.Errorf("failed to resolve path: %w", err)
		}

		uri := types.PathToURI(absPath)
		cwdNode, err := g.Storage().GetNodeByURI(ctx, uri)
		if err != nil {
			// Directory not indexed - prompt to index
			fmt.Printf("Directory not indexed: %s\nIndex now? [Y/n] ", absPath)
			var response string
			fmt.Scanln(&response)
			response = strings.TrimSpace(strings.ToLower(response))
			if response != "" && response != "y" && response != "yes" {
				return fmt.Errorf("directory not indexed: %s", absPath)
			}

			fmt.Printf("Indexing %s...\n", absPath)
			if _, err := ax.Index(ctx, absPath); err != nil {
				return fmt.Errorf("indexing failed: %w", err)
			}
			fmt.Println("Done.")

			cwdNode, err = g.Storage().GetNodeByURI(ctx, uri)
			if err != nil {
				return fmt.Errorf("failed to find directory after indexing: %w", err)
			}
		}

		// Node types with DescendantsOf scoping
		nodeQ := aql.Nodes.
			Select(aql.Type, aql.Count()).
			Where(aql.Nodes.ScopedTo(cwdNode.ID)).
			GroupBy(aql.Type).
			Build()
		nodeResult, err := g.Storage().Query(ctx, nodeQ)
		if err != nil {
			return fmt.Errorf("failed to query node types: %w", err)
		}
		nodeTypes = nodeResult.Counts
		for _, c := range nodeTypes {
			data.Nodes += c.Count
		}

		// Edge types using scoped pattern
		edgeQ := aql.Edges.
			Select(aql.Type, aql.Count()).
			Where(aql.Edges.ScopedTo(cwdNode.ID)).
			GroupBy(aql.Type).
			Build()
		edgeResult, err := g.Storage().Query(ctx, edgeQ)
		if err != nil {
			return fmt.Errorf("failed to query edge types: %w", err)
		}
		edgeTypes = edgeResult.Counts
		for _, c := range edgeTypes {
			data.Edges += c.Count
		}

		// Extensions using DescendantsOf
		extQ := aql.Nodes.
			Select(aql.DataExt, aql.Count()).
			Where(aql.And(
				aql.DataExt.IsNotNull(),
				aql.Nodes.ScopedTo(cwdNode.ID),
			)).
			GroupBy(aql.DataExt).
			Build()
		extResult, err := g.Storage().Query(ctx, extQ)
		if err != nil {
			return fmt.Errorf("failed to query extensions: %w", err)
		}
		extensions = extResult.Counts

		// Labels using json_each with DescendantsOf
		labelsQ := aql.Nodes.JsonEach(aql.Labels).
			Select(aql.Val, aql.Count()).
			Where(aql.And(
				aql.Val.Ne(""),
				aql.Nodes.ScopedTo(cwdNode.ID),
			)).
			GroupBy(aql.Val).
			Build()
		labelsResult, err := g.Storage().Query(ctx, labelsQ)
		if err != nil {
			return fmt.Errorf("failed to query labels: %w", err)
		}
		labels = labelsResult.Counts
	}

	// Store detailed breakdowns
	if statsVerbose || statsOutput == "json" {
		data.NodeTypes = countItemsToMap(nodeTypes)
		data.EdgeTypes = countItemsToMap(edgeTypes)
		data.Extensions = topN(countItemsToMap(extensions), 10)
		data.Labels = countItemsToMap(labels)
	}

	// Last indexed (always from database, not affected by scope)
	lastRun, err := cmdCtx.Storage.GetLastIndexRun(cmdCtx.Ctx)
	if err != nil {
		return fmt.Errorf("failed to get last index run: %w", err)
	}
	if lastRun != nil {
		data.LastIndexed = &lastIndexedData{
			Timestamp:     lastRun.FinishedAt,
			Ago:           formatTimeAgo(lastRun.FinishedAt),
			DurationMs:    lastRun.DurationMs,
			DurationHuman: formatDurationMs(lastRun.DurationMs),
			FilesIndexed:  lastRun.FilesIndexed,
			RootPath:      lastRun.RootPath,
		}
	}

	// Render output
	if statsOutput == "json" {
		return renderStatsJSON(data)
	}
	return renderStatsText(data, statsVerbose)
}

// countItemsToMap converts a []graph.CountItem slice to a map[string]int.
func countItemsToMap(items []graph.CountItem) map[string]int {
	m := make(map[string]int, len(items))
	for _, item := range items {
		m[item.Name] = item.Count
	}
	return m
}

// topN returns the top N entries by count from a map
func topN(m map[string]int, n int) map[string]int {
	type kv struct {
		Key   string
		Value int
	}
	var sorted []kv
	for k, v := range m {
		sorted = append(sorted, kv{k, v})
	}
	sort.Slice(sorted, func(i, j int) bool {
		return sorted[i].Value > sorted[j].Value
	})

	result := make(map[string]int)
	for i := 0; i < n && i < len(sorted); i++ {
		result[sorted[i].Key] = sorted[i].Value
	}
	return result
}

func renderStatsJSON(data statsData) error {
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	return enc.Encode(data)
}

func renderStatsText(data statsData, verbose bool) error {
	p := message.NewPrinter(language.English)

	// Basic info
	fmt.Printf("Database:     %s\n", data.Database)
	fmt.Printf("File size:    %s\n", data.FileSizeHuman)
	fmt.Println()
	if statsGlobal {
		p.Printf("Nodes:        %d  (global)\n", data.Nodes)
		p.Printf("Edges:        %d  (global)\n", data.Edges)
	} else {
		p.Printf("Nodes:        %d  (scoped to CWD)\n", data.Nodes)
		p.Printf("Edges:        %d  (scoped to CWD)\n", data.Edges)
	}

	// Last indexed
	if data.LastIndexed != nil {
		fmt.Println()
		p.Printf("Last indexed: %s (%s, %d files)\n",
			data.LastIndexed.Ago,
			data.LastIndexed.DurationHuman,
			data.LastIndexed.FilesIndexed)
	}

	if !verbose {
		return nil
	}

	// Node types
	if len(data.NodeTypes) > 0 {
		fmt.Printf("\nNode types (%d):\n", len(data.NodeTypes))
		renderCountMap(data.NodeTypes, p)
	}

	// Edge types
	if len(data.EdgeTypes) > 0 {
		fmt.Printf("\nEdge types (%d):\n", len(data.EdgeTypes))
		renderCountMap(data.EdgeTypes, p)
	}

	// Extensions
	if len(data.Extensions) > 0 {
		fmt.Printf("\nExtensions (top 10):\n")
		renderCountMap(data.Extensions, p)
	}

	// Labels
	if len(data.Labels) > 0 {
		fmt.Printf("\nLabels (%d):\n", len(data.Labels))
		renderCountMap(data.Labels, p)
	}

	return nil
}

func renderCountMap(m map[string]int, p *message.Printer) {
	// Sort by count descending
	type kv struct {
		Key   string
		Value int
	}
	var sorted []kv
	for k, v := range m {
		sorted = append(sorted, kv{k, v})
	}
	sort.Slice(sorted, func(i, j int) bool {
		return sorted[i].Value > sorted[j].Value
	})

	// Find max key length for alignment
	maxLen := 0
	for _, kv := range sorted {
		if len(kv.Key) > maxLen {
			maxLen = len(kv.Key)
		}
	}
	if maxLen > 20 {
		maxLen = 20
	}

	for _, kv := range sorted {
		key := kv.Key
		if len(key) > 20 {
			key = key[:17] + "..."
		}
		p.Printf("  %*s  %d\n", maxLen, key, kv.Value)
	}
}

func formatFileSize(bytes int64) string {
	const (
		KB = 1024
		MB = KB * 1024
		GB = MB * 1024
	)

	switch {
	case bytes >= GB:
		return fmt.Sprintf("%.1f GB", float64(bytes)/GB)
	case bytes >= MB:
		return fmt.Sprintf("%.1f MB", float64(bytes)/MB)
	case bytes >= KB:
		return fmt.Sprintf("%.1f KB", float64(bytes)/KB)
	default:
		return fmt.Sprintf("%d B", bytes)
	}
}

func formatTimeAgo(t time.Time) string {
	d := time.Since(t)

	switch {
	case d < time.Minute:
		return "just now"
	case d < time.Hour:
		mins := int(d.Minutes())
		if mins == 1 {
			return "1 minute ago"
		}
		return fmt.Sprintf("%d minutes ago", mins)
	case d < 24*time.Hour:
		hours := int(d.Hours())
		if hours == 1 {
			return "1 hour ago"
		}
		return fmt.Sprintf("%d hours ago", hours)
	default:
		days := int(d.Hours() / 24)
		if days == 1 {
			return "1 day ago"
		}
		return fmt.Sprintf("%d days ago", days)
	}
}

func formatDurationMs(ms int64) string {
	d := time.Duration(ms) * time.Millisecond

	if d < time.Second {
		return "< 1s"
	}

	hours := int(d.Hours())
	minutes := int(d.Minutes()) % 60
	seconds := int(d.Seconds()) % 60

	if hours > 0 {
		if minutes > 0 {
			return fmt.Sprintf("%dh %dm", hours, minutes)
		}
		return fmt.Sprintf("%dh", hours)
	}

	if minutes > 0 {
		if seconds > 0 {
			return fmt.Sprintf("%dm %ds", minutes, seconds)
		}
		return fmt.Sprintf("%dm", minutes)
	}

	return fmt.Sprintf("%ds", seconds)
}
