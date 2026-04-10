# Axon

**AI-native graph database and indexing system for local knowledge management**

Axon transforms your filesystem, git repositories, and documents into a queryable knowledge graph—designed from the ground up for AI agents. It provides both persistent agent memory and powerful retrieval capabilities, all running locally on your machine.

## Why Axon?

**AI-Native Storage + Retrieval**: Unlike traditional RAG systems that only handle retrieval, Axon provides full storage and context management. AI agents can persist memories, track relationships, and query structured knowledge across sessions.

**Local-First Privacy**: All data stays on your machine. No external APIs, no data transmission. Your knowledge graph is stored in a single SQLite database file that you control.

**Universal Indexing**: Transforms unstructured data (files, git history, markdown) into structured, context-rich nodes and edges. Everything becomes queryable with type safety and relationship awareness.

**Performance at Scale**: Built on SQLite with tree-based access patterns and efficient JSON field extraction. Fast queries even with millions of nodes.

**Graph Query Language**: AQL (Axon Query Language) provides SQL-like table queries, Cypher-inspired pattern matching, and variable-length path traversal—all in one unified syntax.

## Installation

```bash
go install github.com/codewandler/axon/cmd/axon@latest
```

Requires Go 1.23 or later.

## Quickstart

Initialize and index a directory:

```bash
# Index current directory (uses ~/.axon/graph.db)
axon init .

# Or use project-local database
axon init --local .

# Check what was indexed
axon tree

# Query for Go files
axon query "SELECT * FROM nodes WHERE type = 'fs:file' AND data.ext = 'go'"
```

Basic CLI commands:

- `axon init [path]` - Index a directory
- `axon init --embed [path]` - Index and generate embeddings for semantic search
- `axon index --watch [path]` - Watch for changes and keep graph up to date
- `axon query "<aql>"` - Execute AQL queries
- `axon tree [path]` - Display graph as tree
- `axon find` - Search nodes with filters
- `axon show <node-id>` - Show node details
- `axon impact <symbol>` - Show blast radius of changing a symbol
- `axon search --semantic "<query>"` - Semantic vector similarity search
- `axon stats` - Database statistics

## AQL: Axon Query Language

AQL is a SQL-like query language with graph pattern matching. It supports both flat table queries and relationship traversal.

### Basic Table Queries

Query nodes and edges like traditional database tables:

```sql
-- All files
SELECT * FROM nodes WHERE type = 'fs:file'

-- Go files larger than 1KB
SELECT * FROM nodes 
WHERE type = 'fs:file' 
  AND data.ext = 'go' 
  AND data.size > 1000

-- Count nodes by type
SELECT type, COUNT(*) FROM nodes GROUP BY type
```

**JSON Field Access**: Use dot notation to query nested data:

```sql
SELECT * FROM nodes WHERE data.ext = 'go'
SELECT * FROM nodes WHERE data.size BETWEEN 100 AND 1000
SELECT * FROM nodes WHERE data.mode = 755
```

**Operators**: `=`, `!=`, `<`, `>`, `<=`, `>=`, `LIKE`, `GLOB`, `IN`, `BETWEEN`, `IS NULL`

**Label Operations**:

```sql
SELECT * FROM nodes WHERE labels CONTAINS ANY ('important', 'reviewed')
SELECT * FROM nodes WHERE labels CONTAINS ALL ('test', 'verified')
SELECT * FROM nodes WHERE labels NOT CONTAINS ('archived')
```

### Pattern Matching

Query the graph using Cypher-inspired patterns:

```sql
-- Files in directories
SELECT file FROM (dir:fs:dir)-[:contains]->(file:fs:file)

-- Go files in specific directory
SELECT file 
FROM (dir:fs:dir)-[:contains]->(file:fs:file)
WHERE dir.name = 'cmd' AND file.data.ext = 'go'

-- Branches in repositories
SELECT branch 
FROM (repo:vcs:repo)-[:has]->(branch:vcs:branch)
WHERE repo.name = 'myproject'
```

**Pattern Syntax**:

- `(var:type)` - Node with variable and type
- `->` - Outgoing edge
- `<-` - Incoming edge
- `-` - Undirected (either direction)
- `[var:type]` - Edge with variable binding

**Multi-Type Edges** (OR logic):

```sql
-- Match contains OR has edges
SELECT child FROM (parent)-[:contains|has]->(child)

-- Match any of three edge types
SELECT n FROM (root)-[:contains|has|references]->(n)
```

**Multiple Patterns** (implicit JOIN):

```sql
-- Files in repos located in specific dirs
SELECT file
FROM (repo:vcs:repo)-[:located_at]->(dir:fs:dir),
     (dir)-[:contains]->(file:fs:file)
WHERE repo.name = 'myrepo' AND file.data.ext = 'go'
```

### Variable-Length Paths

Traverse relationships recursively:

