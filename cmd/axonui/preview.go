package main

import (
	"context"
	"fmt"
	"os"
	"sort"
	"strings"
	"sync"

	"github.com/charmbracelet/glamour"
	"github.com/codewandler/axon/graph"
	"github.com/codewandler/axon/types"
)

// Cached glamour renderer to avoid cold-start penalty on each render.
// glamour.NewTermRenderer is expensive (~2-5ms); caching drops subsequent renders
// to just the render call itself.
var (
	glamourMu       sync.Mutex
	glamourRenderer *glamour.TermRenderer
	glamourWidth    int
)

// Max bytes to read from a file for preview
const maxPreviewBytes = 32 * 1024 // 32KB

// Known code extensions mapped to glamour/chroma language names.
// Keys are without leading dot, matching the data.ext field (e.g. "go", "py").
var codeExtensions = map[string]string{
	"go":     "go",
	"py":     "python",
	"js":     "javascript",
	"ts":     "typescript",
	"tsx":    "tsx",
	"jsx":    "jsx",
	"rs":     "rust",
	"c":      "c",
	"h":      "c",
	"cpp":    "cpp",
	"cc":     "cpp",
	"java":   "java",
	"rb":     "ruby",
	"sh":     "bash",
	"bash":   "bash",
	"zsh":    "bash",
	"yaml":   "yaml",
	"yml":    "yaml",
	"json":   "json",
	"toml":   "toml",
	"xml":    "xml",
	"html":   "html",
	"css":    "css",
	"sql":    "sql",
	"lua":    "lua",
	"vim":    "vim",
	"zig":    "zig",
	"swift":  "swift",
	"kt":     "kotlin",
	"scala":  "scala",
	"hs":     "haskell",
	"ex":     "elixir",
	"exs":    "elixir",
	"erl":    "erlang",
	"ml":     "ocaml",
	"r":      "r",
	"php":    "php",
	"pl":     "perl",
	"tf":     "hcl",
	"proto":  "protobuf",
	"mod":    "gomod",
	"sum":    "gomod",
}

// resolvePreview returns rendered preview content for a node.
// Returns empty string if no preview is available.
func resolvePreview(ctx context.Context, g *graph.Graph, node *graph.Node, width int) string {
	if node == nil {
		return ""
	}

	switch data := node.Data.(type) {
	case types.SectionData:
		return previewSection(ctx, g, node, data, width)
	case types.CodeBlockData:
		return previewCodeBlock(data, width)
	case types.DocumentData:
		return previewDocument(ctx, g, node, width)
	case types.MarkdownLinkData:
		md := fmt.Sprintf("[%s](%s)", data.Text, data.URL)
		return renderGlamour(md, width)
	case types.ImageData:
		md := fmt.Sprintf("![%s](%s)", data.Alt, data.URL)
		return renderGlamour(md, width)
	case types.FileData:
		return previewFile(node, data, width)
	case map[string]any:
		return previewMapData(ctx, g, node, data, width)
	}

	return ""
}

// previewSection renders a markdown section with its content and child code blocks.
func previewSection(ctx context.Context, g *graph.Graph, node *graph.Node, data types.SectionData, width int) string {
	var md strings.Builder

	heading := strings.Repeat("#", data.Level) + " " + data.Title + "\n\n"
	md.WriteString(heading)

	if data.Content != "" {
		md.WriteString(data.Content)
		md.WriteString("\n\n")
	}

	// Include child code blocks for richer preview
	children := orderedChildren(ctx, g, node.ID)
	for _, child := range children {
		switch child.node.Type {
		case types.TypeMarkdownCodeBlock:
			lang, code := extractCBData(child.node)
			md.WriteString(fmt.Sprintf("```%s\n%s```\n\n", lang, code))
		case types.TypeMarkdownSection:
			lvl, title, content := extractSecData(child.node)
			md.WriteString(strings.Repeat("#", lvl) + " " + title + "\n\n")
			if content != "" {
				md.WriteString(content)
				md.WriteString("\n\n")
			}
		}
	}

	return renderGlamour(md.String(), width)
}

// previewCodeBlock renders a fenced code block.
func previewCodeBlock(data types.CodeBlockData, width int) string {
	md := fmt.Sprintf("```%s\n%s```", data.Language, data.Content)
	return renderGlamour(md, width)
}

