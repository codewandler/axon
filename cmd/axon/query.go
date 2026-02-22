package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"text/tabwriter"

	"github.com/codewandler/axon/aql"
	"github.com/codewandler/axon/graph"
	"github.com/spf13/cobra"
)

var (
	queryExplain bool
	queryOutput  string
)

var queryCmd = &cobra.Command{
	Use:   "query <aql-query>",
	Short: "Execute an AQL (Axon Query Language) query",
	Long: `Execute an AQL (Axon Query Language) query against the graph database.

AQL provides a flexible query interface for exploring the graph. The query is compiled
to SQL and executed against the SQLite storage backend.

Examples:
  # Select all nodes
  axon query "SELECT * FROM nodes"

  # Select specific columns
  axon query "SELECT name, type FROM nodes WHERE type = 'fs:file'"

  # Query with JSON field access
  axon query "SELECT * FROM nodes WHERE data.ext = 'go'"

  # Group and count by type
  axon query "SELECT type, COUNT(*) FROM nodes GROUP BY type"

  # Order and limit results
  axon query "SELECT * FROM nodes WHERE type = 'fs:file' ORDER BY name LIMIT 10"

  # Query edges
  axon query "SELECT * FROM edges WHERE type = 'contains'"

  # Show query plan (EXPLAIN)
  axon query --explain "SELECT * FROM nodes WHERE type GLOB 'fs:*'"

Output Formats:
  --output table  : Tabular format with columns (default)
  --output json   : JSON array of results
  --output count  : Just show result count`,
	Args: cobra.ExactArgs(1),
	RunE: runQuery,
}

func init() {
	queryCmd.Flags().BoolVar(&queryExplain, "explain", false, "Show query execution plan instead of results")
	queryCmd.Flags().StringVarP(&queryOutput, "output", "o", "table", "Output format: table, json, count")
}

func runQuery(cmd *cobra.Command, args []string) error {
	cmdCtx, err := openDB(false)
	if err != nil {
		return err
	}
	defer cmdCtx.Close()

	// Get Axon instance
	ax, err := cmdCtx.Axon()
	if err != nil {
		return err
	}

	storage := ax.Graph().Storage()

	// Parse AQL query
	aqlQuery := args[0]
	query, err := aql.Parse(aqlQuery)
	if err != nil {
		return fmt.Errorf("parse error: %w", err)
	}

	ctx := context.Background()

	// Handle EXPLAIN
	if queryExplain {
		plan, err := storage.Explain(ctx, query)
		if err != nil {
			return fmt.Errorf("explain error: %w", err)
		}
		printQueryPlan(plan)
		return nil
	}

	// Execute query
	result, err := storage.Query(ctx, query)
	if err != nil {
		return fmt.Errorf("query error: %w", err)
	}

	// Print results based on output format
	switch queryOutput {
	case "json":
		return printQueryResultJSON(result)
	case "count":
		return printQueryResultCount(result)
	case "table":
		return printQueryResultTable(result)
	default:
		return fmt.Errorf("unknown output format: %s", queryOutput)
	}
}

// printQueryPlan prints the query execution plan.
func printQueryPlan(plan *graph.QueryPlan) {
	fmt.Println("Query Plan:")
	fmt.Println("SQL:", plan.SQL)
	if len(plan.Args) > 0 {
		fmt.Println("Args:", plan.Args)
	}
	fmt.Println()
	fmt.Println("Execution Plan:")
	fmt.Println(plan.SQLitePlan)
}

// printQueryResultJSON prints results as JSON.
func printQueryResultJSON(result *graph.QueryResult) error {
	var data any
	switch result.Type {
	case graph.ResultTypeNodes:
		data = result.Nodes
	case graph.ResultTypeEdges:
		data = result.Edges
	case graph.ResultTypeCounts:
		data = result.Counts
	}

	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	if err := enc.Encode(data); err != nil {
		return fmt.Errorf("failed to encode JSON: %w", err)
	}
	return nil
}

