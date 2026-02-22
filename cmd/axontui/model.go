package main

import (
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/codewandler/axon/graph"
)

// focusArea tracks which region has keyboard focus.
type focusArea int

const (
	focusPanes   focusArea = iota // edge panes (default)
	focusPreview                  // preview viewport
	focusQuery                    // query input bar
)

// model is the root BubbleTea model for the graph explorer.
type model struct {
	app *appContext
	nav *navigator

	// UI components
	leftPane  *pane
	rightPane *pane
	query     queryBar
	preview   viewport.Model

	// Preview state — shows content for the hovered (cursor) node
	previewContent string
	hasPreview     bool
	previewLoading bool
	previewNodeID  string // which node the current preview is for

	// Preview cache — preloaded on center change for all children (right pane)
	previewCache map[string]string // nodeID → rendered content

	// State
	activePane  int // 0 = left, 1 = right
	focus       focusArea
	width       int
	height      int
	showPreview bool // whether to show preview panel (toggle with 'p')
	err         error
	ready       bool

	// Filter debounce
	filterSeq uint64 // increments on each keystroke, used to debounce
}

// nodeLoadedMsg is sent when the initial node is loaded.
type nodeLoadedMsg struct {
	err error
}

// edgesLoadedMsg is sent when edges are reloaded after navigation.
type edgesLoadedMsg struct {
	err error
}

// filterTickMsg is sent after a debounce delay to trigger filter application.
type filterTickMsg struct {
	seq uint64 // sequence number to detect stale ticks
}

// previewBatchMsg delivers a batch of preloaded preview renders.
type previewBatchMsg struct {
	results map[string]string // nodeID → rendered content
}

func newModel(app *appContext, startArg string) model {
	vp := viewport.New(80, 10)

	m := model{
		app:          app,
		nav:          newNavigator(app.Ctx, app.Storage, app.Graph),
		leftPane:     newPane("SIBLINGS", true),
		rightPane:    newPane("CHILDREN", false),
		query:        newQueryBar(),
		preview:      vp,
		previewCache: make(map[string]string),
		activePane:   1,
		focus:        focusPanes,
		showPreview:  true,
	}

	return m
}

func (m model) Init() tea.Cmd {
	return tea.Batch(
		// Warm up glamour renderer in background (eliminates cold-start penalty)
		func() tea.Msg {
			warmUpGlamour()
			return nil
		},
		// Load initial node
		func() tea.Msg {
			node, err := resolveStartNode(m.app.Ctx, m.app.Storage, m.app.Graph, startNodeArg, m.app.Cwd)
			if err != nil {
				return nodeLoadedMsg{err: err}
			}
			if err := m.nav.SetCenter(node); err != nil {
				return nodeLoadedMsg{err: err}
			}
			return nodeLoadedMsg{}
		},
	)
}

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.updateLayout()
		m.ready = true
		return m, nil

	case nodeLoadedMsg:
		if msg.err != nil {
			m.err = msg.err
			return m, tea.Quit
		}
		m.previewCache = make(map[string]string) // clear cache on new root
		m.syncPanes()
		m.ready = true
		// Preload previews for all children (right pane) in background
		m.showHoveredPreview()
		return m, m.preloadChildPreviews()

	case edgesLoadedMsg:
		if msg.err != nil {
			m.err = msg.err
		}
		m.previewCache = make(map[string]string) // clear cache on navigation
		m.syncPanes()
		// Preload previews for all children (right pane) in background
		m.showHoveredPreview()
		return m, m.preloadChildPreviews()

	case previewBatchMsg:
		// Merge preloaded previews into cache
		for id, content := range msg.results {
			m.previewCache[id] = content
		}
		// If the currently hovered node was in the batch and we're still loading, show it
		node := m.hoveredNode()
		if node != nil && m.previewLoading && m.previewNodeID == node.ID {
			if cached, ok := m.previewCache[node.ID]; ok {
				m.previewContent = cached
				m.hasPreview = true
				m.previewLoading = false
				m.preview.SetContent(cached)
				m.preview.GotoTop()
				m.updateLayout()
			}
		}
		return m, nil

	case filterTickMsg:
		// Only apply if this tick matches the current sequence (debounce)
		if msg.seq != m.filterSeq {
			return m, nil
		}
		return m, m.applyFilter()

	case tea.KeyMsg:
		// Query input mode
		if m.focus == focusQuery {
			return m.updateQueryInput(msg)
		}

		// Preview viewport mode
		if m.focus == focusPreview {
			return m.updatePreviewFocus(msg)
		}

		// Default: pane navigation mode
		switch msg.String() {
		case "esc", "q", "ctrl+c":
			return m, tea.Quit

		case "up", "k":
			m.activeP().CursorUp()
			m.showHoveredPreview()
			return m, nil

		case "down", "j":
			m.activeP().CursorDown()
			m.showHoveredPreview()
			return m, nil

		case "left", "h":
			// Go back in history (up the tree)
			return m.goBack()

		case "right", "l", "enter":
			// Drill into selected node (deeper into the tree)
			return m.drillIn()

		case "backspace":
			return m.goBack()

		case "+", "=":
			return m.zoomIn()

		case "-":
			return m.zoomOut()

		case "p":
			m.showPreview = !m.showPreview
			m.updateLayout()
			return m, nil

		case "tab", "/":
			// Focus the query bar for immediate typing
			m.focus = focusQuery
			cmd := m.query.Focus()
			return m, cmd
		}
	}

	return m, nil
}

