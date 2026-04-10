package main

import (
	"context"
	"fmt"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/charmbracelet/glamour"
	"github.com/charmbracelet/lipgloss"

	"github.com/codewandler/axon/aql"
	"github.com/codewandler/axon/graph"
	"github.com/codewandler/axon/types"
	"github.com/spf13/cobra"
)

// Lipgloss styles for node header
var (
	headerLabelStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("245"))
	headerValueStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("212")).Bold(true)
)

var showCmd = &cobra.Command{
	Use:   "show <node-id>",
	Short: "Show details of a node",
	Long: `Display detailed information about a specific node in the graph.

You can provide a full node ID or a prefix (minimum 4 characters).
If multiple nodes match the prefix, all matches will be listed.

The output includes:
- Node metadata (type, URI, key)
- Node data (name, size, mode, etc.)
- Incoming and outgoing edges`,
	Args: cobra.ExactArgs(1),
	RunE: runShow,
}

func runShow(cmd *cobra.Command, args []string) error {
	nodeID := args[0]

	if len(nodeID) < 4 {
		return fmt.Errorf("node ID must be at least 4 characters")
	}

	cmdCtx, err := openDB(false)
	if err != nil {
		return err
	}
	defer cmdCtx.Close()

	// Print database location
	fmt.Printf("Using database: %s\n", cmdCtx.DBLoc.Path)

	// Get Axon instance
	ax, err := cmdCtx.Axon()
	if err != nil {
		return err
	}

	// Find node(s) matching the ID prefix
	nodes, err := findNodesByPrefix(cmdCtx.Ctx, ax.Graph(), nodeID)
	if err != nil {
		return err
	}

	if len(nodes) == 0 {
		return fmt.Errorf("no node found matching '%s'", nodeID)
	}

	if len(nodes) > 1 {
		fmt.Printf("Multiple nodes match '%s':\n\n", nodeID)
		for _, n := range nodes {
			fmt.Printf("  %s  %s\n", n.ID, getNodeSummary(n))
		}
		return nil
	}

	// Single match - show full details
	node := nodes[0]
	return showNodeDetails(cmdCtx.Ctx, ax.Graph(), node)
}

// findNodesByPrefix finds all nodes whose ID starts with the given prefix using AQL
func findNodesByPrefix(ctx context.Context, g *graph.Graph, prefix string) ([]*graph.Node, error) {
	// Build AQL query: SELECT * FROM nodes WHERE id GLOB 'prefix*' LIMIT 100
	// Note: GLOB uses the PRIMARY KEY index, while LIKE does not
	query := aql.Nodes.
		SelectStar().
		Where(aql.ID.Glob(prefix + "*")).
		Limit(100).
		Build()

	// Execute query through storage
	result, err := g.Storage().Query(ctx, query)
	if err != nil {
		return nil, err
	}

	// Return nodes from result
	return result.Nodes, nil
}

