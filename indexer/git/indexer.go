package git

import (
	"context"
	"fmt"
	"io"
	"path/filepath"
	"strings"

	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing"

	"github.com/codewandler/axon/graph"
	"github.com/codewandler/axon/indexer"
	"github.com/codewandler/axon/indexer/git/commitparser"
	"github.com/codewandler/axon/progress"
	"github.com/codewandler/axon/types"
)

// Config holds configuration for the git indexer.
type Config struct {
	// MaxCommits limits the number of commits to index per repository.
	// Default: 500. Set to 0 for unlimited (not recommended for large repos).
	MaxCommits int
}

func defaultConfig() Config {
	return Config{MaxCommits: 500}
}

// Indexer indexes git repositories.
type Indexer struct {
	cfg Config
}

// New creates a new git indexer with optional configuration.
func New(cfg ...Config) *Indexer {
	c := defaultConfig()
	if len(cfg) > 0 {
		c = cfg[0]
		if c.MaxCommits == 0 {
			c.MaxCommits = defaultConfig().MaxCommits
		}
	}
	return &Indexer{cfg: c}
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
	// Git indexer is event-driven only, direct invocation is a no-op
	return nil
}

func (i *Indexer) HandleEvent(ctx context.Context, ictx *indexer.Context, event indexer.Event) error {
	// Determine repo path from event - .git is in the visited directory
	repoPath := filepath.Dir(event.Path)

	// Check if triggered by a deletion event - clean up instead of indexing
	if event.Type == indexer.EventNodeDeleting {
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
		WithName(repoName).
		WithData(types.RepoData{
			Name:       repoName,
			IsBare:     isBare,
			HeadBranch: headBranch,
			HeadCommit: headCommit,
		})

	if err := ictx.Emitter.EmitNode(ctx, repoNode); err != nil {
		return err
	}

	// Link repo to directory (compute ID directly to avoid read during write)
	dirURI := types.PathToURI(repoPath)
	dirID := graph.IDFromURI(dirURI)
	edge := graph.NewEdge(types.EdgeLocatedAt, repoNode.ID, dirID)
	if err := ictx.Emitter.EmitEdge(ctx, edge); err != nil {
		return err
	}

	// Index remotes
	if err := i.indexRemotes(ctx, ictx, repo, repoNode.ID, repoURI); err != nil {
		return err
	}

	// Index branches
	if err := i.indexBranches(ctx, ictx, repo, repoNode.ID, repoURI, repoPath, head); err != nil {
		return err
	}

	// Index tags
	if err := i.indexTags(ctx, ictx, repo, repoNode.ID, repoURI, repoPath); err != nil {
		if ictx.Progress != nil {
			ictx.Progress <- progress.Error(i.Name(), err)
		}
		return err
	}

	// Index commits
	if err := i.indexCommits(ctx, ictx, repo, repoNode, repoPath); err != nil {
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
			WithName(cfg.Name).
			WithData(types.RemoteData{
				Name: cfg.Name,
				URLs: cfg.URLs,
			})

		if err := ictx.Emitter.EmitNode(ctx, remoteNode); err != nil {
			return err
		}

		if err := indexer.EmitOwnership(ctx, ictx.Emitter, repoID, remoteNode.ID); err != nil {
			return err
		}
	}

	return nil
}

func (i *Indexer) indexBranches(ctx context.Context, ictx *indexer.Context, repo *git.Repository, repoID string, repoURI string, repoPath string, head *plumbing.Reference) error {
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
			WithName(branchName).
			WithData(types.BranchData{
				Name:     branchName,
				IsHead:   isHead,
				IsRemote: false,
				Commit:   ref.Hash().String(),
			})

		if err := ictx.Emitter.EmitNode(ctx, branchNode); err != nil {
			return err
		}

		if err := indexer.EmitOwnership(ctx, ictx.Emitter, repoID, branchNode.ID); err != nil {
			return err
		}

		// references edge: branch → tip commit
		commitID := graph.IDFromURI(types.CommitToURI(repoPath, ref.Hash().String()))
		refEdge := graph.NewEdge(types.EdgeReferences, branchNode.ID, commitID)
		return ictx.Emitter.EmitEdge(ctx, refEdge)
	})

	return err
}

func (i *Indexer) indexTags(ctx context.Context, ictx *indexer.Context, repo *git.Repository, repoID string, repoURI string, repoPath string) error {
	tags, err := repo.Tags()
	if err != nil {
		return err
	}

	err = tags.ForEach(func(ref *plumbing.Reference) error {
		tagName := ref.Name().Short()

		tagNode := graph.NewNode(types.TypeTag).
			WithURI(repoURI + "/tag/" + tagName).
			WithKey(tagName).
			WithName(tagName).
			WithData(types.TagData{
				Name:   tagName,
				Commit: ref.Hash().String(),
			})

		if err := ictx.Emitter.EmitNode(ctx, tagNode); err != nil {
			return err
		}

		if err := indexer.EmitOwnership(ctx, ictx.Emitter, repoID, tagNode.ID); err != nil {
			return err
		}

		// references edge: tag → commit
		commitID := graph.IDFromURI(types.CommitToURI(repoPath, ref.Hash().String()))
		refEdge := graph.NewEdge(types.EdgeReferences, tagNode.ID, commitID)
		return ictx.Emitter.EmitEdge(ctx, refEdge)
	})

	return err
}

