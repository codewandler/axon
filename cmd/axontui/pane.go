package main

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/codewandler/axon/graph"
)

// paneItem is a selectable item in the pane (either a section header or a node).
type paneItem struct {
	isHeader  bool
	isCurrent bool // true if this is the center node among siblings
	// For headers:
	edgeType string
	count    int
	// For nodes:
	node     *graph.Node
	groupIdx int // which edge group this belongs to
}

// pane is a scrollable list of edge groups with items.
type pane struct {
	title    string
	incoming bool
	items    []paneItem
	cursor   int
	offset   int // scroll offset for viewport
	active   bool
	width    int
	height   int
}

func newPane(title string, incoming bool) *pane {
	return &pane{
		title:    title,
		incoming: incoming,
	}
}

// loadFromGroups rebuilds the item list from edge groups.
func (p *pane) loadFromGroups(groups []*edgeGroup) {
	p.items = nil
	for i, g := range groups {
		// Section header
		p.items = append(p.items, paneItem{
			isHeader: true,
			edgeType: g.EdgeType,
			count:    g.Count,
			groupIdx: i,
		})
		// Node items
		for _, node := range g.Nodes {
			p.items = append(p.items, paneItem{
				node:     node,
				groupIdx: i,
			})
		}
		// "Show more" indicator if there are more nodes than loaded
		if g.Loaded && len(g.Nodes) < g.Count {
			// We'll show "... +N more" inline
		}
	}

	// Reset cursor if it's out of bounds
	if p.cursor >= len(p.items) {
		p.cursor = len(p.items) - 1
	}
	if p.cursor < 0 {
		p.cursor = 0
	}
	// Skip headers on initial position
	p.skipHeaders(1)
}

// loadSiblings rebuilds the left pane from sibling data.
// Shows the parent as a header, then all siblings, with center highlighted.
func (p *pane) loadSiblings(data *siblingData, centerID string) {
	p.items = nil

	if data == nil || data.Parent == nil {
		return
	}

	// Parent as header
	parentName := displayName(data.Parent)
	p.items = append(p.items, paneItem{
		isHeader: true,
		edgeType: emoji(data.Parent.Type) + " " + parentName,
		count:    len(data.Siblings),
	})

	// Siblings
	cursorTarget := -1
	for _, node := range data.Siblings {
		isCurrent := node.ID == centerID
		p.items = append(p.items, paneItem{
			node:      node,
			isCurrent: isCurrent,
		})
		if isCurrent {
			cursorTarget = len(p.items) - 1
		}
	}

	// Position cursor on the current node
	if cursorTarget >= 0 {
		p.cursor = cursorTarget
	} else if len(p.items) > 1 {
		p.cursor = 1 // first sibling
	} else {
		p.cursor = 0
	}
	p.skipHeaders(1)
	p.ensureVisible()
}

// CursorUp moves the cursor up, skipping headers.
func (p *pane) CursorUp() {
	if len(p.items) == 0 {
		return
	}
	p.cursor--
	if p.cursor < 0 {
		p.cursor = 0
	}
	p.skipHeaders(-1)
	p.ensureVisible()
}

// CursorDown moves the cursor down, skipping headers.
func (p *pane) CursorDown() {
	if len(p.items) == 0 {
		return
	}
	p.cursor++
	if p.cursor >= len(p.items) {
		p.cursor = len(p.items) - 1
	}
	p.skipHeaders(1)
	p.ensureVisible()
}

// skipHeaders moves cursor in the given direction until it lands on a non-header item.
func (p *pane) skipHeaders(dir int) {
	for p.cursor >= 0 && p.cursor < len(p.items) && p.items[p.cursor].isHeader {
		p.cursor += dir
	}
	if p.cursor < 0 {
		p.cursor = 0
		// Find first non-header
		for p.cursor < len(p.items) && p.items[p.cursor].isHeader {
			p.cursor++
		}
	}
	if p.cursor >= len(p.items) {
		p.cursor = len(p.items) - 1
		// Find last non-header
		for p.cursor >= 0 && p.items[p.cursor].isHeader {
			p.cursor--
		}
	}
}

