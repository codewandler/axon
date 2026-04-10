# PLAN: Go Indexer — Call Graph & Caller Context (Issue #13)

## Task 1 — Add edge constants and register them

**Files:** `types/edges.go`, `types/golang.go`

### 1a. `types/edges.go`
- Add `EdgeCalls = "calls"` and `EdgeEmbeds = "embeds"` constants
- Register both in `RegisterCommonEdges` (no type constraints here)

### 1b. `types/golang.go`
- Extend `RefData` with `CallerURI`, `CallerName`, `CallerType string` fields
- In `RegisterGoTypes`, add two `RegisterEdgeType` calls:
  - `EdgeCalls`: FromTypes `[go:func, go:method]`, ToTypes `[go:func, go:method]`
  - `EdgeEmbeds`: FromTypes `[go:struct]`, ToTypes `[go:struct]`

**Verify:** `go build ./types/...`

---

## Task 2 — Write failing tests

**File:** `indexer/golang/indexer_test.go`

### `TestCallGraph`

Create a test module with:
- `func Greet(name string) string` (returns greeting)
- `func Run()` which calls `Greet("World")` and `fmt.Println`

Assertions after indexing:
1. At least one `go:ref` node has `data.caller_uri` matching the URI of `Run`
2. Exactly one `calls` edge exists from Run → Greet
3. No duplicate `calls` edges for the same (caller, callee) pair

### `TestEmbeds`

Create a test module with:
```go
type Base struct { ID int }
type Derived struct {
    Base       // embedded — should produce embeds edge
    Name string
}
type WithPtr struct {
    *Base      // pointer embed — should also produce embeds edge
}
```

Assertions after indexing:
1. `go:struct:Derived -[embeds]-> go:struct:Base` edge exists
2. `go:struct:WithPtr -[embeds]-> go:struct:Base` edge exists

**Run:** `go test -run TestCallGraph ./indexer/golang/...` — must FAIL (red)
**Run:** `go test -run TestEmbeds ./indexer/golang/...` — must FAIL (red)

---

## Task 3 — Implement `funcExtent` and `buildFuncExtents`

**File:** `indexer/golang/indexer.go`

Add `import "sort"` to imports.

Add `funcExtent` struct and `buildFuncExtents` function:

```go
type funcExtent struct {
    start    token.Pos
    end      token.Pos
    uri      string
    name     string
    nodeType string
}

// buildFuncExtents returns all function/method bodies in pkg, sorted by start pos.
func buildFuncExtents(moduleURI string, pkg *packages.Package) []funcExtent {
    pkgURI := moduleURI + "/pkg/" + pkg.PkgPath
    var extents []funcExtent
    for _, file := range pkg.Syntax {
        ast.Inspect(file, func(n ast.Node) bool {
            fd, ok := n.(*ast.FuncDecl)
            if !ok || fd.Body == nil {
                return true
            }
            var uri, nodeType string
            if fd.Recv != nil && len(fd.Recv.List) > 0 {
                recvType, _ := extractReceiverType(fd.Recv.List[0].Type)
                uri = pkgURI + "/method/" + recvType + "." + fd.Name.Name
                nodeType = types.TypeGoMethod
            } else {
                uri = pkgURI + "/func/" + fd.Name.Name
                nodeType = types.TypeGoFunc
            }
            extents = append(extents, funcExtent{
                start:    fd.Pos(),
                end:      fd.End(),
                uri:      uri,
                name:     fd.Name.Name,
                nodeType: nodeType,
            })
            return true
        })
    }
    sort.Slice(extents, func(i, j int) bool {
        return extents[i].start < extents[j].start
    })
    return extents
}

// findEnclosingFunc returns the funcExtent that contains pos, or nil.
func findEnclosingFunc(extents []funcExtent, pos token.Pos) *funcExtent {
    // Binary search: find last extent with start <= pos
    lo, hi := 0, len(extents)-1
    result := -1
    for lo <= hi {
        mid := (lo + hi) / 2
        if extents[mid].start <= pos {
            result = mid
            lo = mid + 1
        } else {
            hi = mid - 1
        }
    }
    if result >= 0 && pos < extents[result].end {
        return &extents[result]
    }
    return nil
}
```

**Verify:** `go build ./indexer/golang/...`

---

## Task 4 — Update `indexReferences` to populate caller context + emit `calls` edges

**File:** `indexer/golang/indexer.go`

Modify `indexReferences`:

1. Before the `TypesInfo.Uses` loop:
   ```go
   extents := buildFuncExtents(moduleURI, pkg)
   callPairs := make(map[string]struct{})
   ```

2. Inside the loop, after `refNode` is built but before `EmitNode`, find the caller:
   ```go
   caller := findEnclosingFunc(extents, ident.Pos())
   if caller != nil {
       refData.CallerURI  = caller.uri
       refData.CallerName = caller.name
       refData.CallerType = caller.nodeType
   }
   ```
   (Since `RefData` is a value, update it before building the node, or use a local variable.)

3. After emitting the ref node's `references` edge, if `kind == RefKindCall && caller != nil`:
   ```go
   pairKey := caller.uri + "→" + targetURI
   callPairs[pairKey] = struct{}{}
   ```

4. After the `TypesInfo.Uses` loop, emit `calls` edges:
   ```go
   for key := range callPairs {
       parts := strings.SplitN(key, "→", 2)
       callerID := graph.IDFromURI(parts[0])
       calleeID := graph.IDFromURI(parts[1])
       edge := graph.NewEdge(types.EdgeCalls, callerID, calleeID)
       if err := ictx.Emitter.EmitEdge(ctx, edge); err != nil {
           return err
       }
   }
   ```

**Verify:** `go test -run TestCallGraph ./indexer/golang/...` — must PASS (green)

---

## Task 5 — Implement `indexEmbeds`

**File:** `indexer/golang/indexer.go`

Add the `indexEmbeds` function (similar shape to `indexImplementations`).

Call it from `indexModule` right after `indexImplementations`:
```go
if err := i.indexEmbeds(ctx, ictx, moduleURI, modFile.Module.Mod.Path, pkgs); err != nil {
    _ = err // best-effort, same pattern as indexImplementations
}
```

**Verify:** `go test -run TestEmbeds ./indexer/golang/...` — must PASS (green)

---

## Task 6 — Update package-level doc comment

**File:** `indexer/golang/indexer.go` (lines 1–28)

Add new entries to the doc block:
- Node types: `go:ref` already listed — add note about `caller_uri`
- Edge relationships:
  - `go:func/go:method -[calls]-> go:func/go:method`
  - `go:struct -[embeds]-> go:struct`

---

## Task 7 — Full test suite

```bash
go test -race ./...
go vet ./...
```

All must pass.
