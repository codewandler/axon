package markdown

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/text"

	"github.com/codewandler/axon/graph"
	"github.com/codewandler/axon/indexer"
	"github.com/codewandler/axon/progress"
	"github.com/codewandler/axon/types"
)

// Indexer indexes markdown files, extracting structure (headings, code blocks, links, images).
type Indexer struct {
	mu           sync.Mutex
	pendingLinks []pendingLink
}

type pendingLink struct {
	fromNodeID string
	targetPath string // Relative path from markdown file
	basePath   string // Directory containing the markdown file
	generation string
}

// New creates a new markdown indexer.
func New() *Indexer {
	return &Indexer{}
}

func (i *Indexer) Name() string { return "markdown" }

func (i *Indexer) Schemes() []string { return []string{"file+md"} }

func (i *Indexer) Handles(uri string) bool {
	return strings.HasPrefix(uri, "file+md://")
}

func (i *Indexer) Subscriptions() []indexer.Subscription {
	return []indexer.Subscription{
		// Index markdown files when visited
		{
			EventType:  indexer.EventEntryVisited,
			NodeType:   types.TypeFile,
			Extensions: []string{".md", ".markdown"},
		},
		// Cleanup when markdown files are deleted
		{
			EventType:  indexer.EventNodeDeleting,
			NodeType:   types.TypeFile,
			Extensions: []string{".md", ".markdown"},
		},
	}
}

func (i *Indexer) Index(ctx context.Context, ictx *indexer.Context) error {
	// Markdown indexer is event-driven only, direct invocation is a no-op
	return nil
}

func (i *Indexer) HandleEvent(ctx context.Context, ictx *indexer.Context, event indexer.Event) error {
	// Handle deletion
	if event.Type == indexer.EventNodeDeleting {
		return i.cleanupEvent(ctx, ictx, event)
	}

	// Index the markdown file
	return i.indexFileEvent(ctx, ictx, event)
}

func (i *Indexer) indexFileEvent(ctx context.Context, ictx *indexer.Context, event indexer.Event) error {
	filePath := event.Path
	fileNodeID := event.NodeID

	// Read file content
	content, err := os.ReadFile(filePath)
	if err != nil {
		return err
	}

	// Parse markdown
	md := goldmark.New()
	reader := text.NewReader(content)
	doc := md.Parser().Parse(reader)

	// Create document node
	docURI := types.MarkdownFileToURI(filePath)
	title := i.extractTitle(doc, content, filePath)
	docNode := graph.NewNode(types.TypeMarkdownDoc).
		WithURI(docURI).
		WithKey(filePath).
		WithName(title).
		WithData(types.DocumentData{Title: title})

	if err := ictx.Emitter.EmitNode(ctx, docNode); err != nil {
		return err
	}

	// Link from fs:file to md:document (ownership - file has parsed content)
	if fileNodeID != "" {
		if err := indexer.EmitOwnership(ctx, ictx.Emitter, fileNodeID, docNode.ID); err != nil {
			return err
		}
	}

	// Walk AST and build nested structure
	walker := &astWalker{
		indexer:       i,
		ctx:           ctx,
		ictx:          ictx,
		filePath:      filePath,
		content:       content,
		docNodeID:     docNode.ID,
		sectionStack:  []sectionInfo{{nodeID: docNode.ID, level: 0, path: ""}},
		sectionOrder:  make(map[int]int),
		childPosition: make(map[string]int),
	}

	return walker.walk(doc)
}

// cleanupEvent removes all markdown nodes for the given file.
func (i *Indexer) cleanupEvent(ctx context.Context, ictx *indexer.Context, event indexer.Event) error {
	filePath := event.Path
	docURI := types.MarkdownFileToURI(filePath)

	// Delete all nodes under this document's URI prefix and track count
	deleted, err := ictx.Graph.Storage().DeleteByURIPrefix(ctx, docURI)
	if deleted > 0 {
		ictx.AddNodesDeleted(deleted)
	}
	return err
}

