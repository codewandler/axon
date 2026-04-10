# Plan: `axon index` Progress & L&F Overhaul

**Design ref**: `.agents/plans/DESIGN-20260410-index-ux.md`  
**Date**: 2026-04-10  
**Estimated total**: ~4h

Phase labels: **Indexing** / **Vectorizing**  
GC row: always shown  
Spinner: keep braille frames  

---

## Task 1 — Add `Phase` to the progress event model

**Files modified**:
- `progress/events.go`
- `progress/coordinator.go`

**Estimated time**: 15 min

### `progress/events.go`

Add `Phase string` to `Event` and a new `StartedInPhase` helper.

```go
// Event — add field:
Phase string // optional display group, e.g. "Indexing", "Vectorizing"

// New helper (after Started):
// StartedInPhase creates a started event tagged with a phase label.
func StartedInPhase(indexer, phase string) Event {
    e := NewEvent(indexer, EventStarted)
    e.Phase = phase
    return e
}
```

### `progress/coordinator.go`

Add `Phase string` to `IndexerState` and `IndexerSummary`. Populate both from the first event received for each indexer.

```go
// IndexerState — add field:
Phase string // display group ("Indexing", "Vectorizing", …)

// IndexerSummary — add field:
Phase string

// processEvent — in the EventStarted case (after setting state.Status):
if event.Phase != "" {
    state.Phase = event.Phase
}

// Summary() — copy phase when building each summary:
summary := IndexerSummary{
    Name:     state.Name,
    Phase:    state.Phase,       // ← add this line
    Status:   state.Status,
    Duration: state.Elapsed(),
    Items:    state.Total,
    Error:    state.Error,
}
```

**Verification**:
```
go build ./progress/...
go test ./progress/...
```

---

## Task 2 — Wire embedding progress events

**Files modified**: `indexer/embeddings/indexer.go`

**Estimated time**: 20 min

`PostIndex` receives `ictx.Progress` already (set by `axon.go`). It just never uses it. Fix:

```go
import (
    "context"
    "strings"

    "github.com/codewandler/axon/graph"
    "github.com/codewandler/axon/indexer"
    "github.com/codewandler/axon/progress"   // ← add import
    "github.com/codewandler/axon/types"
)

// sendProgress sends a progress event, no-op if channel is nil.
func sendProgress(ch chan<- progress.Event, evt progress.Event) {
    if ch == nil {
        return
    }
    ch <- evt
}
```

Replace the silent `PostIndex` body with:

```go
func (i *Indexer) PostIndex(ctx context.Context, ictx *indexer.Context) error {
    storage := ictx.Graph.Storage()

    embStore, ok := storage.(interface {
        PutEmbedding(ctx context.Context, nodeID string, embedding []float32) error
    })
    if !ok {
        return nil
    }

    embedTypes := i.Types
    if len(embedTypes) == 0 {
        embedTypes = DefaultEmbedTypes
    }
    batchSize := i.BatchSize
    if batchSize <= 0 {
        batchSize = DefaultBatchSize
    }

    type entry struct {
        nodeID string
        text   string
    }
    var entries []entry

    for _, nodeType := range embedTypes {
        nodes, err := storage.FindNodes(ctx, graph.NodeFilter{
            Type:       nodeType,
            Generation: ictx.Generation,
        }, graph.QueryOptions{})
        if err != nil {
            return err
        }
        for _, node := range nodes {
            entries = append(entries, entry{node.ID, buildNodeText(node)})
        }
    }

    if len(entries) == 0 {
        return nil // nothing to embed, skip progress noise
    }

    sendProgress(ictx.Progress, progress.StartedInPhase("embeddings", "Vectorizing"))

    embedded := 0
    for start := 0; start < len(entries); start += batchSize {
        end := min(start+batchSize, len(entries))
        batch := entries[start:end]

        texts := make([]string, len(batch))
        for j, e := range batch {
            texts[j] = e.text
        }

        embeddings, err := i.Provider.EmbedBatch(ctx, texts)
        if err != nil {
            // Non-fatal: skip this batch rather than aborting the whole run.
            embedded += len(batch) // still count as processed
            sendProgress(ictx.Progress, progress.ProgressWithTotal(
                "embeddings", embedded, len(entries), ""))
            continue
        }

        for j, e := range batch {
            if j >= len(embeddings) {
                break
            }
            if err := embStore.PutEmbedding(ctx, e.nodeID, embeddings[j]); err != nil {
                return err
            }
        }

        embedded += len(batch)
        // Report current item as the last node name in the batch for display
        lastItem := batch[len(batch)-1].text
        if idx := strings.Index(lastItem, " "); idx > 0 {
            lastItem = lastItem[:idx] // first word = node name
        }
        sendProgress(ictx.Progress, progress.ProgressWithTotal(
            "embeddings", embedded, len(entries), lastItem))
    }

    sendProgress(ictx.Progress, progress.Completed("embeddings", len(entries)))
    return nil
}
```

