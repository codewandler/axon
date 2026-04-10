# DESIGN: AQL pattern query with COUNT(*) and 3+ SELECT columns

## Problem

Pattern queries that select 2+ non-aggregate columns alongside `COUNT(*)` crash with:

```
query error: execute error: sql: expected 3 destination arguments in Scan, not 2
```

Example:
```sql
SELECT callee.name, callee.type, COUNT(*)
FROM (caller)-[:calls]->(callee)
GROUP BY callee.id
ORDER BY COUNT(*) DESC LIMIT 10
```

## Root Cause

In `compilePatternQuery` (`adapters/sqlite/aql.go`), when any `COUNT(*)` is detected in
the SELECT, `resultType` is unconditionally set to `ResultTypeCounts`.

`executeCountQuery` (the handler for `ResultTypeCounts`) always scans into exactly two
destinations: `var key sql.NullString` and `var count int`. When the SQL query produces
3 or more columns (e.g. `n1.name, n1.type, COUNT(*)`), Go's `database/sql` rejects the
mismatch → "expected 3 destination arguments in Scan, not 2".

## Proposed Solution

When `hasCount=true` AND the SELECT contains **more than 1 non-COUNT(\*) column**, switch
`resultType` from `ResultTypeCounts` to `ResultTypeRows`.

`ResultTypeRows` uses `executeRowsQuery`, which reads `rows.Columns()` at runtime and
allocates scan destinations dynamically — it handles any number of columns correctly.

Additionally, force `multiVar=true` in this code path so that `compilePatternSelect`
generates aliased column names (e.g. `n1.name AS "callee.name"`) rather than bare SQL
identifiers. This makes the output map keys informative and avoids collisions when the
same field name appears under different variables.

## Architecture

Single layer change: `adapters/sqlite/aql.go` — `compilePatternQuery`.

No changes to:
- AST / parser (`aql/`)
- `graph.QueryResult` or `graph.ResultType`
- `executeCountQuery` (existing 2-column path is left intact for the common case)
- CLI renderers (they already handle `ResultTypeRows` correctly)

## Key Decisions

| Decision | Rationale |
|---|---|
| Use `ResultTypeRows` not a new result type | `executeRowsQuery` already does dynamic column scanning; no new infrastructure needed |
| Threshold is `nonCountCols > 1` | 0 cols = scalar COUNT, 1 col = standard GROUP BY — both work today. Only 2+ cols was broken |
| Force `multiVar=true` for the new path | Aliased column names (`callee.name`) are more useful to the caller than bare names (`name`) |
| Leave `executeCountQuery` unchanged | Still handles all 0-col and 1-col COUNT cases; no regression risk |

## Out of Scope

- `compileVariableLengthPattern` — a separate path; the same pattern may exist but is
  deferred to a follow-up issue if reproduced.

## Files Changed

| File | Change |
|---|---|
| `adapters/sqlite/aql.go` | `compilePatternQuery`: conditional `resultType` + `multiVar` fix (~10 lines) |
| `adapters/sqlite/aql_test.go` | New test `TestPattern_GroupBy_MultiCol` |
