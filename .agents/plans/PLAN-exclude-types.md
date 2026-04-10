# PLAN: ExcludeTypes filter for NodeFilter (Issue #4)

**Design ref**: `DESIGN-exclude-types.md`
**Date**: 2026-04-10
**Estimated total**: ~25 minutes

---

## Prerequisites

- [ ] Baseline passes: `go test ./...` ✅ `go test -race ./...` ✅

---

## Task 1 — Failing tests in `adapters/sqlite`

**Files modified**: `adapters/sqlite/sqlite_test.go`
**Estimated time**: 5 minutes

Add `TestFindNodes_ExcludeTypes` and `TestFindSimilar_ExcludeTypes`.

`TestFindNodes_ExcludeTypes` covers:
- Single type excluded
- Multiple types excluded
- ExcludeTypes conflicts with Type → zero results

`TestFindSimilar_ExcludeTypes` covers:
- Without filter: all embedded nodes returned
- With ExcludeTypes: excluded type absent from results

**Verification** (must FAIL — field doesn't exist yet):
```bash
go test -run "TestFindNodes_ExcludeTypes|TestFindSimilar_ExcludeTypes" ./adapters/sqlite/
```

---

## Task 2 — Add `ExcludeTypes` to `graph.NodeFilter`

**Files modified**: `graph/storage.go`

Add after the `Root` field:
```go
ExcludeTypes []string // Exclude nodes whose type matches any of these (OR logic)
```

**Verification** (tests still fail — field exists but not wired):
```bash
go build ./...
go test -run "TestFindNodes_ExcludeTypes|TestFindSimilar_ExcludeTypes" ./adapters/sqlite/
```

---

## Task 3 — Wire into `buildNodeFilterArgs` (SQL path)

**Files modified**: `adapters/sqlite/sqlite.go`

After the `Root` block, append:
```go
if len(filter.ExcludeTypes) > 0 {
    placeholders := make([]string, len(filter.ExcludeTypes))
    for i, t := range filter.ExcludeTypes {
        placeholders[i] = "?"
        args = append(args, t)
    }
    *query += ` AND type NOT IN (` + strings.Join(placeholders, ", ") + `)`
}
```

**Verification** (`TestFindNodes_ExcludeTypes` must PASS):
```bash
go test -run TestFindNodes_ExcludeTypes ./adapters/sqlite/
```

---

## Task 4 — Wire into `nodeMatchesFilter` (in-memory path)

**Files modified**: `adapters/sqlite/sqlite.go`

After the `filter.Type` check, add:
```go
for _, excluded := range filter.ExcludeTypes {
    if node.Type == excluded {
        return false
    }
}
```

**Verification** (both tests must PASS):
```bash
go test -run "TestFindNodes_ExcludeTypes|TestFindSimilar_ExcludeTypes" ./adapters/sqlite/
```

---

## Task 5 — Wire `--exclude-type` into `cmd/axon/find.go`

**Files modified**: `cmd/axon/find.go`

1. Add variable:
```go
findExcludeType []string
```

2. Register flag in `init()`:
```go
findCmd.Flags().StringArrayVar(&findExcludeType, "exclude-type", nil,
    "Exclude node type (e.g. vcs:commit). Can be repeated.")
```

3. AQL path — after extension conditions, before data field filters:
```go
if len(findExcludeType) > 0 {
    var excl []aql.Expression
    for _, t := range findExcludeType {
        excl = append(excl, aql.Type.Eq(t))
    }
    if len(excl) == 1 {
        conditions = append(conditions, aql.Not(excl[0]))
    } else {
        conditions = append(conditions, aql.Not(aql.Or(excl...)))
    }
}
```

4. Semantic path — in `runSemanticFind`, after `filter.Extensions = findExt`:
```go
filter.ExcludeTypes = findExcludeType
```

**Verification**:
```bash
go build ./cmd/axon
go build ./...
```

---

## Task 6 — Full verification

```bash
go test ./...
go test -race ./...
go vet ./...

# Smoke test
go build -o ./bin/axon ./cmd/axon
./bin/axon find --exclude-type vcs:commit --help    # flag appears
./bin/axon find --exclude-type vcs:commit --global --limit 5
```
