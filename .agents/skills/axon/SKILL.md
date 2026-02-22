---
name: axon
description: Use Axon CLI to index directories, query graph data with AQL, and explore relationships
license: MIT
compatibility: opencode
---

## Indexing

Initialize and index a directory:

```bash
axon init .                    # Index current dir, use ~/.axon/graph.db
axon init --local .            # Use project-local .axon directory
axon init --no-gc /path        # Skip garbage collection
```

**What gets indexed**: filesystem (files, dirs), git repos (branches, tags, commits), markdown docs.

**Database location**: Auto-resolves by walking up from CWD looking for `.axon/graph.db`, falls back to `~/.axon/graph.db`.

## Tree Exploration

Display graph as tree:

```bash
axon tree                      # Current directory subtree
axon tree --depth 2            # Limit depth
axon tree --ids                # Show node IDs
axon tree --types              # Show node types
axon tree --type fs:file       # Filter by type
```

## Query with AQL

Execute AQL queries (see [references/aql.md](references/aql.md) for complete syntax):

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
axon find --name "main.go"     # By name
axon find --ext go             # By extension
axon find --global             # Search entire graph (not just CWD subtree)
```

## Node Details

```bash
axon show <node-id>            # Show node details
```

## Introspection

```bash
axon stats                     # Database statistics
axon labels                    # List all labels
axon types                     # List all node types
axon edges                     # List all edge types
axon gc                        # Run garbage collection
```

## Node Types

- `fs:file`, `fs:dir` - Filesystem
- `vcs:repo`, `vcs:branch`, `vcs:tag`, `vcs:commit` - Git
- `md:document`, `md:section`, `md:heading` - Markdown

## Edge Types

- `contains` / `contained_by` - Structural containment
- `has` / `belongs_to` - Logical ownership
- `located_at` - Physical location
- `references` - Cross-reference
- `links_to` - Hyperlink
- `imports` - Import statement
