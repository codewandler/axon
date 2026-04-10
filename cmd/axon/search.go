package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/codewandler/axon/aql"
	"github.com/codewandler/axon/graph"
	"github.com/codewandler/axon/indexer/embeddings"
	"github.com/spf13/cobra"
)

var (
	flagSearchSemantic bool
	flagSearchType     string
	flagSearchLimit    int
)

var searchCmd = &cobra.Command{
	Use:   "search <question>",
	Short: "Search the codebase using natural language",
	Long: `Search the indexed codebase using natural language.

This command interprets your question and queries the graph to provide
concise, structured answers optimized for AI agent consumption.

Supported question types:

  WHAT IS / DESCRIBE:
    axon search "what is Storage"
    axon search "describe the Event struct"

  FIND REFERENCES:
    axon search "who calls NewNode"
    axon search "references to EmitNode"
    axon search "find usages of Storage"

  IMPACT ANALYSIS:
    axon search "what uses Storage"
    axon search "impact of changing NewNode"
    axon search "what depends on graph package"

  LIST / FIND:
    axon search "list interfaces"          # exported only
    axon search "list all functions"       # include unexported
    axon search "find functions that return error"
    axon search "show Event types"

  METHODS:
    axon search "what methods does Storage have"
    axon search "methods of Node"

  COMPARE:
    axon search "compare Storage and Graph"
    axon search "diff between Node and Edge"

  WHERE DEFINED:
    axon search "where is NewNode defined"
    axon search "definition of Storage"

  IMPLEMENTATIONS:
    axon search "what implements Storage"
    axon search "implementations of Indexer"

  EXPLAIN:
    axon search "explain the indexer system"
    axon search "how does event routing work"

Examples:
  axon search "what is the Indexer interface"
  axon search "who calls EmitNode"
  axon search "list structs"
  axon search "methods of Storage"
  axon search "compare Node and Edge"`,
	Args: cobra.MinimumNArgs(1),
	RunE: runSearch,
}

func init() {
	rootCmd.AddCommand(searchCmd)
	searchCmd.Flags().BoolVar(&flagSearchSemantic, "semantic", false, "Use vector similarity search (requires embeddings generated with axon init --embed)")
	searchCmd.Flags().StringVar(&flagSearchType, "type", "", "Filter results by node type (e.g. go:func, go:struct)")
	searchCmd.Flags().IntVar(&flagSearchLimit, "limit", 10, "Maximum number of results")
}

// questionType represents the type of question being asked
type questionType int

const (
	questionUnknown    questionType = iota
	questionWhatIs                  // "what is X", "describe X"
	questionReferences              // "who calls X", "references to X", "usages of X"
	questionImpact                  // "what uses X", "impact of X", "depends on X"
	questionList                    // "list all X", "find X", "show X"
	questionExplain                 // "explain X", "how does X work"
	questionImplements              // "what implements X", "implementations of X"
	questionDefinition              // "where is X defined", "definition of X"
	questionMethods                 // "what methods does X have", "methods of X"
	questionCompare                 // "compare X and Y", "diff between X and Y"
)

// parsedQuestion holds the parsed question components
type parsedQuestion struct {
	Type    questionType
	Subject string   // The main subject (e.g., "Storage", "NewNode")
	Filters []string // Additional filters
}

func runSearch(cmd *cobra.Command, args []string) error {
	question := strings.Join(args, " ")

	// Semantic vector search mode
	if flagSearchSemantic {
		return runSemanticSearch(question)
	}

	cmdCtx, err := openDB(false)
	if err != nil {
		return err
	}
	defer cmdCtx.Close()

	ax, err := cmdCtx.Axon()
	if err != nil {
		return err
	}

	storage := ax.Graph().Storage()
	ctx := context.Background()

	// Parse the question
	parsed := parseQuestion(question)

	// Execute based on question type
	switch parsed.Type {
	case questionWhatIs:
		return answerWhatIs(ctx, storage, parsed)
	case questionReferences:
		return answerReferences(ctx, storage, parsed)
	case questionImpact:
		return answerImpact(ctx, storage, parsed)
	case questionList:
		return answerList(ctx, storage, parsed)
	case questionExplain:
		return answerExplain(ctx, storage, parsed)
	case questionImplements:
		return answerImplements(ctx, storage, parsed)
	case questionDefinition:
		return answerDefinition(ctx, storage, parsed)
	case questionMethods:
		return answerMethods(ctx, storage, parsed)
	case questionCompare:
		return answerCompare(ctx, storage, parsed)
	default:
		// Try to be helpful
		return answerFuzzy(ctx, storage, parsed, question)
	}
}

