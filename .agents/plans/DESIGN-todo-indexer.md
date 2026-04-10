# DESIGN: TODO/FIXME Indexer (Issue #12)

**Date**: 2026-04-10
**Status**: APPROVED
**Refs**: https://github.com/codewandler/axon/issues/12

---

## Problem

`// TODO`, `// FIXME`, and similar code annotations are invisible to the graph.
Agents and developers cannot search them semantically, count them per package, or
surface them in context. The only option today is `grep`.

---

## Proposed Solution

A new `indexer/todo` package subscribes to `EventEntryVisited` for every `fs:file`
node, scans the file for annotation comments, and emits one `code:todo` node per
match with a `contains` edge back to the parent file node.

---

## Architecture

### New type: `code:todo`

Registered in `types/todo.go`:

```go
const TypeTodo = "code:todo"

type TodoData struct {
    File    string `json:"file"`    // absolute path
    Line    int    `json:"line"`    // 1-based line number
    Kind    string `json:"kind"`    // "TODO" | "FIXME" | "HACK" | "XXX" | "NOTE"
    Text    string `json:"text"`    // comment text after the keyword
    Context string `json:"context"` // preceding non-comment line for orientation
}
```

**Node shape:**
- `Type`:   `"code:todo"`
- `URI`:    `"file+todo:///abs/path/file.go#L42"`
- `Key`:    `"/abs/path/file.go:42"`
- `Name`:   `"TODO: extract this into a helper"` (≤80 chars, truncated with `...`)
- `Labels`: `["todo"]` (lowercase kind)

**Edge:**
```
(fs:file) --[contains]--> (code:todo)
(code:todo) --[contained_by]--> (fs:file)
```

### Comment pattern

```
(?:\/\/|#|--|;)\s*\*?\s*(TODO|FIXME|HACK|XXX|NOTE)\b[:.]?\s*(.*)
```

Supports `//` (Go, JS, TS, Java, C), `#` (Python, Shell, YAML, Ruby),
`--` (SQL, Lua), `;` (Lisp, Assembly). Case-insensitive.

### Language-agnostic via binary detection

All `fs:file` nodes are candidates. Binary files are skipped by checking for
null bytes in the first 512 bytes. Files >2 MB are also skipped.

### Cleanup

On `EventNodeDeleting` for an `fs:file`, all `code:todo` nodes whose URI starts
with `file+todo:///path/to/file` are deleted via `DeleteByURIPrefix`. On
re-index of a file, old TODOs are cleared before new ones are emitted, so edits
and deletions are handled correctly.

### No git blame

`author` is deferred. Requires cross-indexer coordination with the git indexer
that is out of scope for this issue.

### Registration

- `types.RegisterTodoTypes(registry)` in `axon.go` `newGraph()`
- `idxRegistry.Register(todo.New())` in `axon.go` `newIndexerRegistry()`

---

## Files Changed

| File | Change |
|---|---|
| `types/todo.go` (new) | `TypeTodo`, `TodoData`, `TodoURI()`, `TodoURIPrefix()`, `RegisterTodoTypes()` |
| `indexer/todo/indexer.go` (new) | Full `Indexer` implementation |
| `indexer/todo/indexer_test.go` (new) | Unit tests |
| `axon.go` | Register type + indexer |

---

## Key Decisions

**New package, not a sub-pass of the Go indexer**
The feature is explicitly language-agnostic. Putting it in `indexer/golang` would
couple it to Go and exclude Python, YAML, shell scripts, etc.

**Subscribe to all `fs:file` via `EventEntryVisited`**
This means TODO detection runs automatically for every file the FS indexer visits,
including watch-mode re-indexing of changed files. Zero additional wiring needed.

**`file+todo://` URI scheme**
Parallel to `file+md://` for markdown nodes. Makes URIs self-describing and
keeps different content-derived node types in clearly separate URI namespaces.

**Delete-then-rewrite per file on each visit**
Simpler than generation-based tracking per file. When a file changes, all its
TODOs are wiped and re-scanned. This is correct even when TODOs move lines or
are deleted entirely.

---

## Acceptance Criteria

- [ ] `axon find --type code:todo --global` returns all annotation nodes
- [ ] `axon find --type code:todo --label fixme --global` returns only FIXMEs
- [ ] `axon show <todo-node-id>` shows Kind, Text, File, Line, Context
- [ ] Re-indexing a file after editing a TODO updates the node (old line gone)
- [ ] Deleting a file removes its TODO nodes (cleanup via `EventNodeDeleting`)
- [ ] Binary files are not scanned (no error, no nodes emitted)
- [ ] Files >2 MB are skipped
- [ ] `code:todo` nodes appear under their parent file in `axon tree`
- [ ] All existing tests pass; `go test -race ./...` clean
