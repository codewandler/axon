package markdown

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/codewandler/axon/adapters/sqlite"
	"github.com/codewandler/axon/graph"
	"github.com/codewandler/axon/indexer"
	"github.com/codewandler/axon/types"
)

func setupGraph(t *testing.T) *graph.Graph {
	t.Helper()
	r := graph.NewRegistry()
	types.RegisterCommonEdges(r)
	types.RegisterFSTypes(r)
	types.RegisterMarkdownTypes(r)
	s, err := sqlite.New(":memory:")
	if err != nil {
		t.Fatalf("sqlite.New failed: %v", err)
	}
	t.Cleanup(func() { s.Close() })
	return graph.New(s, r)
}

func setupTestFile(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()

	content := `# My Document

This is the intro.

## Getting Started

Some text here.

` + "```" + `bash
echo "hello world"
` + "```" + `

### Installation

Install with:

` + "```" + `go
go install example.com/tool
` + "```" + `

## API Reference

Check out [the docs](https://example.com/docs).

Also see [local file](./other.md).

![Logo](https://example.com/logo.png)
`

	mdFile := filepath.Join(dir, "README.md")
	if err := os.WriteFile(mdFile, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	// Create the "other.md" file for local link resolution
	otherFile := filepath.Join(dir, "other.md")
	if err := os.WriteFile(otherFile, []byte("# Other\n"), 0644); err != nil {
		t.Fatal(err)
	}

	return mdFile
}

func TestIndexerBasic(t *testing.T) {
	ctx := context.Background()
	mdFile := setupTestFile(t)
	g := setupGraph(t)

	// First, create a fake fs:file node for the markdown file
	fileNode := graph.NewNode(types.TypeFile).
		WithURI(types.PathToURI(mdFile)).
		WithKey(mdFile).
		WithData(types.FileData{Name: "README.md", Ext: ".md"})
	if err := g.Storage().PutNode(ctx, fileNode); err != nil {
		t.Fatal(err)
	}

	idx := New()
	emitter := indexer.NewGraphEmitter(g, "gen-1")

	// Simulate the trigger event from fs indexer
	event := &indexer.Event{
		Type:     indexer.EventEntryVisited,
		URI:      types.PathToURI(mdFile),
		Path:     mdFile,
		Name:     "README.md",
		NodeType: types.TypeFile,
		NodeID:   fileNode.ID,
	}

	ictx := &indexer.Context{
		Root:         types.PathToURI(mdFile),
		Generation:   "gen-1",
		Graph:        g,
		Emitter:      emitter,
		TriggerEvent: event,
	}

	if err := idx.Index(ctx, ictx); err != nil {
		t.Fatalf("Index failed: %v", err)
	}

	// Flush to ensure all nodes are written
	if err := g.Storage().Flush(ctx); err != nil {
		t.Fatal(err)
	}

	// Check document node
	docNodes, err := g.FindNodes(ctx, graph.NodeFilter{Type: types.TypeMarkdownDoc})
	if err != nil {
		t.Fatal(err)
	}
	if len(docNodes) != 1 {
		t.Fatalf("expected 1 document node, got %d", len(docNodes))
	}

	// Check sections
	sectionNodes, err := g.FindNodes(ctx, graph.NodeFilter{Type: types.TypeMarkdownSection})
	if err != nil {
		t.Fatal(err)
	}
	// Expected: My Document (h1), Getting Started (h2), Installation (h3), API Reference (h2)
	if len(sectionNodes) != 4 {
		t.Errorf("expected 4 section nodes, got %d", len(sectionNodes))
		for _, n := range sectionNodes {
			t.Logf("  section: %s", n.URI)
		}
	}

	// Check code blocks
	codeNodes, err := g.FindNodes(ctx, graph.NodeFilter{Type: types.TypeMarkdownCodeBlock})
	if err != nil {
		t.Fatal(err)
	}
	// Expected: bash echo, go install
	if len(codeNodes) != 2 {
		t.Errorf("expected 2 code block nodes, got %d", len(codeNodes))
	}

	// Check external links
	linkNodes, err := g.FindNodes(ctx, graph.NodeFilter{Type: types.TypeMarkdownLink})
	if err != nil {
		t.Fatal(err)
	}
	// Expected: https://example.com/docs
	if len(linkNodes) != 1 {
		t.Errorf("expected 1 link node, got %d", len(linkNodes))
	}

	// Check images
	imgNodes, err := g.FindNodes(ctx, graph.NodeFilter{Type: types.TypeMarkdownImage})
	if err != nil {
		t.Fatal(err)
	}
	if len(imgNodes) != 1 {
		t.Errorf("expected 1 image node, got %d", len(imgNodes))
	}
}

func TestIndexerPostIndex(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	g := setupGraph(t)

	// Create markdown file with local link
	mdContent := `# Main

See [other](./other.md).
`
	mdFile := filepath.Join(dir, "main.md")
	if err := os.WriteFile(mdFile, []byte(mdContent), 0644); err != nil {
		t.Fatal(err)
	}

	// Create the target file
	otherFile := filepath.Join(dir, "other.md")
	if err := os.WriteFile(otherFile, []byte("# Other\n"), 0644); err != nil {
		t.Fatal(err)
	}

	// Create fs:file nodes for both files
	mainFileNode := graph.NewNode(types.TypeFile).
		WithURI(types.PathToURI(mdFile)).
		WithKey(mdFile).
		WithData(types.FileData{Name: "main.md", Ext: ".md"})
	if err := g.Storage().PutNode(ctx, mainFileNode); err != nil {
		t.Fatal(err)
	}

	otherFileNode := graph.NewNode(types.TypeFile).
		WithURI(types.PathToURI(otherFile)).
		WithKey(otherFile).
		WithData(types.FileData{Name: "other.md", Ext: ".md"})
	if err := g.Storage().PutNode(ctx, otherFileNode); err != nil {
		t.Fatal(err)
	}

	idx := New()
	emitter := indexer.NewGraphEmitter(g, "gen-1")

	// Index main.md
	event := &indexer.Event{
		Type:     indexer.EventEntryVisited,
		URI:      types.PathToURI(mdFile),
		Path:     mdFile,
		Name:     "main.md",
		NodeType: types.TypeFile,
		NodeID:   mainFileNode.ID,
	}

	ictx := &indexer.Context{
		Root:         types.PathToURI(mdFile),
		Generation:   "gen-1",
		Graph:        g,
		Emitter:      emitter,
		TriggerEvent: event,
	}

	if err := idx.Index(ctx, ictx); err != nil {
		t.Fatalf("Index failed: %v", err)
	}

	// Flush before PostIndex
	if err := g.Storage().Flush(ctx); err != nil {
		t.Fatal(err)
	}

	// Run PostIndex to resolve local links
	postCtx := &indexer.Context{
		Root:       types.PathToURI(dir),
		Generation: "gen-1",
		Graph:      g,
		Emitter:    emitter,
	}

	if err := idx.PostIndex(ctx, postCtx); err != nil {
		t.Fatalf("PostIndex failed: %v", err)
	}

	// Flush after PostIndex
	if err := g.Storage().Flush(ctx); err != nil {
		t.Fatal(err)
	}

	// Find the document node
	docNodes, err := g.FindNodes(ctx, graph.NodeFilter{Type: types.TypeMarkdownDoc})
	if err != nil {
		t.Fatal(err)
	}
	if len(docNodes) != 1 {
		t.Fatalf("expected 1 document node, got %d", len(docNodes))
	}

	// The link is from the h1 section (which contains the link), not the document
	sectionNodes, err := g.FindNodes(ctx, graph.NodeFilter{Type: types.TypeMarkdownSection})
	if err != nil {
		t.Fatal(err)
	}
	if len(sectionNodes) != 1 {
		t.Fatalf("expected 1 section node, got %d", len(sectionNodes))
	}

	// Check that links_to edge was created from section to other.md
	edges, err := g.GetEdgesFrom(ctx, sectionNodes[0].ID)
	if err != nil {
		t.Fatal(err)
	}

	var foundLinksTo bool
	for _, e := range edges {
		if e.Type == types.EdgeLinksTo && e.To == otherFileNode.ID {
			foundLinksTo = true
			break
		}
	}

	if !foundLinksTo {
		t.Errorf("expected links_to edge from section to other.md, got edges: %+v", edges)
	}
}

func TestSlugify(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"Hello World", "hello-world"},
		{"Getting Started!", "getting-started"},
		{"API Reference (v2)", "api-reference-v2"},
		{"Under_score", "underscore"},
		{"Numbers 123", "numbers-123"},
	}

	for _, tc := range tests {
		got := slugify(tc.input)
		if got != tc.expected {
			t.Errorf("slugify(%q) = %q, want %q", tc.input, got, tc.expected)
		}
	}
}