// previewDocument resolves the source file and renders its content.
func previewDocument(ctx context.Context, g *graph.Graph, node *graph.Node, width int) string {
	// Find source file via belongs_to edge
	edges, err := g.GetEdgesFrom(ctx, node.ID)
	if err != nil {
		return ""
	}

	for _, e := range edges {
		if e.Type == types.EdgeBelongsTo {
			sourceNode, err := g.GetNode(ctx, e.To)
			if err != nil || sourceNode.Type != types.TypeFile {
				continue
			}
			content := readFilePreview(sourceNode.URI)
			if content != "" {
				return renderGlamour(content, width)
			}
		}
	}

	return ""
}

// previewFile renders a file's content if it's markdown or code.
func previewFile(node *graph.Node, data types.FileData, width int) string {
	ext := strings.ToLower(data.Ext)

	// Markdown files
	if ext == "md" || ext == "markdown" || ext == "mdx" {
		content := readFilePreview(node.URI)
		if content != "" {
			return renderGlamour(content, width)
		}
	}

	// Code files
	if lang, ok := codeExtensions[ext]; ok {
		content := readFilePreview(node.URI)
		if content != "" {
			md := fmt.Sprintf("```%s\n%s```", lang, content)
			return renderGlamour(md, width)
		}
	}

	// Plain text files (small ones)
	if strings.HasPrefix(data.ContentType, "text/") && data.Size < maxPreviewBytes {
		content := readFilePreview(node.URI)
		if content != "" {
			return content
		}
	}

	return ""
}

// previewMapData handles data loaded from JSON (not typed structs).
func previewMapData(ctx context.Context, g *graph.Graph, node *graph.Node, data map[string]any, width int) string {
	switch node.Type {
	case types.TypeMarkdownSection:
		level := 1
		if l, ok := data["level"].(float64); ok {
			level = int(l)
		}
		title, _ := data["title"].(string)
		content, _ := data["content"].(string)

		var md strings.Builder
		md.WriteString(strings.Repeat("#", level) + " " + title + "\n\n")
		if content != "" {
			md.WriteString(content)
			md.WriteString("\n\n")
		}

		children := orderedChildren(ctx, g, node.ID)
		for _, child := range children {
			switch child.node.Type {
			case types.TypeMarkdownCodeBlock:
				lang, code := extractCBData(child.node)
				md.WriteString(fmt.Sprintf("```%s\n%s```\n\n", lang, code))
			}
		}

		return renderGlamour(md.String(), width)

	case types.TypeMarkdownCodeBlock:
		lang, _ := data["language"].(string)
		content, _ := data["content"].(string)
		md := fmt.Sprintf("```%s\n%s```", lang, content)
		return renderGlamour(md, width)

	case types.TypeMarkdownDoc:
		return previewDocument(ctx, g, node, width)

	case types.TypeFile:
		ext, _ := data["ext"].(string)
		ext = strings.ToLower(ext)
		if ext == "md" || ext == "markdown" || ext == "mdx" {
			content := readFilePreview(node.URI)
			if content != "" {
				return renderGlamour(content, width)
			}
		}
		if lang, ok := codeExtensions[ext]; ok {
			content := readFilePreview(node.URI)
			if content != "" {
				md := fmt.Sprintf("```%s\n%s```", lang, content)
				return renderGlamour(md, width)
			}
		}
	}

	return ""
}

// readFilePreview reads a file from a file:// URI, limited to maxPreviewBytes.
func readFilePreview(uri string) string {
	path := strings.TrimPrefix(uri, "file://")
	if path == "" {
		return ""
	}

	f, err := os.Open(path)
	if err != nil {
		return ""
	}
	defer f.Close()

	buf := make([]byte, maxPreviewBytes)
	n, _ := f.Read(buf)
	if n == 0 {
		return ""
	}

	content := string(buf[:n])

	// If we truncated, add indicator
	if n == maxPreviewBytes {
		// Find last newline to avoid cutting mid-line
		lastNL := strings.LastIndex(content, "\n")
		if lastNL > 0 {
			content = content[:lastNL]
		}
		content += "\n\n... (truncated)"
	}

	return content
}

// warmUpGlamour pre-creates the glamour renderer so the first real render is fast.
func warmUpGlamour() {
	glamourMu.Lock()
	defer glamourMu.Unlock()

	getGlamourRenderer(80)
	// Do a tiny render to fully initialize chroma styles
	if glamourRenderer != nil {
		glamourRenderer.Render("# warm\n")
	}
}

