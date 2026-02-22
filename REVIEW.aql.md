# AQL Query Review

Date: 2026-02-22

## Issues Found

| Issue | Query | Symptom |
|-------|-------|---------|
| 1 | `SELECT value FROM nodes, json_each(labels)` | ~~Error: "converting NULL to int"~~ FIXED |
| 2 | `SELECT data.ext FROM nodes` | ~~JSON field column not returned~~ FIXED |
| 3 | `SELECT name FROM nodes WHERE type = 'fs:file'` | ~~Column silently dropped~~ FIXED |
| 4 | `SELECT file FROM (dir)-[:contains*]->(file)` | ~~Times out (>30s)~~ FIXED: 23ms (unbounded capped at 20 hops, uses unrolled JOINs) |
| 5 | `WHERE EXISTS (dir)-[:contains]->(:fs:file)` | Compile error: unsupported expression |
| 6 | `SELECT name, type FROM nodes WHERE type = 'fs:file'` | ~~`name` column dropped~~ FIXED |
| 7 | `SELECT data.name FROM nodes WHERE type = 'fs:dir'` | ~~JSON field columns not returned~~ FIXED |
| 8 | `SELECT * FROM nodes` (JSON output) | ~~Returns `type: ""`~~ FIXED (omitempty) |
| 9 | `(dir:fs:dir WHERE dir.key = '...')-[:contains]->(file)` | Inline WHERE in pattern is IGNORED - not in query plan |
| 10 | `SELECT repo, branch FROM (repo)-[:has]->(branch)` | Returns only last variable, not both |
| 11 | `NOT IN (...)` | ~~Parse error~~ FIXED |
| 12 | `NOT LIKE 'pattern'` | ~~Parse error~~ FIXED |
| 13 | `json_array_length(labels) > 1` in WHERE | Parse error - functions not supported in WHERE |

Also observed:
- **LIMIT required**: Pattern queries need LIMIT or return nothing
- **Variable-length slow**: Starts from ALL dirs, not scoped to a root

## CLI Findings

### find command
- ~~`-l` is being parsed as label, not limit~~ - FIXED: `-l` now works for `--limit`
- `axon find --ext go -l 5` generates wrong query: `labels CONTAINS ANY ('5')`

### show command
- Works well for both files and directories
- Shows incoming/outgoing edges correctly
- Displays node data (name, size, mode, etc.)

### tree command
- Works well with `--type` filter
- Default depth is 3

### Other commands
- `gc` works (no orphaned edges in current DB)
- `parse` works for debugging queries

## Performance Notes

- Simple SELECT * queries
- SELECT with single column works, multiple specific columns broken
- WHERE with basic operators: =, !=, <, >, LIKE, GLOB, BETWEEN, IN
- IS NULL / IS NOT NULL
- OR operator
- NOT (expr) with parentheses
- ORDER BY, GROUP BY, HAVING, LIMIT, OFFSET
- DISTINCT
- CONTAINS ANY, CONTAINS ALL, NOT CONTAINS
- Pattern queries without inline WHERE (simple traversal)
- json_each table function (but value column has type issue)
