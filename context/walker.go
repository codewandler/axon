package context

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/codewandler/axon/aql"
	"github.com/codewandler/axon/graph"
)

// Ring represents the distance from the seed symbols in the relevance graph.
type Ring int

const (
	RingDefinition Ring = 0 // The symbol itself (definitions)
	RingDirect     Ring = 1 // Direct children, implementations, containing package
	RingCallers    Ring = 2 // References/callers, test files
	RingSiblings   Ring = 3 // Same-package siblings, related types
	RingTransitive Ring = 4 // Callers of callers (one hop further)
)

// String returns the string representation of the ring.
func (r Ring) String() string {
	switch r {
	case RingDefinition:
		return "definition"
	case RingDirect:
		return "direct"
	case RingCallers:
		return "callers"
	case RingSiblings:
		return "siblings"
	case RingTransitive:
		return "transitive"
	default:
		return fmt.Sprintf("ring-%d", r)
	}
}

// RelevanceItem represents a node with relevance scoring.
type RelevanceItem struct {
	Node      *graph.Node
	Score     float64
	Ring      Ring
	File      string // Resolved file path
	StartLine int
	EndLine   int
	Reason    string // Human-readable reason, e.g., "definition of Storage"
}

// WalkOptions configures the graph walker.
type WalkOptions struct {
	MaxRing      Ring   // Maximum ring to expand to (default: RingSiblings)
	IncludeTests bool   // Include test files (default: true)
	ScopeNodeID  string // Optional: limit to descendants of this node
}

// DefaultWalkOptions returns the default walk options.
func DefaultWalkOptions() WalkOptions {
	return WalkOptions{
		MaxRing:      RingSiblings,
		IncludeTests: true,
	}
}

// Walk expands outward from task symbols to find relevant nodes.
func Walk(ctx context.Context, storage graph.Storage, task *ParsedTask, opts WalkOptions) ([]RelevanceItem, error) {
	if len(task.Symbols) == 0 {
		return nil, nil
	}

	var items []RelevanceItem
	seen := make(map[string]bool) // Track seen node IDs to avoid duplicates

	// Ring 0: Find definitions for each symbol
	ring0, err := findDefinitions(ctx, storage, task.Symbols)
	if err != nil {
		return nil, fmt.Errorf("finding definitions: %w", err)
	}

	for _, item := range ring0 {
		seen[item.Node.ID] = true
		items = append(items, item)
	}

	if opts.MaxRing < RingDirect {
		return items, nil
	}

	// Ring 1: Direct dependencies (children, implementations)
	ring1, err := expandRing1(ctx, storage, ring0, seen, task)
	if err != nil {
		return nil, fmt.Errorf("expanding ring 1: %w", err)
	}
	items = append(items, ring1...)

	if opts.MaxRing < RingCallers {
		return items, nil
	}

	// Ring 2: Callers and references
	ring2, err := expandRing2(ctx, storage, ring0, seen, task, opts.IncludeTests)
	if err != nil {
		return nil, fmt.Errorf("expanding ring 2: %w", err)
	}
	items = append(items, ring2...)

	if opts.MaxRing < RingSiblings {
		return items, nil
	}

	// Ring 3: Siblings (same package, related types)
	ring3, err := expandRing3(ctx, storage, ring0, seen, task)
	if err != nil {
		return nil, fmt.Errorf("expanding ring 3: %w", err)
	}
	items = append(items, ring3...)

	// Ring 4 is expensive and rarely needed; skip for now
	// Could be added for very generous budgets

	// Sort by score descending
	sort.Slice(items, func(i, j int) bool {
		return items[i].Score > items[j].Score
	})

	return items, nil
}

// findDefinitions finds definition nodes for the given symbols.
func findDefinitions(ctx context.Context, storage graph.Storage, symbols []string) ([]RelevanceItem, error) {
	var items []RelevanceItem

	for _, symbol := range symbols {
		// Query for Go definitions (not refs)
		queryStr := fmt.Sprintf(
			`SELECT * FROM nodes WHERE name = '%s' AND type LIKE 'go:%%' AND type != 'go:ref' LIMIT 10`,
			escapeSQL(symbol))

		query, err := aql.Parse(queryStr)
		if err != nil {
			continue // Skip invalid queries
		}

		result, err := storage.Query(ctx, query)
		if err != nil {
			continue
		}

		for _, node := range result.Nodes {
			pos := extractPosition(node.Data)
			items = append(items, RelevanceItem{
				Node:      node,
				Score:     100.0, // Ring 0 base score
				Ring:      RingDefinition,
				File:      pos.File,
				StartLine: pos.Line,
				EndLine:   pos.EndLine,
				Reason:    fmt.Sprintf("definition of %s", symbol),
			})
		}
	}

	return items, nil
}