func (i *Indexer) indexCommits(ctx context.Context, ictx *indexer.Context, repo *git.Repository, repoNode *graph.Node, repoPath string) error {
	iter, err := repo.Log(&git.LogOptions{})
	if err != nil {
		return fmt.Errorf("git log: %w", err)
	}
	defer iter.Close()

	count := 0
	for {
		if i.cfg.MaxCommits > 0 && count >= i.cfg.MaxCommits {
			break
		}
		commit, err := iter.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return fmt.Errorf("iterating commits: %w", err)
		}

		sha := commit.Hash.String()
		commitURI := types.CommitToURI(repoPath, sha)

		// Parse the full commit message into structured fields.
		parsed := commitparser.Parse(commit.Message)

		// Raw first line (kept for backward compatibility with existing nodes).
		// For conventional commits this is the full "type(scope): subject" line.
		// For non-conventional commits it equals parsed.Subject.
		rawFirstLine := strings.TrimSpace(commit.Message)
		if idx := strings.Index(rawFirstLine, "\n"); idx != -1 {
			rawFirstLine = rawFirstLine[:idx]
		}

		// Parent SHAs
		parents := make([]string, len(commit.ParentHashes))
		for pi, h := range commit.ParentHashes {
			parents[pi] = h.String()
		}

		// File stats for single-parent commits only (skip merge commits)
		type fileChange struct{ name string }
		var fileChanges []fileChange
		var filesChanged, insertions, deletions int
		if commit.NumParents() == 1 {
			stats, err := commit.Stats()
			if err == nil {
				for _, fs := range stats {
					filesChanged++
					insertions += fs.Addition
					deletions += fs.Deletion
					fileChanges = append(fileChanges, fileChange{name: fs.Name})
				}
			}
		}

		commitNode := graph.NewNode(types.TypeCommit).
			WithURI(commitURI).
			WithKey(sha).
			WithName(commitName(sha, rawFirstLine)).
			WithData(types.CommitData{
				SHA:            sha,
				Message:        rawFirstLine,
				Body:           parsed.Body,
				AuthorName:     commit.Author.Name,
				AuthorEmail:    commit.Author.Email,
				AuthorDate:     commit.Author.When,
				CommitterName:  commit.Committer.Name,
				CommitterEmail: commit.Committer.Email,
				CommitDate:     commit.Committer.When,
				Parents:        parents,
				FilesChanged:   filesChanged,
				Insertions:     insertions,
				Deletions:      deletions,
				CommitType:     parsed.CommitType,
				Scope:          parsed.Scope,
				Breaking:       parsed.Breaking,
				Subject:        parsed.Subject,
				Footers:        parsed.Footers,
				Refs:           parsed.Refs,
			})

		if err := ictx.Emitter.EmitNode(ctx, commitNode); err != nil {
			return err
		}

		// Repo owns commit
		if err := indexer.EmitOwnership(ctx, ictx.Emitter, repoNode.ID, commitNode.ID); err != nil {
			return err
		}

		// parent_of edges
		for _, parentSHA := range parents {
			parentID := graph.IDFromURI(types.CommitToURI(repoPath, parentSHA))
			pEdge := graph.NewEdge(types.EdgeParentOf, commitNode.ID, parentID)
			if err := ictx.Emitter.EmitEdge(ctx, pEdge); err != nil {
				return err
			}
		}

		// modifies edges (commit → fs:file)
		for _, fc := range fileChanges {
			fileID := graph.IDFromURI(types.PathToURI(filepath.Join(repoPath, fc.name)))
			mEdge := graph.NewEdge(types.EdgeModifies, commitNode.ID, fileID)
			if err := ictx.Emitter.EmitEdge(ctx, mEdge); err != nil {
				return err
			}
		}

		count++
	}

	return nil
}

// cleanup removes all git nodes for the given repository.
// Called when the .git directory is being deleted.
func (i *Indexer) cleanup(ctx context.Context, ictx *indexer.Context, repoPath string) error {
	repoURI := types.RepoPathToURI(repoPath)

	// Delete all nodes under this repo's URI prefix and track count
	deleted, err := ictx.Graph.Storage().DeleteByURIPrefix(ctx, repoURI)
	if deleted > 0 {
		ictx.AddNodesDeleted(deleted)
	}
	return err
}

// commitName builds the human-readable Name for a vcs:commit node.
// Format: "sha8" if no subject, "sha8 -- subject" otherwise.
// Subject is truncated to 72 characters with "..." suffix if longer.
func commitName(sha, subject string) string {
	short := sha
	if len(sha) >= 8 {
		short = sha[:8]
	}
	if subject == "" {
		return short
	}
	if len(subject) > 72 {
		subject = subject[:69] + "..."
	}
	return short + " -- " + subject
}