// PostIndex resolves local file links after all indexers have completed.
func (i *Indexer) PostIndex(ctx context.Context, ictx *indexer.Context) error {
	i.mu.Lock()
	links := i.pendingLinks
	i.pendingLinks = nil
	i.mu.Unlock()

	total := len(links)
	if total == 0 {
		return nil
	}

	// Report start
	if ictx.Progress != nil {
		ictx.Progress <- progress.Started(i.Name())
	}

	resolved := 0
	lastProgressTime := time.Now()

	for idx, link := range links {
		// Skip if from a different generation (stale)
		if link.generation != ictx.Generation {
			continue
		}

		// Resolve relative path to absolute
		targetPath := filepath.Clean(filepath.Join(link.basePath, link.targetPath))
		targetURI := types.PathToURI(targetPath)

		// Compute target node ID directly (avoid read during write)
		targetID := graph.IDFromURI(targetURI)

		// Create links_to edge
		edge := graph.NewEdge(types.EdgeLinksTo, link.fromNodeID, targetID)
		if err := ictx.Emitter.EmitEdge(ctx, edge); err != nil {
			// Log but continue
			continue
		}
		resolved++

		// Progress reporting: every 100 items OR every 100ms
		now := time.Now()
		if ictx.Progress != nil && (idx%100 == 0 || now.Sub(lastProgressTime) > 100*time.Millisecond) {
			ictx.Progress <- progress.ProgressWithTotal(i.Name(), idx+1, total, "resolving links...")
			lastProgressTime = now
		}
	}

	// Report completion
	if ictx.Progress != nil {
		ictx.Progress <- progress.Completed(i.Name(), resolved)
	}

	return nil
}

func (i *Indexer) extractTitle(doc ast.Node, content []byte, filePath string) string {
	// Find first H1
	var title string
	_ = ast.Walk(doc, func(n ast.Node, entering bool) (ast.WalkStatus, error) {
		if entering {
			if h, ok := n.(*ast.Heading); ok && h.Level == 1 {
				title = extractHeadingText(h, content)
				return ast.WalkStop, nil
			}
		}
		return ast.WalkContinue, nil
	})

	if title != "" {
		return title
	}

	// Fallback to filename
	return strings.TrimSuffix(filepath.Base(filePath), filepath.Ext(filePath))
}

// sectionInfo tracks the current section hierarchy during AST walking.
type sectionInfo struct {
	nodeID       string
	level        int
	path         string // e.g., "h1-intro/h2-setup"
}

// astWalker walks the markdown AST and emits nodes/edges.
type astWalker struct {
	indexer            *Indexer
	ctx                context.Context
	ictx               *indexer.Context
	filePath           string
	content            []byte
	docNodeID          string
	sectionStack       []sectionInfo
	sectionOrder       map[int]int    // level -> count at that level
	childPosition      map[string]int // parent node ID -> next child position
	codeBlockIdx       int
	headingPositions   []headingPosition
	codeBlockPositions []int // byte positions where code blocks start
	headingIdx         int   // Current heading index for getSectionContentRange
}

func (w *astWalker) walk(doc ast.Node) error {
	// First pass: collect heading positions for section content extraction
	w.collectHeadingPositions(doc)

	// Second pass: emit nodes and edges
	return ast.Walk(doc, func(n ast.Node, entering bool) (ast.WalkStatus, error) {
		if !entering {
			return ast.WalkContinue, nil
		}

		var err error
		switch node := n.(type) {
		case *ast.Heading:
			err = w.handleHeading(node)
		case *ast.FencedCodeBlock:
			err = w.handleCodeBlock(node)
		case *ast.Link:
			err = w.handleLink(node)
		case *ast.Image:
			err = w.handleImage(node)
		}

		if err != nil {
			return ast.WalkStop, err
		}
		return ast.WalkContinue, nil
	})
}