// expandRing1 finds direct dependencies: methods, fields, implementations.
func expandRing1(ctx context.Context, storage graph.Storage, ring0 []RelevanceItem, seen map[string]bool, task *ParsedTask) ([]RelevanceItem, error) {
	var items []RelevanceItem

	for _, item := range ring0 {
		node := item.Node

		// Find children via "has" edges (methods, fields)
		edges, err := storage.GetEdgesFrom(ctx, node.ID)
		if err != nil {
			continue
		}

		for _, edge := range edges {
			if edge.Type != "has" && edge.Type != "defines" {
				continue
			}
			if seen[edge.To] {
				continue
			}

			child, err := storage.GetNode(ctx, edge.To)
			if err != nil || child == nil {
				continue
			}

			// Only include Go symbols
			if !strings.HasPrefix(child.Type, "go:") || child.Type == "go:ref" {
				continue
			}

			seen[child.ID] = true
			pos := extractPosition(child.Data)
			items = append(items, RelevanceItem{
				Node:      child,
				Score:     80.0, // Ring 1 base score
				Ring:      RingDirect,
				File:      pos.File,
				StartLine: pos.Line,
				EndLine:   pos.EndLine,
				Reason:    fmt.Sprintf("member of %s", node.Name),
			})
		}

		// For interfaces, try to find implementations
		if node.Type == "go:interface" {
			impls, err := findImplementations(ctx, storage, node)
			if err == nil {
				for _, impl := range impls {
					if seen[impl.ID] {
						continue
					}
					seen[impl.ID] = true
					pos := extractPosition(impl.Data)
					items = append(items, RelevanceItem{
						Node:      impl,
						Score:     75.0, // Slightly lower than direct children
						Ring:      RingDirect,
						File:      pos.File,
						StartLine: pos.Line,
						EndLine:   pos.EndLine,
						Reason:    fmt.Sprintf("implements %s", node.Name),
					})
				}
			}
		}
	}

	return items, nil
}

// expandRing2 finds callers and references.
func expandRing2(ctx context.Context, storage graph.Storage, ring0 []RelevanceItem, seen map[string]bool, task *ParsedTask, includeTests bool) ([]RelevanceItem, error) {
	var items []RelevanceItem
	fileRefCounts := make(map[string]int) // Track reference counts per file

	for _, item := range ring0 {
		// Find references to this symbol
		queryStr := fmt.Sprintf(
			`SELECT * FROM nodes WHERE type = 'go:ref' AND name = '%s' LIMIT 100`,
			escapeSQL(item.Node.Name))

		query, err := aql.Parse(queryStr)
		if err != nil {
			continue
		}

		result, err := storage.Query(ctx, query)
		if err != nil {
			continue
		}

		// Group refs by file to count
		for _, ref := range result.Nodes {
			pos := extractPosition(ref.Data)
			if pos.File != "" {
				fileRefCounts[pos.File]++
			}
		}

		// Add ref nodes
		for _, ref := range result.Nodes {
			if seen[ref.ID] {
				continue
			}
			seen[ref.ID] = true

			pos := extractPosition(ref.Data)
			kind := extractKind(ref.Data)

			// Base score with boost for multiple refs in same file
			score := 60.0
			if count := fileRefCounts[pos.File]; count > 1 {
				score += float64(min(count, 10)) * 2 // +2 per ref, max +20
			}

			// Boost for call refs (more important than type refs)
			if kind == "call" {
				score += 5
			}

			// Boost for test files if fixing
			if includeTests && strings.HasSuffix(pos.File, "_test.go") {
				if task.Intent == IntentFix {
					score += 20
				} else {
					score += 5
				}
			}

			items = append(items, RelevanceItem{
				Node:      ref,
				Score:     score,
				Ring:      RingCallers,
				File:      pos.File,
				StartLine: pos.Line,
				EndLine:   pos.EndLine,
				Reason:    fmt.Sprintf("%s %s", kind, item.Node.Name),
			})
		}
	}

	return items, nil
}

