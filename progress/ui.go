package progress

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/spinner"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// DoneMsg is sent when all indexers have completed.
type DoneMsg struct{}

// tickMsg triggers a UI refresh.
type tickMsg time.Time

// Model is the bubbletea model for progress display.
type Model struct {
	coordinator *Coordinator
	spinner     spinner.Model
	width       int
	done        bool
	startTime   time.Time
}

// Styles
var (
	indexerStyle = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("39"))
	doneStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color("42"))
	errorStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("196"))
	dimStyle     = lipgloss.NewStyle().Foreground(lipgloss.Color("240"))
	itemStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color("245"))
)

// NewModel creates a new progress UI model.
func NewModel(coord *Coordinator) Model {
	s := spinner.New()
	s.Spinner = spinner.Dot
	s.Style = lipgloss.NewStyle().Foreground(lipgloss.Color("205"))

	return Model{
		coordinator: coord,
		spinner:     s,
		startTime:   time.Now(),
	}
}

// Init implements tea.Model.
func (m Model) Init() tea.Cmd {
	return tea.Batch(
		m.spinner.Tick,
		tickCmd(),
	)
}

// Update implements tea.Model.
func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		return m, nil

	case tea.KeyMsg:
		switch msg.String() {
		case "ctrl+c", "q":
			return m, tea.Quit
		}

	case DoneMsg:
		m.done = true
		return m, tea.Quit

	case tickMsg:
		// Check if all indexers are done
		if !m.coordinator.IsRunning() && len(m.coordinator.State()) > 0 {
			m.done = true
			return m, tea.Quit
		}
		return m, tickCmd()

	case spinner.TickMsg:
		var cmd tea.Cmd
		m.spinner, cmd = m.spinner.Update(msg)
		return m, cmd
	}

	return m, nil
}

// View implements tea.Model.
func (m Model) View() string {
	if m.done {
		return "" // Clear the progress display when done
	}

	var b strings.Builder

	states := m.coordinator.State()
	if len(states) == 0 {
		b.WriteString(m.spinner.View())
		b.WriteString(" Starting indexers...")
		b.WriteString("\n")
		return b.String()
	}

	// Determine max width for progress bar (leave room for text)
	width := m.width
	if width == 0 {
		width = 80
	}
	barWidth := 20
	if width > 100 {
		barWidth = 30
	}

	for _, state := range states {
		line := m.renderIndexerLine(state, barWidth, width)
		b.WriteString(line)
		b.WriteString("\n")
	}

	return b.String()
}

func (m Model) renderIndexerLine(state *IndexerState, barWidth, totalWidth int) string {
	var b strings.Builder

	// Indexer name
	nameStr := fmt.Sprintf("[%s]", state.Name)
	b.WriteString(indexerStyle.Render(nameStr))
	b.WriteString(" ")

	switch state.Status {
	case "completed":
		b.WriteString(doneStyle.Render("done"))
		b.WriteString(dimStyle.Render(fmt.Sprintf(" (%d items)", state.Total)))

	case "error":
		errMsg := "error"
		if state.Error != nil {
			errMsg = state.Error.Error()
			if len(errMsg) > 50 {
				errMsg = errMsg[:47] + "..."
			}
		}
		b.WriteString(errorStyle.Render(errMsg))

	case "running":
		// Spinner
		b.WriteString(m.spinner.View())
		b.WriteString(" ")

		// Progress bar or count
		if state.Total > 0 {
			// Known total - show progress bar
			pct := float64(state.Current) / float64(state.Total)
			filled := int(pct * float64(barWidth))
			if filled > barWidth {
				filled = barWidth
			}
			bar := strings.Repeat("█", filled) + strings.Repeat("░", barWidth-filled)
			b.WriteString(bar)
			b.WriteString(fmt.Sprintf(" %d%%", int(pct*100)))
		} else {
			// Unknown total - show count
			b.WriteString(fmt.Sprintf("%d items", state.Current))
		}

		// Current item (truncated to fit)
		if state.Item != "" {
			item := state.Item
			// Calculate remaining space
			used := len(nameStr) + 3 + barWidth + 10 // rough estimate
			remaining := totalWidth - used - 5
			if remaining > 10 && len(item) > remaining {
				item = "..." + item[len(item)-remaining+3:]
			}
			if remaining > 10 {
				b.WriteString("  ")
				b.WriteString(itemStyle.Render(item))
			}
		}
	}

	return b.String()
}

// tickCmd returns a command that ticks every 100ms.
func tickCmd() tea.Cmd {
	return tea.Tick(100*time.Millisecond, func(t time.Time) tea.Msg {
		return tickMsg(t)
	})
}