// headingPosition stores the byte positions of a heading.
type headingPosition struct {
	level    int
	startPos int // Where the heading line starts (including # markers)
	endPos   int // Where the heading line ends (including newline)
}

// collectHeadingPositions walks the AST to find all heading and code block positions.
func (w *astWalker) collectHeadingPositions(doc ast.Node) {
	var headings []headingPosition
	var codeBlocks []int

	_ = ast.Walk(doc, func(n ast.Node, entering bool) (ast.WalkStatus, error) {
		if !entering {
			return ast.WalkContinue, nil
		}
		if h, ok := n.(*ast.Heading); ok {
			// Get the byte range of the heading
			// Note: h.Lines() returns the heading TEXT content (after # markers)
			// We need to calculate the full line start/end
			lines := h.Lines()
			if lines.Len() > 0 {
				startLine := lines.At(0)
				endLine := lines.At(lines.Len() - 1)
				// startLine.Start is after "# " (level number of # plus space)
				// So the actual heading line starts at: startLine.Start - h.Level - 1
				lineStart := startLine.Start - h.Level - 1
				if lineStart < 0 {
					lineStart = 0
				}
				// The heading line ends after the text + newline
				lineEnd := endLine.Stop
				// Skip past the newline if present
				if lineEnd < len(w.content) && w.content[lineEnd] == '\n' {
					lineEnd++
				}
				headings = append(headings, headingPosition{
					level:    h.Level,
					startPos: lineStart,
					endPos:   lineEnd,
				})
			}
		}
		if cb, ok := n.(*ast.FencedCodeBlock); ok {
			// Get the start position of the code block (the ``` line)
			// cb.Info.Segment gives us the language identifier position
			// The fence (```) starts 3 characters before that
			if cb.Info != nil && cb.Info.Segment.Start >= 3 {
				fenceStart := cb.Info.Segment.Start - 3
				codeBlocks = append(codeBlocks, fenceStart)
			} else {
				// Fallback: use first content line and walk back
				lines := cb.Lines()
				if lines.Len() > 0 {
					firstLine := lines.At(0)
					fenceStart := firstLine.Start
					for fenceStart > 0 && w.content[fenceStart-1] != '\n' {
						fenceStart--
					}
					codeBlocks = append(codeBlocks, fenceStart)
				}
			}
		}
		return ast.WalkContinue, nil
	})

	w.headingPositions = headings
	w.codeBlockPositions = codeBlocks
}

// getSectionContentRange returns the content start and end positions for a section.
// contentStart is after the heading, contentEnd is before the first child (code block or heading).
func (w *astWalker) getSectionContentRange(headingIdx int) (start, end int) {
	if headingIdx >= len(w.headingPositions) {
		return 0, 0
	}

	pos := w.headingPositions[headingIdx]
	start = pos.endPos // Content starts after heading line

	// Find where this section's "own content" ends
	// It ends at the first child: either a code block or a child heading
	end = len(w.content) // Default to end of file

	// Check for first child heading
	if headingIdx+1 < len(w.headingPositions) {
		nextPos := w.headingPositions[headingIdx+1]
		// Any heading (child or sibling) ends our content
		if nextPos.startPos < end {
			end = nextPos.startPos
		}
	}

	// Check for first code block after our start position
	for _, cbPos := range w.codeBlockPositions {
		if cbPos > start && cbPos < end {
			end = cbPos
			break // Code blocks are in order, so first match is what we want
		}
	}

	return start, end
}