```sql
-- All descendants (1 or more hops)
SELECT desc FROM (root:fs:dir)-[:contains*]->(desc)

-- 1 to 3 hops deep
SELECT child FROM (parent:fs:dir)-[:contains*1..3]->(child)

-- Exactly 2 hops
SELECT node FROM (start)-[:contains*2]->(node)

-- At least 2 hops (unbounded)
SELECT desc FROM (root)-[:contains*2..]->(desc)

-- Multi-type recursive traversal
SELECT node FROM (root)-[:contains|has*1..5]->(node)
```

### Aggregation and Sorting

```sql
-- Count files per directory
SELECT dir.name, COUNT(*)
FROM (dir:fs:dir)-[:contains]->(file:fs:file)
GROUP BY dir.name
ORDER BY COUNT(*) DESC

-- Directories with many files
SELECT dir.name, COUNT(*)
FROM (dir:fs:dir)-[:contains]->(file:fs:file)
GROUP BY dir.name
HAVING COUNT(*) > 10

-- Top 10 largest files
SELECT name, data.size 
FROM nodes 
WHERE type = 'fs:file'
ORDER BY data.size DESC
LIMIT 10
```

### Existence Checks

Test for pattern existence without returning matches:

```sql
-- Directories containing Go files
SELECT dir
FROM (dir:fs:dir)
WHERE EXISTS (dir)-[:contains]->(:fs:file WHERE data.ext = 'go')

-- Repos with no branches
SELECT repo
FROM (repo:vcs:repo)
WHERE NOT EXISTS (repo)-[:has]->(:vcs:branch)
```

### Advanced Patterns

**Edge Variables**:

```sql
-- Examine edge properties
SELECT e.type, from.name, to.name
FROM (from)-[e:contains]->(to)
WHERE from.type = 'fs:dir'
```

**Inline WHERE Clauses**:

```sql
-- Filter inside patterns
SELECT file
FROM (dir:fs:dir WHERE dir.name = 'src')
     -[:contains]->
     (file:fs:file WHERE file.data.ext = 'go')
```

**Complex Boolean Logic**:

```sql
SELECT * FROM nodes
WHERE (type = 'fs:file' OR type = 'fs:dir')
  AND labels CONTAINS ANY ('important', 'reviewed')
  AND labels NOT CONTAINS ('archived')
  AND (data.size > 1000 OR data.size IS NULL)
```

## CLI Reference

### Global Flags

- `--db-dir <path>` - Use specific database directory
- `--local` - Use `.axon` directory in project root

**Database Resolution**: Axon automatically locates the database by walking up directories from the current working directory. If no `.axon/graph.db` is found, it falls back to `~/.axon/graph.db`.

### axon init

Index a directory and create the graph:

```bash
axon init .                    # Index current dir
axon init --local .            # Use project-local .axon
axon init --no-gc /path/to/dir # Skip garbage collection
axon init --embed .            # Index + generate embeddings for semantic search
```

**What gets indexed**:
- Filesystem structure (files, directories)
- Git repositories (repos, branches, tags, commits)
- Markdown documents (structure, sections, links)
- Go modules and packages (structs, interfaces, funcs, imports, implementations)

### axon query

Execute AQL queries:

```bash
# Basic query
axon query "SELECT * FROM nodes WHERE type = 'fs:file'"

# With output format
axon query --output json "SELECT * FROM nodes LIMIT 10"
axon query --output table "SELECT type, COUNT(*) FROM nodes GROUP BY type"
axon query --output count "SELECT * FROM nodes"

# See query execution plan
axon query --explain "SELECT file FROM (dir)-[:contains]->(file)"
```

### axon tree

Display the graph as a tree structure:

```bash
axon tree                      # Current directory subtree
axon tree /path/to/dir         # Specific path
axon tree --depth 2            # Limit depth
axon tree --ids                # Show node IDs
axon tree --types              # Show node types
axon tree --type fs:file       # Filter by type
```

### axon find

Search for nodes with filters:

```bash
axon find --type fs:file               # All files
axon find --name "main.go"             # By name
axon find --ext go                     # By extension
axon find --query "SELECT * FROM ..."  # Custom AQL
axon find --global                     # Search entire graph
axon find --label important            # By label
```

### axon show

Display detailed node information:

```bash
axon show <node-id>            # Show node details
```

### Other Commands

```bash
axon stats                     # Database statistics
axon labels                    # List all labels
axon types                     # List all node types
axon edges                     # List all edge types
axon gc                        # Run garbage collection
```

### Watch Mode

Keep the graph up to date as files change:

```bash
axon index --watch .                   # Watch current directory
axon index --watch ./src               # Watch specific subtree
axon index --watch --watch-quiet .     # Suppress per-change output
axon index --watch --watch-debounce 300ms .  # Custom debounce duration
axon index --watch --embed .           # Watch + re-embed on each change
```

On each file change, axon re-indexes the affected directory and prints:
```
↻  Re-indexed ./pkg/util — 12 files, 3 dirs (done)
```

### Impact Analysis

Understand the blast radius of changing a symbol:

```bash
axon impact Storage            # Show what depends on Storage
axon impact NewNode            # Show callers and importers
axon impact IndexResult        # Find all usages
```