// getNodeSummary returns a brief summary of a node for listing
func getNodeSummary(n *graph.Node) string {
	var name string
	switch data := n.Data.(type) {
	case types.DirData:
		name = data.Name + "/"
	case types.FileData:
		name = data.Name
	case types.LinkData:
		name = data.Name
	case types.RepoData:
		name = data.Name
	case types.RemoteData:
		name = data.Name
	case types.BranchData:
		name = data.Name
	case types.TagData:
		name = data.Name
	case types.CommitData:
		short := data.SHA
		if len(data.SHA) >= 8 {
			short = data.SHA[:8]
		}
		if data.Message != "" {
			name = short + " -- " + data.Message
		} else {
			name = short
		}
	case types.LicenseData:
		if data.SPDXID != "" {
			name = data.SPDXID
		} else {
			name = "unknown"
		}
	case types.TodoData:
		name = data.Kind
		if data.Text != "" {
			name = data.Kind + ": " + data.Text
			if len(name) > 60 {
				name = name[:57] + "..."
			}
		}
	case types.DocumentData:
		name = data.Title
	case types.SectionData:
		name = data.Title
	case types.CodeBlockData:
		if data.Language != "" {
			name = fmt.Sprintf("%s (%d lines)", data.Language, data.Lines)
		} else {
			name = fmt.Sprintf("(%d lines)", data.Lines)
		}
	case types.MarkdownLinkData:
		name = data.Text
	case types.ImageData:
		name = data.Alt
	case map[string]any:
		// Try common name fields
		if title, ok := data["title"].(string); ok && title != "" {
			name = title
		} else if nm, ok := data["name"].(string); ok {
			name = nm
		} else if text, ok := data["text"].(string); ok && text != "" {
			name = text
		} else if n.Type == types.TypeTodo {
			// todo nodes have kind + text instead of a name field
			if kind, ok := data["kind"].(string); ok && kind != "" {
				if text, ok := data["text"].(string); ok && text != "" {
					name = kind + ": " + text
					if len(name) > 60 {
						name = name[:57] + "..."
					}
				} else {
					name = kind
				}
			}
		} else if n.Type == types.TypeCommit {
			// commits have sha + message instead of a name field
			if sha, ok := data["sha"].(string); ok && sha != "" {
				short := sha
				if len(sha) >= 8 {
					short = sha[:8]
				}
				if msg, ok := data["message"].(string); ok && msg != "" {
					name = short + " -- " + msg
				} else {
					name = short
				}
			}
		} else if lang, ok := data["language"].(string); ok {
			lines := 0
			if l, ok := data["lines"].(float64); ok {
				lines = int(l)
			}
			if lang != "" {
				name = fmt.Sprintf("%s (%d lines)", lang, lines)
			} else {
				name = fmt.Sprintf("(%d lines)", lines)
			}
		}
	}

	if name != "" {
		return fmt.Sprintf("%s (%s)", name, n.Type)
	}
	return fmt.Sprintf("(%s)", n.Type)
}

// showNodeDetails displays full details of a node
func showNodeDetails(ctx context.Context, g *graph.Graph, node *graph.Node) error {
	// Styled header
	fmt.Printf("%s %s\n", headerLabelStyle.Render("Node:"), headerValueStyle.Render(node.ID))
	fmt.Printf("%s %s\n", headerLabelStyle.Render("Type:"), headerValueStyle.Render(node.Type))

	if node.URI != "" {
		fmt.Printf("%s %s\n", headerLabelStyle.Render("URI: "), node.URI)
	}
	if node.Key != "" {
		fmt.Printf("%s %s\n", headerLabelStyle.Render("Key: "), node.Key)
	}
	if len(node.Labels) > 0 {
		fmt.Printf("%s %s\n", headerLabelStyle.Render("Labels:"), strings.Join(node.Labels, ", "))
	}

	// Print data fields (or render markdown content)
	printNodeData(ctx, g, node)

	// Get and print edges
	edgesFrom, err := g.GetEdgesFrom(ctx, node.ID)
	if err != nil {
		return err
	}

	edgesTo, err := g.GetEdgesTo(ctx, node.ID)
	if err != nil {
		return err
	}

	if len(edgesTo) > 0 {
		fmt.Println("\nEdges (in):")
		for _, e := range edgesTo {
			fromNode, err := g.GetNode(ctx, e.From)
			if err != nil {
				continue
			}
			fmt.Printf("  <- %s [%s] %s\n", e.Type, shortID(fromNode.ID), getNodeSummary(fromNode))
		}
	}

	if len(edgesFrom) > 0 {
		fmt.Println("\nEdges (out):")
		for _, e := range edgesFrom {
			toNode, err := g.GetNode(ctx, e.To)
			if err != nil {
				continue
			}
			fmt.Printf("  -> %s [%s] %s\n", e.Type, shortID(toNode.ID), getNodeSummary(toNode))
		}
	}

	return nil
}