// hoveredNode returns the node currently under the cursor in the active pane.
// Falls back to center node if no pane item is selected.
func (m *model) hoveredNode() *graph.Node {
	node := m.activeP().SelectedNode()
	if node != nil {
		return node
	}
	return m.nav.center
}

// showHoveredPreview updates the preview panel from the cache for the hovered node.
// This is called on every cursor movement — it must be instant (no async work).
// If the preview isn't cached yet, it shows a loading indicator; the batch
// preload will fill the cache and previewBatchMsg will update the display.
func (m *model) showHoveredPreview() {
	node := m.hoveredNode()
	if node == nil {
		m.hasPreview = false
		m.previewLoading = false
		m.previewContent = ""
		m.previewNodeID = ""
		return
	}

	// Already showing this node's preview?
	if m.previewNodeID == node.ID && m.hasPreview && !m.previewLoading {
		return
	}

	m.previewNodeID = node.ID

	// No preview possible for this type?
	if !hasPreviewContent(node) {
		m.hasPreview = false
		m.previewLoading = false
		m.previewContent = ""
		m.updateLayout()
		return
	}

	// Check cache — instant
	if cached, ok := m.previewCache[node.ID]; ok {
		m.previewContent = cached
		m.hasPreview = true
		m.previewLoading = false
		m.preview.SetContent(cached)
		m.preview.GotoTop()
		m.updateLayout()
		return
	}

	// Not cached yet — show loading, batch preload will deliver it
	m.previewLoading = true
	m.hasPreview = true
	m.previewContent = ""
	m.updateLayout()
}

// preloadChildPreviews fires a background goroutine that renders previews
// for all previewable children (right pane only). Called once on center change.
// Results arrive via previewBatchMsg.
func (m *model) preloadChildPreviews() tea.Cmd {
	ctx := m.app.Ctx
	g := m.app.Graph
	width := m.width - 4

	// Collect previewable nodes from right pane (children) only
	seen := make(map[string]bool)
	var batch []*graph.Node

	for _, grp := range m.nav.edges.Children {
		for _, n := range grp.Nodes {
			if seen[n.ID] {
				continue
			}
			seen[n.ID] = true
			if _, cached := m.previewCache[n.ID]; cached {
				continue
			}
			if hasPreviewContent(n) {
				batch = append(batch, n)
			}
		}
	}

	if len(batch) == 0 {
		return nil
	}

	// Single goroutine renders all sequentially (glamour is mutex-protected)
	return func() tea.Msg {
		results := make(map[string]string, len(batch))
		for _, n := range batch {
			content := resolvePreview(ctx, g, n, width)
			if content != "" {
				results[n.ID] = content
			}
		}
		return previewBatchMsg{results: results}
	}
}

