package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

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

	// Resolve scope using graph traversal
	traverseOpts, err := resolveScopeTraversal(cmdCtx.Ctx, cmdCtx.Storage, ax, statsGlobal, cmdCtx.Cwd, 0)
	if err != nil {
		return err
	}

	// Traverse and collect stats
	results, err := cmdCtx.Storage.Traverse(cmdCtx.Ctx, traverseOpts)
	if err != nil {
		return fmt.Errorf("failed to traverse graph: %w", err)
	}

	// Collect stats from traversal
	nodeTypes := make(map[string]int)
	extensions := make(map[string]int)
	labels := make(map[string]int)
	edgesSeen := make(map[string]bool)
	edgeTypes := make(map[string]int)

	for r := range results {
		if r.Err != nil {
			return fmt.Errorf("traversal error: %w", r.Err)
		}
		data.Nodes++
		nodeTypes[r.Node.Type]++

		// Count extension from name
		if ext := filepath.Ext(r.Node.Name); ext != "" {
			extensions[ext]++
		}

		// Count labels
		for _, label := range r.Node.Labels {
			labels[label]++
		}

		// Count edge (if we haven't seen it)
		if r.Via != nil && !edgesSeen[r.Via.ID] {
			edgesSeen[r.Via.ID] = true
			data.Edges++
			edgeTypes[r.Via.Type]++
		}
	}

	// Store detailed breakdowns
	if statsVerbose || statsOutput == "json" {
		data.NodeTypes = nodeTypes
		data.EdgeTypes = edgeTypes
		data.Extensions = topN(extensions, 10)
		data.Labels = labels
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
	p.Printf("Nodes:        %d\n", data.Nodes)
	p.Printf("Edges:        %d\n", data.Edges)

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

// extractExtension extracts the file extension from a name
func extractExtension(name string) string {
	// Handle cases like .gitignore (no extension)
	if strings.HasPrefix(name, ".") && !strings.Contains(name[1:], ".") {
		return ""
	}
	ext := filepath.Ext(name)
	return ext
}