func runSemanticSearch(query string) error {
	cmdCtx, err := openDB(false)
	if err != nil {
		return err
	}
	defer cmdCtx.Close()

	ctx := context.Background()

	// Resolve embedding provider from environment
	provider, err := resolveEmbeddingProvider()
	if err != nil {
		return err
	}

	fmt.Fprintf(os.Stderr, "Using embedding provider: %s\n", provider.Name())

	// Embed the query
	embedding, err := provider.Embed(ctx, query)
	if err != nil {
		return fmt.Errorf("embedding query: %w", err)
	}

	// Build optional node filter
	var filter *graph.NodeFilter
	if flagSearchType != "" {
		filter = &graph.NodeFilter{Type: flagSearchType}
	}

	// Find similar nodes
	results, err := cmdCtx.Storage.FindSimilar(ctx, embedding, flagSearchLimit, filter)
	if err != nil {
		return fmt.Errorf("similarity search: %w", err)
	}

	if len(results) == 0 {
		fmt.Println("No results found.")
		fmt.Println("Tip: Run 'axon init --embed .' to generate embeddings first.")
		return nil
	}

	fmt.Printf("## Semantic search: %q\n\n", query)
	fmt.Printf("Found **%d** results:\n\n", len(results))

	for _, r := range results {
		pos := extractPositionInfo(r.Data)
		posStr := ""
		if pos.File != "" {
			posStr = fmt.Sprintf(" — `%s:%d`", shortenPath(pos.File), pos.Line)
		}
		fmt.Printf("**%.3f** `%s` (%s)%s\n", r.Score, r.Name, r.Type, posStr)
		doc := extractDoc(r.Data)
		if doc != "" {
			// First line only
			if idx := strings.Index(doc, "\n"); idx != -1 {
				doc = doc[:idx]
			}
			if len(doc) > 100 {
				doc = doc[:100] + "..."
			}
			fmt.Printf("  > %s\n", doc)
		}
	}

	return nil
}

func resolveEmbeddingProvider() (embeddings.Provider, error) {
	providerName := os.Getenv("AXON_EMBED_PROVIDER")
	if providerName == "" {
		providerName = "ollama"
	}

	switch providerName {
	case "ollama":
		baseURL := os.Getenv("AXON_OLLAMA_URL")
		model := os.Getenv("AXON_OLLAMA_MODEL")
		return embeddings.NewOllama(baseURL, model), nil
	default:
		return nil, fmt.Errorf("unknown embedding provider %q (set AXON_EMBED_PROVIDER=ollama)", providerName)
	}
}

// parseQuestion extracts the question type and subject
func parseQuestion(q string) parsedQuestion {
	original := strings.TrimSpace(q)
	_ = strings.ToLower(original) // for potential future use

	// Pattern matchers - use (?i) for case-insensitive matching on keywords
	// but extract subject from original to preserve case
	patterns := []struct {
		re   *regexp.Regexp
		typ  questionType
		subj int // capture group for subject
	}{
		// What implements (before "what is" to catch it first)
		{regexp.MustCompile(`(?i)(?:what implements|implementations? of|types? implementing|structs? implementing)\s+(.+)`), questionImplements, 1},

		// What methods does X have (before "what is" to catch it first)
		{regexp.MustCompile(`(?i)(?:what methods does|methods (?:of|on|for)|what methods are on|list methods (?:of|on|for))\s+(.+?)(?:\s+have)?$`), questionMethods, 1},

		// Compare X and Y
		{regexp.MustCompile(`(?i)(?:compare|diff(?:erence)? between|what's the difference between|how (?:is|are) .+ different from)\s+(.+)`), questionCompare, 1},

		// What is / Describe
		{regexp.MustCompile(`(?i)(?:what is|what's|describe|tell me about|info on)\s+(?:the\s+)?(.+)`), questionWhatIs, 1},

		// Where is defined
		{regexp.MustCompile(`(?i)(?:where is|definition of|find definition|go to definition)\s+(.+?)(?:\s+defined)?$`), questionDefinition, 1},

		// References / Who calls
		{regexp.MustCompile(`(?i)(?:who calls|who uses|find refs?(?:erences)? (?:to|for)|references? (?:to|of)|usages? of|callers? of)\s+(.+)`), questionReferences, 1},

		// Impact / What uses
		{regexp.MustCompile(`(?i)(?:what uses|what calls|impact of(?: changing)?|what depends on|dependents? of)\s+(.+)`), questionImpact, 1},

		// List / Find
		{regexp.MustCompile(`(?i)(?:list|show|find|get)(?: all)?\s+(.+)`), questionList, 1},

		// Explain / How
		{regexp.MustCompile(`(?i)(?:explain|how does|how do|how is|tell me how)\s+(.+?)(?:\s+work)?$`), questionExplain, 1},
	}

	for _, p := range patterns {
		if matches := p.re.FindStringSubmatch(original); matches != nil {
			subject := strings.TrimSpace(matches[p.subj])
			// Clean up common suffixes
			subject = strings.TrimSuffix(subject, "?")
			subject = strings.TrimSuffix(subject, " work")
			subject = strings.TrimSuffix(subject, " works")
			return parsedQuestion{
				Type:    p.typ,
				Subject: subject,
			}
		}
	}

	// Default: treat as fuzzy search on the whole question
	return parsedQuestion{
		Type:    questionUnknown,
		Subject: q,
	}
}

