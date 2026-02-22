package main

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"reflect"
	"strings"

	"github.com/codewandler/axon/aql"
	"github.com/spf13/cobra"
)

var parseOutput string

var parseCmd = &cobra.Command{
	Use:   "parse [query]",
	Short: "Parse an AQL query and display its AST",
	Long: `Parse an AQL query and display its Abstract Syntax Tree (AST).

The query can be provided as a command argument or read from stdin.
Validation is automatically performed after parsing.`,
	Example: `  # Parse a simple query
  axon parse "SELECT * FROM nodes"
  
  # Parse with JSON output
  axon parse "SELECT * FROM nodes WHERE type = 'fs:file'" -o json
  
  # Read complex query from stdin
  echo "SELECT a, b FROM (a)-[:contains]->(b)" | axon parse
  
  # Pipe from file
  cat query.aql | axon parse`,
	RunE: runParse,
}

func init() {
	parseCmd.Flags().StringVarP(&parseOutput, "output", "o", "text", "Output format: text, json")
}

func runParse(cmd *cobra.Command, args []string) error {
	// Read query from arg or stdin
	var query string
	if len(args) > 0 {
		query = args[0]
	} else {
		data, err := io.ReadAll(os.Stdin)
		if err != nil {
			return fmt.Errorf("failed to read from stdin: %w", err)
		}
		query = string(data)
	}

	query = strings.TrimSpace(query)
	if query == "" {
		return fmt.Errorf("empty query provided")
	}

	// Parse the query
	parser := aql.NewParser()
	ast, err := parser.Parse(query)
	if err != nil {
		return fmt.Errorf("parse error: %w", err)
	}

	// Validate the AST
	validationErrs := aql.Validate(ast)

	// Output based on format
	switch parseOutput {
	case "json":
		return outputParseJSON(ast, validationErrs)
	default:
		return outputParseText(ast, validationErrs)
	}
}

// ParseResult is the JSON output structure
type ParseResult struct {
	AST        *aql.Query       `json:"ast"`
	Validation ValidationResult `json:"validation"`
}

type ValidationResult struct {
	OK     bool     `json:"ok"`
	Errors []string `json:"errors"`
}

func outputParseJSON(ast *aql.Query, validationErrs []*aql.ValidationError) error {
	result := ParseResult{
		AST: ast,
		Validation: ValidationResult{
			OK:     len(validationErrs) == 0,
			Errors: make([]string, len(validationErrs)),
		},
	}
	for i, err := range validationErrs {
		result.Validation.Errors[i] = err.Error()
	}

	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	return enc.Encode(result)
}

func outputParseText(ast *aql.Query, validationErrs []*aql.ValidationError) error {
	fmt.Println("Query")
	fmt.Print(formatASTTree(ast.Select, ""))

	fmt.Println()
	if len(validationErrs) == 0 {
		fmt.Println("Validation: OK")
	} else {
		fmt.Println("Validation Errors:")
		for _, err := range validationErrs {
			fmt.Printf("  • %s\n", err)
		}
	}
	return nil
}

