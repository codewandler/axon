package git

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	gogit "github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/config"
	"github.com/go-git/go-git/v5/plumbing/object"

	"github.com/codewandler/axon/graph"
	"github.com/codewandler/axon/indexer"
	"github.com/codewandler/axon/storage/memory"
	"github.com/codewandler/axon/types"
)

func setupTestRepo(t *testing.T) string {
	t.Helper()

	dir := t.TempDir()

	// Initialize a git repository
	repo, err := gogit.PlainInit(dir, false)
	if err != nil {
		t.Fatalf("failed to init repo: %v", err)
	}

	// Create a file and commit
	testFile := filepath.Join(dir, "test.txt")
	if err := os.WriteFile(testFile, []byte("hello"), 0644); err != nil {
		t.Fatalf("failed to write file: %v", err)
	}

	worktree, err := repo.Worktree()
	if err != nil {
		t.Fatalf("failed to get worktree: %v", err)
	}

	_, err = worktree.Add("test.txt")
	if err != nil {
		t.Fatalf("failed to add file: %v", err)
	}

	_, err = worktree.Commit("initial commit", &gogit.CommitOptions{
		Author: &object.Signature{
			Name:  "Test",
			Email: "test@example.com",
			When:  time.Now(),
		},
	})
	if err != nil {
		t.Fatalf("failed to commit: %v", err)
	}

	// Create a tag
	head, _ := repo.Head()
	_, err = repo.CreateTag("v1.0.0", head.Hash(), nil)
	if err != nil {
		t.Fatalf("failed to create tag: %v", err)
	}

	return dir
}

func setupGraph(t *testing.T) *graph.Graph {
	t.Helper()
	r := graph.NewRegistry()
	types.RegisterFSTypes(r)
	types.RegisterVCSTypes(r)
	s := memory.New()
	return graph.New(s, r)
}

func TestIndexerBasic(t *testing.T) {
	ctx := context.Background()
	dir := setupTestRepo(t)
	g := setupGraph(t)

	idx := New()
	emitter := indexer.NewGraphEmitter(g, "gen-1")

	ictx := &indexer.Context{
		Root:       types.RepoPathToURI(dir),
		Generation: "gen-1",
		Graph:      g,
		Emitter:    emitter,
	}

	if err := idx.Index(ctx, ictx); err != nil {
		t.Fatalf("Index failed: %v", err)
	}

	// Should have repo node
	repos, err := g.FindNodes(ctx, graph.NodeFilter{Type: types.TypeRepo})
	if err != nil {
		t.Fatalf("FindNodes failed: %v", err)
	}
	if len(repos) != 1 {
		t.Errorf("expected 1 repo, got %d", len(repos))
	}

	// Check repo data
	repo := repos[0]
	data, ok := repo.Data.(types.RepoData)
	if !ok {
		// Try map conversion (from JSON serialization)
		if m, ok := repo.Data.(map[string]any); ok {
			if name, ok := m["name"].(string); ok && name != "" {
				t.Logf("repo name: %s", name)
			}
		}
	} else {
		if data.HeadBranch == "" {
			t.Error("expected head branch to be set")
		}
		if data.HeadCommit == "" {
			t.Error("expected head commit to be set")
		}
	}

	// Should have branch node (master or main)
	branches, err := g.FindNodes(ctx, graph.NodeFilter{Type: types.TypeBranch})
	if err != nil {
		t.Fatalf("FindNodes failed: %v", err)
	}
	if len(branches) < 1 {
		t.Errorf("expected at least 1 branch, got %d", len(branches))
	}

	// Should have tag node
	tags, err := g.FindNodes(ctx, graph.NodeFilter{Type: types.TypeTag})
	if err != nil {
		t.Fatalf("FindNodes failed: %v", err)
	}
	if len(tags) != 1 {
		t.Errorf("expected 1 tag, got %d", len(tags))
	}
}

func TestIndexerWithRemote(t *testing.T) {
	ctx := context.Background()
	dir := setupTestRepo(t)
	g := setupGraph(t)

	// Add a remote
	repo, _ := gogit.PlainOpen(dir)
	_, err := repo.CreateRemote(&config.RemoteConfig{
		Name: "origin",
		URLs: []string{"https://github.com/example/repo.git"},
	})
	if err != nil {
		t.Fatalf("failed to create remote: %v", err)
	}

	idx := New()
	emitter := indexer.NewGraphEmitter(g, "gen-1")

	ictx := &indexer.Context{
		Root:       types.RepoPathToURI(dir),
		Generation: "gen-1",
		Graph:      g,
		Emitter:    emitter,
	}

	if err := idx.Index(ctx, ictx); err != nil {
		t.Fatalf("Index failed: %v", err)
	}

	// Should have remote node
	remotes, err := g.FindNodes(ctx, graph.NodeFilter{Type: types.TypeRemote})
	if err != nil {
		t.Fatalf("FindNodes failed: %v", err)
	}
	if len(remotes) != 1 {
		t.Errorf("expected 1 remote, got %d", len(remotes))
	}

	// Check edges from repo to remote
	repos, _ := g.FindNodes(ctx, graph.NodeFilter{Type: types.TypeRepo})
	if len(repos) > 0 {
		edges, _ := g.GetEdgesFrom(ctx, repos[0].ID)
		hasRemoteEdge := false
		for _, e := range edges {
			if e.Type == types.EdgeHasRemote {
				hasRemoteEdge = true
				break
			}
		}
		if !hasRemoteEdge {
			t.Error("expected has_remote edge from repo")
		}
	}
}

func TestIndexerMeta(t *testing.T) {
	idx := New()

	if idx.Name() != "git" {
		t.Errorf("expected name 'git', got %q", idx.Name())
	}

	schemes := idx.Schemes()
	if len(schemes) != 1 || schemes[0] != "git" {
		t.Errorf("expected schemes [git], got %v", schemes)
	}

	if !idx.Handles("git:///home/user/repo") {
		t.Error("should handle git:// URIs")
	}

	if idx.Handles("file:///home/user/repo") {
		t.Error("should not handle file:// URIs")
	}
}

func TestIndexFromPath(t *testing.T) {
	ctx := context.Background()
	dir := setupTestRepo(t)
	g := setupGraph(t)

	emitter := indexer.NewGraphEmitter(g, "gen-1")

	if err := IndexFromPath(ctx, g, emitter, dir, "gen-1"); err != nil {
		t.Fatalf("IndexFromPath failed: %v", err)
	}

	repos, _ := g.FindNodes(ctx, graph.NodeFilter{Type: types.TypeRepo})
	if len(repos) != 1 {
		t.Errorf("expected 1 repo, got %d", len(repos))
	}
}