// updatePreviewFocus handles keys when the preview viewport is focused.
func (m model) updatePreviewFocus(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc", "enter":
		m.focus = focusPanes
		return m, nil
	case "tab":
		// Tab from preview → query bar (always visible)
		m.focus = focusQuery
		cmd := m.query.Focus()
		return m, cmd
	case "q", "ctrl+c":
		return m, tea.Quit
	}

	var cmd tea.Cmd
	m.preview, cmd = m.preview.Update(msg)
	return m, cmd
}

func (m model) View() string {
	if m.err != nil {
		return lipgloss.NewStyle().Foreground(colorError).Render("Error: "+m.err.Error()) + "\n"
	}

	if !m.ready || m.nav.center == nil {
		return lipgloss.NewStyle().Foreground(colorMuted).Render("Loading...") + "\n"
	}

	// === Top section (flexible) ===
	var top []string

	// Breadcrumb
	crumbs := m.nav.Breadcrumbs()
	breadcrumb := renderBreadcrumb(crumbs, m.width, m.nav.depth)
	top = append(top, breadcrumb)

	// Top divider
	top = append(top, dividerStyle.Render(strings.Repeat("─", m.width)))

	// Detail header (always shown, compact)
	detail := renderDetail(m.nav.center, m.width, false)
	top = append(top, detail)

	// Middle divider
	top = append(top, dividerStyle.Render(strings.Repeat("─", m.width)))

	// Edge panes (split left/right) — always get full height
	paneHeight := m.paneHeight()
	m.leftPane.height = paneHeight
	m.rightPane.height = paneHeight

	leftRender := m.leftPane.Render()
	rightRender := m.rightPane.Render()

	panes := lipgloss.JoinHorizontal(lipgloss.Top, leftRender, rightRender)

	// Preview overlay — replaces the bottom portion of the pane area
	if m.hasPreview && m.showPreview {
		panes = m.overlayPreview(panes, paneHeight)
	}

	top = append(top, panes)

	topContent := strings.Join(top, "\n")

	// === Bottom section (fixed, pinned to bottom) ===
	var bottom []string

	// Bottom divider
	bottom = append(bottom, dividerStyle.Render(strings.Repeat("─", m.width)))

	// Help bar
	help := renderHelpBar(m.width, m.focus == focusQuery, m.hasPreview && m.showPreview, m.focus == focusPreview, m.query.extended)
	bottom = append(bottom, help)

	// Query bar at very bottom (always visible)
	bottom = append(bottom, m.query.View(m.width))

	bottomContent := strings.Join(bottom, "\n")
	bottomLines := strings.Count(bottomContent, "\n") + 1
	topLines := strings.Count(topContent, "\n") + 1

	// Pad between top and bottom so bottom is pinned to terminal bottom
	padLines := m.height - topLines - bottomLines
	if padLines < 0 {
		padLines = 0
	}
	padding := ""
	if padLines > 0 {
		padding = strings.Repeat("\n", padLines)
	}

	return topContent + padding + "\n" + bottomContent
}

// activeP returns the currently active pane.
func (m *model) activeP() *pane {
	if m.activePane == 0 {
		return m.leftPane
	}
	return m.rightPane
}