// printNodeData prints the data fields of a node
func printNodeData(ctx context.Context, g *graph.Graph, node *graph.Node) {
	switch data := node.Data.(type) {
	case types.DirData:
		fmt.Println("\nData:")
		fmt.Printf("  Name: %s\n", data.Name)
		fmt.Printf("  Mode: %s\n", data.Mode.String())

	case types.FileData:
		fmt.Println("\nData:")
		fmt.Printf("  Name:     %s\n", data.Name)
		fmt.Printf("  Size:     %s\n", formatSize(data.Size))
		fmt.Printf("  Modified: %s\n", data.Modified.Format(time.RFC3339))
		fmt.Printf("  Mode:     %s\n", data.Mode.String())

	case types.LinkData:
		fmt.Println("\nData:")
		fmt.Printf("  Name:   %s\n", data.Name)
		fmt.Printf("  Target: %s\n", data.Target)

	case types.RepoData:
		fmt.Println("\nData:")
		fmt.Printf("  Name:       %s\n", data.Name)
		fmt.Printf("  IsBare:     %v\n", data.IsBare)
		if data.HeadBranch != "" {
			fmt.Printf("  HeadBranch: %s\n", data.HeadBranch)
		}
		if data.HeadCommit != "" {
			fmt.Printf("  HeadCommit: %s\n", data.HeadCommit)
		}

	case types.RemoteData:
		fmt.Println("\nData:")
		fmt.Printf("  Name: %s\n", data.Name)
		if len(data.URLs) > 0 {
			fmt.Printf("  URLs:\n")
			for _, url := range data.URLs {
				fmt.Printf("    - %s\n", url)
			}
		}

	case types.BranchData:
		fmt.Println("\nData:")
		fmt.Printf("  Name:     %s\n", data.Name)
		fmt.Printf("  IsHead:   %v\n", data.IsHead)
		fmt.Printf("  IsRemote: %v\n", data.IsRemote)
		if data.Commit != "" {
			fmt.Printf("  Commit:   %s\n", data.Commit)
		}

	case types.TagData:
		fmt.Println("\nData:")
		fmt.Printf("  Name: %s\n", data.Name)
		if data.Commit != "" {
			fmt.Printf("  Commit: %s\n", data.Commit)
		}

	case types.CommitData:
		fmt.Println("\nData:")
		if len(data.SHA) >= 8 {
			fmt.Printf("  SHA:     %s\n", data.SHA[:8])
		}
		if data.Message != "" {
			fmt.Printf("  Subject: %s\n", data.Message)
		}
		if data.Body != "" {
			fmt.Println("  Body:")
			for _, line := range strings.Split(data.Body, "\n") {
				fmt.Printf("    %s\n", line)
			}
		}
		if data.AuthorEmail != "" {
			fmt.Printf("  Author:  %s <%s>\n", data.AuthorName, data.AuthorEmail)
		} else if data.AuthorName != "" {
			fmt.Printf("  Author:  %s\n", data.AuthorName)
		}
		if !data.AuthorDate.IsZero() {
			fmt.Printf("  Date:    %s\n", data.AuthorDate.Format("2006-01-02"))
		}
		if data.FilesChanged > 0 {
			fmt.Printf("  Files:   %d changed, +%d -%d lines\n", data.FilesChanged, data.Insertions, data.Deletions)
		}
		if len(data.Parents) > 0 {
			fmt.Printf("  Parents: %d\n", len(data.Parents))
		}

	// Markdown types - render content with glamour
	case types.DocumentData:
		fmt.Println("\nData:")
		fmt.Printf("  Title: %s\n", data.Title)
		// Read and render the source file content
		renderDocumentContent(ctx, g, node)

	case types.SectionData:
		fmt.Println("\nData:")
		fmt.Printf("  Order: %d\n", data.Order)
		// Render section content recursively (heading + content + children)
		renderSectionRecursive(ctx, g, node, data.Level, data.Title, data.Content)

	case types.CodeBlockData:
		// Render code block with syntax highlighting
		renderCodeBlockContent(data)

	case types.MarkdownLinkData:
		// Render as markdown link
		renderLinkContent(data)

	case types.ImageData:
		// Render as markdown image
		renderImageContent(data)

	case map[string]any:
		// Data loaded from JSON - format based on node type
		printMapData(ctx, g, node.Type, node, data)

	default:
		if node.Data != nil {
			fmt.Println("\nData:")
			fmt.Printf("  %+v\n", node.Data)
		} else {
			fmt.Println("\nData:")
			fmt.Printf("  (no data)\n")
		}
	}
}

