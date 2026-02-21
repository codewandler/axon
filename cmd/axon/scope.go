package main

import (
	"path/filepath"

	"github.com/codewandler/axon/graph"
)

// Common URI schemes used in axon.
const (
	SchemeFile    = "file://"
	SchemeFileMD  = "file+md://"
	SchemeGitFile = "git+file://"
)

// buildScopedNodeFilter creates a NodeFilter scoped to the given path.
// If path is empty, returns an unscoped filter.
// The filter uses URIPrefix which matches nodes with URIs starting with file://<path>.
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