// answerWhatIs answers "what is X" questions
// Focus on DEFINITIONS only - no references, no noise
func answerWhatIs(ctx context.Context, storage graph.Storage, q parsedQuestion) error {
	subject := q.Subject

	// Search for DEFINITIONS only (exclude go:ref, exclude fs:* noise)
	// Prioritize exact matches, then prefix matches
	query, _ := aql.Parse(fmt.Sprintf(
		`SELECT * FROM nodes WHERE 
		 (name = '%s' OR name LIKE '%s%%') 
		 AND type LIKE 'go:%%' 
		 AND type != 'go:ref'
		 AND type != 'go:field'
		 ORDER BY type LIMIT 10`,
		escapeSQL(subject), escapeSQL(subject)))

	result, err := storage.Query(ctx, query)
	if err != nil {
		return err
	}

	if len(result.Nodes) == 0 {
		// Fallback: maybe it's a file or directory
		query, _ = aql.Parse(fmt.Sprintf(
			`SELECT * FROM nodes WHERE name = '%s' AND type IN ('fs:file', 'fs:dir', 'go:package') LIMIT 5`,
			escapeSQL(subject)))
		result, err = storage.Query(ctx, query)
		if err != nil {
			return err
		}
	}

	if len(result.Nodes) == 0 {
		fmt.Printf("No definition found for '%s'\n", subject)
		fmt.Println("\nTry:")
		fmt.Printf("  axon search \"references to %s\"\n", subject)
		fmt.Printf("  axon search \"list %s\"\n", subject)
		return nil
	}

	fmt.Printf("## %s\n\n", subject)

	// Group by type, prioritize important types
	byType := make(map[string][]*graph.Node)
	for _, n := range result.Nodes {
		byType[n.Type] = append(byType[n.Type], n)
	}

	// Print in priority order
	typeOrder := []string{"go:interface", "go:struct", "go:func", "go:method", "go:const", "go:var", "go:package", "fs:file", "fs:dir"}
	for _, nodeType := range typeOrder {
		if nodes, ok := byType[nodeType]; ok {
			typeName := strings.TrimPrefix(nodeType, "go:")
			typeName = strings.TrimPrefix(typeName, "fs:")
			fmt.Printf("**%s**\n\n", typeName)
			for _, n := range nodes {
				printNodeDetail(n)
			}
			fmt.Println()
			delete(byType, nodeType)
		}
	}

	// Print any remaining types
	for nodeType, nodes := range byType {
		fmt.Printf("**%s**\n\n", nodeType)
		for _, n := range nodes {
			printNodeDetail(n)
		}
		fmt.Println()
	}

	return nil
}

// printNodeDetail prints detailed info about a single node (for "what is")
func printNodeDetail(n *graph.Node) {
	pos := extractPositionInfo(n.Data)
	doc := extractDoc(n.Data)
	sig := extractSignature(n.Data)
	receiver := extractReceiver(n.Data)

	// Name with receiver for methods
	name := n.Name
	if receiver != "" {
		name = fmt.Sprintf("(%s) %s", receiver, n.Name)
	}

	fmt.Printf("`%s`\n", name)

	if sig != "" {
		fmt.Printf("```go\nfunc %s\n```\n", sig)
	}

	if doc != "" {
		fmt.Printf("> %s\n", doc)
	}

	if pos.File != "" {
		fmt.Printf("\nDefined in `%s:%d`\n", shortenPath(pos.File), pos.Line)
	}
}

// answerDefinition answers "where is X defined" - quick jump to definition
func answerDefinition(ctx context.Context, storage graph.Storage, q parsedQuestion) error {
	subject := cleanSubject(q.Subject)

	// Find the definition (not refs)
	query, _ := aql.Parse(fmt.Sprintf(
		`SELECT * FROM nodes WHERE 
		 name = '%s' 
		 AND type LIKE 'go:%%' 
		 AND type != 'go:ref'
		 ORDER BY type LIMIT 5`,
		escapeSQL(subject)))

	result, err := storage.Query(ctx, query)
	if err != nil {
		return err
	}

	if len(result.Nodes) == 0 {
		fmt.Printf("No definition found for '%s'\n", subject)
		return nil
	}

	// Just print locations, super concise
	for _, n := range result.Nodes {
		pos := extractPositionInfo(n.Data)
		if pos.File != "" {
			fmt.Printf("%s:%d  (%s)\n", shortenPath(pos.File), pos.Line, n.Type)
		}
	}

	return nil
}

// answerMethods answers "what methods does X have" - list methods of a type
func answerMethods(ctx context.Context, storage graph.Storage, q parsedQuestion) error {
	subject := cleanSubject(q.Subject)

	// First find the type (struct or interface)
	typeQuery, _ := aql.Parse(fmt.Sprintf(
		`SELECT * FROM nodes WHERE name = '%s' AND type IN ('go:struct', 'go:interface') LIMIT 1`,
		escapeSQL(subject)))

	typeResult, err := storage.Query(ctx, typeQuery)
	if err != nil {
		return err
	}

	if len(typeResult.Nodes) == 0 {
		fmt.Printf("No type found named '%s'\n", subject)
		return nil
	}

	targetType := typeResult.Nodes[0]
	fmt.Printf("## Methods of `%s` (%s)\n\n", subject, targetType.Type)

	// Find methods - for structs via receiver, for interfaces via URI hierarchy
	var methodQuery *aql.Query
	if targetType.Type == "go:interface" {
		// Interface methods are children under the interface URI
		methodQuery, _ = aql.Parse(fmt.Sprintf(
			`SELECT * FROM nodes WHERE type = 'go:method' AND uri LIKE '%s/method/%%' ORDER BY name`,
			escapeSQL(targetType.URI)))
	} else {
		// Struct methods via receiver field
		methodQuery, _ = aql.Parse(fmt.Sprintf(
			`SELECT * FROM nodes WHERE type = 'go:method' AND data.receiver = '%s' ORDER BY name`,
			escapeSQL(subject)))
	}

	methodResult, err := storage.Query(ctx, methodQuery)
	if err != nil {
		return err
	}

	if len(methodResult.Nodes) == 0 {
		fmt.Println("No methods found.")
		return nil
	}

	fmt.Printf("Found **%d** methods:\n\n", len(methodResult.Nodes))

	for _, m := range methodResult.Nodes {
		sig := extractSignature(m.Data)
		doc := extractDoc(m.Data)
		pos := extractPositionInfo(m.Data)

		if sig != "" {
			fmt.Printf("- `%s` - `%s`\n", m.Name, sig)
		} else {
			fmt.Printf("- `%s`\n", m.Name)
		}
		if doc != "" {
			// Truncate doc to first line
			if idx := strings.Index(doc, "\n"); idx != -1 {
				doc = doc[:idx]
			}
			if len(doc) > 80 {
				doc = doc[:80] + "..."
			}
			fmt.Printf("    %s\n", doc)
		}
		if pos.File != "" {
			fmt.Printf("    → %s:%d\n", shortenPath(pos.File), pos.Line)
		}
	}

	return nil
}