// formatASTTree recursively formats AST nodes as a tree
func formatASTTree(node interface{}, prefix string) string {
	if node == nil {
		return ""
	}

	var sb strings.Builder
	val := reflect.ValueOf(node)

	// Dereference pointers
	for val.Kind() == reflect.Ptr {
		if val.IsNil() {
			return ""
		}
		val = val.Elem()
	}

	typeName := val.Type().Name()

	// Handle different AST node types
	switch n := node.(type) {
	case *aql.SelectStmt:
		sb.WriteString(formatSelectStmt(n, prefix))
	case *aql.Column:
		sb.WriteString(formatColumn(n, prefix))
	case *aql.Star:
		sb.WriteString(prefix + "└─ Star\n")
	case *aql.CountCall:
		sb.WriteString(prefix + "└─ COUNT(*)\n")
	case *aql.Selector:
		sb.WriteString(prefix + "└─ Selector: " + n.String() + "\n")
	case *aql.TableSource:
		sb.WriteString(prefix + "└─ TableSource\n")
		sb.WriteString(prefix + "   └─ Table: " + n.Table + "\n")
	case *aql.PatternSource:
		sb.WriteString(formatPatternSource(n, prefix))
	case *aql.Pattern:
		sb.WriteString(formatPattern(n, prefix))
	case *aql.NodePattern:
		sb.WriteString(formatNodePattern(n, prefix))
	case *aql.EdgePattern:
		sb.WriteString(formatEdgePattern(n, prefix))
	case *aql.ComparisonExpr:
		sb.WriteString(formatComparisonExpr(n, prefix))
	case *aql.BinaryExpr:
		sb.WriteString(formatBinaryExpr(n, prefix))
	case *aql.UnaryExpr:
		sb.WriteString(formatUnaryExpr(n, prefix))
	case *aql.InExpr:
		sb.WriteString(formatInExpr(n, prefix))
	case *aql.BetweenExpr:
		sb.WriteString(formatBetweenExpr(n, prefix))
	case *aql.LabelExpr:
		sb.WriteString(formatLabelExpr(n, prefix))
	case *aql.IsNullExpr:
		sb.WriteString(formatIsNullExpr(n, prefix))
	case *aql.ExistsExpr:
		sb.WriteString(formatExistsExpr(n, prefix))
	case *aql.ParenExpr:
		sb.WriteString(prefix + "└─ ParenExpr\n")
		sb.WriteString(formatASTTree(n.Inner, prefix+"   "))
	case *aql.StringLit:
		sb.WriteString(prefix + "└─ String: \"" + n.Value + "\"\n")
	case *aql.NumberLit:
		sb.WriteString(fmt.Sprintf("%s└─ Number: %v\n", prefix, n.Value))
	case *aql.BoolLit:
		sb.WriteString(fmt.Sprintf("%s└─ Bool: %v\n", prefix, n.Value))
	case *aql.Parameter:
		sb.WriteString(prefix + "└─ Parameter: " + n.String() + "\n")
	case *aql.OrderSpec:
		sb.WriteString(formatOrderSpec(n, prefix))
	default:
		sb.WriteString(prefix + "└─ " + typeName + "\n")
	}

	return sb.String()
}

func formatSelectStmt(stmt *aql.SelectStmt, prefix string) string {
	var sb strings.Builder
	sb.WriteString(prefix + "└─ SelectStmt\n")

	newPrefix := prefix + "   "

	if stmt.Distinct {
		sb.WriteString(newPrefix + "├─ Distinct: true\n")
	}

	// Columns
	sb.WriteString(newPrefix + "├─ Columns\n")
	for i, col := range stmt.Columns {
		colPrefix := newPrefix + "│  "
		if i == len(stmt.Columns)-1 && stmt.From == nil && stmt.Where == nil && stmt.GroupBy == nil && stmt.Having == nil && stmt.OrderBy == nil && stmt.Limit == nil && stmt.Offset == nil {
			colPrefix = newPrefix + "   "
		}
		sb.WriteString(formatColumn(&col, colPrefix))
	}

	// From
	if stmt.From != nil {
		sb.WriteString(newPrefix + "├─ From\n")
		sb.WriteString(formatASTTree(stmt.From, newPrefix+"│  "))
	}

	// Where
	if stmt.Where != nil {
		sb.WriteString(newPrefix + "├─ Where\n")
		sb.WriteString(formatASTTree(stmt.Where, newPrefix+"│  "))
	}

	// GroupBy
	if len(stmt.GroupBy) > 0 {
		sb.WriteString(newPrefix + "├─ GroupBy\n")
		for _, sel := range stmt.GroupBy {
			sb.WriteString(newPrefix + "│  └─ " + sel.String() + "\n")
		}
	}

	// Having
	if stmt.Having != nil {
		sb.WriteString(newPrefix + "├─ Having\n")
		sb.WriteString(formatASTTree(stmt.Having, newPrefix+"│  "))
	}

	// OrderBy
	if len(stmt.OrderBy) > 0 {
		sb.WriteString(newPrefix + "├─ OrderBy\n")
		for _, order := range stmt.OrderBy {
			sb.WriteString(formatOrderSpec(&order, newPrefix+"│  "))
		}
	}

	// Limit
	if stmt.Limit != nil {
		sb.WriteString(fmt.Sprintf("%s├─ Limit: %d\n", newPrefix, *stmt.Limit))
	}

	// Offset
	if stmt.Offset != nil {
		sb.WriteString(fmt.Sprintf("%s└─ Offset: %d\n", newPrefix, *stmt.Offset))
	}

	return sb.String()
}

