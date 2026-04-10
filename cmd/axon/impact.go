package main

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/codewandler/axon/aql"
	"github.com/codewandler/axon/graph"
	"github.com/spf13/cobra"
)

var impactCmd = &cobra.Command{
	Use:   "impact <symbol>",
	Short: "Show what would be affected if a symbol changes",
	Long: `Analyse the blast radius of changing a symbol.

Given a symbol name, impact finds:
  - All direct references (go:ref nodes) to the symbol
  - Which packages contain those references
  - Which other packages import those packages (indirect impact)

This requires the graph to be indexed with import graph support
(available after 'axon init' with the latest axon version).

Examples:
  axon impact Storage
  axon impact NewNode
  axon impact IndexResult`,
	Args: cobra.ExactArgs(1),
	RunE: runImpact,
}

func runImpact(cmd *cobra.Command, args []string) error {
	symbol := args[0]
	ctx := context.Background()

	cmdCtx, err := openDB(false)
	if err != nil {
		return err
	}
	defer cmdCtx.Close()

	storage := cmdCtx.Storage

	// Step 1: Find the target symbol node
	targetQuery, _ := aql.Parse(fmt.Sprintf(
		`SELECT * FROM nodes WHERE name = '%s' AND type IN ('go:func', 'go:struct', 'go:interface', 'go:method', 'go:const', 'go:var') LIMIT 5`,
		escapeSQL(symbol)))

	targetResult, err := storage.Query(ctx, targetQuery)
	if err != nil {
		return fmt.Errorf("finding target: %w", err)
	}

	if len(targetResult.Nodes) == 0 {
		fmt.Printf("Symbol '%s' not found in the graph.\n", symbol)
		fmt.Printf("Try: axon find --name %s\n", symbol)
		return nil
	}

	// Print header - use first match (most likely match)
	target := targetResult.Nodes[0]
	if len(targetResult.Nodes) > 1 {
		fmt.Printf("Multiple matches for '%s' — showing results for first match:\n\n", symbol)
	}
	fmt.Printf("Impact analysis: %s (%s)\n\n", target.Name, target.Type)

	// Step 2: Find all go:ref nodes that reference the target via `references` edges
	// Get edges pointing TO the target
	edges, err := storage.GetEdgesTo(ctx, target.ID)
	if err != nil {
		return fmt.Errorf("getting references: %w", err)
	}

	// Filter to `references` edges only
	var refNodeIDs []string
	for _, e := range edges {
		if e.Type == "references" {
			refNodeIDs = append(refNodeIDs, e.From)
		}
	}

	// Step 3: Load the ref nodes and group by package path
	type refGroup struct {
		pkgPath string
		kinds   map[string]struct{}
		count   int
	}
	refsByPkg := make(map[string]*refGroup)

	for _, refID := range refNodeIDs {
		refNode, err := storage.GetNode(ctx, refID)
		if err != nil || refNode == nil {
			continue
		}
		if refNode.Type != "go:ref" {
			continue
		}

		// Extract package path from ref data
		pkgPath := extractRefPkgPath(refNode)
		if pkgPath == "" {
			pkgPath = "(unknown)"
		}

		if _, ok := refsByPkg[pkgPath]; !ok {
			refsByPkg[pkgPath] = &refGroup{pkgPath: pkgPath, kinds: make(map[string]struct{})}
		}
		refsByPkg[pkgPath].count++

		// Get kind from ref data
		if data, ok := refNode.Data.(map[string]interface{}); ok {
			if kind, ok := data["kind"].(string); ok {
				refsByPkg[pkgPath].kinds[kind] = struct{}{}
			}
		}
	}

	totalRefs := 0
	for _, g := range refsByPkg {
		totalRefs += g.count
	}

	if totalRefs == 0 {
		fmt.Println("No direct references found.")
		fmt.Println("(This symbol may not be referenced within the indexed codebase, or the graph needs to be re-indexed.)")
		return nil
	}

	fmt.Printf("Direct references (%d):\n", totalRefs)

	// Sort by count descending
	groups := make([]*refGroup, 0, len(refsByPkg))
	for _, g := range refsByPkg {
		groups = append(groups, g)
	}
	sort.Slice(groups, func(i, j int) bool {
		return groups[i].count > groups[j].count
	})

	affectedPkgs := make([]string, 0, len(groups))
	for _, g := range groups {
		kinds := make([]string, 0, len(g.kinds))
		for k := range g.kinds {
			kinds = append(kinds, k)
		}
		sort.Strings(kinds)
		kindStr := ""
		if len(kinds) > 0 {
			kindStr = fmt.Sprintf("  [%s]", strings.Join(kinds, ", "))
		}
		shortPkg := g.pkgPath
		if len(shortPkg) > 30 {
			parts := strings.Split(shortPkg, "/")
			if len(parts) > 2 {
				shortPkg = strings.Join(parts[len(parts)-2:], "/")
			}
		}
		fmt.Printf("  %-30s %3d refs%s\n", shortPkg, g.count, kindStr)
		affectedPkgs = append(affectedPkgs, g.pkgPath)
	}

	// Step 4: Find packages that import the affected packages (requires import graph from Phase A)
	fmt.Printf("\nPackages importing affected packages:\n")

	// For each affected package, find who imports it via `imports` edges
	importerMap := make(map[string][]string) // importer pkg path -> imported pkg paths
	for _, pkgPath := range affectedPkgs {
		// Find the go:package node for this pkgPath
		pkgQuery, _ := aql.Parse(fmt.Sprintf(
			`SELECT * FROM nodes WHERE type = 'go:package' AND data.import_path = '%s' LIMIT 1`,
			escapeSQL(pkgPath)))
		pkgResult, err := storage.Query(ctx, pkgQuery)
		if err != nil || len(pkgResult.Nodes) == 0 {
			continue
		}
		pkgNode := pkgResult.Nodes[0]

		// Find all edges pointing TO this package via `imports`
		importerEdges, err := storage.GetEdgesTo(ctx, pkgNode.ID)
		if err != nil {
			continue
		}
		for _, e := range importerEdges {
			if e.Type != "imports" {
				continue
			}
			// Get the importer package node
			importerNode, err := storage.GetNode(ctx, e.From)
			if err != nil || importerNode == nil {
				continue
			}
			importerPath := importerNode.Name
			if data, ok := importerNode.Data.(map[string]interface{}); ok {
				if ip, ok := data["import_path"].(string); ok {
					importerPath = ip
				}
			}
			importerMap[importerPath] = append(importerMap[importerPath], pkgPath)
		}
	}

	if len(importerMap) == 0 {
		fmt.Println("  (none found — run 'axon init' to rebuild the import graph)")
	} else {
		importers := make([]string, 0, len(importerMap))
		for k := range importerMap {
			importers = append(importers, k)
		}
		sort.Strings(importers)
		for _, importer := range importers {
			imported := importerMap[importer]
			shortImporter := importer
			if len(shortImporter) > 25 {
				parts := strings.Split(shortImporter, "/")
				if len(parts) > 2 {
					shortImporter = strings.Join(parts[len(parts)-2:], "/")
				}
			}
			shortImported := make([]string, len(imported))
			for i, ip := range imported {
				parts := strings.Split(ip, "/")
				if len(parts) > 0 {
					shortImported[i] = parts[len(parts)-1]
				} else {
					shortImported[i] = ip
				}
			}
			fmt.Printf("  %-25s imports %s\n", shortImporter, strings.Join(shortImported, ", "))
		}
	}

	return nil
}

// extractRefPkgPath extracts the package path of the file that contains a go:ref node.
func extractRefPkgPath(node *graph.Node) string {
	if data, ok := node.Data.(map[string]interface{}); ok {
		// Use the position file to derive the containing package
		if pos, ok := data["position"].(map[string]interface{}); ok {
			if file, ok := pos["file"].(string); ok {
				return derivePackageFromFile(file)
			}
		}
		// Fallback: use target_pkg
		if pkg, ok := data["target_pkg"].(string); ok {
			return pkg
		}
	}
	// Fallback: use the URI
	return node.URI
}

// derivePackageFromFile derives a short package identifier from a file path.
func derivePackageFromFile(file string) string {
	if file == "" {
		return ""
	}
	// Return the directory portion of the file
	idx := strings.LastIndex(file, "/")
	if idx < 0 {
		return file
	}
	return file[:idx]
}