// ensureVisible adjusts scroll offset so cursor is visible.
func (p *pane) ensureVisible() {
	if p.height <= 0 {
		return
	}
	visibleLines := p.height - 1 // subtract title line
	if visibleLines < 1 {
		visibleLines = 1
	}
	if p.cursor < p.offset {
		p.offset = p.cursor
	}
	if p.cursor >= p.offset+visibleLines {
		p.offset = p.cursor - visibleLines + 1
	}
}

// SelectedNode returns the node at the cursor, or nil if on a header.
func (p *pane) SelectedNode() *graph.Node {
	if p.cursor < 0 || p.cursor >= len(p.items) {
		return nil
	}
	item := p.items[p.cursor]
	if item.isHeader {
		return nil
	}
	return item.node
}

// SelectedGroupIdx returns the group index of the cursor item.
func (p *pane) SelectedGroupIdx() int {
	if p.cursor < 0 || p.cursor >= len(p.items) {
		return -1
	}
	return p.items[p.cursor].groupIdx
}

// Render renders the pane.
func (p *pane) Render() string {
	var b strings.Builder

	// Title line
	var titleStr string
	if p.active {
		titleStr = paneTitleActiveStyle.Render(" " + p.title)
	} else {
		titleStr = paneTitleInactiveStyle.Render(" " + p.title)
	}
	b.WriteString(titleStr)
	b.WriteString("\n")

	if len(p.items) == 0 {
		b.WriteString(lipgloss.NewStyle().Foreground(colorDim).Render("  (none)"))
		b.WriteString("\n")
		return lipgloss.NewStyle().Width(p.width).Render(b.String())
	}

	// Visible items
	visibleLines := p.height - 1
	if visibleLines < 1 {
		visibleLines = 1
	}
	// Clamp offset to valid range
	if p.offset >= len(p.items) {
		p.offset = len(p.items) - 1
	}
	if p.offset < 0 {
		p.offset = 0
	}
	end := p.offset + visibleLines
	if end > len(p.items) {
		end = len(p.items)
	}

	for i := p.offset; i < end; i++ {
		item := p.items[i]
		if item.isHeader {
			header := sectionDivider(item.edgeType, item.count, p.width-2)
			b.WriteString(sectionHeaderStyle.Render(header))
		} else {
			b.WriteString(p.renderItem(item, i == p.cursor))
		}
		b.WriteString("\n")
	}

	return lipgloss.NewStyle().Width(p.width).Render(b.String())
}

// renderItem renders a single node item.
func (p *pane) renderItem(item paneItem, selected bool) string {
	if item.node == nil {
		return ""
	}

	e := emoji(item.node.Type)
	name := displayName(item.node)
	typeStr := lipgloss.NewStyle().Foreground(typeColor(item.node.Type)).Render(item.node.Type)

	maxName := p.width - 18 // space for emoji + type + padding
	if maxName < 10 {
		maxName = 10
	}
	name = truncate(name, maxName)

	var line string
	if selected && p.active {
		cursor := cursorStyle.Render("▸")
		line = fmt.Sprintf(" %s %s %s  %s", cursor, e, itemSelectedStyle.Render(name), typeStr)
	} else if item.isCurrent {
		// Current node among siblings — subtle underline/dim indicator
		nameStyled := lipgloss.NewStyle().Foreground(colorAccentWarm).Render(name)
		line = fmt.Sprintf("   %s %s  %s", e, nameStyled, typeStr)
	} else {
		line = fmt.Sprintf("   %s %s  %s", e, itemStyle.Render(name), typeStr)
	}

	return line
}

// HasItems returns whether the pane has any non-header items.
func (p *pane) HasItems() bool {
	for _, item := range p.items {
		if !item.isHeader {
			return true
		}
	}
	return false
}