func TestSectionContent(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	g := setupGraph(t)

	// Create markdown with clear section boundaries
	mdContent := `# Main Title

This is the intro paragraph.

## First Section

Content of first section.
More content here.

### Nested Section

Content of nested section.

## Second Section

Content of second section.
`
	mdFile := filepath.Join(dir, "test.md")
	if err := os.WriteFile(mdFile, []byte(mdContent), 0644); err != nil {
		t.Fatal(err)
	}

	// Create fs:file node
	fileNode := graph.NewNode(types.TypeFile).
		WithURI(types.PathToURI(mdFile)).
		WithKey(mdFile).
		WithData(types.FileData{Name: "test.md", Ext: ".md"})
	if err := g.Storage().PutNode(ctx, fileNode); err != nil {
		t.Fatal(err)
	}

	idx := New()
	emitter := indexer.NewGraphEmitter(g, "gen-1")

	event := &indexer.Event{
		Type:     indexer.EventEntryVisited,
		URI:      types.PathToURI(mdFile),
		Path:     mdFile,
		Name:     "test.md",
		NodeType: types.TypeFile,
		NodeID:   fileNode.ID,
	}

	ictx := &indexer.Context{
		Root:         types.PathToURI(mdFile),
		Generation:   "gen-1",
		Graph:        g,
		Emitter:      emitter,
		TriggerEvent: event,
	}

	if err := idx.Index(ctx, ictx); err != nil {
		t.Fatalf("Index failed: %v", err)
	}

	if err := g.Storage().Flush(ctx); err != nil {
		t.Fatal(err)
	}

	// Find all sections
	sections, err := g.FindNodes(ctx, graph.NodeFilter{Type: types.TypeMarkdownSection})
	if err != nil {
		t.Fatal(err)
	}

	// Expected: Main Title, First Section, Nested Section, Second Section
	if len(sections) != 4 {
		t.Fatalf("expected 4 sections, got %d", len(sections))
	}

	// Build a map of section title -> data for testing
	sectionData := make(map[string]types.SectionData)
	for _, s := range sections {
		data, ok := s.Data.(types.SectionData)
		if !ok {
			// Try map[string]any (from JSON)
			if m, ok := s.Data.(map[string]any); ok {
				data = types.SectionData{
					Title:   m["title"].(string),
					Content: m["content"].(string),
				}
				if pos, ok := m["position"].(float64); ok {
					data.Position = int(pos)
				}
			} else {
				t.Fatalf("unexpected data type: %T", s.Data)
			}
		}
		sectionData[data.Title] = data
	}

	// Main Title should have intro paragraph only (not First Section content)
	mainContent := sectionData["Main Title"].Content
	if mainContent != "This is the intro paragraph." {
		t.Errorf("Main Title content = %q, want %q", mainContent, "This is the intro paragraph.")
	}

	// First Section should have its own content (not nested section content)
	firstContent := sectionData["First Section"].Content
	expected := "Content of first section.\nMore content here."
	if firstContent != expected {
		t.Errorf("First Section content = %q, want %q", firstContent, expected)
	}

	// Nested Section content
	nestedContent := sectionData["Nested Section"].Content
	if nestedContent != "Content of nested section." {
		t.Errorf("Nested Section content = %q, want %q", nestedContent, "Content of nested section.")
	}

	// Second Section content
	secondContent := sectionData["Second Section"].Content
	if secondContent != "Content of second section." {
		t.Errorf("Second Section content = %q, want %q", secondContent, "Content of second section.")
	}
}

