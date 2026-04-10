# DESIGN: Edge Traversal / Neighbor Discovery (Issue #2)

## Problem

`axon_search` finds nodes but cannot walk edges. There is no first-class way to
ask "what calls this function?", "what implements this interface?", or "what does
this struct depend on?" — even though all the relationships exist in the graph.
`axon_query` with raw AQL works but requires schema knowledge that most callers
don't have.

## Proposed Solution

Add a `Neighbors` API to the `Axon` type that, given a node URI and a direction,
returns all immediately connected nodes together with the edge metadata that
connects them.

### Public API signature

```go
// NeighborResult is one entry: the connected node + the edge linking it to the origin.
type NeighborResult struct {
    Node      *graph.Node
    EdgeType  string  // e.g. "calls", "implements", "contains"
    Direction string  // "in" or "out"
    EdgeID    string
}

// NeighborsOptions configures the traversal.
type NeighborsOptions struct {
    Direction string   // "in", "out", "both" — default "both"
    EdgeTypes []string // optional whitelist; empty = all types
    Max       int      // 0 = no limit
}

func (a *Axon) Neighbors(ctx context.Context, uri string, opts NeighborsOptions) ([]*NeighborResult, error)
```

### CLI command

```
axon neighbors <uri-or-name-or-id>
  --direction  in|out|both   (default: both)
  --edge-type  <type>        (repeatable)
  --max        <n>           (default: 50)
  --output     text|table|json
```

URI resolution follows the same three-step order as `axon path`:
URI → exact name → node ID.

### Output example (text)

```
3 neighbor(s) of "Storage":

  <- implements  [abc1234] SQLiteStorage  (go:struct)
  -> defines     [def5678] PutNode        (go:func)
  -> defines     [ghi9012] GetNode        (go:func)
```

## Architecture

| Layer | Change | Reason |
|-------|--------|--------|
| `neighbors.go` (new, package `axon`) | `NeighborResult`, `NeighborsOptions`, `(*Axon).Neighbors` | All public API lives at the top-level package, same as `FindPath` |
| `cmd/axon/neighbors.go` (new) | Cobra command, three output renderers | Consistent with `path.go`, `find.go` pattern |
| `cmd/axon/main.go` | Register `neighborsCmd` | One-line addition |

No storage layer changes needed — `GetEdgesFrom` / `GetEdgesTo` already exist
on `graph.Storage` and return full edge structs.

## Key Decisions

| Decision | Rationale |
|----------|-----------|
| URI as the input identifier (not node ID) | URIs are stable, human-readable, and used in all other tools |
| Same 3-step resolution as `axon path` | Consistent UX; users can pass a function name without knowing the URI |
| `NeighborResult` carries edge ID | Enables follow-up queries (e.g. edge metadata) without a second call |
| Direction defaults to "both" | Most useful default for exploratory use |
| Orphaned edges skipped silently | Storage integrity issues shouldn't surface as errors to the caller |
| `Max` defaults to 50 in CLI, 0 (unlimited) in library | CLI needs a safety bound; library callers handle limits themselves |

## Out of Scope

- Multi-hop traversal (that's `FindPath`)
- Edge data / metadata beyond type and ID
- Pagination

## Files Changed

| File | Action |
|------|--------|
| `neighbors.go` | Create |
| `neighbors_test.go` | Create |
| `cmd/axon/neighbors.go` | Create |
| `cmd/axon/main.go` | Modify (register command) |
| `README.md` | Modify (add `axon neighbors` to CLI reference) |
| `.agents/skills/axon/SKILL.md` | Modify (add `axon neighbors` to commands list) |

## Acceptance Criteria (from issue)

- [ ] Given a node URI, return all connected nodes with edge type and direction
- [ ] Support `direction`: `in`, `out`, `both`
- [ ] Support optional `edge_type` filter
- [ ] Support `max` limit
- [ ] Output includes node name, type (file/line is on the node's data, not a
      first-class field — surfaced via the node's URI and data payload)
