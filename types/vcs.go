package types

import "github.com/codewandler/axon/graph"

// VCS node types
const (
	TypeRepo   = "vcs:repo"
	TypeRemote = "vcs:remote"
	TypeBranch = "vcs:branch"
	TypeTag    = "vcs:tag"
)

// VCS edge types
const (
	EdgeHasRemote = "has_remote"
	EdgeHasBranch = "has_branch"
	EdgeHasTag    = "has_tag"
	EdgeLocatedAt = "located_at"
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

	r.RegisterEdgeType(graph.EdgeSpec{
		Type:        EdgeHasRemote,
		Description: "Repository has a remote",
		FromTypes:   []string{TypeRepo},
		ToTypes:     []string{TypeRemote},
	})

	r.RegisterEdgeType(graph.EdgeSpec{
		Type:        EdgeHasBranch,
		Description: "Repository has a branch",
		FromTypes:   []string{TypeRepo},
		ToTypes:     []string{TypeBranch},
	})

	r.RegisterEdgeType(graph.EdgeSpec{
		Type:        EdgeHasTag,
		Description: "Repository has a tag",
		FromTypes:   []string{TypeRepo},
		ToTypes:     []string{TypeTag},
	})

	r.RegisterEdgeType(graph.EdgeSpec{
		Type:        EdgeLocatedAt,
		Description: "Repository is located at a directory",
		FromTypes:   []string{TypeRepo},
		ToTypes:     []string{TypeDir},
	})
}

// RepoPathToURI converts a repo path to a git:// URI.
func RepoPathToURI(path string) string {
	return "git://" + path
}

// URIToRepoPath extracts the path from a git:// URI.
func URIToRepoPath(uri string) string {
	const prefix = "git://"
	if len(uri) > len(prefix) && uri[:len(prefix)] == prefix {
		return uri[len(prefix):]
	}
	return uri
}