// expandRing3 finds siblings (same package, related types).
func expandRing3(ctx context.Context, storage graph.Storage, ring0 []RelevanceItem, seen map[string]bool, task *ParsedTask) ([]RelevanceItem, error) {
	var items []RelevanceItem

	// Collect packages containing ring0 nodes
	packages := make(map[string]*graph.Node)
	for _, item := range ring0 {
		// Find the package for this node by following belongs_to edges
		edges, err := storage.GetEdgesFrom(ctx, item.Node.ID)
		if err != nil {
			continue
		}
		for _, edge := range edges {
			if edge.Type == "belongs_to" {
				pkg, err := storage.GetNode(ctx, edge.To)
				if err == nil && pkg != nil && pkg.Type == "go:package" {
					packages[pkg.ID] = pkg
				}
			}
		}
	}

	// For each package, find sibling symbols
	for _, pkg := range packages {
		// Find symbols defined by this package
		queryStr := fmt.Sprintf(
			`SELECT * FROM nodes WHERE type LIKE 'go:%%' AND type != 'go:ref' AND type != 'go:package' AND uri LIKE '%s/%%' LIMIT 50`,
			escapeSQL(pkg.URI))

		query, err := aql.Parse(queryStr)
		if err != nil {
			continue
		}

		result, err := storage.Query(ctx, query)
		if err != nil {
			continue
		}

		for _, sibling := range result.Nodes {
			if seen[sibling.ID] {
				continue
			}

			// Only include exported siblings
			if !isExported(sibling.Name) {
				continue
			}

			seen[sibling.ID] = true
			pos := extractPosition(sibling.Data)

			// Boost if mentioned in keywords
			score := 40.0
			for _, kw := range task.Keywords {
				if strings.Contains(strings.ToLower(sibling.Name), kw) {
					score += 10
					break
				}
			}

			items = append(items, RelevanceItem{
				Node:      sibling,
				Score:     score,
				Ring:      RingSiblings,
				File:      pos.File,
				StartLine: pos.Line,
				EndLine:   pos.EndLine,
				Reason:    fmt.Sprintf("in package %s", pkg.Name),
			})
		}
	}

	return items, nil
}

// findImplementations finds structs that implement an interface.
// Uses method name matching (heuristic, not full type checking).
func findImplementations(ctx context.Context, storage graph.Storage, iface *graph.Node) ([]*graph.Node, error) {
	// Find methods of this interface
	methodQuery, err := aql.Parse(fmt.Sprintf(
		`SELECT name FROM nodes WHERE type = 'go:method' AND uri LIKE '%s/method/%%'`,
		escapeSQL(iface.URI)))
	if err != nil {
		return nil, err
	}

	methodResult, err := storage.Query(ctx, methodQuery)
	if err != nil {
		return nil, err
	}

	if len(methodResult.Nodes) == 0 {
		return nil, nil // Empty interface
	}

	// Collect required method names
	requiredMethods := make(map[string]bool)
	for _, m := range methodResult.Nodes {
		requiredMethods[m.Name] = true
	}

	// Find all structs
	structQuery, err := aql.Parse(`SELECT * FROM nodes WHERE type = 'go:struct' LIMIT 100`)
	if err != nil {
		return nil, err
	}

	structResult, err := storage.Query(ctx, structQuery)
	if err != nil {
		return nil, err
	}

	var impls []*graph.Node
	for _, s := range structResult.Nodes {
		// Check if this struct has all required methods
		methodQuery, err := aql.Parse(fmt.Sprintf(
			`SELECT name FROM nodes WHERE type = 'go:method' AND data.receiver = '%s'`,
			escapeSQL(s.Name)))
		if err != nil {
			continue
		}

		structMethods, err := storage.Query(ctx, methodQuery)
		if err != nil {
			continue
		}

		structMethodSet := make(map[string]bool)
		for _, m := range structMethods.Nodes {
			structMethodSet[m.Name] = true
		}

		// Check if all interface methods are present
		hasAll := true
		for method := range requiredMethods {
			if !structMethodSet[method] {
				hasAll = false
				break
			}
		}

		if hasAll {
			impls = append(impls, s)
		}
	}

	return impls, nil
}

// Position holds extracted position info from node data.
type Position struct {
	File    string
	Line    int
	EndLine int
}

// extractPosition extracts position info from node data.
func extractPosition(data any) Position {
	m, ok := data.(map[string]any)
	if !ok {
		return Position{}
	}

	posData, ok := m["position"].(map[string]any)
	if !ok {
		return Position{}
	}

	p := Position{}
	if f, ok := posData["file"].(string); ok {
		p.File = f
	}
	if l, ok := posData["line"].(float64); ok {
		p.Line = int(l)
	}
	if el, ok := posData["end_line"].(float64); ok {
		p.EndLine = int(el)
	}

	return p
}

// extractKind extracts the reference kind from node data.
func extractKind(data any) string {
	m, ok := data.(map[string]any)
	if !ok {
		return ""
	}
	if kind, ok := m["kind"].(string); ok {
		return kind
	}
	return ""
}

// isExported checks if a name is exported (starts with uppercase).
func isExported(name string) bool {
	if len(name) == 0 {
		return false
	}
	r := rune(name[0])
	return r >= 'A' && r <= 'Z'
}

// escapeSQL escapes single quotes for SQL strings.
func escapeSQL(s string) string {
	return strings.ReplaceAll(s, "'", "''")
}

// min returns the minimum of two ints.
func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
