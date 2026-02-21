package progress

import (
	"fmt"
	"math/rand"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/spinner"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"golang.org/x/text/language"
	"golang.org/x/text/message"
)

// DoneMsg is sent when all indexers have completed.
type DoneMsg struct{}

// tickMsg triggers a UI refresh.
type tickMsg time.Time

// Model is the bubbletea model for progress display.
type Model struct {
	coordinator   *Coordinator
	spinner       spinner.Model
	spinnerFrames map[string]string // Per-indexer random spinner frames
	width         int
	done          bool
}

// Styles
var (
	indexerStyle = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("39"))
	doneStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color("42"))
	errorStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("196"))
	dimStyle     = lipgloss.NewStyle().Foreground(lipgloss.Color("240"))
	itemStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color("245"))
	rateStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color("244"))
	etaStyle     = lipgloss.NewStyle().Foreground(lipgloss.Color("214"))
)

// Spinner frames for random selection
var spinnerFrames = []string{"⣾", "⣽", "⣻", "⢿", "⡿", "⣟", "⣯", "⣷"}

// Number formatter with comma separators
var numPrinter = message.NewPrinter(language.English)

// NewModel creates a new progress UI model.
func NewModel(coord *Coordinator) Model {
	s := spinner.New()
	s.Spinner = spinner.Dot
	s.Style = lipgloss.NewStyle().Foreground(lipgloss.Color("205"))

	return Model{
		coordinator:   coord,
		spinner:       s,
		spinnerFrames: make(map[string]string),
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
		// Pick a new random spinner frame for each running indexer
		for _, state := range m.coordinator.State() {
			if state.Status == "running" {
				m.spinnerFrames[state.Name] = spinnerFrames[rand.Intn(len(spinnerFrames))]
			}
		}

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

	// Indexer name (right-aligned to 10 chars for alignment)
	nameStr := fmt.Sprintf("%10s", state.Name)
	b.WriteString(indexerStyle.Render(nameStr))
	b.WriteString(" ")

	switch state.Status {
	case "completed":
		b.WriteString(doneStyle.Render("█"))
		b.WriteString(" ")
		b.WriteString(dimStyle.Render(fmt.Sprintf("(%s)", formatDuration(state.Elapsed()))))

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
		// Random spinner frame per indexer (styled in magenta/pink)
		frame := m.spinnerFrames[state.Name]
		if frame == "" {
			frame = spinnerFrames[0] // Fallback
		}
		b.WriteString(m.spinner.Style.Render(frame))
		b.WriteString(" ")

		if state.Total > 0 {
			// Known total - show progress bar + percentage + rate + ETA
			pct := float64(state.Current) / float64(state.Total)
			filled := int(pct * float64(barWidth))
			if filled > barWidth {
				filled = barWidth
			}
			bar := strings.Repeat("█", filled) + strings.Repeat("░", barWidth-filled)
			b.WriteString(bar)
			b.WriteString(fmt.Sprintf(" %3d%%", int(pct*100)))

			// Rate
			rate := state.Rate()
			if rate > 0 {
				b.WriteString("  ")
				b.WriteString(rateStyle.Render(formatRate(rate)))
			}

			// ETA
			eta := state.ETA()
			if eta > 0 {
				b.WriteString("  ")
				b.WriteString(etaStyle.Render(formatETA(eta)))
			}
		} else {
			// Unknown total - show count + rate + elapsed
			b.WriteString(formatCount(state.Current))
			b.WriteString(" items")

			rate := state.Rate()
			if rate > 0 {
				b.WriteString("  ")
				b.WriteString(rateStyle.Render(formatRate(rate)))
			}

			b.WriteString("  ")
			b.WriteString(dimStyle.Render(formatDuration(state.Elapsed()) + " elapsed"))
		}

		// Current item (truncated to fit)
		if state.Item != "" {
			item := state.Item
			// Calculate remaining space
			used := len(nameStr) + 3 + barWidth + 40 // rough estimate
			remaining := totalWidth - used - 5
			if remaining > 15 && len(item) > remaining {
				item = "..." + item[len(item)-remaining+3:]
			}
			if remaining > 15 {
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

// formatDuration returns a human-friendly duration string.
// Examples: "< 1s", "5s", "1m 30s", "2h 15m"
func formatDuration(d time.Duration) string {
	if d < time.Second {
		return "< 1s"
	}

	d = d.Round(time.Second)

	hours := int(d.Hours())
	minutes := int(d.Minutes()) % 60
	seconds := int(d.Seconds()) % 60

	if hours > 0 {
		if minutes > 0 {
			return fmt.Sprintf("%dh %dm", hours, minutes)
		}
		return fmt.Sprintf("%dh", hours)
	}

	if minutes > 0 {
		if seconds > 0 {
			return fmt.Sprintf("%dm %ds", minutes, seconds)
		}
		return fmt.Sprintf("%dm", minutes)
	}

	return fmt.Sprintf("%ds", seconds)
}

// formatETA returns "X left" right-padded to 12 chars, or empty string if duration is 0.
func formatETA(d time.Duration) string {
	if d <= 0 {
		return ""
	}
	s := formatDuration(d) + " left"
	// Pad to 12 chars for consistent width (handles up to "59m 59s left")
	if len(s) < 12 {
		s = s + strings.Repeat(" ", 12-len(s))
	}
	return s
}

// formatRate returns a fixed-width rate string like "(  1,234/s)" or "( 12.3k/s)".
// Fixed width of 11 chars to prevent UI wiggle.
func formatRate(rate float64) string {
	if rate < 1 {
		return "           " // 11 spaces to maintain alignment
	}
	if rate >= 10000 {
		// Format like "( 12.3k/s)" - 10 chars inside parens
		return fmt.Sprintf("(%6.1fk/s)", rate/1000)
	}
	if rate >= 1000 {
		// Format like "(  1,234/s)" - use comma formatting
		return fmt.Sprintf("(%7s/s)", formatCount(int(rate)))
	}
	// Format like "(    123/s)"
	return fmt.Sprintf("(%7d/s)", int(rate))
}

// formatCount returns a number with comma separators.
func formatCount(n int) string {
	return numPrinter.Sprintf("%d", n)
}

// FormatSummary formats the final summary for printing after TUI exits.
func FormatSummary(summaries []IndexerSummary, totalDuration time.Duration) string {
	var b strings.Builder

	b.WriteString(fmt.Sprintf("\nIndexed in %s\n", formatDuration(totalDuration)))

	for _, s := range summaries {
		if s.Status == "error" {
			b.WriteString(fmt.Sprintf("  %10s  error: %v\n", s.Name, s.Error))
		} else if s.Items > 0 {
			b.WriteString(fmt.Sprintf("  %10s  %s  (%s, %s)\n",
				s.Name,
				formatDuration(s.Duration),
				formatCount(s.Items),
				formatRate(s.Rate)))
		} else {
			b.WriteString(fmt.Sprintf("  %10s  %s\n",
				s.Name,
				formatDuration(s.Duration)))
		}
	}

	return b.String()
}