// answerCompare answers "compare X and Y" - compare two symbols
func answerCompare(ctx context.Context, storage graph.Storage, q parsedQuestion) error {
	// Parse "X and Y" or "X vs Y" or "X with Y"
	subject := q.Subject
	var symbolA, symbolB string

	// Try different separators
	for _, sep := range []string{" and ", " vs ", " with ", " to "} {
		if idx := strings.Index(strings.ToLower(subject), sep); idx != -1 {
			symbolA = strings.TrimSpace(subject[:idx])
			symbolB = strings.TrimSpace(subject[idx+len(sep):])
			break
		}
	}

	if symbolA == "" || symbolB == "" {
		fmt.Printf("Could not parse comparison. Use: \"compare X and Y\"\n")
		return nil
	}

	symbolA = cleanSubject(symbolA)
	symbolB = cleanSubject(symbolB)

	fmt.Printf("## Comparing `%s` vs `%s`\n\n", symbolA, symbolB)

	// Find both symbols
	queryA, _ := aql.Parse(fmt.Sprintf(
		`SELECT * FROM nodes WHERE name = '%s' AND type LIKE 'go:%%' AND type != 'go:ref' LIMIT 1`,
		escapeSQL(symbolA)))
	queryB, _ := aql.Parse(fmt.Sprintf(
		`SELECT * FROM nodes WHERE name = '%s' AND type LIKE 'go:%%' AND type != 'go:ref' LIMIT 1`,
		escapeSQL(symbolB)))

	resultA, errA := storage.Query(ctx, queryA)
	resultB, errB := storage.Query(ctx, queryB)

	if errA != nil {
		return errA
	}
	if errB != nil {
		return errB
	}

	if len(resultA.Nodes) == 0 {
		fmt.Printf("Symbol '%s' not found\n", symbolA)
		return nil
	}
	if len(resultB.Nodes) == 0 {
		fmt.Printf("Symbol '%s' not found\n", symbolB)
		return nil
	}

	nodeA := resultA.Nodes[0]
	nodeB := resultB.Nodes[0]

	// Compare types
	fmt.Printf("### Type\n\n")
	fmt.Printf("| Symbol | Type |\n")
	fmt.Printf("|--------|------|\n")
	fmt.Printf("| %s | %s |\n", symbolA, nodeA.Type)
	fmt.Printf("| %s | %s |\n", symbolB, nodeB.Type)
	fmt.Println()

	// Compare signatures if both are functions/methods
	sigA := extractSignature(nodeA.Data)
	sigB := extractSignature(nodeB.Data)
	if sigA != "" || sigB != "" {
		fmt.Printf("### Signature\n\n")
		fmt.Printf("| Symbol | Signature |\n")
		fmt.Printf("|--------|----------|\n")
		fmt.Printf("| %s | `%s` |\n", symbolA, sigA)
		fmt.Printf("| %s | `%s` |\n", symbolB, sigB)
		fmt.Println()
	}

	// Compare locations
	posA := extractPositionInfo(nodeA.Data)
	posB := extractPositionInfo(nodeB.Data)
	fmt.Printf("### Location\n\n")
	fmt.Printf("| Symbol | File | Line |\n")
	fmt.Printf("|--------|------|------|\n")
	fmt.Printf("| %s | %s | %d |\n", symbolA, shortenPath(posA.File), posA.Line)
	fmt.Printf("| %s | %s | %d |\n", symbolB, shortenPath(posB.File), posB.Line)
	fmt.Println()

	// If both are structs or interfaces, compare methods
	if (nodeA.Type == "go:struct" || nodeA.Type == "go:interface") &&
		(nodeB.Type == "go:struct" || nodeB.Type == "go:interface") {
		fmt.Printf("### Methods\n\n")

		// Get methods for A
		methodsA := getMethodsForType(ctx, storage, nodeA)
		methodsB := getMethodsForType(ctx, storage, nodeB)

		methodSetA := make(map[string]bool)
		methodSetB := make(map[string]bool)
		for _, m := range methodsA {
			methodSetA[m] = true
		}
		for _, m := range methodsB {
			methodSetB[m] = true
		}

		// Find common and unique
		var common, onlyA, onlyB []string
		for m := range methodSetA {
			if methodSetB[m] {
				common = append(common, m)
			} else {
				onlyA = append(onlyA, m)
			}
		}
		for m := range methodSetB {
			if !methodSetA[m] {
				onlyB = append(onlyB, m)
			}
		}

		if len(common) > 0 {
			fmt.Printf("**Common methods** (%d): %s\n\n", len(common), strings.Join(common, ", "))
		}
		if len(onlyA) > 0 {
			fmt.Printf("**Only in %s** (%d): %s\n\n", symbolA, len(onlyA), strings.Join(onlyA, ", "))
		}
		if len(onlyB) > 0 {
			fmt.Printf("**Only in %s** (%d): %s\n\n", symbolB, len(onlyB), strings.Join(onlyB, ", "))
		}
	}

	// Count references
	refQueryA, _ := aql.Parse(fmt.Sprintf(
		`SELECT * FROM nodes WHERE type = 'go:ref' AND name = '%s'`, escapeSQL(symbolA)))
	refQueryB, _ := aql.Parse(fmt.Sprintf(
		`SELECT * FROM nodes WHERE type = 'go:ref' AND name = '%s'`, escapeSQL(symbolB)))

	refResultA, _ := storage.Query(ctx, refQueryA)
	refResultB, _ := storage.Query(ctx, refQueryB)

	fmt.Printf("### Usage\n\n")
	fmt.Printf("| Symbol | References |\n")
	fmt.Printf("|--------|------------|\n")
	fmt.Printf("| %s | %d |\n", symbolA, len(refResultA.Nodes))
	fmt.Printf("| %s | %d |\n", symbolB, len(refResultB.Nodes))

	return nil
}

