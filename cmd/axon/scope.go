package main

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/codewandler/axon"
	"github.com/codewandler/axon/graph"
	"github.com/codewandler/axon/types"
)

// Common URI schemes used in axon.
const (
	SchemeFile    = "file://"
	SchemeFileMD  = "file+md://"
	SchemeGitFile = "git+file://"
)

// ScopeEdgeFilters defines the default edge filters for scope traversal.
// - contains, has: follow outgoing (parent to child)
// - located_at: follow incoming (find things located at this node)
var ScopeEdgeFilters = []graph.EdgeFilter{
	{Types: []string{"contains", "has"}, Direction: "outgoing"},
	{Types: []string{"located_at"}, Direction: "incoming"},
}

// resolveScopeTraversal returns TraverseOptions for the current scope.
// If global is true, seeds from all root nodes.
// If scoped to cwd, seeds from the cwd directory node.
// If cwd is not indexed, prompts user to index it first.
func resolveScopeTraversal(ctx context.Context, storage graph.Storage, ax *axon.Axon, global bool, cwd string, maxDepth int) (graph.TraverseOptions, error) {
	opts := graph.TraverseOptions{
		MaxDepth:    maxDepth,
		EdgeFilters: ScopeEdgeFilters,
	}

	if global {
		// Seed from all root nodes
		opts.Seed = graph.NodeFilter{Root: true}
		return opts, nil
	}

	// Scoped to current directory
	absPath, err := filepath.Abs(cwd)
	if err != nil {
		return opts, fmt.Errorf("failed to resolve path: %w", err)
	}

	uri := types.PathToURI(absPath)

	// Check if directory is indexed
	node, err := storage.GetNodeByURI(ctx, uri)
	if err != nil {
		// Directory not indexed - prompt to index
		indexed, err := promptIndex(ctx, ax, absPath)
		if err != nil {
			return opts, err
		}
		if !indexed {
			return opts, fmt.Errorf("directory not indexed: %s", absPath)
		}

		// Try again after indexing
		node, err = storage.GetNodeByURI(ctx, uri)
		if err != nil {
			return opts, fmt.Errorf("failed to find directory after indexing: %w", err)
		}
	}

	// Seed from this specific node
	opts.Seed = graph.NodeFilter{NodeIDs: []string{node.ID}}
	return opts, nil
}

// promptIndex asks the user if they want to index the directory.
// Returns true if indexed successfully, false if user declined.
func promptIndex(ctx context.Context, ax *axon.Axon, path string) (bool, error) {
	fmt.Printf("Directory not indexed: %s\nIndex now? [Y/n] ", path)

	reader := bufio.NewReader(os.Stdin)
	response, err := reader.ReadString('\n')
	if err != nil {
		return false, err
	}

	response = strings.TrimSpace(strings.ToLower(response))
	if response != "" && response != "y" && response != "yes" {
		return false, nil
	}

	fmt.Printf("Indexing %s...\n", path)
	_, err = ax.Index(ctx, path)
	if err != nil {
		return false, fmt.Errorf("indexing failed: %w", err)
	}

	fmt.Println("Done.")
	return true, nil
}

// collectTraversalNodes collects all nodes from a traversal into a slice.
func collectTraversalNodes(results <-chan graph.TraverseResult) ([]*graph.Node, error) {
	var nodes []*graph.Node
	for r := range results {
		if r.Err != nil {
			return nil, r.Err
		}
		nodes = append(nodes, r.Node)
	}
	return nodes, nil
}

// countTraversalTypes counts node types from a traversal.
func countTraversalTypes(results <-chan graph.TraverseResult) (map[string]int, error) {
	counts := make(map[string]int)
	for r := range results {
		if r.Err != nil {
			return nil, r.Err
		}
		counts[r.Node.Type]++
	}
	return counts, nil
}

// Legacy functions for backwards compatibility - can be removed later

// buildScopedNodeFilter creates a NodeFilter scoped to the given path.
// If path is empty, returns an unscoped filter.
// The filter uses URIPrefix which matches nodes with URIs starting with file://<path>.
// DEPRECATED: Use resolveScopeTraversal instead for proper graph traversal.
func buildScopedNodeFilter(path string) graph.NodeFilter {
	if path == "" {
		return graph.NodeFilter{}
	}
	// Use file:// prefix - this covers most nodes
	// Note: This won't match file+md:// or git+file:// URIs directly,
	// but for counting purposes we handle this in the SQL with multiple LIKE conditions
	return graph.NodeFilter{
		URIPrefix: SchemeFile + path,
	}
}

// buildScopedEdgeFilter creates an EdgeFilter scoped to the given path.
// Scopes by the "from" node's URI prefix.
// DEPRECATED: Use resolveScopeTraversal instead for proper graph traversal.
func buildScopedEdgeFilter(path string) graph.EdgeFilter {
	if path == "" {
		return graph.EdgeFilter{}
	}
	return graph.EdgeFilter{
		From: &graph.NodeFilter{
			URIPrefix: SchemeFile + path,
		},
	}
}

// resolveScope returns the scope path based on flags.
// If global is true, returns empty string (no scope).
// Otherwise, returns the absolute path of cwd.
// DEPRECATED: Use resolveScopeTraversal instead for proper graph traversal.
func resolveScope(global bool, cwd string) string {
	if global {
		return ""
	}
	absPath, err := filepath.Abs(cwd)
	if err != nil {
		return cwd
	}
	return absPath
}
