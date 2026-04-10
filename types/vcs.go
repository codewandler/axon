package types

import (
	"fmt"
	"strings"
	"time"

	"github.com/codewandler/axon/graph"
)

// VCS node types
const (
	TypeRepo   = "vcs:repo"
	TypeRemote = "vcs:remote"
	TypeBranch = "vcs:branch"
	TypeTag    = "vcs:tag"
	TypeCommit = "vcs:commit"
)

// RepoData holds data for a repository node.
type RepoData struct {
	Name       string `json:"name"`
	IsBare     bool   `json:"is_bare"`
	HeadBranch string `json:"head_branch,omitempty"`
	HeadCommit string `json:"head_commit,omitempty"`
}

// RemoteData holds data for a remote node.
type RemoteData struct {
	Name string   `json:"name"`
	URLs []string `json:"urls"`
}

// BranchData holds data for a branch node.
type BranchData struct {
	Name     string `json:"name"`
	IsHead   bool   `json:"is_head"`
	IsRemote bool   `json:"is_remote"`
	Commit   string `json:"commit,omitempty"`
}

// TagData holds data for a tag node.
type TagData struct {
	Name   string `json:"name"`
	Commit string `json:"commit,omitempty"`
}

// CommitData holds data for a commit node.
type CommitData struct {
	SHA            string    `json:"sha"`
	Message        string    `json:"message"`        // First line only (raw subject)
	Body           string    `json:"body,omitempty"` // Full body after subject
	AuthorName     string    `json:"author_name"`
	AuthorEmail    string    `json:"author_email"`
	AuthorDate     time.Time `json:"author_date"`
	CommitterName  string    `json:"committer_name,omitempty"`
	CommitterEmail string    `json:"committer_email,omitempty"`
	CommitDate     time.Time `json:"commit_date,omitempty"`
	Parents        []string  `json:"parents"`       // Parent commit SHAs
	FilesChanged   int       `json:"files_changed"`
	Insertions     int       `json:"insertions"`
	Deletions      int       `json:"deletions"`

	// Conventional Commits structured fields (populated by the commit parser).
	// CommitType is the conventional commit type (e.g. "feat", "fix").
	// Empty for non-conventional commits.
	CommitType string `json:"commit_type,omitempty"`
	// Scope is the optional scope in parentheses (e.g. "aql", "cli").
	Scope string `json:"scope,omitempty"`
	// Breaking is true when the commit includes a breaking-change marker.
	Breaking bool `json:"breaking,omitempty"`
	// Subject is the description part of the first line (after type/scope).
	// For non-conventional commits this equals Message.
	Subject string `json:"subject,omitempty"`
	// Footers contains all git trailer key/value pairs from the commit footer.
	Footers map[string][]string `json:"footers,omitempty"`
	// Refs contains deduplicated ticket/issue references extracted from the
	// "Refs:" footer (e.g. ["#8", "DEV-100"]).
	Refs []string `json:"refs,omitempty"`
}

// CommitToURI returns the URI for a commit node.
func CommitToURI(repoPath, sha string) string {
	return RepoPathToURI(repoPath) + "/commit/" + sha
}

// RegisterVCSTypes registers VCS node and edge types with the registry.
func RegisterVCSTypes(r *graph.Registry) {
	graph.RegisterNodeType[RepoData](r, graph.NodeSpec{
		Type:        TypeRepo,
		Description: "A version control repository",
	})

	graph.RegisterNodeType[RemoteData](r, graph.NodeSpec{
		Type:        TypeRemote,
		Description: "A remote repository reference",
	})

	graph.RegisterNodeType[BranchData](r, graph.NodeSpec{
		Type:        TypeBranch,
		Description: "A branch in the repository",
	})

	graph.RegisterNodeType[TagData](r, graph.NodeSpec{
		Type:        TypeTag,
		Description: "A tag in the repository",
	})

	graph.RegisterNodeType[CommitData](r, graph.NodeSpec{
		Type:        TypeCommit,
		Description: "A git commit",
	})

	// VCS uses common edge types: has, belongs_to, located_at
	// Repo ownership edges (has/belongs_to) are registered in RegisterCommonEdges
	r.RegisterEdgeType(graph.EdgeSpec{
		Type:        EdgeLocatedAt,
		Description: "Repository is located at a directory",
		FromTypes:   []string{TypeRepo},
		ToTypes:     []string{TypeDir},
	})
}

// RepoPathToURI converts a repo path to a git+file:// URI.
func RepoPathToURI(path string) string {
	return "git+file://" + path
}

// URIToRepoPath extracts the path from a git+file:// URI.
func URIToRepoPath(uri string) string {
	const prefix = "git+file://"
	if len(uri) > len(prefix) && uri[:len(prefix)] == prefix {
		return uri[len(prefix):]
	}
	return uri
}

// Description returns a human-readable one-liner for display in search results,
// CLIs, and any other consumer of the library.
// Format: sha8 -- subject  by author (YYYY-MM-DD, N files)
// All parts are omitted gracefully when the underlying field is empty/zero.
func (d CommitData) Description() string {
	short := d.SHA
	if len(d.SHA) >= 8 {
		short = d.SHA[:8]
	}
	var meta []string
	if !d.AuthorDate.IsZero() {
		meta = append(meta, d.AuthorDate.Format("2006-01-02"))
	}
	if d.FilesChanged > 0 {
		fileWord := "files"
		if d.FilesChanged == 1 {
			fileWord = "file"
		}
		meta = append(meta, fmt.Sprintf("%d %s", d.FilesChanged, fileWord))
	}
	suffix := ""
	if len(meta) > 0 {
		suffix = " (" + strings.Join(meta, ", ") + ")"
	}
	by := ""
	if d.AuthorName != "" {
		by = "  by " + d.AuthorName
	}
	if d.Message == "" {
		return short + by + suffix
	}
	return short + " -- " + d.Message + by + suffix
}
