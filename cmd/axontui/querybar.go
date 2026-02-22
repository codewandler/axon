package main

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/codewandler/axon/aql"
)

// category is a toggleable filter chip with a pre-defined AQL expression.
// Each category maps to a builder function that creates an AQL WHERE clause
// applied to the "target" variable in pattern queries.
type category struct {
	Name    string // display name (short)
	Key     string // keyboard shortcut (1-9)
	Active  bool
	buildFn func() aql.Expression // returns the filter expression for this category
}

// categories defines the available filter categories.
// These are not 1:1 type mappings — each can combine multiple types or conditions.
var defaultCategories = []category{
	{
		Name: "dirs",
		Key:  "1",
		buildFn: func() aql.Expression {
			return aql.Var("target").Field("type").Eq("fs:dir")
		},
	},
	{
		Name: "files",
		Key:  "2",
		buildFn: func() aql.Expression {
			return aql.Var("target").Field("type").Eq("fs:file")
		},
	},
	{
		Name: "code",
		Key:  "3",
		buildFn: func() aql.Expression {
			return aql.And(
				aql.Var("target").Field("type").Eq("fs:file"),
				aql.Var("target").DataField("ext").In(".go", ".py", ".js", ".ts", ".tsx", ".rs", ".c", ".cpp", ".java", ".rb", ".sh", ".lua", ".zig", ".swift"),
			)
		},
	},
	{
		Name: "docs",
		Key:  "4",
		buildFn: func() aql.Expression {
			return aql.Var("target").Field("type").Glob("md:*")
		},
	},
	{
		Name: "git",
		Key:  "5",
		buildFn: func() aql.Expression {
			return aql.Var("target").Field("type").Glob("vcs:*")
		},
	},
}

// queryBar shows category chips and a text input, always visible at the bottom.
//
// Layout:
//
//	[1:dirs] [2:files] [3:code] [4:docs] [5:git]  🔍 name GLOB [filter text___]
//
// Categories are toggled with number keys (when focused) or always via alt+N.
// Text input filters by name GLOB pattern.
type queryBar struct {
	// Structural parts (read-only, set by navigator)
	centerID string
	edgeType string
	depth    int

	// Categories (toggleable chips)
	categories []category

	// Text filter
	input   textinput.Model
	focused bool

	// Extended mode
	extended bool
	extInput textinput.Model

	// Layout
	width int
}

func newQueryBar() queryBar {
	ti := textinput.New()
	ti.Placeholder = "filter..."
	ti.CharLimit = 128
	ti.Width = 30

	ei := textinput.New()
	ei.Placeholder = "AQL query..."
	ei.CharLimit = 512

	cats := make([]category, len(defaultCategories))
	copy(cats, defaultCategories)

	return queryBar{
		categories: cats,
		input:      ti,
		extInput:   ei,
	}
}

// SetStructural updates the read-only parts based on navigator state.
func (q *queryBar) SetStructural(centerID string, edgeType string, depth int) {
	q.centerID = centerID
	q.edgeType = edgeType
	q.depth = depth
}

// Focus activates the text input for typing.
func (q *queryBar) Focus() tea.Cmd {
	q.focused = true
	if q.extended {
		return q.extInput.Focus()
	}
	return q.input.Focus()
}

// Blur deactivates input.
func (q *queryBar) Blur() {
	q.focused = false
	q.input.Blur()
	q.extInput.Blur()
}

// Focused returns whether the query bar has focus.
func (q *queryBar) Focused() bool {
	return q.focused
}

// Value returns the text filter value.
func (q *queryBar) Value() string {
	return q.input.Value()
}

// HasFilter returns true if any category is active or text is entered.
func (q *queryBar) HasFilter() bool {
	if strings.TrimSpace(q.input.Value()) != "" {
		return true
	}
	for _, c := range q.categories {
		if c.Active {
			return true
		}
	}
	return false
}

// ActiveCategories returns the display string of active categories.
func (q *queryBar) ActiveCategories() string {
	var names []string
	for _, c := range q.categories {
		if c.Active {
			names = append(names, c.Name)
		}
	}
	return strings.Join(names, "+")
}