func formatColumn(col *aql.Column, prefix string) string {
	var sb strings.Builder
	sb.WriteString(prefix + "└─ Column")
	if col.Alias != "" {
		sb.WriteString(" (AS " + col.Alias + ")")
	}
	sb.WriteString("\n")
	sb.WriteString(formatASTTree(col.Expr, prefix+"   "))
	return sb.String()
}

func formatPatternSource(ps *aql.PatternSource, prefix string) string {
	var sb strings.Builder
	sb.WriteString(prefix + "└─ PatternSource\n")
	for _, pattern := range ps.Patterns {
		sb.WriteString(formatPattern(pattern, prefix+"   "))
	}
	return sb.String()
}

func formatPattern(p *aql.Pattern, prefix string) string {
	var sb strings.Builder
	sb.WriteString(prefix + "└─ Pattern\n")
	for _, elem := range p.Elements {
		sb.WriteString(formatASTTree(elem, prefix+"   "))
	}
	return sb.String()
}

func formatNodePattern(np *aql.NodePattern, prefix string) string {
	var sb strings.Builder
	sb.WriteString(prefix + "└─ NodePattern")

	parts := []string{}
	if np.Variable != "" {
		parts = append(parts, "var: "+np.Variable)
	}
	if np.Type != "" {
		parts = append(parts, "type: "+np.Type)
	}

	if len(parts) > 0 {
		sb.WriteString(" (" + strings.Join(parts, ", ") + ")")
	}
	sb.WriteString("\n")

	if np.Where != nil {
		sb.WriteString(prefix + "   └─ Where\n")
		sb.WriteString(formatASTTree(np.Where, prefix+"      "))
	}

	return sb.String()
}

func formatEdgePattern(ep *aql.EdgePattern, prefix string) string {
	var sb strings.Builder
	sb.WriteString(prefix + "└─ EdgePattern")

	parts := []string{}
	parts = append(parts, "dir: "+ep.Direction.String())

	if ep.Variable != "" {
		parts = append(parts, "var: "+ep.Variable)
	}
	if ep.Type != "" {
		parts = append(parts, "type: "+ep.Type)
	}
	if len(ep.Types) > 0 {
		parts = append(parts, "types: ["+strings.Join(ep.Types, "|")+"]")
	}
	if ep.MinHops != nil {
		if ep.MaxHops != nil {
			parts = append(parts, fmt.Sprintf("hops: %d..%d", *ep.MinHops, *ep.MaxHops))
		} else {
			parts = append(parts, fmt.Sprintf("hops: %d..", *ep.MinHops))
		}
	}

	if len(parts) > 0 {
		sb.WriteString(" (" + strings.Join(parts, ", ") + ")")
	}
	sb.WriteString("\n")

	return sb.String()
}

