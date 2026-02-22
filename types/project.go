package types

import "github.com/codewandler/axon/graph"

// Project node type
const (
	TypeProject = "project:root"
)

// Project language identifiers
const (
	LangGo     = "go"
	LangNode   = "node"
	LangRust   = "rust"
	LangPython = "python"
	LangJava   = "java"
	LangRuby   = "ruby"
	LangPHP    = "php"
)

// Project labels for tagging directory nodes
const (
	LabelProjectGo     = "project:go"
	LabelProjectNode   = "project:node"
	LabelProjectRust   = "project:rust"
	LabelProjectPython = "project:python"
	LabelProjectJava   = "project:java"
	LabelProjectRuby   = "project:ruby"
	LabelProjectPHP    = "project:php"
)

// ProjectData holds data for a project node.
type ProjectData struct {
	Language string `json:"language"`            // Language identifier (go, node, rust, python, java, ruby, php)
	Name     string `json:"name,omitempty"`      // Project name from manifest
	Version  string `json:"version,omitempty"`   // Project version if available
	DepCount int    `json:"dep_count,omitempty"` // Number of dependencies
}

// RegisterProjectTypes registers project node and edge types with the registry.
func RegisterProjectTypes(r *graph.Registry) {
	graph.RegisterNodeType[ProjectData](r, graph.NodeSpec{
		Type:        TypeProject,
		Description: "A project root detected from manifest files",
	})

	// Project located at directory
	r.RegisterEdgeType(graph.EdgeSpec{
		Type:        EdgeLocatedAt,
		Description: "Project is located at a directory",
		FromTypes:   []string{TypeProject},
		ToTypes:     []string{TypeDir},
	})
}

// ProjectPathToURI converts a project directory path to a project+file:// URI.
func ProjectPathToURI(path string) string {
	return "project+file://" + path
}

// URIToProjectPath extracts the path from a project+file:// URI.
func URIToProjectPath(uri string) string {
	const prefix = "project+file://"
	if len(uri) > len(prefix) && uri[:len(prefix)] == prefix {
		return uri[len(prefix):]
	}
	return uri
}
