package git

import (
	"context"
	"path/filepath"
	"strings"

	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing"

	"github.com/codewandler/axon/graph"
	"github.com/codewandler/axon/indexer"
	"github.com/codewandler/axon/types"
)

// Indexer indexes git repositories.
type Indexer struct{}

// New creates a new git indexer.
func New() *Indexer {
	return &Indexer{}
}

func (i *Indexer) Name() string {
	return "git"
}

func (i *Indexer) Schemes() []string {
	return []string{"git"}
}

func (i *Indexer) Handles(uri string) bool {
	return strings.HasPrefix(uri, "git://")
}

func (i *Indexer) Index(ctx context.Context, ictx *indexer.Context) error {
	repoPath := types.URIToRepoPath(ictx.Root)

	// Open the repository
	repo, err := git.PlainOpen(repoPath)
	if err != nil {
		return err
	}

	// Check if bare
	worktree, err := repo.Worktree()
	isBare := err == git.ErrIsBareRepository

	// Get HEAD info
	var headBranch, headCommit string
	head, err := repo.Head()
	if err == nil {
		headCommit = head.Hash().String()
		if head.Name().IsBranch() {
			headBranch = head.Name().Short()
		}
	}

	// Create repo node
	repoName := filepath.Base(repoPath)
	repoNode := graph.NewNode(types.TypeRepo).
		WithURI(types.RepoPathToURI(repoPath)).
		WithKey(repoPath).
		WithData(types.RepoData{
			Name:       repoName,
			IsBare:     isBare,
			HeadBranch: headBranch,
			HeadCommit: headCommit,
		})

	if err := ictx.Emitter.EmitNode(ctx, repoNode); err != nil {
		return err
	}

	// Link repo to directory (if we can find it)
	dirURI := types.PathToURI(repoPath)
	dirNode, err := ictx.Graph.GetNodeByURI(ctx, dirURI)
	if err == nil {
		edge := graph.NewEdge(types.EdgeLocatedAt, repoNode.ID, dirNode.ID)
		if err := ictx.Emitter.EmitEdge(ctx, edge); err != nil {
			return err
		}
	}

	// Index remotes
	if err := i.indexRemotes(ctx, ictx, repo, repoNode.ID); err != nil {
		return err
	}

	// Index branches
	if err := i.indexBranches(ctx, ictx, repo, repoNode.ID, head); err != nil {
		return err
	}

	// Index tags
	if err := i.indexTags(ctx, ictx, repo, repoNode.ID); err != nil {
		return err
	}

	_ = worktree // silence unused warning
	return nil
}

func (i *Indexer) indexRemotes(ctx context.Context, ictx *indexer.Context, repo *git.Repository, repoID string) error {
	remotes, err := repo.Remotes()
	if err != nil {
		return err
	}

	for _, remote := range remotes {
		cfg := remote.Config()
		remoteNode := graph.NewNode(types.TypeRemote).
			WithURI(types.RepoPathToURI(ictx.Root) + "/remote/" + cfg.Name).
			WithKey(cfg.Name).
			WithData(types.RemoteData{
				Name: cfg.Name,
				URLs: cfg.URLs,
			})

		if err := ictx.Emitter.EmitNode(ctx, remoteNode); err != nil {
			return err
		}

		edge := graph.NewEdge(types.EdgeHasRemote, repoID, remoteNode.ID)
		if err := ictx.Emitter.EmitEdge(ctx, edge); err != nil {
			return err
		}
	}

	return nil
}

func (i *Indexer) indexBranches(ctx context.Context, ictx *indexer.Context, repo *git.Repository, repoID string, head *plumbing.Reference) error {
	branches, err := repo.Branches()
	if err != nil {
		return err
	}

	err = branches.ForEach(func(ref *plumbing.Reference) error {
		branchName := ref.Name().Short()
		isHead := head != nil && ref.Name() == head.Name()

		branchNode := graph.NewNode(types.TypeBranch).
			WithURI(types.RepoPathToURI(ictx.Root) + "/branch/" + branchName).
			WithKey(branchName).
			WithData(types.BranchData{
				Name:     branchName,
				IsHead:   isHead,
				IsRemote: false,
				Commit:   ref.Hash().String(),
			})

		if err := ictx.Emitter.EmitNode(ctx, branchNode); err != nil {
			return err
		}

		edge := graph.NewEdge(types.EdgeHasBranch, repoID, branchNode.ID)
		return ictx.Emitter.EmitEdge(ctx, edge)
	})

	return err
}

func (i *Indexer) indexTags(ctx context.Context, ictx *indexer.Context, repo *git.Repository, repoID string) error {
	tags, err := repo.Tags()
	if err != nil {
		return err
	}

	err = tags.ForEach(func(ref *plumbing.Reference) error {
		tagName := ref.Name().Short()

		tagNode := graph.NewNode(types.TypeTag).
			WithURI(types.RepoPathToURI(ictx.Root) + "/tag/" + tagName).
			WithKey(tagName).
			WithData(types.TagData{
				Name:   tagName,
				Commit: ref.Hash().String(),
			})

		if err := ictx.Emitter.EmitNode(ctx, tagNode); err != nil {
			return err
		}

		edge := graph.NewEdge(types.EdgeHasTag, repoID, tagNode.ID)
		return ictx.Emitter.EmitEdge(ctx, edge)
	})

	return err
}

// IndexFromPath is a helper that indexes a git repo at a given path.
// It's used when auto-detecting .git directories.
func IndexFromPath(ctx context.Context, g *graph.Graph, emitter indexer.Emitter, path string, generation string) error {
	idx := New()
	ictx := &indexer.Context{
		Root:       types.RepoPathToURI(path),
		Generation: generation,
		Graph:      g,
		Emitter:    emitter,
	}
	return idx.Index(ctx, ictx)
}
