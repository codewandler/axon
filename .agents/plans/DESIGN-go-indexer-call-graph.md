# DESIGN: Go Indexer — Call Graph & Caller Context (Issue #13)

## Problem

The Go indexer creates `go:ref` nodes for every symbol usage site, with
`go:ref -[references]-> go:func/go:struct/...` edges. However:

1. `RefData` stores only the *position* of the reference — there is no
   `CallerURI` field linking the usage site back to the enclosing function.
2. There are no direct `go:func -[calls]-> go:func` edges. A call graph
   cannot be queried via AQL patterns.
3. Embedded struct relationships are stored as `[]string` names in `StructData`
   rather than as graph edges, so "what structs embed X?" is unanswerable.

## Scope

| Item | In scope |
|---|---|
| Caller context (`CallerURI/Name/Type`) on `go:ref` nodes | ✅ |
| Direct `calls` edges between func/method nodes | ✅ |
| `embeds` edges for struct embedding | ✅ |
| Field reference resolution | ❌ (deferred — genuinely complex, see issue comment) |
| `EndLine` gaps | ❌ (already complete — all AST nodes set EndLine) |

## Design

### New edge types (`types/edges.go`)

```
EdgeCalls  = "calls"   // caller → callee (func/method)
EdgeEmbeds = "embeds"  // struct → embedded struct
```

Both are generic enough for the common edges file. `RegisterGoTypes` will
re-register them with Go-specific type constraints.

### `RefData` extension (`types/golang.go`)

```go
type RefData struct {
    Kind        string   `json:"kind"`
    Name        string   `json:"name"`
    TargetType  string   `json:"target_type,omitempty"`
    TargetPkg   string   `json:"target_pkg,omitempty"`
    // NEW
    CallerURI   string   `json:"caller_uri,omitempty"`
    CallerName  string   `json:"caller_name,omitempty"`
    CallerType  string   `json:"caller_type,omitempty"`
    Position    Position `json:"position"`
}
```

`CallerType` is `go:func` or `go:method`. Empty if the usage is at package
scope (e.g. a var/const initialiser).

### Caller lookup — `funcExtent` slice + binary search

Before iterating `pkg.TypesInfo.Uses`, walk the AST of each file in the
package and collect all `*ast.FuncDecl` nodes that have a body:

```go
type funcExtent struct {
    start    token.Pos  // fd.Pos()
    end      token.Pos  // fd.End()
    uri      string     // URI of the go:func or go:method node
    name     string     // fd.Name.Name
    nodeType string     // go:func or go:method
}
```

Sort by `start`. For each identifier position `p`, binary-search to find
the last extent whose `start <= p`, then verify `p < end`. This is O(F log F)
to build (F = number of functions in package) and O(log F) per reference.

### `calls` edges — deduplication

During `indexReferences`, when `kind == RefKindCall` and a caller was found:

```
callPairs := map[string]struct{}{}   // key = callerURI + "→" + targetURI
```

After the loop, emit one `calls` edge per unique pair. This keeps the call
graph compact even when a function calls the same target dozens of times.

`calls` edges are only emitted when `IndexReferences == true` (same flag
that guards ref node creation).

### `indexEmbeds` — new pass

Mirrors the structure of `indexImplementations`. After all packages are
indexed, iterate each module-local package's type scope. For each struct
type, use `go/types` to enumerate embedded fields:

```go
structType.Field(i).Embedded()  // true for anonymous fields
```

Resolve the embedded field type (stripping pointer) to a `*gotypes.Named`,
build its URI, and emit `go:struct -[embeds]-> go:struct` if both are in
the same module. Cross-module embeds (e.g. embedding `sync.Mutex`) are
intentionally skipped since those nodes are not in the graph.

### Call site in `indexModule`

```go
if err := i.indexImplementations(...); err != nil { ... }
if err := i.indexEmbeds(...);           err != nil { ... }
```

`indexEmbeds` is NOT guarded by `IndexReferences` — it describes static
structure, not usage sites.

## Files Changed

| File | Change |
|---|---|
| `types/edges.go` | Add `EdgeCalls`, `EdgeEmbeds` constants + register in `RegisterCommonEdges` |
| `types/golang.go` | Extend `RefData`; re-register `EdgeCalls`/`EdgeEmbeds` with Go constraints |
| `indexer/golang/indexer.go` | `funcExtent`, `buildFuncExtents`, updated `indexReferences`, new `indexEmbeds` |
| `indexer/golang/indexer_test.go` | `TestCallGraph`, `TestEmbeds` |

## Acceptance Criteria

- `go test -race ./indexer/golang/...` passes
- `go test ./...` passes
- After indexing a module with `A()` calling `B()`:
  - `go:ref` for the call site has `caller_uri` pointing to `A`'s node
  - `go:func:A -[calls]-> go:func:B` edge exists (one, not N)
- After indexing a module with `Derived` embedding `Base`:
  - `go:struct:Derived -[embeds]-> go:struct:Base` edge exists