// getMethodsForType returns method names for a struct or interface
func getMethodsForType(ctx context.Context, storage graph.Storage, node *graph.Node) []string {
	var query *aql.Query
	if node.Type == "go:interface" {
		query, _ = aql.Parse(fmt.Sprintf(
			`SELECT name FROM nodes WHERE type = 'go:method' AND uri LIKE '%s/method/%%'`,
			escapeSQL(node.URI)))
	} else {
		query, _ = aql.Parse(fmt.Sprintf(
			`SELECT name FROM nodes WHERE type = 'go:method' AND data.receiver = '%s'`,
			escapeSQL(node.Name)))
	}

	result, err := storage.Query(ctx, query)
	if err != nil {
		return nil
	}

	methods := make([]string, 0, len(result.Nodes))
	for _, n := range result.Nodes {
		methods = append(methods, n.Name)
	}
	return methods
}

// answerImplements answers "what implements X" - find interface implementations
func answerImplements(ctx context.Context, storage graph.Storage, q parsedQuestion) error {
	subject := cleanSubject(q.Subject)

	// First, verify it's an interface
	ifaceQuery, _ := aql.Parse(fmt.Sprintf(
		`SELECT * FROM nodes WHERE name = '%s' AND type = 'go:interface' LIMIT 1`,
		escapeSQL(subject)))

	ifaceResult, err := storage.Query(ctx, ifaceQuery)
	if err != nil {
		return err
	}

	if len(ifaceResult.Nodes) == 0 {
		fmt.Printf("'%s' is not an interface\n", subject)
		return nil
	}

	fmt.Printf("## Implementations of `%s`\n\n", subject)

	// Strategy: Find structs that have methods matching the interface methods
	// For now, simple heuristic: find structs with methods named like interface methods
	// TODO: proper type checking would require go/types analysis

	// Get interface methods
	iface := ifaceResult.Nodes[0]
	ifaceURI := iface.URI

	// Find methods belonging to this interface
	methodQuery, _ := aql.Parse(fmt.Sprintf(
		`SELECT * FROM nodes WHERE type = 'go:method' AND uri LIKE '%s/method/%%'`,
		escapeSQL(ifaceURI)))

	methodResult, err := storage.Query(ctx, methodQuery)
	if err != nil {
		return err
	}

	if len(methodResult.Nodes) == 0 {
		fmt.Println("Interface has no methods (empty interface)")
		return nil
	}

	// Collect method names
	methodNames := make([]string, 0, len(methodResult.Nodes))
	for _, m := range methodResult.Nodes {
		methodNames = append(methodNames, m.Name)
	}

	fmt.Printf("Interface requires: %s\n\n", strings.Join(methodNames, ", "))

	// Find structs that have ALL these methods
	// This is a heuristic - real implementation would use go/types
	if len(methodNames) > 0 {
		// Find structs with the first method, then check others
		structQuery, _ := aql.Parse(`SELECT DISTINCT * FROM nodes WHERE type = 'go:struct' ORDER BY name LIMIT 50`)

		structResult, err := storage.Query(ctx, structQuery)
		if err != nil {
			return err
		}

		fmt.Printf("### Potential implementations\n\n")
		fmt.Println("*(Based on method name matching - not type-checked)*")
		fmt.Println()

		found := 0
		for _, s := range structResult.Nodes {
			// Check if this struct has methods matching interface
			structMethodQuery, _ := aql.Parse(fmt.Sprintf(
				`SELECT name FROM nodes WHERE type = 'go:method' AND data.receiver = '%s'`,
				escapeSQL(s.Name)))

			structMethodResult, err := storage.Query(ctx, structMethodQuery)
			if err != nil {
				continue
			}

			structMethods := make(map[string]bool)
			for _, m := range structMethodResult.Nodes {
				structMethods[m.Name] = true
			}

			// Check if all interface methods are present
			hasAll := true
			for _, required := range methodNames {
				if !structMethods[required] {
					hasAll = false
					break
				}
			}

			if hasAll {
				pos := extractPositionInfo(s.Data)
				fmt.Printf("- `%s` (%s:%d)\n", s.Name, shortenPath(pos.File), pos.Line)
				found++
			}
		}

		if found == 0 {
			fmt.Println("No implementations found matching all methods")
		}
	}

	return nil
}

