# PLAN: Semantic Commit Parser (#8)

## Task 1 — Write failing tests for commitparser

**Files:** `indexer/git/commitparser/parser_test.go` (new)

Write table-driven tests covering:
- Full conventional commit: `feat(aql): add pattern matching`
- Breaking with `!`: `feat(api)!: remove deprecated endpoint`
- Breaking with `BREAKING CHANGE` footer
- Scope-less: `fix: handle nil pointer`
- Non-conventional (lenient fallback): `Initial commit`
- Multi-line body + footers
- `Refs:` extraction with commas and `#` prefixes
- Empty message
- Whitespace-only message
- Multiple Refs values
- Co-authored-by trailer preserved

**Verify:** `go test ./indexer/git/commitparser/...` → compilation fails (package doesn't exist yet).

---

## Task 2 — Implement commitparser

**Files:** `indexer/git/commitparser/parser.go` (new)

```go
package commitparser

// ParsedCommit holds the structured result of parsing a commit message.
type ParsedCommit struct {
    CommitType string
    Scope      string
    Breaking   bool
    Subject    string
    Body       string
    Footers    map[string][]string
    Refs       []string
}

// Parse parses a raw commit message into a ParsedCommit.
// Non-conventional commits are stored with CommitType="" and Subject=full first line.
func Parse(message string) ParsedCommit { ... }
```

Implementation steps:
1. Split on `\n` to get lines
2. Parse first line with regex `^(\w+)(\(([^)]+)\))?(!)?: (.+)$`
3. Scan remaining lines for the footer block (trailer pattern `^[\w-]+: ` or `^[\w-]+ #`)
4. Extract body vs footer block
5. Check `BREAKING CHANGE` in footers → set `Breaking = true`
6. Extract and deduplicate `Refs` from `Footers["Refs"]`

**Verify:** `go test ./indexer/git/commitparser/...` → all tests pass.

---

## Task 3 — Extend CommitData

**Files:** `types/vcs.go`

Add 6 new fields after `Body`:

```go
CommitType string              `json:"commit_type,omitempty"`
Scope       string             `json:"scope,omitempty"`
Breaking    bool               `json:"breaking,omitempty"`
Subject     string             `json:"subject,omitempty"`
Footers     map[string][]string `json:"footers,omitempty"`
Refs        []string           `json:"refs,omitempty"`
```

**Verify:** `go build ./types/...` and `go test ./types/...` → pass (existing tests unaffected).

---

## Task 4 — Wire parser into git indexer

**Files:** `indexer/git/indexer.go`

In `indexCommits`, after splitting message into subject/body:

```go
parsed := commitparser.Parse(commit.Message)

commitNode := graph.NewNode(types.TypeCommit).
    WithURI(commitURI).
    WithKey(sha).
    WithName(commitName(sha, subject)).
    WithData(types.CommitData{
        SHA:            sha,
        Message:        subject,       // existing: raw first line
        Body:           body,          // existing: free-form body
        CommitType:     parsed.CommitType,
        Scope:          parsed.Scope,
        Breaking:       parsed.Breaking,
        Subject:        parsed.Subject,
        Footers:        parsed.Footers,
        Refs:           parsed.Refs,
        AuthorName:     commit.Author.Name,
        // ... rest of existing fields
    })
```

Add import: `"github.com/codewandler/axon/indexer/git/commitparser"`

**Verify:** `go test ./indexer/git/...` → all existing tests pass.

---

## Task 5 — Add integration test for CC commits

**Files:** `indexer/git/indexer_test.go`

Add `setupTestRepoWithCC` helper that creates a commit with message:
```
feat(parser): add semantic commit parsing

This enables structured AQL queries on commit data.

Refs: #8, DEV-100
```

Add `TestIndexerConventionalCommit` that:
1. Indexes the CC repo
2. Finds the commit node
3. Asserts `data.commit_type == "feat"`, `scope == "parser"`, `breaking == false`
4. Asserts `data.refs` contains `"#8"` and `"DEV-100"`

**Verify:** `go test -run TestIndexerConventionalCommit ./indexer/git/...` → passes.

---

## Task 6 — Final pre-flight

```bash
go build ./...
go vet ./...
go test -race ./...
```

All must exit 0.