// syncPanes loads edge data from navigator into panes and updates the query bar.
func (m *model) syncPanes() {
	// Left pane: siblings with parent highlighted
	if m.nav.edges.SiblingData != nil {
		m.leftPane.loadSiblings(m.nav.edges.SiblingData, m.nav.center.ID)
	} else {
		m.leftPane.loadFromGroups(m.nav.edges.Parents)
	}
	// Right pane: children
	m.rightPane.loadFromGroups(m.nav.edges.Children)
	m.leftPane.active = m.activePane == 0
	m.rightPane.active = m.activePane == 1

	// Sync query bar structural parts
	if m.nav.center != nil {
		edgeType := "*"
		if len(m.nav.edges.Children) == 1 {
			edgeType = m.nav.edges.Children[0].EdgeType
		}
		m.query.SetStructural(m.nav.center.ID, edgeType, m.nav.depth)
	}

	m.updateLayout()
}

// updateLayout recalculates pane sizes.
func (m *model) updateLayout() {
	halfWidth := m.width / 2
	m.leftPane.width = halfWidth
	m.rightPane.width = m.width - halfWidth

	previewH := m.previewHeight()
	m.preview.Width = m.width
	if previewH > 1 {
		m.preview.Height = previewH - 1 // -1 for divider line
	} else {
		m.preview.Height = previewH
	}

	paneH := m.paneHeight()
	m.leftPane.height = paneH
	m.rightPane.height = paneH
	m.leftPane.active = m.activePane == 0
	m.rightPane.active = m.activePane == 1
}

// previewHeight returns the height allocated for the preview overlay.
// This is ~50% of the pane area, capped to actual content lines.
func (m model) previewHeight() int {
	if !m.hasPreview || !m.showPreview {
		return 0
	}
	paneH := m.paneHeight()
	// 50% of pane area (including 1 line for preview divider)
	h := paneH / 2
	if h < 3 {
		h = 3
	}
	if h > 15 {
		h = 15
	}
	// Don't allocate more than actual content (+ 1 for divider)
	if m.previewContent != "" {
		contentLines := strings.Count(m.previewContent, "\n") + 1
		if h > contentLines+1 {
			h = contentLines + 1
		}
		if h < 3 {
			h = 3
		}
	}
	return h
}

// fixedOverhead returns the number of lines used by fixed UI elements.
// Preview is an overlay and does NOT add to overhead.
func (m model) fixedOverhead() int {
	return 9 // breadcrumb + dividers + detail + help + query bar (always visible)
}

// paneHeight calculates available height for edge panes.
// Preview overlays the panes — it does NOT reduce pane height.
func (m model) paneHeight() int {
	overhead := m.fixedOverhead()
	h := m.height - overhead
	if h < 3 {
		h = 3
	}
	return h
}

// overlayPreview replaces the bottom portion of the rendered pane content
// with the preview panel. The preview sits on top of the panes.
func (m model) overlayPreview(panesContent string, paneHeight int) string {
	previewH := m.previewHeight()
	if previewH <= 0 {
		return panesContent
	}

	// Build preview block: divider line + content
	previewLabel := " PREVIEW "
	if m.previewLoading {
		previewLabel = " LOADING... "
	}
	if m.focus == focusPreview {
		previewLabel = paneTitleActiveStyle.Render(previewLabel)
	} else {
		previewLabel = paneTitleInactiveStyle.Render(previewLabel)
	}
	divLen := (m.width - lipgloss.Width(previewLabel)) / 2
	if divLen < 0 {
		divLen = 0
	}
	previewDiv := dividerStyle.Render(strings.Repeat("─", divLen)) +
		previewLabel +
		dividerStyle.Render(strings.Repeat("─", divLen))

	var previewBlock string
	if m.previewLoading && m.previewContent == "" {
		placeholder := lipgloss.NewStyle().
			Foreground(colorMuted).
			Italic(true).
			Width(m.width).
			Render("  Rendering preview...")
		previewBlock = previewDiv + "\n" + placeholder
	} else {
		// Set viewport height to previewH - 1 (divider takes 1 line)
		m.preview.Width = m.width
		m.preview.Height = previewH - 1
		previewBlock = previewDiv + "\n" + m.preview.View()
	}

	// Split pane content into lines
	paneLines := strings.Split(panesContent, "\n")

	// Preview block lines
	pvLines := strings.Split(previewBlock, "\n")

	// Replace bottom portion: keep top lines, replace bottom with preview
	keepLines := len(paneLines) - previewH
	if keepLines < 1 {
		keepLines = 1
	}

	var result []string
	result = append(result, paneLines[:keepLines]...)
	result = append(result, pvLines...)

	// Ensure we don't exceed the original total line count
	totalTarget := len(paneLines)
	if len(result) > totalTarget {
		result = result[:totalTarget]
	}

	return strings.Join(result, "\n")
}