// answerReferences answers "who calls X" / "references to X" questions
func answerReferences(ctx context.Context, storage graph.Storage, q parsedQuestion) error {
	subject := cleanSubject(q.Subject)

	// Find all references to this symbol (case-sensitive match on name)
	// First try exact match, then try case-insensitive
	query, _ := aql.Parse(fmt.Sprintf(
		`SELECT * FROM nodes WHERE type = 'go:ref' AND (name = '%s' OR name LIKE '%s')`,
		escapeSQL(subject), escapeSQL(subject)))

	result, err := storage.Query(ctx, query)
	if err != nil {
		return err
	}

	if len(result.Nodes) == 0 {
		fmt.Printf("No references found to '%s'\n", subject)
		return nil
	}

	fmt.Printf("## References to `%s`\n\n", subject)
	fmt.Printf("Found **%d** references:\n\n", len(result.Nodes))

	// Group by file
	byFile := make(map[string][]*graph.Node)
	for _, n := range result.Nodes {
		pos := extractPositionInfo(n.Data)
		if pos.File != "" {
			byFile[pos.File] = append(byFile[pos.File], n)
		}
	}

	for file, nodes := range byFile {
		// Shorten file path
		shortFile := shortenPath(file)
		fmt.Printf("**%s** (%d)\n", shortFile, len(nodes))
		for _, n := range nodes {
			pos := extractPositionInfo(n.Data)
			kind := extractKind(n.Data)
			fmt.Printf("  - Line %d: %s\n", pos.Line, kind)
		}
		fmt.Println()
	}

	return nil
}

// answerImpact answers "what uses X" / "impact of X" questions
func answerImpact(ctx context.Context, storage graph.Storage, q parsedQuestion) error {
	subject := cleanSubject(q.Subject)

	// First find the definition
	defQuery, _ := aql.Parse(fmt.Sprintf(
		`SELECT * FROM nodes WHERE type LIKE 'go:%%' AND type != 'go:ref' AND name = '%s' LIMIT 5`,
		escapeSQL(subject)))

	defResult, err := storage.Query(ctx, defQuery)
	if err != nil {
		return err
	}

	if len(defResult.Nodes) == 0 {
		fmt.Printf("No definition found for '%s'\n", subject)
		return nil
	}

	fmt.Printf("## Impact Analysis: `%s`\n\n", subject)

	// Show definition(s)
	fmt.Printf("### Definition\n\n")
	for _, n := range defResult.Nodes {
		printNodeSummary(n)
	}
	fmt.Println()

	// Find references
	refQuery, _ := aql.Parse(fmt.Sprintf(
		`SELECT * FROM nodes WHERE type = 'go:ref' AND name = '%s'`,
		escapeSQL(subject)))

	refResult, err := storage.Query(ctx, refQuery)
	if err != nil {
		return err
	}

	fmt.Printf("### Impact\n\n")
	fmt.Printf("- **%d** direct references\n", len(refResult.Nodes))

	// Count by kind
	kindCounts := make(map[string]int)
	pkgCounts := make(map[string]int)
	for _, n := range refResult.Nodes {
		kind := extractKind(n.Data)
		kindCounts[kind]++

		pos := extractPositionInfo(n.Data)
		if pos.File != "" {
			pkg := extractPackageFromPath(pos.File)
			pkgCounts[pkg]++
		}
	}

	if len(kindCounts) > 0 {
		fmt.Printf("- By type: ")
		parts := []string{}
		for k, v := range kindCounts {
			parts = append(parts, fmt.Sprintf("%d %s", v, k))
		}
		fmt.Println(strings.Join(parts, ", "))
	}

	if len(pkgCounts) > 0 {
		fmt.Printf("- **%d** packages affected\n", len(pkgCounts))
		fmt.Printf("- Packages: ")
		pkgs := []string{}
		for pkg := range pkgCounts {
			pkgs = append(pkgs, pkg)
		}
		fmt.Println(strings.Join(pkgs, ", "))
	}

	return nil
}

