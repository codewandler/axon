package main

import (
	"strings"
)

type helpEntry struct {
	key  string
	desc string
}

// renderHelpBar renders the bottom help bar.
func renderHelpBar(width int, queryFocused bool, previewVisible bool, previewFocused bool, queryExtended bool) string {
	var entries []helpEntry

	if queryFocused {
		entries = []helpEntry{
			{"type", "filter"},
			{"1-5", "category"},
			{"Enter", "apply"},
			{"Esc", "back"},
		}
		if queryExtended {
			entries = append(entries, helpEntry{"Tab", "simple"})
		} else {
			entries = append(entries, helpEntry{"Tab", "full AQL"})
		}
	} else if previewFocused {
		entries = []helpEntry{
			{"↑↓", "scroll"},
			{"Tab", "filter"},
			{"Esc", "back"},
		}
	} else {
		entries = []helpEntry{
			{"←", "back"},
			{"→", "open"},
			{"↑↓", "select"},
			{"+/-", "zoom"},
			{"Tab", "filter"},
			{"Esc", "quit"},
		}
	}

	var b strings.Builder
	for i, e := range entries {
		if i > 0 {
			b.WriteString("  ")
		}
		b.WriteString(helpKeyStyle.Render(e.key))
		b.WriteString(" ")
		b.WriteString(helpDescStyle.Render(e.desc))
	}

	return b.String()
}