**Verification**:
```
go build ./indexer/embeddings/...
go test ./indexer/embeddings/...
```

---

## Task 3 — Phase section headers in the live TUI

**Files modified**: `progress/ui.go`

**Estimated time**: 30 min

### 3a — Add `phaseStyle` and a `normalizePhase` helper

```go
var (
    indexerStyle = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("39"))
    doneStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color("42"))
    errorStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("196"))
    dimStyle     = lipgloss.NewStyle().Foreground(lipgloss.Color("240"))
    itemStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color("245"))
    rateStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color("244"))
    etaStyle     = lipgloss.NewStyle().Foreground(lipgloss.Color("214"))
    phaseStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("240")).Bold(false) // ← new
)

// normalizePhase returns "Indexing" for the empty phase (main indexers never set a phase).
func normalizePhase(p string) string {
    if p == "" {
        return "Indexing"
    }
    return p
}
```

### 3b — Update `Model` struct

```go
type Model struct {
    coordinator   *Coordinator
    spinner       spinner.Model
    spinnerFrames map[string]string // Per-indexer random spinner frames
    tickCount     int               // ← new: increments each tick for deterministic spinner
    width         int
    done          bool
}
```

### 3c — Increment `tickCount` in `Update`

In the `tickMsg` case, replace the existing body with:

```go
case tickMsg:
    m.tickCount++

    // Update per-indexer spinner frames using deterministic offset
    for idx, state := range m.coordinator.State() {
        if state.Status == "running" {
            frame := spinnerFrames[(m.tickCount+idx*3)%len(spinnerFrames)]
            m.spinnerFrames[state.Name] = frame
        }
    }

    if !m.coordinator.IsRunning() && len(m.coordinator.State()) > 0 {
        m.done = true
        return m, tea.Quit
    }
    return m, tickCmd()
```

### 3d — Render phase headers in `View()`

Replace the `View()` body from `states := m.coordinator.State()` onwards:

```go
states := m.coordinator.State()
if len(states) == 0 {
    b.WriteString(m.spinner.View())
    b.WriteString(" Starting indexers...")
    b.WriteString("\n")
    return b.String()
}

// Compute name column width from all known indexer names
nameColWidth := 10
for _, s := range states {
    if len(s.Name) > nameColWidth {
        nameColWidth = len(s.Name)
    }
}
nameColWidth++ // 1 char padding

width := m.width
if width == 0 {
    width = 80
}
// Smooth bar width: leave room for name, spinner, %, rate, ETA, item
fixedCols := nameColWidth + 60
barWidth := (width - fixedCols) / 3
if barWidth < 10 {
    barWidth = 10
}
if barWidth > 40 {
    barWidth = 40
}

lastPhase := ""
for _, state := range states {
    phase := normalizePhase(state.Phase)

    // Emit phase header when phase changes
    if phase != lastPhase {
        if lastPhase != "" {
            b.WriteString("\n") // blank line between phases
        }
        b.WriteString(phaseStyle.Render("  " + phase))
        b.WriteString("\n")
        lastPhase = phase
    }

    line := m.renderIndexerLine(state, barWidth, width, nameColWidth)
    b.WriteString(line)
    b.WriteString("\n")
}

return b.String()
```

### 3e — Update `renderIndexerLine` signature and internals

Change signature:
```go
func (m Model) renderIndexerLine(state *IndexerState, barWidth, totalWidth, nameColWidth int) string {
```

Replace the name formatting line:
```go
// old:
nameStr := fmt.Sprintf("%10s", state.Name)
// new:
nameStr := fmt.Sprintf("%*s", nameColWidth, state.Name)
```

Replace `" items"` with `" nodes"`:
```go
// old:
b.WriteString(" items")
// new:
b.WriteString(" nodes")
```

**Verification**:
```
go build ./progress/...
go test ./progress/...
```

---

## Task 4 — Summary redesign

**Files modified**: `progress/ui.go`, `progress/ui_test.go`

**Estimated time**: 45 min

### 4a — Rewrite `FormatSummary`

Replace the entire `FormatSummary` function:

