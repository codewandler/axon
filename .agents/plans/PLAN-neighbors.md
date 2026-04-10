# PLAN: Edge Traversal / Neighbor Discovery (Issue #2)

Design reference: `DESIGN-neighbors.md`
Estimated total: ~20 minutes

---

## Task 1: Library types and `Neighbors` method

**Files created**: `neighbors.go`
**Files created**: `neighbors_test.go`

### Code

`neighbors.go` — `NeighborResult`, `NeighborsOptions`, `(*Axon).Neighbors`.
See design for full signatures. Key behaviour:
- Resolve URI via `storage.GetNodeByURI`
- Collect outgoing edges via `storage.GetEdgesFrom`, incoming via `storage.GetEdgesTo`
- Filter by edge type set if `EdgeTypes` non-empty
- Skip orphaned edges (GetNode failure) silently
- Truncate to `Max` if > 0

`neighbors_test.go` — 9 table-driven tests:
- `TestNeighbors_Outgoing`
- `TestNeighbors_Incoming`
- `TestNeighbors_Both`
- `TestNeighbors_DefaultDirection`
- `TestNeighbors_EdgeTypeFilter`
- `TestNeighbors_MaxLimit`
- `TestNeighbors_NoEdges`
- `TestNeighbors_NodeNotFound`
- `TestNeighbors_ResultMetadata`

**Verification**:
```bash
go test -run TestNeighbors -v ./...
```

---

## Task 2: CLI command

**Files created**: `cmd/axon/neighbors.go`
**Files modified**: `cmd/axon/main.go`

### Code

`cmd/axon/neighbors.go`:
- `neighborsCmd` cobra command with flags: `--direction`, `--edge-type`, `--max`, `--output`
- `runNeighbors` — opens DB, resolves URI via `resolveNeighborURI` (URI → name → ID),
  calls `ax.Neighbors`, dispatches to renderer
- `resolveNeighborURI` — reuses `resolvePathNodeID` from `path.go`
- Three renderers: `renderNeighborsText`, `renderNeighborsTable`, `renderNeighborsJSON`

`cmd/axon/main.go`:
```go
rootCmd.AddCommand(neighborsCmd)
```

**Verification**:
```bash
go build ./cmd/axon/...
go run ./cmd/axon neighbors --help
```

---

## Task 3: Documentation

**Files modified**: `README.md`, `.agents/skills/axon/SKILL.md`

### README.md
Add `axon neighbors` entry to the CLI Reference section alongside `axon path`.

### SKILL.md
Add `axon neighbors` to the **Available commands** list with a one-line description
and example usage.

**Verification**:
```bash
grep "neighbors" README.md
grep "neighbors" .agents/skills/axon/SKILL.md
```

---

## Task 4: Self-review + pre-flight

```bash
go build ./...
go vet ./...
go test -race ./...
go run ./cmd/axon neighbors --help
```

Status at task start: Tasks 1 and 2 are already implemented. Task 3 and 4 remain.
