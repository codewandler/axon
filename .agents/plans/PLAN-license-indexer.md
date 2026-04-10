# PLAN: LICENSE File Parser — project:license Node (#21)

Worktree: `./worktrees/license-indexer`  
Branch: `feature/license-indexer`

---

## Task 1 — Types: add `TypeLicense` and `LicenseData`

**File**: `types/project.go`

Add to constants:
```go
TypeLicense = "project:license"
```

Add struct:
```go
type LicenseData struct {
    SPDXID     string `json:"spdx_id"`
    Confidence string `json:"confidence"`
    File       string `json:"file"`
}
```

Add helper functions:
```go
func LicensePathToURI(path string) string { return "license+file://" + path }
func URIToLicensePath(uri string) string  { /* strip "license+file://" prefix */ }
```

Extend `RegisterProjectTypes`:
```go
graph.RegisterNodeType[LicenseData](r, graph.NodeSpec{
    Type:        TypeLicense,
    Description: "A software licence detected from a LICENSE/COPYING file",
})
r.RegisterEdgeType(graph.EdgeSpec{
    Type:      EdgeHas,
    FromTypes: []string{TypeProject, TypeDir},
    ToTypes:   []string{TypeLicense},
})
```

**Verification**: `go build ./types/...`

---

## Task 2 — Indexer: `LicenseIndexer` (RED → GREEN)

**File**: `indexer/project/license_test.go` — write tests first:
- `TestLicenseIndexer_MIT`
- `TestLicenseIndexer_Apache`
- `TestLicenseIndexer_GPL3`
- `TestLicenseIndexer_GPL2`
- `TestLicenseIndexer_LGPL21`
- `TestLicenseIndexer_BSD3`
- `TestLicenseIndexer_BSD2`
- `TestLicenseIndexer_ISC`
- `TestLicenseIndexer_MPL2`
- `TestLicenseIndexer_Unknown`
- `TestLicenseIndexer_Cleanup`
- `TestLicenseIndexer_EdgeToProjectRoot` (has edge when project:root node exists)
- `TestLicenseIndexer_EdgeToDir` (has edge when no project:root node, falls back to fs:dir)

**File**: `indexer/project/license.go` — implementation:

```go
type LicenseIndexer struct{}

func NewLicenseIndexer() *LicenseIndexer { return &LicenseIndexer{} }

func (i *LicenseIndexer) Name() string    { return "license" }
func (i *LicenseIndexer) Schemes() []string { return []string{"license+file"} }
func (i *LicenseIndexer) Handles(uri string) bool {
    return strings.HasPrefix(uri, "license+file://")
}

func (i *LicenseIndexer) Subscriptions() []indexer.Subscription {
    return []indexer.Subscription{
        {EventType: indexer.EventEntryVisited, NodeType: types.TypeFile, Pattern: "LICENSE*"},
        {EventType: indexer.EventEntryVisited, NodeType: types.TypeFile, Pattern: "LICENCE*"},
        {EventType: indexer.EventEntryVisited, NodeType: types.TypeFile, Name: "COPYING"},
        {EventType: indexer.EventNodeDeleting, NodeType: types.TypeFile, Pattern: "LICENSE*"},
        {EventType: indexer.EventNodeDeleting, NodeType: types.TypeFile, Pattern: "LICENCE*"},
        {EventType: indexer.EventNodeDeleting, NodeType: types.TypeFile, Name: "COPYING"},
    }
}

func (i *LicenseIndexer) Index(ctx context.Context, ictx *indexer.Context) error { return nil }

func (i *LicenseIndexer) HandleEvent(ctx context.Context, ictx *indexer.Context, event indexer.Event) error {
    if event.Type == indexer.EventNodeDeleting {
        return i.cleanup(ctx, ictx, event.Path)
    }
    return i.indexLicense(ctx, ictx, event)
}
```

SPDX detection table (case-insensitive, order matters for GPL2 vs GPL3):

```go
type spdxEntry struct {
    ID       string
    Required []string // all must match (AND)
    Exclude  []string // none must match (NOT)
}

var spdxTable = []spdxEntry{
    {ID: "MIT",          Required: []string{"permission is hereby granted, free of charge"}},
    {ID: "Apache-2.0",   Required: []string{"apache license, version 2.0"}},
    {ID: "GPL-3.0-only", Required: []string{"gnu general public license", "version 3"}},
    {ID: "GPL-2.0-only", Required: []string{"gnu general public license", "version 2"}},
    {ID: "LGPL-2.1-only",Required: []string{"gnu lesser general public license", "version 2.1"}},
    {ID: "BSD-3-Clause", Required: []string{"redistribution and use in source and binary forms", "neither the name"}},
    {ID: "BSD-2-Clause", Required: []string{"redistribution and use in source and binary forms"},
                          Exclude: []string{"neither the name"}},
    {ID: "ISC",          Required: []string{"permission to use, copy, modify, and/or distribute"}},
    {ID: "MPL-2.0",      Required: []string{"mozilla public license, version 2.0"}},
}
```

**Verification**: `go test ./indexer/project/...`

---

## Task 3 — Register indexer in `axon.go`

**File**: `axon.go`

Add import for `project` package (already imported).
Add after `project.New()` registration:
```go
idxRegistry.Register(project.NewLicenseIndexer())
```

**Verification**: `go build ./...`

---

## Task 4 — CLI `show` command: display `project:license`

**File**: `cmd/axon/show.go`

Add to `getNodeSummary`:
```go
case types.LicenseData:
    name = data.SPDXID
    if name == "" {
        name = "unknown"
    }
```

Add to `printMapData`:
```go
case types.TypeLicense:
    fmt.Println("\nData:")
    spdxID := getMapString(data, "spdx_id")
    if spdxID == "" {
        spdxID = "(unknown)"
    }
    fmt.Printf("  SPDX ID:    %s\n", spdxID)
    fmt.Printf("  Confidence: %s\n", getMapString(data, "confidence"))
    fmt.Printf("  File:       %s\n", getMapString(data, "file"))
```

**Verification**: `go build ./cmd/axon/...`

---

## Task 5 — Documentation

Update `README.md`:
- Add `project:license` under Node Types → Project section

Update `.agents/skills/axon/SKILL.md`:
- Add `project:license` to node types list

**Verification**: Manual review

---

## Final Verification

```bash
go build ./...
go vet ./...
go test -race ./...
```