```go
// FormatSummary formats the final summary grouped by phase.
// Phases appear in the order they were first seen across summaries.
// Each row is prefixed with ✓ (completed) or ✗ (error).
func FormatSummary(summaries []IndexerSummary, totalDuration time.Duration) string {
    var b strings.Builder

    b.WriteString(fmt.Sprintf("\nIndexed in %s\n", formatDuration(totalDuration)))

    if len(summaries) == 0 {
        return b.String()
    }

    // Auto-size name column
    nameColWidth := 0
    for _, s := range summaries {
        if len(s.Name) > nameColWidth {
            nameColWidth = len(s.Name)
        }
    }

    // Collect phases in order of first appearance
    seen := make(map[string]bool)
    var phases []string
    byPhase := make(map[string][]IndexerSummary)
    for _, s := range summaries {
        phase := normalizePhase(s.Phase)
        if !seen[phase] {
            seen[phase] = true
            phases = append(phases, phase)
        }
        byPhase[phase] = append(byPhase[phase], s)
    }

    multiPhase := len(phases) > 1

    for _, phase := range phases {
        if multiPhase {
            b.WriteString("\n")
            b.WriteString(fmt.Sprintf("  %s\n", phase))
        }

        for _, s := range byPhase[phase] {
            icon := "✓"
            if s.Status == "error" {
                icon = "✗"
            }

            if s.Status == "error" {
                b.WriteString(fmt.Sprintf("  %s  %*s  error: %v\n",
                    errorStyle.Render(icon), nameColWidth, s.Name, s.Error))
            } else if s.Items > 0 {
                b.WriteString(fmt.Sprintf("  %s  %*s  %s  (%s, %s)\n",
                    doneStyle.Render(icon),
                    nameColWidth, s.Name,
                    formatDuration(s.Duration),
                    formatCount(s.Items),
                    formatRate(s.Rate)))
            } else {
                b.WriteString(fmt.Sprintf("  %s  %*s  %s\n",
                    doneStyle.Render(icon),
                    nameColWidth, s.Name,
                    formatDuration(s.Duration)))
            }
        }
    }

    return b.String()
}
```

### 4b — Update `TestFormatSummary`

Add `Phase` fields to the test summaries and extend assertions:

```go
func TestFormatSummary(t *testing.T) {
    summaries := []IndexerSummary{
        {Name: "fs", Phase: "Indexing", Status: "completed", Duration: 3*time.Minute + 20*time.Second, Items: 631421, Rate: 3150},
        {Name: "markdown", Phase: "Indexing", Status: "completed", Duration: 1*time.Minute + 25*time.Second, Items: 43477, Rate: 508},
        {Name: "gc", Phase: "Indexing", Status: "completed", Duration: 200 * time.Millisecond, Items: 0},
        {Name: "embeddings", Phase: "Vectorizing", Status: "completed", Duration: 30 * time.Second, Items: 1204, Rate: 40},
    }

    output := FormatSummary(summaries, 3*time.Minute+45*time.Second)

    checks := []struct {
        want string
        desc string
    }{
        {"3m 45s", "total duration"},
        {"fs", "fs row"},
        {"631,421", "fs item count"},
        {"markdown", "markdown row"},
        {"embeddings", "embeddings row"},
        {"1,204", "embeddings item count"},
        {"Indexing", "Indexing phase header"},
        {"Vectorizing", "Vectorizing phase header"},
        {"✓", "done icon"},
    }

    for _, c := range checks {
        if !contains(output, c.want) {
            t.Errorf("Summary missing %s: want %q in:\n%s", c.desc, c.want, output)
        }
    }
}
```

**Verification**:
```
go build ./progress/...
go test ./progress/... -v -run TestFormatSummary
```

---

## Task 5 — Full build + all tests

**Estimated time**: 10 min

```
go build ./...
go vet ./...
go test ./...
```

Expected: all tests pass, zero vet warnings.

Also do a manual smoke check — build and run against a real repo:
```
go build -o ./bin/axon ./cmd/axon
./bin/axon index .
```

Confirm:
- [ ] Live TUI shows `  Indexing` section header with fs/git/golang/markdown/project rows
- [ ] Summary shows `✓` icons and rows are properly aligned
- [ ] No regressions on non-embedding runs

If `--embed` is available:
```
./bin/axon index --embed .
```
Confirm:
- [ ] TUI shows `  Vectorizing` section header with a live embedding row (bar, %, rate, ETA)
- [ ] Summary shows both `Indexing` and `Vectorizing` sections

---

## Implementation order

```
Task 1  →  Task 2  →  Task 3  →  Task 4  →  Task 5
 data       events      TUI        summary    verify
 model      wired       phases     redesign
```

Each task leaves the build in a passing state. Tasks 3 and 4 can be worked in parallel if desired (both only touch `progress/ui.go` but separate functions).
