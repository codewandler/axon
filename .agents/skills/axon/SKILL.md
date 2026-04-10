---
name: axon
description: Use Axon CLI to index directories, query graph data with AQL, and explore relationships
license: MIT
compatibility: opencode
---

## Indexing

Initialize and index a directory:

```bash
axon index .                   # Index current dir, creates .axon/graph.db here
axon index --no-gc /path       # Skip garbage collection
axon index --embed .           # Index + generate embeddings for semantic search
axon index --watch .           # Watch for changes and re-index automatically
```

**What gets indexed**: filesystem (files, dirs), git repos (branches, tags, commits), markdown docs, Go packages.

**Database location**: by default uses `<cwd>/.axon/graph.db`. Pass `--global` to walk up directories and fall back to `~/.axon/graph.db`.

## Tree Exploration

Display graph as tree:

```bash
axon tree                      # Current directory subtree (depth 3, IDs + types shown by default)
axon tree --depth 2            # Limit depth (0 = unlimited)
axon tree --type fs:file       # Filter by node type (glob: 'fs:*')
axon tree --no-color           # Disable colored output
```

## Query with AQL

Execute AQL queries (see [./references/aql.md](./references/aql.md) for complete syntax, and [./references/aql_go_querybuilder.md](./references/aql_go_querybuilder.md) for Go builder API):

```bash
axon query "SELECT * FROM nodes WHERE type = 'fs:file'"
axon query --output json "SELECT * FROM nodes LIMIT 10"
axon query --output count "SELECT * FROM nodes"
axon query --explain "SELECT file FROM (dir)-[:contains]->(file)"
```

**Output formats**: `table` (default), `json`, `count`

## Finding Nodes

Search with filters:

```bash
axon find --type fs:file       # All files
axon find --name "main.go"     # Exact name match
axon find --ext go             # By extension
axon find --query "README*"    # Name wildcard pattern
axon find --label important    # By label (repeatable, OR logic)
axon find --global             # Search entire graph (not just CWD subtree)
axon find --count              # Just show the count
axon find --show-parent        # Show parent chain to CWD or root
axon find --output json        # Output format: path, uri, json, table
```

## Semantic Search

Pass a text argument to `axon find` for vector similarity search
(requires `axon index --embed` first):

```bash
axon find "what is the Indexer interface"
axon find "who calls NewNode"
axon find "what implements Storage"
axon find "handles token budget overflow"
axon find "error recovery" --type go:func
axon find "recent commits about logo" --type vcs:commit --limit 5
```

## Context Generation

```bash
axon context --task "add caching to Storage interface"
axon context --task "refactor Query method" --tokens 8000
axon context --task "fix NewNode" --output json
echo "add error handling to Flush" | axon context
```

## Node Details

```bash
axon show <node-id>            # Show node details (4-char prefix is enough)
```

## Database Info

```bash
axon info                      # Dashboard: status, location, statistics
axon info -o json
```

## Introspection

```bash
axon stats                     # Database statistics
axon labels                    # List all labels with counts
axon types                     # List all node types with counts
axon edges                     # List all edge types with counts
axon gc                        # Run garbage collection
```

## Node Types

- `fs:file`, `fs:dir` - Filesystem
- `vcs:repo`, `vcs:branch`, `vcs:tag`, `vcs:commit` - Git
- `md:document`, `md:section`, `md:heading` - Markdown
- `go:package`, `go:func`, `go:struct`, `go:interface`, `go:ref` - Go code

## Edge Types

- `contains` / `contained_by` - Structural containment
- `has` / `belongs_to` - Logical ownership
- `located_at` - Physical location
- `references` - Cross-reference
- `links_to` - Hyperlink
- `imports` - Import statement
- `implements` - Struct implements interface
- `defines` - Package defines symbol