// drillIn navigates into the selected node.
func (m model) drillIn() (tea.Model, tea.Cmd) {
	node := m.activeP().SelectedNode()
	if node == nil {
		return m, nil
	}

	// Immediately clear preview (will be reloaded async after edges arrive)
	m.previewContent = ""
	m.hasPreview = false
	m.previewLoading = false

	return m, func() tea.Msg {
		if err := m.nav.NavigateTo(node); err != nil {
			return edgesLoadedMsg{err: err}
		}
		return edgesLoadedMsg{}
	}
}

// goBack navigates back in history.
func (m model) goBack() (tea.Model, tea.Cmd) {
	if !m.nav.CanGoBack() {
		return m, nil
	}

	m.previewContent = ""
	m.hasPreview = false
	m.previewLoading = false

	return m, func() tea.Msg {
		if err := m.nav.GoBack(); err != nil {
			return edgesLoadedMsg{err: err}
		}
		return edgesLoadedMsg{}
	}
}

// zoomIn increases depth.
func (m model) zoomIn() (tea.Model, tea.Cmd) {
	return m, func() tea.Msg {
		if err := m.nav.ZoomIn(); err != nil {
			return edgesLoadedMsg{err: err}
		}
		return edgesLoadedMsg{}
	}
}

// zoomOut decreases depth.
func (m model) zoomOut() (tea.Model, tea.Cmd) {
	return m, func() tea.Msg {
		if err := m.nav.ZoomOut(); err != nil {
			return edgesLoadedMsg{err: err}
		}
		return edgesLoadedMsg{}
	}
}

// updateQueryInput handles key events when the query input is focused.
func (m model) updateQueryInput(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "ctrl+c":
		return m, tea.Quit
	case "esc":
		// Return to panes (don't quit — esc from panes quits)
		m.query.Blur()
		m.focus = focusPanes
		return m, nil
	case "enter":
		// Apply immediately and return to panes
		m.query.Blur()
		m.focus = focusPanes
		return m, m.applyFilter()
	}

	// Track previous value to detect changes
	prevValue := m.query.Value()
	prevField := m.query.Field()
	prevOp := m.query.Operator()

	cmd := m.query.Update(msg)

	// If filter changed, schedule a debounced apply
	if m.query.Value() != prevValue || m.query.Field() != prevField || m.query.Operator() != prevOp {
		m.filterSeq++
		seq := m.filterSeq
		debounceCmd := tea.Tick(200*time.Millisecond, func(t time.Time) tea.Msg {
			return filterTickMsg{seq: seq}
		})
		return m, tea.Batch(cmd, debounceCmd)
	}

	return m, cmd
}

// applyFilter applies the current query bar filter to the navigator.
func (m *model) applyFilter() tea.Cmd {
	if !m.query.HasFilter() {
		// Clear any existing filter
		return func() tea.Msg {
			if err := m.nav.ClearFilter(); err != nil {
				return edgesLoadedMsg{err: err}
			}
			return edgesLoadedMsg{}
		}
	}

	field := m.query.Field()
	op := m.query.Operator()
	val := m.query.Value()
	catExpr := m.query.CategoryExpression()

	return func() tea.Msg {
		if err := m.nav.SetFilter(field, op, val, catExpr); err != nil {
			return edgesLoadedMsg{err: err}
		}
		return edgesLoadedMsg{}
	}
}