func (w *astWalker) handleHeading(node *ast.Heading) error {
	level := node.Level
	title := extractHeadingText(node, w.content)
	slug := slugify(title)

	// Pop sections that are same level or deeper (higher number = deeper)
	for len(w.sectionStack) > 1 && w.sectionStack[len(w.sectionStack)-1].level >= level {
		w.sectionStack = w.sectionStack[:len(w.sectionStack)-1]
	}

	// Determine parent
	parent := w.sectionStack[len(w.sectionStack)-1]

	// Build section path
	sectionPath := fmt.Sprintf("h%d-%s", level, slug)
	if parent.path != "" {
		sectionPath = parent.path + "/" + sectionPath
	}

	// Track order among siblings (sections only)
	w.sectionOrder[level]++
	order := w.sectionOrder[level]

	// Reset child counters
	for l := level + 1; l <= 6; l++ {
		w.sectionOrder[l] = 0
	}

	// Get position among ALL children of parent (sections + code blocks)
	position := w.childPosition[parent.nodeID]
	w.childPosition[parent.nodeID]++

	// Get section content range and extract content
	contentStart, contentEnd := w.getSectionContentRange(w.headingIdx)
	w.headingIdx++

	content := ""
	if contentStart < contentEnd && contentEnd <= len(w.content) {
		content = strings.TrimSpace(string(w.content[contentStart:contentEnd]))
	}

	// Create section node
	sectionURI := types.MarkdownSectionURI(w.filePath, sectionPath)
	sectionNode := graph.NewNode(types.TypeMarkdownSection).
		WithURI(sectionURI).
		WithKey(sectionPath).
		WithName(title).
		WithData(types.SectionData{
			Title:    title,
			Level:    level,
			Order:    order,
			Position: position,
			Content:  content,
		})

	if err := w.ictx.Emitter.EmitNode(w.ctx, sectionNode); err != nil {
		return err
	}

	// Ownership edge from parent (document/section owns child section)
	if err := indexer.EmitOwnership(w.ctx, w.ictx.Emitter, parent.nodeID, sectionNode.ID); err != nil {
		return err
	}

	// Push to stack
	w.sectionStack = append(w.sectionStack, sectionInfo{
		nodeID: sectionNode.ID,
		level:  level,
		path:   sectionPath,
	})

	return nil
}

func (w *astWalker) handleCodeBlock(node *ast.FencedCodeBlock) error {
	lang := string(node.Language(w.content))

	// Extract content
	var contentBuilder strings.Builder
	lines := node.Lines()
	for i := 0; i < lines.Len(); i++ {
		line := lines.At(i)
		contentBuilder.Write(line.Value(w.content))
	}
	codeContent := contentBuilder.String()
	lineCount := lines.Len()

	// Content hash (first 8 bytes of SHA256)
	hash := sha256.Sum256([]byte(codeContent))
	contentHash := hex.EncodeToString(hash[:8])

	// Current parent (most recent section or doc)
	parent := w.sectionStack[len(w.sectionStack)-1]

	// Get position among ALL children of parent (sections + code blocks)
	position := w.childPosition[parent.nodeID]
	w.childPosition[parent.nodeID]++

	// Build ID: section-path/cb-index-hash
	w.codeBlockIdx++
	var cbID string
	if parent.path != "" {
		cbID = fmt.Sprintf("%s/cb-%d-%s", parent.path, w.codeBlockIdx, contentHash)
	} else {
		cbID = fmt.Sprintf("cb-%d-%s", w.codeBlockIdx, contentHash)
	}

	cbURI := types.MarkdownCodeBlockURI(w.filePath, cbID)
	cbNode := graph.NewNode(types.TypeMarkdownCodeBlock).
		WithURI(cbURI).
		WithKey(cbID).
		WithData(types.CodeBlockData{
			Language:    lang,
			Content:     codeContent,
			Lines:       lineCount,
			ContentHash: contentHash,
			Position:    position,
		})

	if err := w.ictx.Emitter.EmitNode(w.ctx, cbNode); err != nil {
		return err
	}

	// Ownership edge from parent (section owns code block)
	return indexer.EmitOwnership(w.ctx, w.ictx.Emitter, parent.nodeID, cbNode.ID)
}

