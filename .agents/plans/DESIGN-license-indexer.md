# DESIGN: LICENSE File Parser — project:license Node (#21)

## Problem

The graph has no structured representation of licence information. `LICENSE` files are stored only as `fs:file` nodes with a `docs:license` label applied by the tagger. Agents and library consumers cannot answer "what SPDX licence does this project use?" without reading the file content themselves.

## Proposed Solution

Add a lightweight `LicenseIndexer` to the `indexer/project` package. It subscribes to `EventEntryVisited` for known licence filenames, reads the first 1 KB, matches against a table of SPDX header fragments, and emits a `project:license` node. No licence text is ever stored.

## Architecture

### New node type: `project:license`

Defined in `types/project.go`:

```go
const TypeLicense = "project:license"

type LicenseData struct {
    SPDXID     string `json:"spdx_id"`     // e.g. "MIT", "" for unknown
    Confidence string `json:"confidence"`  // "high" | "medium" | "unknown"
    File       string `json:"file"`        // absolute path to the licence file
}
```

URI scheme: `license+file:///abs/path/to/LICENSE`

### Edge

`project:root -[has]-> project:license` (falls back to `fs:dir -[has]-> project:license` when no manifest node exists for that directory).

### Detection strategy

Read first 1024 bytes of file. Match against SPDX table in order:

| SPDX ID | Key fragment(s) |
|---|---|
| MIT | "Permission is hereby granted, free of charge" |
| Apache-2.0 | "Apache License, Version 2.0" |
| GPL-3.0-only | "GNU GENERAL PUBLIC LICENSE" + "Version 3" |
| GPL-2.0-only | "GNU GENERAL PUBLIC LICENSE" + "Version 2" |
| LGPL-2.1-only | "GNU LESSER GENERAL PUBLIC LICENSE" + "Version 2.1" |
| BSD-3-Clause | "Redistribution and use in source and binary forms" + "neither the name" |
| BSD-2-Clause | "Redistribution and use in source and binary forms" (no "neither the name") |
| ISC | "Permission to use, copy, modify, and/or distribute" |
| MPL-2.0 | "Mozilla Public License, Version 2.0" |

All string matching is case-insensitive. Confidence is `"high"` for any recognised match, `"unknown"` otherwise.

### Key decisions

1. **Same package as project indexer** — `project:license` is conceptually a project-level concern; no new package needed.
2. **Separate `LicenseIndexer` struct** — keeps the existing `Indexer` (manifest detection) unchanged; easier to test independently.
3. **Subscriptions use `Pattern`** — the glob `LICENSE*` and `LICENCE*` plus exact `COPYING` covers all required filenames without listing each variant.
4. **URI format** — `license+file:///abs/path` mirrors the `project+file://` convention; `graph.IDFromURI` produces a stable deterministic ID.
5. **Edge target selection** — look up the directory's `project:root` node first (by its `project+file://dir` URI/ID); fall back to the `fs:dir` node. This avoids a full graph scan.

## Out of Scope

- Multi-licence detection in a single file
- SPDX expression parsing
- Dependency licence scanning
- Storing the full licence text

## Files Changed

| File | Change |
|---|---|
| `types/project.go` | Add `TypeLicense`, `LicenseData`, `LicensePathToURI`, `URIToLicensePath`; extend `RegisterProjectTypes` |
| `indexer/project/license.go` | New `LicenseIndexer` |
| `indexer/project/license_test.go` | Unit tests for all SPDX entries + unknown path |
| `axon.go` | Register `NewLicenseIndexer()` |
| `cmd/axon/show.go` | Add `project:license` case in `getNodeSummary` and `printMapData` |
| `README.md` | Document new node type |
| `.agents/skills/axon/SKILL.md` | Document new node type |