// renderMarkdown renders markdown content using glamour
func renderMarkdown(content string) string {
	r, err := glamour.NewTermRenderer(
		glamour.WithAutoStyle(),
		glamour.WithWordWrap(120),
	)
	if err != nil {
		return content
	}
	out, err := r.Render(content)
	if err != nil {
		return content
	}
	return strings.TrimSpace(out)
}

// renderDocumentContent reads and renders the source markdown file
func renderDocumentContent(ctx context.Context, g *graph.Graph, node *graph.Node) {
	// Find the source file via incoming has_content edge
	edgesTo, err := g.GetEdgesTo(ctx, node.ID)
	if err != nil {
		return
	}

	for _, e := range edgesTo {
		// Document belongs_to file via ownership edge
		if e.Type == types.EdgeBelongsTo {
			sourceNode, err := g.GetNode(ctx, e.To)
			if err != nil || sourceNode.Type != types.TypeFile {
				continue
			}
			// Extract path from file:///path URI
			filePath := strings.TrimPrefix(sourceNode.URI, "file://")
			content, err := os.ReadFile(filePath)
			if err != nil {
				fmt.Printf("\n(Could not read file: %v)\n", err)
				return
			}
			fmt.Println("\nContent:")
			fmt.Println(renderMarkdown(string(content)))
			return
		}
	}
}

// renderSectionRecursive renders a section with its heading, content, and children recursively
func renderSectionRecursive(ctx context.Context, g *graph.Graph, node *graph.Node, level int, title, content string) {
	// Build markdown for this section
	var md strings.Builder
	headingMarker := strings.Repeat("#", level)
	md.WriteString(fmt.Sprintf("%s %s\n\n", headingMarker, title))
	if content != "" {
		md.WriteString(content)
		md.WriteString("\n\n")
	}

	// Get children and sort by position
	children := getOrderedChildren(ctx, g, node.ID)

	// Render each child
	for _, child := range children {
		switch child.node.Type {
		case types.TypeMarkdownSection:
			// Get section data and render recursively
			childLevel, childTitle, childContent := extractSectionData(child.node)
			childMd := renderSectionToMarkdown(ctx, g, child.node, childLevel, childTitle, childContent)
			md.WriteString(childMd)

		case types.TypeMarkdownCodeBlock:
			// Get code block data and render
			lang, code := extractCodeBlockData(child.node)
			md.WriteString(fmt.Sprintf("```%s\n%s```\n\n", lang, code))
		}
	}

	fmt.Println("\nContent:")
	fmt.Println(renderMarkdown(md.String()))
}

// renderSectionToMarkdown renders a section and its children to a markdown string (for recursive calls)
func renderSectionToMarkdown(ctx context.Context, g *graph.Graph, node *graph.Node, level int, title, content string) string {
	var md strings.Builder
	headingMarker := strings.Repeat("#", level)
	md.WriteString(fmt.Sprintf("%s %s\n\n", headingMarker, title))
	if content != "" {
		md.WriteString(content)
		md.WriteString("\n\n")
	}

	// Get children and sort by position
	children := getOrderedChildren(ctx, g, node.ID)

	// Render each child
	for _, child := range children {
		switch child.node.Type {
		case types.TypeMarkdownSection:
			childLevel, childTitle, childContent := extractSectionData(child.node)
			childMd := renderSectionToMarkdown(ctx, g, child.node, childLevel, childTitle, childContent)
			md.WriteString(childMd)

		case types.TypeMarkdownCodeBlock:
			lang, code := extractCodeBlockData(child.node)
			md.WriteString(fmt.Sprintf("```%s\n%s```\n\n", lang, code))
		}
	}

	return md.String()
}