// answerList answers "list all X" / "find X" questions
func answerList(ctx context.Context, storage graph.Storage, q parsedQuestion) error {
	subject := strings.ToLower(q.Subject)

	// Check if user wants unexported symbols too
	includeUnexported := strings.Contains(subject, "all ") ||
		strings.Contains(subject, "unexported") ||
		strings.Contains(subject, "private")

	var query *aql.Query
	var title string

	switch {
	case strings.Contains(subject, "interface"):
		query, _ = aql.Parse(`SELECT name, data FROM nodes WHERE type = 'go:interface' ORDER BY name`)
		title = "Interfaces"
	case strings.Contains(subject, "struct"):
		query, _ = aql.Parse(`SELECT name, data FROM nodes WHERE type = 'go:struct' ORDER BY name`)
		title = "Structs"
	case strings.Contains(subject, "func") || strings.Contains(subject, "function"):
		query, _ = aql.Parse(`SELECT name, data FROM nodes WHERE type = 'go:func' ORDER BY name LIMIT 100`)
		title = "Functions"
	case strings.Contains(subject, "method"):
		query, _ = aql.Parse(`SELECT name, data FROM nodes WHERE type = 'go:method' ORDER BY name LIMIT 100`)
		title = "Methods"
	case strings.Contains(subject, "package"):
		query, _ = aql.Parse(`SELECT name, data FROM nodes WHERE type = 'go:package' ORDER BY name`)
		title = "Packages"
	case strings.Contains(subject, "const"):
		query, _ = aql.Parse(`SELECT name, data FROM nodes WHERE type = 'go:const' ORDER BY name LIMIT 100`)
		title = "Constants"
	case strings.Contains(subject, "var"):
		query, _ = aql.Parse(`SELECT name, data FROM nodes WHERE type = 'go:var' ORDER BY name LIMIT 100`)
		title = "Variables"
	case strings.Contains(subject, "event"):
		query, _ = aql.Parse(`SELECT name, type, data FROM nodes WHERE name LIKE '%Event%' AND type LIKE 'go:%' AND type != 'go:ref' ORDER BY type, name`)
		title = "Event-related symbols"
	case strings.Contains(subject, "error"):
		query, _ = aql.Parse(`SELECT name, data FROM nodes WHERE type = 'go:func' AND data.results LIKE '%error%' ORDER BY name LIMIT 50`)
		title = "Functions returning error"
	default:
		// Fuzzy search
		query, _ = aql.Parse(fmt.Sprintf(
			`SELECT name, type, data FROM nodes WHERE name LIKE '%%%s%%' AND type LIKE 'go:%%' AND type != 'go:ref' ORDER BY type, name LIMIT 50`,
			escapeSQL(subject)))
		title = fmt.Sprintf("Symbols matching '%s'", subject)
	}

	result, err := storage.Query(ctx, query)
	if err != nil {
		return err
	}

	// Filter to exported symbols only (unless user asked for all)
	var filtered []*graph.Node
	if includeUnexported {
		filtered = result.Nodes
	} else {
		for _, n := range result.Nodes {
			if len(n.Name) > 0 && isExported(n.Name) {
				filtered = append(filtered, n)
			}
		}
	}

	fmt.Printf("## %s\n\n", title)

	if len(filtered) == 0 {
		if !includeUnexported && len(result.Nodes) > 0 {
			fmt.Printf("No exported symbols found. Use \"list all %s\" to include unexported.\n", q.Subject)
		} else {
			fmt.Println("No results found.")
		}
		return nil
	}

	exportedNote := ""
	if !includeUnexported && len(result.Nodes) > len(filtered) {
		exportedNote = fmt.Sprintf(" (exported only, %d total)", len(result.Nodes))
	}
	fmt.Printf("Found **%d** results%s:\n\n", len(filtered), exportedNote)

	// Group by package for better readability
	byPkg := make(map[string][]*graph.Node)
	for _, n := range filtered {
		pkg := extractPackageFromPath(extractPositionInfo(n.Data).File)
		if pkg == "" {
			pkg = "(unknown)"
		}
		byPkg[pkg] = append(byPkg[pkg], n)
	}

	// If only one package, skip grouping
	if len(byPkg) == 1 {
		for _, n := range filtered {
			printListItem(n)
		}
	} else {
		// Print grouped by package
		for pkg, nodes := range byPkg {
			fmt.Printf("### %s\n\n", pkg)
			for _, n := range nodes {
				printListItem(n)
			}
			fmt.Println()
		}
	}

	return nil
}

// printListItem prints a single item in a list format
func printListItem(n *graph.Node) {
	sig := extractSignature(n.Data)
	receiver := extractReceiver(n.Data)
	pos := extractPositionInfo(n.Data)

	name := n.Name
	if receiver != "" {
		name = fmt.Sprintf("(%s) %s", receiver, n.Name)
	}

	if sig != "" {
		fmt.Printf("- `%s` `%s`\n", name, sig)
	} else {
		shortType := strings.TrimPrefix(n.Type, "go:")
		fmt.Printf("- `%s` (%s)\n", name, shortType)
	}

	if pos.File != "" {
		fmt.Printf("    %s:%d\n", shortenPath(pos.File), pos.Line)
	}
}

// isExported returns true if the name starts with an uppercase letter
func isExported(name string) bool {
	if len(name) == 0 {
		return false
	}
	r := rune(name[0])
	return r >= 'A' && r <= 'Z'
}

// answerExplain answers "explain X" / "how does X work" questions
func answerExplain(ctx context.Context, storage graph.Storage, q parsedQuestion) error {
	subject := strings.ToLower(q.Subject)

	fmt.Printf("## Explaining: %s\n\n", q.Subject)

	// Find related symbols
	query, _ := aql.Parse(fmt.Sprintf(
		`SELECT name, type, data FROM nodes WHERE (name LIKE '%%%s%%' OR uri LIKE '%%%s%%') AND type LIKE 'go:%%' AND type != 'go:ref' ORDER BY type, name LIMIT 30`,
		escapeSQL(subject), escapeSQL(subject)))

	result, err := storage.Query(ctx, query)
	if err != nil {
		return err
	}

	if len(result.Nodes) == 0 {
		fmt.Printf("No symbols found related to '%s'\n", subject)
		return nil
	}

	// Group by type
	byType := make(map[string][]*graph.Node)
	for _, n := range result.Nodes {
		byType[n.Type] = append(byType[n.Type], n)
	}

	// Print summary
	fmt.Printf("### Overview\n\n")
	fmt.Printf("Found **%d** related symbols:\n", len(result.Nodes))
	for t, nodes := range byType {
		fmt.Printf("- %d %s\n", len(nodes), t)
	}
	fmt.Println()

	// Print key types first
	printOrder := []string{"go:interface", "go:struct", "go:func", "go:method", "go:const"}
	for _, t := range printOrder {
		if nodes, ok := byType[t]; ok {
			fmt.Printf("### %s\n\n", strings.TrimPrefix(t, "go:"))
			for _, n := range nodes {
				printNodeSummary(n)
			}
			fmt.Println()
		}
	}

	return nil
}

