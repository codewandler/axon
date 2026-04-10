# PLAN: TODO/FIXME Indexer (Issue #12)

**Design ref**: `DESIGN-todo-indexer.md`
**Date**: 2026-04-10
**Estimated total**: ~40 minutes

---

## Task 1 — Write failing tests

**Files created**: `indexer/todo/indexer_test.go`
**Estimated time**: 8 minutes

Tests to write:

- `TestIndexer_BasicAnnotations` — file with TODO + FIXME, correct nodes emitted
- `TestIndexer_MultipleKinds` — all five kinds (TODO FIXME HACK XXX NOTE)
- `TestIndexer_CommentStyles` — `//`, `#`, `--` prefixes all matched
- `TestIndexer_BinarySkipped` — file with null bytes produces zero nodes
- `TestIndexer_LargeFileSkipped` — file >2 MB produces zero nodes (use a fake stat)
- `TestIndexer_Cleanup` — `EventNodeDeleting` removes existing TODO nodes
- `TestIndexer_ReIndex` — re-visiting a file replaces old nodes (delete+rewrite)

**Verification** (must FAIL — package doesn't exist):
```bash
go test ./indexer/todo/
```

---

## Task 2 — `types/todo.go`

**Files created**: `types/todo.go`
**Estimated time**: 5 minutes

```go
package types

import (
    "fmt"
    "github.com/codewandler/axon/graph"
)

const TypeTodo = "code:todo"

type TodoData struct {
    File    string `json:"file"`
    Line    int    `json:"line"`
    Kind    string `json:"kind"`
    Text    string `json:"text"`
    Context string `json:"context"`
}

func TodoURI(filePath string, line int) string {
    return fmt.Sprintf("file+todo://%s#L%d", filePath, line)
}

func TodoURIPrefix(filePath string) string {
    return "file+todo://" + filePath
}

func RegisterTodoTypes(r *graph.Registry) {
    graph.RegisterNodeType[TodoData](r, graph.NodeSpec{
        Type:        TypeTodo,
        Description: "A TODO/FIXME/HACK/XXX/NOTE annotation comment in source code",
    })
}
```

**Verification**:
```bash
go build ./types/
```

---

## Task 3 — `indexer/todo/indexer.go`

**Files created**: `indexer/todo/indexer.go`
**Estimated time**: 15 minutes

Key sections:

```go
var commentPattern = regexp.MustCompile(
    `(?i)(?:\/\/|#|--|;)\s*\*?\s*(TODO|FIXME|HACK|XXX|NOTE)\b[:.]?\s*(.*)`,
)

const maxFileSize = 2 * 1024 * 1024 // 2 MB

type Indexer struct{}

func (i *Indexer) Subscriptions() []indexer.Subscription {
    return []indexer.Subscription{
        {EventType: indexer.EventEntryVisited, NodeType: types.TypeFile},
        {EventType: indexer.EventNodeDeleting, NodeType: types.TypeFile},
    }
}

func (i *Indexer) HandleEvent(...) error {
    if event.Type == indexer.EventNodeDeleting {
        return i.cleanup(ctx, ictx, event)
    }
    return i.indexFile(ctx, ictx, event)
}

func (i *Indexer) indexFile(...) error {
    // 1. Stat — skip if >maxFileSize
    // 2. ReadFile — skip on error
    // 3. Binary check — skip if null byte in first 512 bytes
    // 4. DeleteByURIPrefix to clear old nodes
    // 5. Scan lines with bufio.Scanner
    //    For each match: emit node + EmitContainment(fileNodeID, node.ID)
}
```

**Verification**:
```bash
go build ./indexer/todo/
go test -run TestIndexer_BasicAnnotations ./indexer/todo/
```

---

## Task 4 — Register in `axon.go`

**Files modified**: `axon.go`
**Estimated time**: 3 minutes

1. Add import: `todo "github.com/codewandler/axon/indexer/todo"`
2. In `newGraph()` (types registration block): `types.RegisterTodoTypes(registry)`
3. In `newIndexerRegistry()` (or equivalent): `idxRegistry.Register(todo.New())`

**Verification**:
```bash
go build ./...
```

---

## Task 5 — Full verification

```bash
go test ./...
go test -race ./...
go vet ./...

# Smoke test
go build -o ./bin/axon ./cmd/axon
./bin/axon find --type code:todo --global --limit 10
./bin/axon find --type code:todo --label fixme --global
```