func TestPositionOrdering(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	g := setupGraph(t)

	// Create markdown with interleaved sections and code blocks
	// Structure:
	// - Main (h1)
	//   - Section A (h2) - position 0 among Main's children
	//     - bash code block - position 0 among Section A's children
	//     - Nested (h3) - position 1 among Section A's children
	//   - Section B (h2) - position 1 among Main's children
	mdContent := "# Main\n\nIntro text.\n\n" +
		"## Section A\n\nContent A.\n\n" +
		"```bash\necho hello\n```\n\n" +
		"### Nested\n\nNested content.\n\n" +
		"## Section B\n\nContent B.\n"

	mdFile := filepath.Join(dir, "test.md")
	if err := os.WriteFile(mdFile, []byte(mdContent), 0644); err != nil {
		t.Fatal(err)
	}

	// Create fs:file node
	fileNode := graph.NewNode(types.TypeFile).
		WithURI(types.PathToURI(mdFile)).
		WithKey(mdFile).
		WithData(types.FileData{Name: "test.md", Ext: ".md"})
	if err := g.Storage().PutNode(ctx, fileNode); err != nil {
		t.Fatal(err)
	}

	idx := New()
	emitter := indexer.NewGraphEmitter(g, "gen-1")

	event := &indexer.Event{
		Type:     indexer.EventEntryVisited,
		URI:      types.PathToURI(mdFile),
		Path:     mdFile,
		Name:     "test.md",
		NodeType: types.TypeFile,
		NodeID:   fileNode.ID,
	}

	ictx := &indexer.Context{
		Root:         types.PathToURI(mdFile),
		Generation:   "gen-1",
		Graph:        g,
		Emitter:      emitter,
		TriggerEvent: event,
	}

	if err := idx.Index(ctx, ictx); err != nil {
		t.Fatalf("Index failed: %v", err)
	}

	if err := g.Storage().Flush(ctx); err != nil {
		t.Fatal(err)
	}

	// Helper to get position from node data
	getPosition := func(n *graph.Node) int {
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
		return -1
	}

	// Helper to get name from node data
	getName := func(n *graph.Node) string {
		switch data := n.Data.(type) {
		case types.SectionData:
			return data.Title
		case types.CodeBlockData:
			return data.Language + " code"
		case map[string]any:
			if title, ok := data["title"].(string); ok {
				return title
			}
			if lang, ok := data["language"].(string); ok {
				return lang + " code"
			}
		}
		return ""
	}

	// Find the document node
	docNodes, err := g.FindNodes(ctx, graph.NodeFilter{Type: types.TypeMarkdownDoc})
	if err != nil {
		t.Fatal(err)
	}
	if len(docNodes) != 1 {
		t.Fatalf("expected 1 document node, got %d", len(docNodes))
	}
	docNode := docNodes[0]

	// Get children of document (should be Main section at position 0)
	docEdges, err := g.GetEdgesFrom(ctx, docNode.ID)
	if err != nil {
		t.Fatal(err)
	}

	var mainSectionID string
	for _, e := range docEdges {
		if e.Type == types.EdgeHas {
			// Check if target is a section
			target, err := g.GetNode(ctx, e.To)
			if err == nil && target.Type == types.TypeMarkdownSection {
				mainSectionID = e.To
				break
			}
		}
	}
	if mainSectionID == "" {
		t.Fatal("expected to find Main section")
	}

	// Get Main section node and verify position
	mainSection, err := g.GetNode(ctx, mainSectionID)
	if err != nil {
		t.Fatal(err)
	}
	if pos := getPosition(mainSection); pos != 0 {
		t.Errorf("Main section position = %d, want 0", pos)
	}

	// Get children of Main section (Section A and Section B)
	mainEdges, err := g.GetEdgesFrom(ctx, mainSectionID)
	if err != nil {
		t.Fatal(err)
	}

	// Collect Main's children
	type childInfo struct {
		id       string
		position int
		name     string
	}
	var mainChildren []childInfo
	for _, e := range mainEdges {
		if e.Type != types.EdgeHas {
			continue
		}
		childNode, err := g.GetNode(ctx, e.To)
		if err != nil {
			continue
		}
		// Only include sections
		if childNode.Type != types.TypeMarkdownSection {
			continue
		}
		mainChildren = append(mainChildren, childInfo{
			id:       e.To,
			position: getPosition(childNode),
			name:     getName(childNode),
		})
	}

	// Main should have 2 section children: Section A (pos 0), Section B (pos 1)
	if len(mainChildren) != 2 {
		t.Fatalf("Main expected 2 section children, got %d: %+v", len(mainChildren), mainChildren)
	}

	// Build position map for Main's children
	mainChildPos := make(map[string]int)
	var sectionAID string
	for _, c := range mainChildren {
		mainChildPos[c.name] = c.position
		if c.name == "Section A" {
			sectionAID = c.id
		}
	}

	if pos := mainChildPos["Section A"]; pos != 0 {
		t.Errorf("Section A position = %d, want 0", pos)
	}
	if pos := mainChildPos["Section B"]; pos != 1 {
		t.Errorf("Section B position = %d, want 1", pos)
	}

	// Now check Section A's children (code block at pos 0, Nested section at pos 1)
	sectionAEdges, err := g.GetEdgesFrom(ctx, sectionAID)
	if err != nil {
		t.Fatal(err)
	}

	var sectionAChildren []childInfo
	for _, e := range sectionAEdges {
		if e.Type != types.EdgeHas {
			continue
		}
		childNode, err := g.GetNode(ctx, e.To)
		if err != nil {
			continue
		}
		// Only include sections and code blocks
		if childNode.Type != types.TypeMarkdownSection && childNode.Type != types.TypeMarkdownCodeBlock {
			continue
		}
		sectionAChildren = append(sectionAChildren, childInfo{
			id:       e.To,
			position: getPosition(childNode),
			name:     getName(childNode),
		})
	}

	// Section A should have 2 children: bash code (pos 0), Nested (pos 1)
	if len(sectionAChildren) != 2 {
		t.Fatalf("Section A expected 2 children, got %d: %+v", len(sectionAChildren), sectionAChildren)
	}

	sectionAChildPos := make(map[string]int)
	for _, c := range sectionAChildren {
		sectionAChildPos[c.name] = c.position
	}

	if pos := sectionAChildPos["bash code"]; pos != 0 {
		t.Errorf("bash code position = %d, want 0", pos)
	}
	if pos := sectionAChildPos["Nested"]; pos != 1 {
		t.Errorf("Nested section position = %d, want 1", pos)
	}
}