// printQueryResultCount prints just the count of results.
func printQueryResultCount(result *graph.QueryResult) error {
	var count int
	switch result.Type {
	case graph.ResultTypeNodes:
		count = len(result.Nodes)
	case graph.ResultTypeEdges:
		count = len(result.Edges)
	case graph.ResultTypeCounts:
		// Sum all counts
		for _, c := range result.Counts {
			count += c
		}
	}
	fmt.Println(count)
	return nil
}

// printQueryResultTable prints results as a table.
func printQueryResultTable(result *graph.QueryResult) error {
	switch result.Type {
	case graph.ResultTypeNodes:
		return printNodesTable(result.Nodes)
	case graph.ResultTypeEdges:
		return printEdgesTable(result.Edges)
	case graph.ResultTypeCounts:
		return printCountsTable(result.Counts)
	default:
		return fmt.Errorf("unknown result type: %v", result.Type)
	}
}

// printNodesTable prints nodes as a table.
func printNodesTable(nodes []*graph.Node) error {
	if len(nodes) == 0 {
		fmt.Println("No results")
		return nil
	}

	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	defer w.Flush()

	// Detect which columns have data across ALL nodes (not just first, since first might be empty)
	hasID := false
	hasType := false
	hasURI := false
	hasName := false
	hasKey := false
	hasLabels := false
	hasData := false

	for _, n := range nodes {
		if n.ID != "" {
			hasID = true
		}
		if n.Type != "" {
			hasType = true
		}
		if n.URI != "" {
			hasURI = true
		}
		if n.Name != "" {
			hasName = true
		}
		if n.Key != "" {
			hasKey = true
		}
		if len(n.Labels) > 0 {
			hasLabels = true
		}
		if n.Data != nil {
			hasData = true
		}
	}

	// Print header
	var headers []string
	if hasID {
		headers = append(headers, "ID")
	}
	if hasType {
		headers = append(headers, "Type")
	}
	if hasName {
		headers = append(headers, "Name")
	}
	if hasURI {
		headers = append(headers, "URI")
	}
	if hasKey {
		headers = append(headers, "Key")
	}
	if hasLabels {
		headers = append(headers, "Labels")
	}
	if hasData {
		headers = append(headers, "Data")
	}
	fmt.Fprintln(w, strings.Join(headers, "\t"))

	// Print rows
	for _, node := range nodes {
		var cols []string
		if hasID {
			cols = append(cols, truncate(node.ID, 22))
		}
		if hasType {
			cols = append(cols, node.Type)
		}
		if hasName {
			cols = append(cols, node.Name)
		}
		if hasURI {
			cols = append(cols, truncate(node.URI, 60))
		}
		if hasKey {
			cols = append(cols, truncate(node.Key, 40))
		}
		if hasLabels {
			cols = append(cols, strings.Join(node.Labels, ", "))
		}
		if hasData {
			dataJSON, _ := json.Marshal(node.Data)
			cols = append(cols, truncate(string(dataJSON), 40))
		}
		fmt.Fprintln(w, strings.Join(cols, "\t"))
	}

	return nil
}

// printEdgesTable prints edges as a table.
func printEdgesTable(edges []*graph.Edge) error {
	if len(edges) == 0 {
		fmt.Println("No results")
		return nil
	}

	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	defer w.Flush()

	fmt.Fprintln(w, "ID\tType\tFrom\tTo\tData")
	for _, edge := range edges {
		var dataStr string
		if edge.Data != nil {
			dataJSON, _ := json.Marshal(edge.Data)
			dataStr = truncate(string(dataJSON), 40)
		}
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\n",
			truncate(edge.ID, 22),
			edge.Type,
			truncate(edge.From, 22),
			truncate(edge.To, 22),
			dataStr,
		)
	}

	return nil
}

// printCountsTable prints count aggregations as a table.
func printCountsTable(counts map[string]int) error {
	if len(counts) == 0 {
		fmt.Println("No results")
		return nil
	}

	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	defer w.Flush()

	fmt.Fprintln(w, "Key\tCount")
	for key, count := range counts {
		fmt.Fprintf(w, "%s\t%d\n", key, count)
	}

	return nil
}

// truncate truncates a string to maxLen, adding "..." if truncated.
func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	if maxLen <= 3 {
		return s[:maxLen]
	}
	return s[:maxLen-3] + "..."
}