Output:
```
Impact analysis: Storage (go:interface)

Direct references (17):
  adapters/sqlite               12 refs  [call, field, type]
  cmd/axon                       2 refs  [call]
  context                        3 refs  [type]

Packages importing affected packages:
  axon                  imports adapters/sqlite
  cmd/axon              imports sqlite, graph
```

### Semantic Search

Find code by meaning, not just keywords. Two providers are supported — both run fully locally, no data leaves the machine.

```bash
# First generate embeddings during indexing
axon init --embed .                              # uses Ollama by default
axon init --embed --embed-provider=hugot .       # in-process, no daemon needed

# Then search semantically
axon search --semantic "handles token budget overflow"
axon search --semantic "error recovery" --type go:func
axon search --semantic "storage interface" --limit 5
```

#### Provider: Ollama (default)

Requires the [Ollama](https://ollama.ai) daemon running locally.

```bash
ollama pull nomic-embed-text
axon init --embed .
```

#### Provider: Hugot (in-process, no daemon)

Runs ONNX sentence-embedding models fully inside the axon process.
No external service needed. Model is downloaded once (~90 MB) and cached.

```bash
# Hugot provider — downloads model on first run, then cached at ~/.axon/models/
axon init --embed --embed-provider=hugot .

# Custom model directory
axon init --embed --embed-provider=hugot --embed-model-path=/data/models/MiniLM .

# Via environment variable
AXON_EMBED_PROVIDER=hugot axon init --embed .
```

| | Hugot | Ollama |
|---|---|---|
| External daemon | ❌ none | ✅ required |
| CGO / shared libs | ❌ none | ❌ none |
| First-run setup | ~90 MB model download | `ollama pull <model>` |
| Throughput — single embed | ~114 ms (CPU, pure Go) | ~23 ms (GPU via HTTP) |
| Throughput — batched (32 nodes) | ~21 ms/node | ~12 ms/node |
| Best for | offline / CI / Docker | existing Ollama users |

Environment variables:
- `AXON_EMBED_PROVIDER` — provider name: `ollama` (default) or `hugot`
- `AXON_OLLAMA_URL` — Ollama base URL (default: `http://localhost:11434`)
- `AXON_OLLAMA_MODEL` — Ollama model name (default: `nomic-embed-text`)
- `AXON_HUGOT_MODEL` — HuggingFace repo slug (default: `KnightsAnalytics/all-MiniLM-L6-v2`)
- `AXON_HUGOT_MODEL_PATH` — local model directory (default: `~/.axon/models/<model>`)

## Node Types

Axon uses typed nodes with `domain:name` format:

### Filesystem

- `fs:file` - File node
  - Data: `ext` (extension), `size` (bytes), `mode` (permissions)
- `fs:dir` - Directory node

### Version Control

- `vcs:repo` - Git repository
- `vcs:branch` - Branch
- `vcs:tag` - Tag
- `vcs:commit` - Commit

### Documents

- `md:document` - Markdown document
- `md:section` - Document section
- `md:heading` - Heading

## Edge Types

Common edge types follow generic semantics:

- `contains` / `contained_by` - Structural containment (dir → file)
- `has` / `belongs_to` - Logical ownership (repo → branch)
- `located_at` - Physical location (repo → dir)
- `references` - Soft cross-reference
- `links_to` - Explicit hyperlink
- `depends_on` - Dependency relationship
- `imports` - Import statement (go:package → go:package)
- `implements` - Struct implements interface (go:struct → go:interface)
- `tests` - Test package tests source package (go:package → go:package)
- `defines` - Package defines symbol (go:package → go:func/struct/etc.)

## Architecture

Axon consists of:

- **Graph Core** (`graph/`) - Node, Edge, Storage interface
- **SQLite Adapter** (`adapters/sqlite/`) - Persistent storage with buffered writes
- **AQL Engine** (`aql/`) - Parser, AST, query compiler
- **Indexers** (`indexer/`) - Pluggable indexers for different data sources
  - `fs` - Filesystem indexer
  - `git` - Git repository indexer
  - `markdown` - Markdown document indexer
- **CLI** (`cmd/axon/`) - Command-line interface

**Key Features**:

- Generation-based garbage collection (tracks stale nodes across index runs)
- Event-driven cascade deletion (when files are deleted, dependent data is cleaned up)
- Buffered writes for performance (5000 items or 100ms batches)
- Pluggable indexer architecture (subscribe to events, handle specific URI schemes)

## Use Cases

**For AI Agents**:

- Persistent memory across sessions
- Context-aware file retrieval
- Relationship tracking (imports, dependencies, references)
- Structured knowledge graphs from unstructured data
- Multi-hop reasoning (variable-length paths)

**For Developers**:

- Code navigation and exploration
- Dependency analysis
- Git history queries
- Documentation cross-referencing
- Project structure understanding

## License

MIT

## Contributing

Contributions welcome! See the codebase structure in `AGENTS.md` for development guidelines.