// answerFuzzy handles unknown question types with fuzzy search
func answerFuzzy(ctx context.Context, storage graph.Storage, q parsedQuestion, original string) error {
	// Try to find anything matching the subject
	words := strings.Fields(q.Subject)
	if len(words) == 0 {
		fmt.Println("Please ask a question about the codebase.")
		return nil
	}

	// Use the last word as the likely subject
	subject := words[len(words)-1]
	subject = strings.Trim(subject, "?.,")

	fmt.Printf("## Search: %s\n\n", subject)

	query, _ := aql.Parse(fmt.Sprintf(
		`SELECT name, type, data FROM nodes WHERE name LIKE '%%%s%%' AND type LIKE 'go:%%' AND type != 'go:ref' ORDER BY type, name LIMIT 20`,
		escapeSQL(subject)))

	result, err := storage.Query(ctx, query)
	if err != nil {
		return err
	}

	if len(result.Nodes) == 0 {
		fmt.Printf("No results found for '%s'\n\n", subject)
		fmt.Println("Try:")
		fmt.Println("  axon search \"what is <symbol>\"")
		fmt.Println("  axon search \"who calls <function>\"")
		fmt.Println("  axon search \"list all interfaces\"")
		return nil
	}

	fmt.Printf("Found **%d** matches:\n\n", len(result.Nodes))
	for _, n := range result.Nodes {
		printNodeSummary(n)
	}

	return nil
}

// Helper functions

func printNodeSummary(n *graph.Node) {
	pos := extractPositionInfo(n.Data)
	doc := extractDoc(n.Data)
	sig := extractSignature(n.Data)

	fmt.Printf("**%s** `%s`\n", n.Type, n.Name)
	if sig != "" {
		fmt.Printf("  Signature: `%s`\n", sig)
	}
	if doc != "" {
		// Truncate doc
		if len(doc) > 100 {
			doc = doc[:100] + "..."
		}
		fmt.Printf("  Doc: %s\n", doc)
	}
	if pos.File != "" {
		fmt.Printf("  Location: %s:%d\n", shortenPath(pos.File), pos.Line)
	}
}

type positionInfo struct {
	File   string
	Line   int
	Column int
}

func extractPositionInfo(data any) positionInfo {
	if m, ok := data.(map[string]any); ok {
		if posData, ok := m["position"].(map[string]any); ok {
			p := positionInfo{}
			if f, ok := posData["file"].(string); ok {
				p.File = f
			}
			if l, ok := posData["line"].(float64); ok {
				p.Line = int(l)
			}
			if c, ok := posData["column"].(float64); ok {
				p.Column = int(c)
			}
			return p
		}
	}
	return positionInfo{}
}

func extractDoc(data any) string {
	if m, ok := data.(map[string]any); ok {
		if doc, ok := m["doc"].(string); ok {
			return strings.TrimSpace(doc)
		}
	}
	return ""
}

func extractSignature(data any) string {
	if m, ok := data.(map[string]any); ok {
		if sig, ok := m["signature"].(string); ok {
			return sig
		}
	}
	return ""
}

func extractKind(data any) string {
	if m, ok := data.(map[string]any); ok {
		if kind, ok := m["kind"].(string); ok {
			return kind
		}
	}
	return ""
}

func extractReceiver(data any) string {
	if m, ok := data.(map[string]any); ok {
		if recv, ok := m["receiver"].(string); ok {
			return recv
		}
	}
	return ""
}

func extractPackageFromPath(path string) string {
	// Extract package name from path like /home/.../axon/indexer/fs/indexer.go
	parts := strings.Split(path, "/")
	if len(parts) >= 2 {
		return parts[len(parts)-2]
	}
	return path
}

func shortenPath(path string) string {
	// Use CWD-relative path for accurate display in any project
	if cwd, err := os.Getwd(); err == nil {
		if rel, err2 := filepath.Rel(cwd, path); err2 == nil && !strings.HasPrefix(rel, "..") {
			return rel
		}
	}
	// Fallback: last 3 path components
	parts := strings.Split(path, string(filepath.Separator))
	if len(parts) > 3 {
		return strings.Join(parts[len(parts)-3:], string(filepath.Separator))
	}
	return path
}

func cleanSubject(s string) string {
	s = strings.TrimSpace(s)
	s = strings.Trim(s, "`'\"")
	// Remove common words
	s = strings.TrimPrefix(s, "the ")
	s = strings.TrimPrefix(s, "function ")
	s = strings.TrimPrefix(s, "method ")
	s = strings.TrimPrefix(s, "type ")
	s = strings.TrimPrefix(s, "struct ")
	s = strings.TrimPrefix(s, "interface ")
	return s
}

func escapeSQL(s string) string {
	return strings.ReplaceAll(s, "'", "''")
}
