package main

import "github.com/charmbracelet/lipgloss"

// Color palette - futuristic terminal aesthetic
var (
	// Primary accent - electric cyan
	colorAccent = lipgloss.Color("#00D4FF")
	// Secondary accent - warm amber
	colorAccentWarm = lipgloss.Color("#FFB347")
	// Muted text
	colorMuted = lipgloss.Color("#6C7086")
	// Subtle borders and separators
	colorBorder = lipgloss.Color("#45475A")
	// Bright text
	colorBright = lipgloss.Color("#CDD6F4")
	// Success / active indicator
	colorActive = lipgloss.Color("#A6E3A1")
	// Error / warning
	colorError = lipgloss.Color("#F38BA8")
	// Type badge colors
	colorTypeFS  = lipgloss.Color("#89B4FA")
	colorTypeVCS = lipgloss.Color("#A6E3A1")
	colorTypeMD  = lipgloss.Color("#CBA6F7")
	// Section header
	colorSection = lipgloss.Color("#F9E2AF")
	// Dim for IDs and secondary info
	colorDim = lipgloss.Color("#585B70")
	// Selected item background
	colorSelectedBg = lipgloss.Color("#313244")
)

// Layout styles
var (
	// Detail panel (top region)
	detailStyle = lipgloss.NewStyle().
			Padding(0, 1)

	// Node name in detail panel
	nodeNameStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(colorBright)

	// Node ID
	nodeIDStyle = lipgloss.NewStyle().
			Foreground(colorDim)

	// Label badges
	labelStyle = lipgloss.NewStyle().
			Foreground(colorAccent)

	// URI style
	uriStyle = lipgloss.NewStyle().
			Foreground(colorMuted)

	// Pane title (INCOMING / OUTGOING)
	paneTitleActiveStyle = lipgloss.NewStyle().
				Bold(true).
				Foreground(colorAccent)

	paneTitleInactiveStyle = lipgloss.NewStyle().
				Bold(true).
				Foreground(colorMuted)

	// Section headers (edge type groups)
	sectionHeaderStyle = lipgloss.NewStyle().
				Foreground(colorSection).
				Bold(true)

	// Item in edge list (normal)
	itemStyle = lipgloss.NewStyle().
			Foreground(colorBright)

	// Item in edge list (selected / cursor)
	itemSelectedStyle = lipgloss.NewStyle().
				Foreground(colorAccent).
				Bold(true).
				Background(colorSelectedBg)

	// Cursor indicator
	cursorStyle = lipgloss.NewStyle().
			Foreground(colorAccent).
			Bold(true)

	// Help bar
	helpKeyStyle = lipgloss.NewStyle().
			Foreground(colorAccent).
			Bold(true)

	helpDescStyle = lipgloss.NewStyle().
			Foreground(colorMuted)

	// Breadcrumb
	breadcrumbActiveStyle = lipgloss.NewStyle().
				Foreground(colorBright).
				Bold(true)

	// Divider line between sections
	dividerStyle = lipgloss.NewStyle().
			Foreground(colorBorder)

	// Zoom indicator
	zoomStyle = lipgloss.NewStyle().
			Foreground(colorAccentWarm).
			Bold(true)
)

// Type emoji mappings
var typeEmojis = map[string]string{
	"fs:dir":       "📁",
	"fs:file":      "📄",
	"fs:link":      "🔗",
	"vcs:repo":     "📦",
	"vcs:remote":   "🌐",
	"vcs:branch":   "🌿",
	"vcs:tag":      "🏷️",
	"md:document":  "📝",
	"md:section":   "📑",
	"md:codeblock": "💻",
	"md:link":      "🔗",
	"md:image":     "🖼️",
}

// typeColor returns a color based on the node type domain.
func typeColor(nodeType string) lipgloss.Color {
	if len(nodeType) >= 2 {
		switch nodeType[:2] {
		case "fs":
			return colorTypeFS
		case "vc":
			return colorTypeVCS
		case "md":
			return colorTypeMD
		}
	}
	return colorMuted
}