// orderedChild represents a child node with its position for sorting
type orderedChild struct {
	node     *graph.Node
	position int
}

// getOrderedChildren returns child nodes (sections and code blocks) sorted by position
func getOrderedChildren(ctx context.Context, g *graph.Graph, nodeID string) []orderedChild {
	edges, err := g.GetEdgesFrom(ctx, nodeID)
	if err != nil {
		return nil
	}

	var children []orderedChild
	for _, e := range edges {
		// Only include 'has' edges (ownership)
		if e.Type != types.EdgeHas {
			continue
		}

		childNode, err := g.GetNode(ctx, e.To)
		if err != nil {
			continue
		}

		// Only include sections and code blocks (skip links, images)
		if childNode.Type != types.TypeMarkdownSection && childNode.Type != types.TypeMarkdownCodeBlock {
			continue
		}

		pos := extractPosition(childNode)
		children = append(children, orderedChild{
			node:     childNode,
			position: pos,
		})
	}

	// Sort by position
	sort.Slice(children, func(i, j int) bool {
		return children[i].position < children[j].position
	})

	return children
}

// extractPosition extracts the position from a node's data
func extractPosition(n *graph.Node) int {
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

// extractSectionData extracts level, title, content from a section node
func extractSectionData(n *graph.Node) (level int, title, content string) {
	switch data := n.Data.(type) {
	case types.SectionData:
		return data.Level, data.Title, data.Content
	case map[string]any:
		level = getMapInt(data, "level")
		title = getMapString(data, "title")
		content = getMapString(data, "content")
		return
	}
	return 0, "", ""
}

// extractCodeBlockData extracts language and content from a code block node
func extractCodeBlockData(n *graph.Node) (language, content string) {
	switch data := n.Data.(type) {
	case types.CodeBlockData:
		return data.Language, data.Content
	case map[string]any:
		return getMapString(data, "language"), getMapString(data, "content")
	}
	return "", ""
}

// renderCodeBlockContent renders a code block with syntax highlighting
func renderCodeBlockContent(data types.CodeBlockData) {
	// Wrap in markdown code fence
	md := fmt.Sprintf("```%s\n%s```", data.Language, data.Content)
	fmt.Println("\nContent:")
	fmt.Println(renderMarkdown(md))
}

// renderLinkContent renders a markdown link
func renderLinkContent(data types.MarkdownLinkData) {
	var md string
	if data.Title != "" {
		md = fmt.Sprintf("[%s](%s \"%s\")", data.Text, data.URL, data.Title)
	} else {
		md = fmt.Sprintf("[%s](%s)", data.Text, data.URL)
	}
	fmt.Println("\nContent:")
	fmt.Println(renderMarkdown(md))
}

// renderImageContent renders a markdown image reference
func renderImageContent(data types.ImageData) {
	md := fmt.Sprintf("![%s](%s)", data.Alt, data.URL)
	fmt.Println("\nContent:")
	fmt.Println(renderMarkdown(md))
}

// printMapData formats map data based on node type (for data loaded from JSON)
func printMapData(ctx context.Context, g *graph.Graph, nodeType string, node *graph.Node, data map[string]any) {
	switch nodeType {
	case types.TypeDir:
		fmt.Println("\nData:")
		if name, ok := data["name"].(string); ok {
			fmt.Printf("  Name: %s\n", name)
		}
		if mode, ok := data["mode"].(float64); ok {
			fmt.Printf("  Mode: %s\n", os.FileMode(uint32(mode)).String())
		}

	case types.TypeFile:
		fmt.Println("\nData:")
		if name, ok := data["name"].(string); ok {
			fmt.Printf("  Name:     %s\n", name)
		}
		if size, ok := data["size"].(float64); ok {
			fmt.Printf("  Size:     %s\n", formatSize(int64(size)))
		}
		if modified, ok := data["modified"].(string); ok {
			fmt.Printf("  Modified: %s\n", modified)
		}
		if mode, ok := data["mode"].(float64); ok {
			fmt.Printf("  Mode:     %s\n", os.FileMode(uint32(mode)).String())
		}

	case types.TypeLink:
		fmt.Println("\nData:")
		if name, ok := data["name"].(string); ok {
			fmt.Printf("  Name:   %s\n", name)
		}
		if target, ok := data["target"].(string); ok {
			fmt.Printf("  Target: %s\n", target)
		}

	case types.TypeRepo:
		fmt.Println("\nData:")
		if name, ok := data["name"].(string); ok {
			fmt.Printf("  Name:       %s\n", name)
		}
		if isBare, ok := data["is_bare"].(bool); ok {
			fmt.Printf("  IsBare:     %v\n", isBare)
		}
		if headBranch, ok := data["head_branch"].(string); ok && headBranch != "" {
			fmt.Printf("  HeadBranch: %s\n", headBranch)
		}
		if headCommit, ok := data["head_commit"].(string); ok && headCommit != "" {
			fmt.Printf("  HeadCommit: %s\n", headCommit)
		}

	case types.TypeRemote:
		fmt.Println("\nData:")
		if name, ok := data["name"].(string); ok {
			fmt.Printf("  Name: %s\n", name)
		}
		if urls, ok := data["urls"].([]any); ok && len(urls) > 0 {
			fmt.Printf("  URLs:\n")
			for _, url := range urls {
				if urlStr, ok := url.(string); ok {
					fmt.Printf("    - %s\n", urlStr)
				}
			}
		}

	case types.TypeBranch:
		fmt.Println("\nData:")
		if name, ok := data["name"].(string); ok {
			fmt.Printf("  Name:     %s\n", name)
		}
		if isHead, ok := data["is_head"].(bool); ok {
			fmt.Printf("  IsHead:   %v\n", isHead)
		}
		if isRemote, ok := data["is_remote"].(bool); ok {
			fmt.Printf("  IsRemote: %v\n", isRemote)
		}
		if commit, ok := data["commit"].(string); ok && commit != "" {
			fmt.Printf("  Commit:   %s\n", commit)
		}

	case types.TypeTag:
		fmt.Println("\nData:")
		if name, ok := data["name"].(string); ok {
			fmt.Printf("  Name: %s\n", name)
		}
		if commit, ok := data["commit"].(string); ok && commit != "" {
			fmt.Printf("  Commit: %s\n", commit)
		}

	case types.TypeCommit:
		fmt.Println("\nData:")
		sha := getMapString(data, "sha")
		if len(sha) >= 8 {
			fmt.Printf("  SHA:     %s\n", sha[:8])
		}
		if msg := getMapString(data, "message"); msg != "" {
			fmt.Printf("  Subject: %s\n", msg)
		}
		if body := getMapString(data, "body"); body != "" {
			fmt.Println("  Body:")
			for _, line := range strings.Split(body, "\n") {
				fmt.Printf("    %s\n", line)
			}
		}
		author := getMapString(data, "author_name")
		email := getMapString(data, "author_email")
		if author != "" {
			if email != "" {
				fmt.Printf("  Author:  %s <%s>\n", author, email)
			} else {
				fmt.Printf("  Author:  %s\n", author)
			}
		}
		if dateStr := getMapString(data, "author_date"); dateStr != "" {
			if t, err := time.Parse(time.RFC3339, dateStr); err == nil {
				fmt.Printf("  Date:    %s\n", t.Format("2006-01-02"))
			} else {
				fmt.Printf("  Date:    %s\n", dateStr)
			}
		}
		fc := int(getMapFloat(data, "files_changed"))
		if fc > 0 {
			ins := int(getMapFloat(data, "insertions"))
			del := int(getMapFloat(data, "deletions"))
			fmt.Printf("  Files:   %d changed, +%d -%d lines\n", fc, ins, del)
		}
		if parents, ok := data["parents"].([]any); ok && len(parents) > 0 {
			fmt.Printf("  Parents: %d\n", len(parents))
		}

	case types.TypeLicense:
		fmt.Println("\nData:")
		spdxID := getMapString(data, "spdx_id")
		if spdxID == "" {
			spdxID = "(unknown)"
		}
		fmt.Printf("  SPDX ID:    %s\n", spdxID)
		fmt.Printf("  Confidence: %s\n", getMapString(data, "confidence"))
		fmt.Printf("  File:       %s\n", getMapString(data, "file"))

	case types.TypeTodo:
		fmt.Println("\nData:")
		if kind := getMapString(data, "kind"); kind != "" {
			fmt.Printf("  Kind:    %s\n", kind)
		}
		if text := getMapString(data, "text"); text != "" {
			fmt.Printf("  Text:    %s\n", text)
		}
		if file := getMapString(data, "file"); file != "" {
			fmt.Printf("  File:    %s\n", file)
		}
		if line := int(getMapFloat(data, "line")); line > 0 {
			fmt.Printf("  Line:    %d\n", line)
		}
		if ctx := getMapString(data, "context"); ctx != "" {
			fmt.Printf("  Context: %s\n", ctx)
		}

	// Markdown types from JSON
	case types.TypeMarkdownDoc:
		fmt.Println("\nData:")
		if title, ok := data["title"].(string); ok {
			fmt.Printf("  Title: %s\n", title)
		}
		renderDocumentContent(ctx, g, node)

	case types.TypeMarkdownSection:
		fmt.Println("\nData:")
		if order, ok := data["order"].(float64); ok {
			fmt.Printf("  Order: %d\n", int(order))
		}
		// Build SectionData from map and render recursively
		level := getMapInt(data, "level")
		title := getMapString(data, "title")
		content := getMapString(data, "content")
		renderSectionRecursive(ctx, g, node, level, title, content)

	case types.TypeMarkdownCodeBlock:
		// Render code block with syntax highlighting
		cbData := types.CodeBlockData{
			Language: getMapString(data, "language"),
			Content:  getMapString(data, "content"),
		}
		renderCodeBlockContent(cbData)

	case types.TypeMarkdownLink:
		// Render as markdown link
		linkData := types.MarkdownLinkData{
			URL:   getMapString(data, "url"),
			Text:  getMapString(data, "text"),
			Title: getMapString(data, "title"),
		}
		renderLinkContent(linkData)

	case types.TypeMarkdownImage:
		// Render as markdown image
		imgData := types.ImageData{
			URL: getMapString(data, "url"),
			Alt: getMapString(data, "alt"),
		}
		renderImageContent(imgData)

	default:
		// Generic fallback
		fmt.Println("\nData:")
		for k, v := range data {
			fmt.Printf("  %s: %v\n", k, v)
		}
	}
}

// getMapString extracts a string from a map, returning empty string if not found
func getMapString(data map[string]any, key string) string {
	if v, ok := data[key].(string); ok {
		return v
	}
	return ""
}

// getMapInt extracts an int from a map (JSON numbers come as float64)
func getMapInt(data map[string]any, key string) int {
	if v, ok := data[key].(float64); ok {
		return int(v)
	}
	return 0
}

// formatSize formats a byte count as a human-readable string
func formatSize(bytes int64) string {
	const unit = 1024
	if bytes < unit {
		return fmt.Sprintf("%d B", bytes)
	}
	div, exp := int64(unit), 0
	for n := bytes / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %cB", float64(bytes)/float64(div), "KMGTPE"[exp])
}

// shortID returns a shortened version of the node ID
func shortID(id string) string {
	if len(id) > 7 {
		return id[:7]
	}
	return id
}

// getMapFloat extracts a float64 from a map (JSON numbers are always float64).
func getMapFloat(data map[string]any, key string) float64 {
	if v, ok := data[key].(float64); ok {
		return v
	}
	return 0
}
