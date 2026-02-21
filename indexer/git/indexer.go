package git

import (
	"context"
	"path/filepath"
	"strings"

	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing"

	"github.com/codewandler/axon/graph"
	"github.com/codewandler/axon/indexer"
	"github.com/codewandler/axon/progress"
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
	return []string{"git+file"}
}

func (i *Indexer) Handles(uri string) bool {
	return strings.HasPrefix(uri, "git+file://")
}

func (i *Indexer) Subscriptions() []indexer.Subscription {
	// Git indexer subscribes to .git directories being visited or deleted
	return []indexer.Subscription{
		{
			EventType: indexer.EventEntryVisited,
			NodeType:  types.TypeDir,
			Name:      ".git",
		},
		{
			EventType: indexer.EventNodeDeleting,
			NodeType:  types.TypeDir,
			Name:      ".git",
		},
	}
}

func (i *Indexer) Index(ctx context.Context, ictx *indexer.Context) error {
	repoPath := types.URIToRepoPath(ictx.Root)

	// Check if triggered by a deletion event - clean up instead of indexing
	if ictx.TriggerEvent != nil && ictx.TriggerEvent.Type == indexer.EventNodeDeleting {
		return i.cleanup(ctx, ictx, repoPath)
	}

	// Report start
	if ictx.Progress != nil {
		ictx.Progress <- progress.Started(i.Name())
	}

	// Open the repository
	repo, err := git.PlainOpen(repoPath)
	if err != nil {
		if ictx.Progress != nil {
			ictx.Progress <- progress.Error(i.Name(), err)
		}
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
	repoURI := types.RepoPathToURI(repoPath)
	repoNode := graph.NewNode(types.TypeRepo).
		WithURI(repoURI).
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
	if err := i.indexRemotes(ctx, ictx, repo, repoNode.ID, repoURI); err != nil {
		return err
	}

	// Index branches
	if err := i.indexBranches(ctx, ictx, repo, repoNode.ID, repoURI, head); err != nil {
		return err
	}

	// Index tags
	if err := i.indexTags(ctx, ictx, repo, repoNode.ID, repoURI); err != nil {
		if ictx.Progress != nil {
			ictx.Progress <- progress.Error(i.Name(), err)
		}
		return err
	}

	// Report completion
	if ictx.Progress != nil {
		ictx.Progress <- progress.Completed(i.Name(), 1)
	}

	_ = worktree // silence unused warning
	return nil
}

func (i *Indexer) indexRemotes(ctx context.Context, ictx *indexer.Context, repo *git.Repository, repoID string, repoURI string) error {
	remotes, err := repo.Remotes()
	if err != nil {
		return err
	}

	for _, remote := range remotes {
		cfg := remote.Config()
		remoteNode := graph.NewNode(types.TypeRemote).
			WithURI(repoURI + "/remote/" + cfg.Name).
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

func (i *Indexer) indexBranches(ctx context.Context, ictx *indexer.Context, repo *git.Repository, repoID string, repoURI string, head *plumbing.Reference) error {
	branches, err := repo.Branches()
	if err != nil {
		return err
	}

	err = branches.ForEach(func(ref *plumbing.Reference) error {
		branchName := ref.Name().Short()
		isHead := head != nil && ref.Name() == head.Name()

		branchNode := graph.NewNode(types.TypeBranch).
			WithURI(repoURI + "/branch/" + branchName).
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

func (i *Indexer) indexTags(ctx context.Context, ictx *indexer.Context, repo *git.Repository, repoID string, repoURI string) error {
	tags, err := repo.Tags()
	if err != nil {
		return err
	}

	err = tags.ForEach(func(ref *plumbing.Reference) error {
		tagName := ref.Name().Short()

		tagNode := graph.NewNode(types.TypeTag).
			WithURI(repoURI + "/tag/" + tagName).
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

// cleanup removes all git nodes for the given repository.
// Called when the .git directory is being deleted.
func (i *Indexer) cleanup(ctx context.Context, ictx *indexer.Context, repoPath string) error {
	repoURI := types.RepoPathToURI(repoPath)

	// Delete all nodes under this repo's URI prefix
	_, err := ictx.Graph.Storage().DeleteByURIPrefix(ctx, repoURI)
	return err
}
