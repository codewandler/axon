package main

import (
	"encoding/json"
	"fmt"
	"os"
	"time"

	"github.com/codewandler/axon/aql"
	"github.com/codewandler/axon/types"
	"github.com/spf13/cobra"
	"golang.org/x/text/language"
	"golang.org/x/text/message"
)

var (
	infoOutput string
)

var infoCmd = &cobra.Command{
	Use:   "info",
	Short: "Show database information and status",
	Long: `Display comprehensive database information including version, location,
statistics, project summary, and last index details.

This command provides a dashboard view of the Axon database.

Examples:
  axon info           # Show info for current database
  axon info -o json   # JSON output`,
	RunE: runInfo,
}

func init() {
	infoCmd.Flags().StringVarP(&infoOutput, "output", "o", "text", "Output format: text, json")
}

// infoData holds all info data for rendering
type infoData struct {
	Version       string           `json:"version"`
	Database      string           `json:"database"`
	IsGlobal      bool             `json:"is_global"`
	FileSizeBytes int64            `json:"file_size_bytes"`
	FileSizeHuman string           `json:"file_size_human"`
	SchemaVersion int              `json:"schema_version"`
	Nodes         int              `json:"nodes"`
	Edges         int              `json:"edges"`
	OrphanedEdges int              `json:"orphaned_edges"`
	NodeTypeCount int              `json:"node_type_count"`
	EdgeTypeCount int              `json:"edge_type_count"`
	Projects      *projectSummary  `json:"projects,omitempty"`
	LastIndexed   *lastIndexedData `json:"last_indexed,omitempty"`
}

type projectSummary struct {
	Total  int            `json:"total"`
	ByLang map[string]int `json:"by_language"`
}

func runInfo(cmd *cobra.Command, args []string) error {
	cmdCtx, err := openDB(false)
	if err != nil {
		return err
	}
	defer cmdCtx.Close()

	ctx := cmdCtx.Ctx
	storage := cmdCtx.Storage

	// Gather info
	data := infoData{
		Version:  version,
		Database: storage.GetDatabasePath(),
		IsGlobal: cmdCtx.DBLoc.IsGlobal,
	}

	// File size
	if fileInfo, err := os.Stat(data.Database); err == nil {
		data.FileSizeBytes = fileInfo.Size()
		data.FileSizeHuman = formatFileSize(data.FileSizeBytes)
	}

	// Schema version
	schemaVer, err := storage.GetSchemaVersion(ctx)
	if err != nil {
		return fmt.Errorf("failed to get schema version: %w", err)
	}
	data.SchemaVersion = schemaVer

	// Node count and types
	nodeQ := aql.Nodes.
		Select(aql.Type, aql.Count()).
		GroupBy(aql.Type).
		Build()
	nodeResult, err := storage.Query(ctx, nodeQ)
	if err != nil {
		return fmt.Errorf("failed to query node types: %w", err)
	}
	data.NodeTypeCount = len(nodeResult.Counts)
	for _, c := range nodeResult.Counts {
		data.Nodes += c.Count
	}

	// Edge count and types
	edgeQ := aql.Edges.
		Select(aql.Type, aql.Count()).
		GroupBy(aql.Type).
		Build()
	edgeResult, err := storage.Query(ctx, edgeQ)
	if err != nil {
		return fmt.Errorf("failed to query edge types: %w", err)
	}
	data.EdgeTypeCount = len(edgeResult.Counts)
	for _, c := range edgeResult.Counts {
		data.Edges += c.Count
	}

	// Orphaned edges count
	orphaned, err := storage.CountOrphanedEdges(ctx)
	if err != nil {
		return fmt.Errorf("failed to count orphaned edges: %w", err)
	}
	data.OrphanedEdges = orphaned

	// Project summary
	projQ := aql.Nodes.
		Select(aql.Data.Field("language"), aql.Count()).
		Where(aql.Type.Eq(types.TypeProject)).
		GroupBy(aql.Data.Field("language")).
		Build()
	projResult, err := storage.Query(ctx, projQ)
	if err != nil {
		return fmt.Errorf("failed to query projects: %w", err)
	}
	if len(projResult.Counts) > 0 {
		total := 0
		byLang := make(map[string]int)
		for _, c := range projResult.Counts {
			total += c.Count
			byLang[c.Name] = c.Count
		}
		data.Projects = &projectSummary{
			Total:  total,
			ByLang: byLang,
		}
	}

	// Last indexed
	lastRun, err := storage.GetLastIndexRun(ctx)
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
	if infoOutput == "json" {
		return renderInfoJSON(data)
	}
	return renderInfoText(data)
}

func renderInfoJSON(data infoData) error {
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	return enc.Encode(data)
}

func renderInfoText(data infoData) error {
	p := message.NewPrinter(language.English)

	// Version header
	fmt.Printf("Axon %s\n\n", data.Version)

	// Database info
	dbType := "local"
	if data.IsGlobal {
		dbType = "global"
	}
	fmt.Printf("Database:      %s (%s)\n", data.Database, dbType)
	fmt.Printf("File size:     %s\n", data.FileSizeHuman)
	fmt.Printf("Schema:        v%d\n", data.SchemaVersion)
	fmt.Println()

	// Counts
	p.Printf("Nodes:         %d\n", data.Nodes)
	p.Printf("Edges:         %d\n", data.Edges)
	if data.OrphanedEdges > 0 {
		p.Printf("Orphaned edges: %d (run 'axon gc' to clean)\n", data.OrphanedEdges)
	} else {
		fmt.Println("Orphaned edges: 0")
	}
	fmt.Println()

	// Type counts
	p.Printf("Node types:    %d\n", data.NodeTypeCount)
	p.Printf("Edge types:    %d\n", data.EdgeTypeCount)

	// Projects
	if data.Projects != nil && data.Projects.Total > 0 {
		fmt.Println()
		p.Printf("Projects:      %d\n", data.Projects.Total)
		for lang, count := range data.Projects.ByLang {
			p.Printf("  %-12s %d\n", lang, count)
		}
	}

	// Last indexed
	if data.LastIndexed != nil {
		fmt.Println()
		p.Printf("Last indexed:  %s (%s, %d files)\n",
			data.LastIndexed.Ago,
			data.LastIndexed.DurationHuman,
			data.LastIndexed.FilesIndexed)
		fmt.Printf("  Path:        %s\n", data.LastIndexed.RootPath)
	} else {
		fmt.Println()
		fmt.Println("Last indexed:  never")
	}

	return nil
}

// formatTimestamp formats a time as ISO-8601
func formatTimestamp(t time.Time) string {
	return t.Format(time.RFC3339)
}
