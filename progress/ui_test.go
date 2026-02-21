package progress

import (
	"testing"
	"time"
)

func TestFormatDuration(t *testing.T) {
	tests := []struct {
		d    time.Duration
		want string
	}{
		{0, "< 1s"},
		{500 * time.Millisecond, "< 1s"},
		{1 * time.Second, "1s"},
		{5 * time.Second, "5s"},
		{59 * time.Second, "59s"},
		{60 * time.Second, "1m"},
		{61 * time.Second, "1m 1s"},
		{90 * time.Second, "1m 30s"},
		{2 * time.Minute, "2m"},
		{2*time.Minute + 30*time.Second, "2m 30s"},
		{60 * time.Minute, "1h"},
		{61 * time.Minute, "1h 1m"},
		{90 * time.Minute, "1h 30m"},
		{2 * time.Hour, "2h"},
		{2*time.Hour + 15*time.Minute, "2h 15m"},
	}

	for _, tt := range tests {
		got := formatDuration(tt.d)
		if got != tt.want {
			t.Errorf("formatDuration(%v) = %q, want %q", tt.d, got, tt.want)
		}
	}
}

func TestFormatRate(t *testing.T) {
	tests := []struct {
		rate float64
		want string
	}{
		{0, "           "},      // 11 spaces
		{0.5, "           "},    // 11 spaces
		{1, "(      1/s)"},      // right-aligned
		{10, "(     10/s)"},     // right-aligned
		{100, "(    100/s)"},    // right-aligned
		{999, "(    999/s)"},    // right-aligned
		{1000, "(  1,000/s)"},   // with comma
		{1234, "(  1,234/s)"},   // with comma
		{9999, "(  9,999/s)"},   // with comma
		{10000, "(  10.0k/s)"},  // k notation
		{12345, "(  12.3k/s)"},  // k notation
		{100000, "( 100.0k/s)"}, // k notation
	}

	for _, tt := range tests {
		got := formatRate(tt.rate)
		if got != tt.want {
			t.Errorf("formatRate(%v) = %q, want %q", tt.rate, got, tt.want)
		}
		// Verify all outputs are same length
		if len(got) != 11 {
			t.Errorf("formatRate(%v) length = %d, want 11", tt.rate, len(got))
		}
	}
}

func TestFormatCount(t *testing.T) {
	tests := []struct {
		n    int
		want string
	}{
		{0, "0"},
		{1, "1"},
		{10, "10"},
		{100, "100"},
		{999, "999"},
		{1000, "1,000"},
		{1234, "1,234"},
		{12345, "12,345"},
		{123456, "123,456"},
		{1234567, "1,234,567"},
	}

	for _, tt := range tests {
		got := formatCount(tt.n)
		if got != tt.want {
			t.Errorf("formatCount(%d) = %q, want %q", tt.n, got, tt.want)
		}
	}
}

func TestFormatETA(t *testing.T) {
	tests := []struct {
		d    time.Duration
		want string
	}{
		{0, ""},
		{-1 * time.Second, ""},
		{1 * time.Second, "1s left     "},  // padded to 12 chars
		{90 * time.Second, "1m 30s left "}, // padded to 12 chars
		{5 * time.Minute, "5m left     "},  // padded to 12 chars
	}

	for _, tt := range tests {
		got := formatETA(tt.d)
		if got != tt.want {
			t.Errorf("formatETA(%v) = %q, want %q", tt.d, got, tt.want)
		}
		// Verify non-empty outputs are 12 chars
		if tt.want != "" && len(got) != 12 {
			t.Errorf("formatETA(%v) length = %d, want 12", tt.d, len(got))
		}
	}
}

func TestIndexerStateRate(t *testing.T) {
	now := time.Now()

	state := &IndexerState{
		Name:      "test",
		Status:    "running",
		Current:   1000,
		StartedAt: now.Add(-10 * time.Second),
	}

	// Without samples, should use overall rate
	rate := state.Rate()
	if rate < 90 || rate > 110 { // Should be ~100/s
		t.Errorf("Rate() without samples = %v, want ~100", rate)
	}

	// Add samples
	state.addRateSample(0, now.Add(-5*time.Second))
	state.addRateSample(500, now.Add(-2500*time.Millisecond))
	state.addRateSample(1000, now)

	rate = state.Rate()
	if rate < 180 || rate > 220 { // Should be ~200/s (1000 items in 5 seconds)
		t.Errorf("Rate() with samples = %v, want ~200", rate)
	}
}

func TestIndexerStateETA(t *testing.T) {
	now := time.Now()

	state := &IndexerState{
		Name:      "test",
		Status:    "running",
		Current:   500,
		Total:     1000,
		StartedAt: now.Add(-5 * time.Second),
	}

	// Add samples for rate calculation
	state.addRateSample(0, now.Add(-5*time.Second))
	state.addRateSample(500, now)

	eta := state.ETA()
	// Rate is 100/s, 500 remaining, so ETA should be ~5s
	if eta < 4*time.Second || eta > 6*time.Second {
		t.Errorf("ETA() = %v, want ~5s", eta)
	}

	// No ETA when total is unknown
	state.Total = 0
	if eta := state.ETA(); eta != 0 {
		t.Errorf("ETA() with no total = %v, want 0", eta)
	}

	// No ETA when completed
	state.Total = 1000
	state.Status = "completed"
	if eta := state.ETA(); eta != 0 {
		t.Errorf("ETA() when completed = %v, want 0", eta)
	}
}

func TestFormatSummary(t *testing.T) {
	summaries := []IndexerSummary{
		{Name: "fs", Status: "completed", Duration: 3*time.Minute + 20*time.Second, Items: 631421, Rate: 3150},
		{Name: "markdown", Status: "completed", Duration: 1*time.Minute + 25*time.Second, Items: 43477, Rate: 508},
		{Name: "gc", Status: "completed", Duration: 200 * time.Millisecond, Items: 0},
	}

	output := FormatSummary(summaries, 3*time.Minute+45*time.Second)

	// Check it contains expected parts
	if !contains(output, "3m 45s") {
		t.Error("Summary should contain total duration '3m 45s'")
	}
	if !contains(output, "fs") {
		t.Error("Summary should contain 'fs'")
	}
	if !contains(output, "631,421") {
		t.Error("Summary should contain '631,421'")
	}
	if !contains(output, "markdown") {
		t.Error("Summary should contain 'markdown'")
	}
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > 0 && containsHelper(s, substr))
}

func containsHelper(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