func formatComparisonExpr(expr *aql.ComparisonExpr, prefix string) string {
	var sb strings.Builder
	sb.WriteString(prefix + "└─ ComparisonExpr\n")
	sb.WriteString(prefix + "   ├─ Left: " + expr.Left.String() + "\n")
	sb.WriteString(prefix + "   ├─ Op: " + expr.Op.String() + "\n")
	sb.WriteString(prefix + "   └─ Right\n")
	sb.WriteString(formatASTTree(expr.Right, prefix+"      "))
	return sb.String()
}

func formatBinaryExpr(expr *aql.BinaryExpr, prefix string) string {
	var sb strings.Builder
	sb.WriteString(prefix + "└─ BinaryExpr (" + expr.Op.String() + ")\n")
	sb.WriteString(prefix + "   ├─ Left\n")
	sb.WriteString(formatASTTree(expr.Left, prefix+"   │  "))
	sb.WriteString(prefix + "   └─ Right\n")
	sb.WriteString(formatASTTree(expr.Right, prefix+"      "))
	return sb.String()
}

func formatUnaryExpr(expr *aql.UnaryExpr, prefix string) string {
	var sb strings.Builder
	sb.WriteString(prefix + "└─ UnaryExpr (" + expr.Op.String() + ")\n")
	sb.WriteString(formatASTTree(expr.Operand, prefix+"   "))
	return sb.String()
}

func formatInExpr(expr *aql.InExpr, prefix string) string {
	var sb strings.Builder
	sb.WriteString(prefix + "└─ InExpr\n")
	sb.WriteString(prefix + "   ├─ Left: " + expr.Left.String() + "\n")
	sb.WriteString(prefix + "   └─ Values\n")
	for _, val := range expr.Values {
		sb.WriteString(formatASTTree(val, prefix+"      "))
	}
	return sb.String()
}

func formatBetweenExpr(expr *aql.BetweenExpr, prefix string) string {
	var sb strings.Builder
	sb.WriteString(prefix + "└─ BetweenExpr\n")
	sb.WriteString(prefix + "   ├─ Left: " + expr.Left.String() + "\n")
	sb.WriteString(prefix + "   ├─ Low\n")
	sb.WriteString(formatASTTree(expr.Low, prefix+"   │  "))
	sb.WriteString(prefix + "   └─ High\n")
	sb.WriteString(formatASTTree(expr.High, prefix+"      "))
	return sb.String()
}

func formatLabelExpr(expr *aql.LabelExpr, prefix string) string {
	var sb strings.Builder
	sb.WriteString(prefix + "└─ LabelExpr (" + expr.Op.String() + ")\n")
	sb.WriteString(prefix + "   ├─ Selector: " + expr.Selector.String() + "\n")
	sb.WriteString(prefix + "   └─ Labels\n")
	for _, label := range expr.Labels {
		sb.WriteString(formatASTTree(label, prefix+"      "))
	}
	return sb.String()
}

func formatIsNullExpr(expr *aql.IsNullExpr, prefix string) string {
	var sb strings.Builder
	sb.WriteString(prefix + "└─ IsNullExpr")
	if expr.Not {
		sb.WriteString(" (NOT)")
	}
	sb.WriteString("\n")
	sb.WriteString(prefix + "   └─ Selector: " + expr.Selector.String() + "\n")
	return sb.String()
}

func formatExistsExpr(expr *aql.ExistsExpr, prefix string) string {
	var sb strings.Builder
	sb.WriteString(prefix + "└─ ExistsExpr")
	if expr.Not {
		sb.WriteString(" (NOT)")
	}
	sb.WriteString("\n")
	sb.WriteString(formatPattern(expr.Pattern, prefix+"   "))
	return sb.String()
}

func formatOrderSpec(spec *aql.OrderSpec, prefix string) string {
	var sb strings.Builder
	sb.WriteString(prefix + "└─ OrderSpec")
	if spec.Descending {
		sb.WriteString(" (DESC)")
	} else {
		sb.WriteString(" (ASC)")
	}
	sb.WriteString("\n")
	sb.WriteString(formatASTTree(spec.Expr, prefix+"   "))
	return sb.String()
}
