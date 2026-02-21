package types

import (
	"os"
	"time"

	"github.com/codewandler/axon/graph"
)

// Filesystem node types
const (
	TypeDir  = "fs:dir"
	TypeFile = "fs:file"
	TypeLink = "fs:link"
)

// DirData holds data for a directory node.
type DirData struct {
	Name string      `json:"name"`
	Mode os.FileMode `json:"mode"`
}

// FileData holds data for a file node.
type FileData struct {
	Name        string      `json:"name"`
	Size        int64       `json:"size"`
	Modified    time.Time   `json:"modified"`
	Mode        os.FileMode `json:"mode"`
	Ext         string      `json:"ext"`          // File extension (e.g., ".md", ".go")
	ContentType string      `json:"content_type"` // MIME type (e.g., "text/markdown")
}

// LinkData holds data for a symbolic link node.
type LinkData struct {
	Name   string `json:"name"`
	Target string `json:"target"`
}

// RegisterFSTypes registers filesystem node and edge types with the registry.
func RegisterFSTypes(r *graph.Registry) {
	graph.RegisterNodeType[DirData](r, graph.NodeSpec{
		Type:        TypeDir,
		Description: "A directory in the filesystem",
	})

	graph.RegisterNodeType[FileData](r, graph.NodeSpec{
		Type:        TypeFile,
		Description: "A file in the filesystem",
	})

	graph.RegisterNodeType[LinkData](r, graph.NodeSpec{
		Type:        TypeLink,
		Description: "A symbolic link in the filesystem",
	})

	r.RegisterEdgeType(graph.EdgeSpec{
		Type:        EdgeContains,
		Description: "Parent directory contains child",
		FromTypes:   []string{TypeDir},
		ToTypes:     []string{TypeDir, TypeFile, TypeLink},
	})

	r.RegisterEdgeType(graph.EdgeSpec{
		Type:        EdgeContainedBy,
		Description: "Child is contained by parent directory",
		FromTypes:   []string{TypeDir, TypeFile, TypeLink},
		ToTypes:     []string{TypeDir},
	})
}

// PathToURI converts a filesystem path to a file:// URI.
func PathToURI(path string) string {
	return "file://" + path
}

// URIToPath extracts the path from a file:// URI.
func URIToPath(uri string) string {
	const prefix = "file://"
	if len(uri) > len(prefix) && uri[:len(prefix)] == prefix {
		return uri[len(prefix):]
	}
	return uri
}