func (w *astWalker) handleLink(node *ast.Link) error {
	destination := string(node.Destination)
	linkText := extractLinkText(node, w.content)
	title := string(node.Title)

	parent := w.sectionStack[len(w.sectionStack)-1]

	// Check if external URL or local file reference
	if strings.HasPrefix(destination, "http://") || strings.HasPrefix(destination, "https://") {
		// External link - create md:link node
		linkURI := types.MarkdownLinkURI(w.filePath, destination)
		linkNode := graph.NewNode(types.TypeMarkdownLink).
			WithURI(linkURI).
			WithKey(destination).
			WithData(types.MarkdownLinkData{
				URL:   destination,
				Text:  linkText,
				Title: title,
			})

		if err := w.ictx.Emitter.EmitNode(w.ctx, linkNode); err != nil {
			return err
		}

		edge := graph.NewEdge(types.EdgeLinksTo, parent.nodeID, linkNode.ID)
		return w.ictx.Emitter.EmitEdge(w.ctx, edge)
	} else if !strings.HasPrefix(destination, "#") {
		// Local file reference - defer to PostIndex
		// Strip any anchor from the path
		targetPath := destination
		if idx := strings.Index(targetPath, "#"); idx != -1 {
			targetPath = targetPath[:idx]
		}

		w.indexer.mu.Lock()
		w.indexer.pendingLinks = append(w.indexer.pendingLinks, pendingLink{
			fromNodeID: parent.nodeID,
			targetPath: targetPath,
			basePath:   filepath.Dir(w.filePath),
			generation: w.ictx.Generation,
		})
		w.indexer.mu.Unlock()
	}
	// Ignore anchor-only links (#section) for now

	return nil
}

func (w *astWalker) handleImage(node *ast.Image) error {
	destination := string(node.Destination)
	alt := extractImageAlt(node, w.content)

	parent := w.sectionStack[len(w.sectionStack)-1]

	imgURI := types.MarkdownImageURI(w.filePath, destination)
	imgNode := graph.NewNode(types.TypeMarkdownImage).
		WithURI(imgURI).
		WithKey(destination).
		WithData(types.ImageData{
			URL: destination,
			Alt: alt,
		})

	if err := w.ictx.Emitter.EmitNode(w.ctx, imgNode); err != nil {
		return err
	}

	// Ownership edge from parent (section owns image)
	return indexer.EmitOwnership(w.ctx, w.ictx.Emitter, parent.nodeID, imgNode.ID)
}

// extractHeadingText extracts the text content of a heading node.
func extractHeadingText(h *ast.Heading, source []byte) string {
	var text strings.Builder
	for child := h.FirstChild(); child != nil; child = child.NextSibling() {
		if t, ok := child.(*ast.Text); ok {
			text.Write(t.Segment.Value(source))
		}
	}
	return text.String()
}

// extractLinkText extracts the text content of a link node.
func extractLinkText(l *ast.Link, source []byte) string {
	var text strings.Builder
	for child := l.FirstChild(); child != nil; child = child.NextSibling() {
		if t, ok := child.(*ast.Text); ok {
			text.Write(t.Segment.Value(source))
		}
	}
	return text.String()
}

// extractImageAlt extracts the alt text of an image node.
func extractImageAlt(img *ast.Image, source []byte) string {
	var text strings.Builder
	for child := img.FirstChild(); child != nil; child = child.NextSibling() {
		if t, ok := child.(*ast.Text); ok {
			text.Write(t.Segment.Value(source))
		}
	}
	return text.String()
}

// slugify converts a string to a URL-friendly slug.
func slugify(s string) string {
	s = strings.ToLower(s)
	s = strings.ReplaceAll(s, " ", "-")
	// Remove non-alphanumeric except hyphens
	var result strings.Builder
	for _, r := range s {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '-' {
			result.WriteRune(r)
		}
	}
	return result.String()
}
