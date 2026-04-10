# Design: `axon index` UX & Progress Overhaul

**Date**: 2026-04-10  
**Status**: Draft — awaiting approval  
**Scope**: Progress reporting, Look & Feel of `axon index` (TTY mode)

---

## Problem Statement

Running `axon index --embed` today leaves the user completely in the dark during the embedding phase:

- Indexers (fs, git, golang, markdown, project) show live progress via the bubbletea TUI.
- When they finish, the embedding `PostIndexer` runs **silently** — no row in the UI, no spinner, no count, no ETA. On large codebases with Ollama or Hugot this can take tens of seconds.
- Beyond embeddings, the overall Look & Feel of the progress display has rough edges that make it feel unpolished.

---

## Root Cause (Technical)

In `axon.go`, `PostIndex` is called with a `postCtx` that has `Progress: prog` wired up correctly.  
However `indexer/embeddings/indexer.go::PostIndex` **never sends a single event** to `ictx.Progress`.  
The channel is there — it just isn't used.

The fix for the embedding blind spot is therefore surgical: emit `Started / ProgressWithTotal / Completed` events from `PostIndex`.  
Everything else described below is an opportunity to improve the broader L&F.

---

## Proposed Changes

### Area 1 — Embedding Progress (high priority, small effort)

Wire up embedding progress events in `indexer/embeddings/indexer.go::PostIndex`:

```
progress.Started("embeddings")
 for each batch:
   progress.ProgressWithTotal("embeddings", nodesEmbedded, totalNodes, currentNodeName)
progress.Completed("embeddings", totalNodes)
```

Since the total is known upfront (we collect all entries before batching), the UI can show:
- A proper fill bar with percentage
- Items/s rate
- ETA

**What the user sees:**
```
 embeddings  ██████████░░░░░░░░░░  51%  (  38.2/s)  12s left  go:func Node.Clone
```

This is a one-file change with zero architectural impact.

---

### Area 2 — Phase Grouping in the TUI (medium priority, medium effort)

Currently all indexers appear as a flat list. The user cannot tell when the main scan is done and post-processing begins.

**Proposal**: Introduce optional phase labels in the coordinator.

Two phases:
- **Indexing** — fs, git, golang, markdown, project (run concurrently/event-driven)
- **Post-processing** — markdown link resolution, embeddings (run serially after flush)

The TUI renders a faint section header before the first row of each phase:

```
  Indexing
        fs  █  (4s)
       git  █  (2s)
    golang  █  (3s)
  markdown  █  (1s)
   project  █  (< 1s)

  Post-processing
  embeddings  ████████░░░░░░░░░░░░  40%  (  42/s)  8s left
```

**Implementation sketch**:
- Add `Phase string` to `progress.Event` (backward-compatible, zero value = no phase).
- Coordinator tracks which phase each indexer belongs to and maintains phase order.
- `Model.View()` renders a dim section header when the phase changes between rows.
- `IndexWithOptions` in `axon.go` tags PostIndexer progress events with `Phase: "post-processing"`.

---

### Area 3 — Completion Summary Redesign (medium priority, small effort)

Current `FormatSummary` output:
```
Indexed in 8s
        fs  4s  (12,341, 3,085/s)
       git  2s  (  312,   156/s)
    golang  3s  (1,204,   401/s)
  markdown  1s
   project  < 1s
```

Problems:
- No visual hierarchy — every row looks the same.
- No phase separation.
- Embedding is absent (because it never reported anything).
- "Indexed in 8s" is misleading when embedding took 30s more.

**Proposed summary:**
```
  Indexed in 38s  (8s indexing · 30s embeddings)

  Indexing
   ✓  fs          4s    12,341 nodes   3,085/s
   ✓  git         2s       312 nodes     156/s
   ✓  golang      3s     1,204 nodes     401/s
   ✓  markdown    1s
   ✓  project    <1s

  Post-processing
   ✓  embeddings  30s    1,204 nodes      42/s
```

Changes needed:
- `Coordinator` tracks phase start/end times.
- `FormatSummary` accepts phase groupings.
- Total line breaks down time by phase.
- ✓ / ✗ icons for completed/errored rows.

---

### Area 4 — Visual Polish (low priority, small effort)

Several small things that make the running UI feel jittery or inconsistent:

| Issue | Fix |
|---|---|
| Indexer name column is 10 chars right-padded — short names look oddly spaced | Auto-size column to longest name + 1 |
| "X items" label when total is unknown feels ambiguous | Show "X nodes" to match axon's vocabulary |
| Rate and ETA columns jump width as numbers grow | Already has fixed-width formatting for rate; apply same discipline to ETA |
| `barWidth` hard-switches at width=100 | Smooth linear calculation: `barWidth = (termWidth - fixedCols) / 3`, clamped 10–40 |
| Spinner uses random frames per indexer — visually noisy | Give each indexer a *deterministic* spinner offset (index % len(frames)) so they cycle together |
| The "gc" row appears at the very end and always says 0s | Suppress gc row if it completes in < 50ms (it's almost always instant) |
| No indication that `--embed` was activated before indexing begins | Print a short "Embeddings: ollama (nomic-embed-text)" line before the TUI starts, like we do for "Using database:" |

---

### Area 5 — Watch Mode Polish (out of scope for now, noted)

Watch mode never shows a TUI — it falls back to plain stderr lines. That's fine for now but worth revisiting later if the watch loop becomes a primary workflow. Noted for future work, not blocking.

---

## Out of Scope

- Changing the progress event schema in a breaking way.
- Changing any non-TTY (piped) output format.
- Embedding provider UX (model download progress, etc.).
- Watch mode TUI.

---

## Acceptance Criteria

- [ ] `axon index --embed` shows a live embedding row with bar, %, rate, ETA.
- [ ] The running TUI shows a visual separation between "Indexing" and "Post-processing" phases.
- [ ] The final summary includes embedding stats and a per-phase time breakdown.
- [ ] All existing tests pass (`go test ./...`).
- [ ] No regression on non-TTY output (piped mode).
- [ ] Visual polish items (Area 4) applied.

---

## Implementation Order

1. **Area 1** — Embedding progress events (pure `indexer/embeddings/indexer.go` change, 30 min)
2. **Area 4** — Visual polish (pure `progress/ui.go` change, 1h)
3. **Area 3** — Summary redesign (`progress/ui.go` + `Coordinator`, 1h)
4. **Area 2** — Phase grouping (coordinator + events + UI + axon.go, 2h)

Total estimated effort: ~4–5h of focused coding.

---

## Open Questions for User

1. **Phase labels** — do you want "Indexing / Post-processing" as the section names, or something else (e.g. "Scan / Enrich")?
2. **GC row** — should GC always be hidden (it's nearly instant), or shown when it actually removes nodes?
3. **Spinner style** — the current braille spinner (⣾⣽⣻…) — keep it, or switch to something else (dots, arc, line)?
4. **Summary position** — should the summary print above or below the final result line (`Indexed 1,204 files...`)?