// CategoryExpression builds the combined AQL expression for all active categories.
// Multiple active categories are OR-ed together (union).
// Returns nil if no categories are active.
func (q *queryBar) CategoryExpression() aql.Expression {
	var exprs []aql.Expression
	for _, c := range q.categories {
		if c.Active {
			exprs = append(exprs, c.buildFn())
		}
	}
	if len(exprs) == 0 {
		return nil
	}
	if len(exprs) == 1 {
		return exprs[0]
	}
	return aql.Or(exprs...)
}

// Field returns "name" (text filter always applies to name).
func (q *queryBar) Field() string {
	return "name"
}

// Operator returns "GLOB" (text filter always uses GLOB).
func (q *queryBar) Operator() string {
	return "GLOB"
}

// Reset clears text input and deactivates all categories.
func (q *queryBar) Reset() {
	q.input.SetValue("")
	for i := range q.categories {
		q.categories[i].Active = false
	}
}

// Update handles key events.
func (q *queryBar) Update(msg tea.KeyMsg) tea.Cmd {
	if q.extended {
		return q.updateExtended(msg)
	}

	key := msg.String()

	// Number keys toggle categories
	for i := range q.categories {
		if key == q.categories[i].Key {
			q.categories[i].Active = !q.categories[i].Active
			return nil
		}
	}

	switch key {
	case "tab":
		// Toggle extended mode
		q.extended = true
		q.extInput.SetValue(q.fullQueryString())
		cmd := q.extInput.Focus()
		q.input.Blur()
		return cmd
	}

	// Forward to text input
	var cmd tea.Cmd
	q.input, cmd = q.input.Update(msg)
	return cmd
}

// updateExtended handles keys in extended mode.
func (q *queryBar) updateExtended(msg tea.KeyMsg) tea.Cmd {
	switch msg.String() {
	case "tab":
		q.extended = false
		q.extInput.Blur()
		return q.input.Focus()
	}

	var cmd tea.Cmd
	q.extInput, cmd = q.extInput.Update(msg)
	return cmd
}

// fullQueryString returns a human-readable AQL representation of the current filter.
func (q *queryBar) fullQueryString() string {
	short := shortID(q.centerID)

	edge := "*"
	if q.edgeType != "" && q.edgeType != "*" {
		edge = q.edgeType
	}
	edgePart := fmt.Sprintf("[:%s]", edge)
	if q.depth > 1 {
		edgePart = fmt.Sprintf("[:%s*1..%d]", edge, q.depth)
	}

	base := fmt.Sprintf("SELECT target FROM (c)-%s->(t) WHERE c.id = '%s'", edgePart, short)

	cats := q.ActiveCategories()
	if cats != "" {
		base += " AND (" + cats + ")"
	}

	v := strings.TrimSpace(q.input.Value())
	if v != "" {
		base += fmt.Sprintf(" AND t.name GLOB '%s'", v)
	}

	return base
}

// View renders the query bar: category chips + text input.
func (q *queryBar) View(width int) string {
	q.width = width

	if q.extended {
		return q.viewExtended()
	}

	var b strings.Builder

	// Category chips
	for _, c := range q.categories {
		chip := q.renderChip(c)
		b.WriteString(chip)
		b.WriteString(" ")
	}

	// Separator
	b.WriteString(lipgloss.NewStyle().Foreground(colorDim).Render("│ "))

	// Search icon + input
	b.WriteString(lipgloss.NewStyle().Foreground(colorAccent).Render("/ "))

	if q.focused {
		b.WriteString(q.input.View())
	} else {
		v := q.input.Value()
		if v == "" {
			b.WriteString(lipgloss.NewStyle().Foreground(colorDim).Italic(true).Render("filter..."))
		} else {
			b.WriteString(lipgloss.NewStyle().Foreground(colorActive).Render(v))
		}
	}

	return b.String()
}

// renderChip renders a single category chip.
func (q *queryBar) renderChip(c category) string {
	label := c.Key + ":" + c.Name

	if c.Active {
		return lipgloss.NewStyle().
			Foreground(lipgloss.Color("#1E1E2E")).
			Background(colorAccent).
			Bold(true).
			Padding(0, 0).
			Render(" " + label + " ")
	}

	return lipgloss.NewStyle().
		Foreground(colorMuted).
		Render("[" + label + "]")
}

// viewExtended renders the full AQL editor.
func (q *queryBar) viewExtended() string {
	label := lipgloss.NewStyle().
		Foreground(colorAccentWarm).
		Bold(true).
		Render("AQL ")
	return label + q.extInput.View()
}
