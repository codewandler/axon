# DESIGN: Graph Path Finding (issue #6)

## Problem

When exploring a codebase through axon, a common question is **"how does X relate to Y?"**
There is currently no way to discover the *path* between two nodes in the knowledge graph.
Callers (e.g. `flai`) can search for X and Y independently, but cannot bridge them.

## Consumer perspective (flai)

`flai` is an agent harness that uses axon as a library and exposes graph operations as
tool calls. The issue proposes an `axon_path` tool:

```json
{ "from": "go:struct:Call", "to": "go:interface:Connector", "max_depth": 5, "max_paths": 3 }
```

To build that tool, flai needs axon to expose a typed library API — it cannot shell out
to the CLI. The design translates the tool contract into idiomatic Go API.

## Proposed solution

### Public library API (`axon.go` + `path.go`)

```go
// PathStep is one node in a path, annotated with the edge used to reach it.
type PathStep struct {
    Node     *graph.Node
    EdgeType string // "" for the origin node
    Incoming bool   // true when the underlying edge runs from this node to the previous
}

// Path is an ordered sequence of PathSteps.
type Path struct { Steps []PathStep }
func (p *Path) Length() int  // number of edges

// PathOptions configures FindPath.
type PathOptions struct {
    MaxDepth  int      // default 6
    MaxPaths  int      // default 3
    EdgeTypes []string // nil = all types
}

// FindPath finds the shortest paths between two nodes identified by node ID.
// Bidirectional BFS: traverses both outgoing and incoming edges.
// Returns an empty slice (no error) when no path exists within MaxDepth.
func (a *Axon) FindPath(ctx context.Context, fromID, toID string, opts PathOptions) ([]*Path, error)
```

`FindPath` is also added to the `Querier` interface so callers that depend on the
interface (rather than `*Axon` directly) get the method automatically.

### Algorithm (`path.go`)

Bidirectional BFS from `fromID`:
- Each queue entry carries: current node ID, the path steps so far, and a *per-branch*
  visited set (prevents cycles without globally marking nodes, which would block finding
  multiple distinct paths through shared intermediates).
- At each step, expand **outgoing** edges (→ neighbors) then **incoming** edges (← neighbors).
- When a neighbor matches `toID`, record the path.
- Stop when `MaxPaths` paths are found or the queue is empty.
- BFS guarantees shortest-first ordering.

Complexity: O(V + E) per path with depth bounded by `MaxDepth`.

### CLI command (`cmd/axon/path.go`)

```
axon path <from> <to> [flags]

  --max-depth int          Maximum path depth in number of edges (default 6)
  --max-paths int          Maximum number of paths to return (default 3)
  --edge-type stringArray  Restrict traversal to this edge type (repeatable)
  -o, --output string      Output format: text, json (default "text")
```

`<from>` and `<to>` are resolved: URI → exact name → node ID.

## Key decisions

| Decision | Rationale |
|---|---|
| Accept node IDs (not names) in the library API | Names are not unique; ID is the stable identifier. Callers use `Find`/`Search`/`GetNodeByURI` to resolve. |
| Bidirectional traversal (both outgoing + incoming) | Edges like `calls`, `imports`, `contains` are directional; path finding must cross them in both directions or many real paths are missed. |
| Per-branch visited set | Allows finding K distinct paths through shared nodes (e.g. A→B→C and A→B→D→C share B). Global marking would block the second path. |
| `PathStep.Incoming` flag | Callers (flai, CLI) need to render the edge direction accurately in output. |
| `Querier` interface extended | All read methods belong on the interface; this keeps the contract complete. |
| BFS (not Yen's K-shortest) | Yen's is O(KV(V+E)); BFS with depth cap is sufficient for the interactive use case and much simpler. |

## Out of scope

- Weighted paths / Dijkstra
- Named-path AQL syntax (`[:edge*1..5]` already exists in AQL for pattern queries)
- Fuzzy name resolution in the library layer (CLI handles this; library takes IDs)

## Files changed

| File | Change |
|---|---|
| `path.go` | New — `PathStep`, `Path`, `PathOptions`, `findPaths()` BFS, helpers |
| `axon.go` | `FindPath` method + `Querier` interface entry |
| `path_test.go` | New — 10 unit tests covering all branches |
| `cmd/axon/path.go` | New — `axon path` CLI command |
| `cmd/axon/main.go` | Register `pathCmd` |
| `AGENTS.md` | Document new `path` command |

## Acceptance criteria

- [x] `ax.FindPath(ctx, fromID, toID, PathOptions{})` returns shortest paths
- [x] `PathOptions.MaxDepth` caps search depth
- [x] `PathOptions.MaxPaths` caps result count
- [x] `PathOptions.EdgeTypes` filters traversable edge types
- [x] Reverse traversal works (incoming edges)
- [x] No path → empty slice, no error
- [x] Same from/to → empty slice, no error
- [x] `Querier` interface satisfied by `*Axon` (compile-time check)
- [x] `axon path <from> <to>` CLI works with `--output text` and `--output json`
- [x] All tests pass with `-race`