// getGlamourRenderer returns a cached glamour renderer, creating one if needed.
// The renderer is recreated if the width changes.
func getGlamourRenderer(width int) *glamour.TermRenderer {
	if width < 20 {
		width = 80
	}
	w := width - 4 // leave margin

	// Fast path: reuse cached renderer if width matches
	if glamourRenderer != nil && glamourWidth == w {
		return glamourRenderer
	}

	// First call or width changed: create new renderer
	r, err := glamour.NewTermRenderer(
		glamour.WithAutoStyle(),
		glamour.WithWordWrap(w),
	)
	if err != nil {
		return nil
	}
	glamourRenderer = r
	glamourWidth = w
	return r
}

// renderGlamour renders markdown using glamour with the given width.
// Uses a cached renderer to avoid the ~5ms cold-start penalty per render.
// Thread-safe: serializes access to the shared renderer.
func renderGlamour(content string, width int) string {
	glamourMu.Lock()
	defer glamourMu.Unlock()

	r := getGlamourRenderer(width)
	if r == nil {
		return content
	}
	out, err := r.Render(content)
	if err != nil {
		return content
	}
	return strings.TrimSpace(out)
}

// orderedChild2 is a helper for sorting children by position.
type orderedChild2 struct {
	node     *graph.Node
	position int
}

// orderedChildren returns child nodes sorted by position.
func orderedChildren(ctx context.Context, g *graph.Graph, nodeID string) []orderedChild2 {
	edges, err := g.GetEdgesFrom(ctx, nodeID)
	if err != nil {
		return nil
	}

	var children []orderedChild2
	for _, e := range edges {
		if e.Type != types.EdgeHas {
			continue
		}
		childNode, err := g.GetNode(ctx, e.To)
		if err != nil {
			continue
		}
		if childNode.Type != types.TypeMarkdownSection && childNode.Type != types.TypeMarkdownCodeBlock {
			continue
		}
		pos := extractPos(childNode)
		children = append(children, orderedChild2{node: childNode, position: pos})
	}

	sort.Slice(children, func(i, j int) bool {
		return children[i].position < children[j].position
	})

	return children
}

// extractPos gets the position from a node's data.
func extractPos(n *graph.Node) int {
	switch data := n.Data.(type) {
	case types.SectionData:
		return data.Position
	case types.CodeBlockData:
		return data.Position
	case map[string]any:
		if pos, ok := data["position"].(float64); ok {
			return int(pos)
		}
	}
	return 0
}

// extractCBData extracts language and content from a code block node.
func extractCBData(n *graph.Node) (language, content string) {
	switch data := n.Data.(type) {
	case types.CodeBlockData:
		return data.Language, data.Content
	case map[string]any:
		lang, _ := data["language"].(string)
		c, _ := data["content"].(string)
		return lang, c
	}
	return "", ""
}

// extractSecData extracts level, title, content from a section node.
func extractSecData(n *graph.Node) (level int, title, content string) {
	switch data := n.Data.(type) {
	case types.SectionData:
		return data.Level, data.Title, data.Content
	case map[string]any:
		if l, ok := data["level"].(float64); ok {
			level = int(l)
		}
		title, _ = data["title"].(string)
		content, _ = data["content"].(string)
		return
	}
	return 0, "", ""
}

// hasPreviewContent returns true if the node type can show a content preview.
func hasPreviewContent(node *graph.Node) bool {
	if node == nil {
		return false
	}

	switch node.Type {
	case types.TypeMarkdownDoc, types.TypeMarkdownSection,
		types.TypeMarkdownCodeBlock, types.TypeMarkdownLink,
		types.TypeMarkdownImage:
		return true
	case types.TypeFile:
		// Check extension
		switch data := node.Data.(type) {
		case types.FileData:
			ext := strings.ToLower(data.Ext)
			if ext == "md" || ext == "markdown" || ext == "mdx" {
				return true
			}
			if _, ok := codeExtensions[ext]; ok {
				return data.Size < maxPreviewBytes
			}
		case map[string]any:
			ext, _ := data["ext"].(string)
			ext = strings.ToLower(ext)
			if ext == "md" || ext == "markdown" || ext == "mdx" {
				return true
			}
			if _, ok := codeExtensions[ext]; ok {
				return true
			}
		}
	}

	return false
}

