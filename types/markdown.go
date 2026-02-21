package types

import "github.com/codewandler/axon/graph"

// Markdown node types
const (
	TypeMarkdownDoc       = "md:document"
	TypeMarkdownSection   = "md:section"
	TypeMarkdownCodeBlock = "md:codeblock"
	TypeMarkdownLink      = "md:link"
	TypeMarkdownImage     = "md:image"
)

// Markdown uses common edge types: has, belongs_to, links_to
// These are defined in types/edges.go

// DocumentData holds data for a markdown document node.
type DocumentData struct {
	Title string `json:"title"` // First H1 or filename
}

// SectionData holds data for a markdown section node.
type SectionData struct {
	Title    string `json:"title"`
	Level    int    `json:"level"`    // 1-6
	Order    int    `json:"order"`    // Position among sibling sections
	Position int    `json:"position"` // Position among all children of parent (for ordering)
	Content  string `json:"content"`  // Section content (excluding child sections)
}

// CodeBlockData holds data for a fenced code block node.
type CodeBlockData struct {
	Language    string `json:"language"`
	Content     string `json:"content"`
	Lines       int    `json:"lines"`
	ContentHash string `json:"content_hash"` // SHA256 prefix for change detection
	Position    int    `json:"position"`     // Position among all children of parent (for ordering)
}

// MarkdownLinkData holds data for an external link node.
// Named MarkdownLinkData to avoid conflict with types.LinkData for symlinks.
type MarkdownLinkData struct {
	URL   string `json:"url"`
	Text  string `json:"text"`
	Title string `json:"title,omitempty"`
}

// ImageData holds data for an image reference node.
type ImageData struct {
	URL string `json:"url"`
	Alt string `json:"alt"`
}

// MarkdownFileToURI converts a file path to a file+md:// URI.
func MarkdownFileToURI(path string) string {
	return "file+md://" + path
}

// MarkdownSectionURI creates a URI for a section within a markdown file.
// sectionPath is like "h1-intro/h2-setup" representing the nesting.
func MarkdownSectionURI(filePath, sectionPath string) string {
	return "file+md://" + filePath + "#" + sectionPath
}

// MarkdownCodeBlockURI creates a URI for a code block.
// id contains section path + index + content hash, e.g., "h1-intro/h2-setup/cb-0-abc123"
func MarkdownCodeBlockURI(filePath, id string) string {
	return "file+md://" + filePath + "#" + id
}

// MarkdownLinkURI creates a URI for an external link discovered in markdown.
func MarkdownLinkURI(filePath, url string) string {
	return "file+md://" + filePath + "#link-" + url
}

// MarkdownImageURI creates a URI for an image reference in markdown.
func MarkdownImageURI(filePath, url string) string {
	return "file+md://" + filePath + "#img-" + url
}

// RegisterMarkdownTypes registers markdown node and edge types with the registry.
func RegisterMarkdownTypes(r *graph.Registry) {
	graph.RegisterNodeType[DocumentData](r, graph.NodeSpec{
		Type:        TypeMarkdownDoc,
		Description: "A parsed markdown document",
	})

	graph.RegisterNodeType[SectionData](r, graph.NodeSpec{
		Type:        TypeMarkdownSection,
		Description: "A section in a markdown document (defined by heading)",
	})

	graph.RegisterNodeType[CodeBlockData](r, graph.NodeSpec{
		Type:        TypeMarkdownCodeBlock,
		Description: "A fenced code block in markdown",
	})

	graph.RegisterNodeType[MarkdownLinkData](r, graph.NodeSpec{
		Type:        TypeMarkdownLink,
		Description: "An external link in markdown",
	})

	graph.RegisterNodeType[ImageData](r, graph.NodeSpec{
		Type:        TypeMarkdownImage,
		Description: "An image reference in markdown",
	})

	// Markdown uses common edge types (has, belongs_to, links_to)
	// registered in RegisterCommonEdges. Only links_to needs specific constraints.
	r.RegisterEdgeType(graph.EdgeSpec{
		Type:        EdgeLinksTo,
		Description: "Markdown element links to a URL or file",
		FromTypes:   []string{TypeMarkdownDoc, TypeMarkdownSection},
		ToTypes:     []string{TypeMarkdownLink, TypeFile},
	})
}
