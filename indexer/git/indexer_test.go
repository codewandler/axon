package git

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	gogit "github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/config"
	"github.com/go-git/go-git/v5/plumbing/object"

	"github.com/codewandler/axon/adapters/sqlite"
	"github.com/codewandler/axon/graph"
	"github.com/codewandler/axon/indexer"
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
	types.RegisterCommonEdges(r)
	types.RegisterFSTypes(r)
	types.RegisterVCSTypes(r)
	s, err := sqlite.New(":memory:")
	if err != nil {
		t.Fatalf("sqlite.New failed: %v", err)
	}
	t.Cleanup(func() { s.Close() })
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

	// Simulate the event that FS indexer would send when visiting .git
	gitDir := filepath.Join(dir, ".git")
	event := indexer.Event{
		Type:     indexer.EventEntryVisited,
		URI:      types.PathToURI(gitDir),
		Path:     gitDir,
		Name:     ".git",
		NodeType: types.TypeDir,
		NodeID:   "test-git-dir-id",
	}

	if err := idx.HandleEvent(ctx, ictx, event); err != nil {
		t.Fatalf("HandleEvent failed: %v", err)
	}

	// Flush storage
	if err := g.Storage().Flush(ctx); err != nil {
		t.Fatalf("Flush failed: %v", err)
	}

	// Should have repo node
	repos, err := g.FindNodes(ctx, graph.NodeFilter{Type: types.TypeRepo}, graph.QueryOptions{})
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
	branches, err := g.FindNodes(ctx, graph.NodeFilter{Type: types.TypeBranch}, graph.QueryOptions{})
	if err != nil {
		t.Fatalf("FindNodes failed: %v", err)
	}
	if len(branches) < 1 {
		t.Errorf("expected at least 1 branch, got %d", len(branches))
	}

	// Should have tag node
	tags, err := g.FindNodes(ctx, graph.NodeFilter{Type: types.TypeTag}, graph.QueryOptions{})
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

	// Simulate the event that FS indexer would send when visiting .git
	gitDir := filepath.Join(dir, ".git")
	event := indexer.Event{
		Type:     indexer.EventEntryVisited,
		URI:      types.PathToURI(gitDir),
		Path:     gitDir,
		Name:     ".git",
		NodeType: types.TypeDir,
		NodeID:   "test-git-dir-id",
	}

	if err := idx.HandleEvent(ctx, ictx, event); err != nil {
		t.Fatalf("HandleEvent failed: %v", err)
	}

	// Flush storage
	if err := g.Storage().Flush(ctx); err != nil {
		t.Fatalf("Flush failed: %v", err)
	}

	// Should have remote node
	remotes, err := g.FindNodes(ctx, graph.NodeFilter{Type: types.TypeRemote}, graph.QueryOptions{})
	if err != nil {
		t.Fatalf("FindNodes failed: %v", err)
	}
	if len(remotes) != 1 {
		t.Errorf("expected 1 remote, got %d", len(remotes))
	}

	// Check edges from repo to remote (now using 'has' edge type)
	repos, _ := g.FindNodes(ctx, graph.NodeFilter{Type: types.TypeRepo}, graph.QueryOptions{})
	if len(repos) > 0 {
		edges, _ := g.GetEdgesFrom(ctx, repos[0].ID)
		hasRemoteEdge := false
		for _, e := range edges {
			if e.Type == types.EdgeHas {
				// Check if target is a remote
				target, err := g.GetNode(ctx, e.To)
				if err == nil && target.Type == types.TypeRemote {
					hasRemoteEdge = true
					break
				}
			}
		}
		if !hasRemoteEdge {
			t.Error("expected 'has' edge from repo to remote")
		}
	}
}

func TestIndexerMeta(t *testing.T) {
	idx := New()

	if idx.Name() != "git" {
		t.Errorf("expected name 'git', got %q", idx.Name())
	}

	schemes := idx.Schemes()
	if len(schemes) != 1 || schemes[0] != "git+file" {
		t.Errorf("expected schemes [git+file], got %v", schemes)
	}

	if !idx.Handles("git+file:///home/user/repo") {
		t.Error("should handle git+file:// URIs")
	}

	if idx.Handles("file:///home/user/repo") {
		t.Error("should not handle file:// URIs")
	}
}

func TestSubscriptions(t *testing.T) {
	idx := New()
	subs := idx.Subscriptions()

	if len(subs) != 2 {
		t.Fatalf("expected 2 subscriptions, got %d", len(subs))
	}

	// First subscription: EventEntryVisited for .git dirs
	sub := subs[0]
	if sub.EventType != indexer.EventEntryVisited {
		t.Error("expected first subscription to be EventEntryVisited")
	}
	if sub.NodeType != types.TypeDir {
		t.Errorf("expected NodeType fs:dir, got %s", sub.NodeType)
	}
	if sub.Name != ".git" {
		t.Errorf("expected Name .git, got %s", sub.Name)
	}

	// Second subscription: EventNodeDeleting for .git dirs
	sub2 := subs[1]
	if sub2.EventType != indexer.EventNodeDeleting {
		t.Error("expected second subscription to be EventNodeDeleting")
	}
	if sub2.NodeType != types.TypeDir {
		t.Errorf("expected NodeType fs:dir, got %s", sub2.NodeType)
	}
	if sub2.Name != ".git" {
		t.Errorf("expected Name .git, got %s", sub2.Name)
	}
}

func setupTestRepoWithCommits(t *testing.T, n int) string {
	t.Helper()
	dir := t.TempDir()

	repo, err := gogit.PlainInit(dir, false)
	if err != nil {
		t.Fatalf("PlainInit: %v", err)
	}

	worktree, err := repo.Worktree()
	if err != nil {
		t.Fatalf("Worktree: %v", err)
	}

	for i := 0; i < n; i++ {
		fname := fmt.Sprintf("file%d.txt", i)
		if err := os.WriteFile(filepath.Join(dir, fname), []byte(fmt.Sprintf("content %d", i)), 0644); err != nil {
			t.Fatalf("WriteFile: %v", err)
		}
		if _, err := worktree.Add(fname); err != nil {
			t.Fatalf("Add: %v", err)
		}
		if _, err := worktree.Commit(fmt.Sprintf("commit %d", i), &gogit.CommitOptions{
			Author: &object.Signature{
				Name:  "Test",
				Email: "test@example.com",
				When:  time.Now(),
			},
		}); err != nil {
			t.Fatalf("Commit: %v", err)
		}
	}

	return dir
}

func TestIndexerCommits(t *testing.T) {
	ctx := context.Background()
	dir := setupTestRepoWithCommits(t, 3)
	g := setupGraph(t)

	idx := New()
	emitter := indexer.NewGraphEmitter(g, "gen-1")
	ictx := &indexer.Context{
		Root:       types.RepoPathToURI(dir),
		Generation: "gen-1",
		Graph:      g,
		Emitter:    emitter,
	}

	gitDir := filepath.Join(dir, ".git")
	event := indexer.Event{
		Type:     indexer.EventEntryVisited,
		URI:      types.PathToURI(gitDir),
		Path:     gitDir,
		Name:     ".git",
		NodeType: types.TypeDir,
		NodeID:   "test-git-dir-id",
	}

	if err := idx.HandleEvent(ctx, ictx, event); err != nil {
		t.Fatalf("HandleEvent: %v", err)
	}
	if err := g.Storage().Flush(ctx); err != nil {
		t.Fatalf("Flush: %v", err)
	}

	// 3 commit nodes
	commits, err := g.FindNodes(ctx, graph.NodeFilter{Type: types.TypeCommit}, graph.QueryOptions{})
	if err != nil {
		t.Fatalf("FindNodes: %v", err)
	}
	if len(commits) != 3 {
		t.Errorf("expected 3 commits, got %d", len(commits))
	}

	// Repo → commits via 'has' edges
	repos, _ := g.FindNodes(ctx, graph.NodeFilter{Type: types.TypeRepo}, graph.QueryOptions{})
	if len(repos) == 0 {
		t.Fatal("no repo found")
	}
	edges, _ := g.GetEdgesFrom(ctx, repos[0].ID)
	hasCommitEdges := 0
	for _, e := range edges {
		if e.Type == types.EdgeHas {
			target, err := g.GetNode(ctx, e.To)
			if err == nil && target.Type == types.TypeCommit {
				hasCommitEdges++
			}
		}
	}
	if hasCommitEdges != 3 {
		t.Errorf("expected 3 'has' edges to commits, got %d", hasCommitEdges)
	}

	// parent_of edges: chain of 3 → 2 parent edges
	parentEdges := 0
	for _, c := range commits {
		ces, _ := g.GetEdgesFrom(ctx, c.ID)
		for _, e := range ces {
			if e.Type == types.EdgeParentOf {
				parentEdges++
			}
		}
	}
	if parentEdges != 2 {
		t.Errorf("expected 2 parent_of edges, got %d", parentEdges)
	}
}

func TestIndexerCommitDepthLimit(t *testing.T) {
	ctx := context.Background()
	dir := setupTestRepoWithCommits(t, 10)
	g := setupGraph(t)

	idx := New(Config{MaxCommits: 3})
	emitter := indexer.NewGraphEmitter(g, "gen-1")
	ictx := &indexer.Context{
		Root:       types.RepoPathToURI(dir),
		Generation: "gen-1",
		Graph:      g,
		Emitter:    emitter,
	}

	gitDir := filepath.Join(dir, ".git")
	event := indexer.Event{
		Type:     indexer.EventEntryVisited,
		URI:      types.PathToURI(gitDir),
		Path:     gitDir,
		Name:     ".git",
		NodeType: types.TypeDir,
		NodeID:   "test-git-dir-id",
	}

	if err := idx.HandleEvent(ctx, ictx, event); err != nil {
		t.Fatalf("HandleEvent: %v", err)
	}
	if err := g.Storage().Flush(ctx); err != nil {
		t.Fatalf("Flush: %v", err)
	}

	commits, err := g.FindNodes(ctx, graph.NodeFilter{Type: types.TypeCommit}, graph.QueryOptions{})
	if err != nil {
		t.Fatalf("FindNodes: %v", err)
	}
	if len(commits) != 3 {
		t.Errorf("expected 3 commits (MaxCommits limit), got %d", len(commits))
	}
}

func TestIndexerModifiesEdges(t *testing.T) {
	ctx := context.Background()
	dir := setupTestRepoWithCommits(t, 2)
	g := setupGraph(t)

	idx := New()
	emitter := indexer.NewGraphEmitter(g, "gen-1")
	ictx := &indexer.Context{
		Root:       types.RepoPathToURI(dir),
		Generation: "gen-1",
		Graph:      g,
		Emitter:    emitter,
	}

	gitDir := filepath.Join(dir, ".git")
	event := indexer.Event{
		Type:     indexer.EventEntryVisited,
		URI:      types.PathToURI(gitDir),
		Path:     gitDir,
		Name:     ".git",
		NodeType: types.TypeDir,
		NodeID:   "test-git-dir-id",
	}

	if err := idx.HandleEvent(ctx, ictx, event); err != nil {
		t.Fatalf("HandleEvent: %v", err)
	}
	if err := g.Storage().Flush(ctx); err != nil {
		t.Fatalf("Flush: %v", err)
	}

	commits, _ := g.FindNodes(ctx, graph.NodeFilter{Type: types.TypeCommit}, graph.QueryOptions{})
	if len(commits) == 0 {
		t.Fatal("no commits found")
	}

	found := false
	for _, c := range commits {
		ces, _ := g.GetEdgesFrom(ctx, c.ID)
		for _, e := range ces {
			if e.Type == types.EdgeModifies {
				found = true
				break
			}
		}
		if found {
			break
		}
	}
	if !found {
		t.Error("expected at least one 'modifies' edge from a commit to a file")
	}
}

func TestIndexerBranchCommitReference(t *testing.T) {
	ctx := context.Background()
	dir := setupTestRepoWithCommits(t, 2)
	g := setupGraph(t)

	idx := New()
	emitter := indexer.NewGraphEmitter(g, "gen-1")
	ictx := &indexer.Context{
		Root:       types.RepoPathToURI(dir),
		Generation: "gen-1",
		Graph:      g,
		Emitter:    emitter,
	}

	gitDir := filepath.Join(dir, ".git")
	event := indexer.Event{
		Type:     indexer.EventEntryVisited,
		URI:      types.PathToURI(gitDir),
		Path:     gitDir,
		Name:     ".git",
		NodeType: types.TypeDir,
		NodeID:   "test-git-dir-id",
	}

	if err := idx.HandleEvent(ctx, ictx, event); err != nil {
		t.Fatalf("HandleEvent: %v", err)
	}
	if err := g.Storage().Flush(ctx); err != nil {
		t.Fatalf("Flush: %v", err)
	}

	branches, err := g.FindNodes(ctx, graph.NodeFilter{Type: types.TypeBranch}, graph.QueryOptions{})
	if err != nil {
		t.Fatalf("FindNodes: %v", err)
	}
	if len(branches) == 0 {
		t.Fatal("no branches found")
	}

	found := false
	for _, branch := range branches {
		edges, _ := g.GetEdgesFrom(ctx, branch.ID)
		for _, e := range edges {
			if e.Type == types.EdgeReferences {
				target, err := g.GetNode(ctx, e.To)
				if err == nil && target.Type == types.TypeCommit {
					found = true
					break
				}
			}
		}
		if found {
			break
		}
	}
	if !found {
		t.Error("expected 'references' edge from branch to its tip commit")
	}
}

func TestIndexerTagCommitReference(t *testing.T) {
	ctx := context.Background()
	dir := setupTestRepo(t) // creates 1 commit + 1 tag (v1.0.0)
	g := setupGraph(t)

	idx := New()
	emitter := indexer.NewGraphEmitter(g, "gen-1")
	ictx := &indexer.Context{
		Root:       types.RepoPathToURI(dir),
		Generation: "gen-1",
		Graph:      g,
		Emitter:    emitter,
	}

	gitDir := filepath.Join(dir, ".git")
	event := indexer.Event{
		Type:     indexer.EventEntryVisited,
		URI:      types.PathToURI(gitDir),
		Path:     gitDir,
		Name:     ".git",
		NodeType: types.TypeDir,
		NodeID:   "test-git-dir-id",
	}

	if err := idx.HandleEvent(ctx, ictx, event); err != nil {
		t.Fatalf("HandleEvent: %v", err)
	}
	if err := g.Storage().Flush(ctx); err != nil {
		t.Fatalf("Flush: %v", err)
	}

	tags, err := g.FindNodes(ctx, graph.NodeFilter{Type: types.TypeTag}, graph.QueryOptions{})
	if err != nil {
		t.Fatalf("FindNodes: %v", err)
	}
	if len(tags) == 0 {
		t.Fatal("no tags found")
	}

	found := false
	for _, tag := range tags {
		edges, _ := g.GetEdgesFrom(ctx, tag.ID)
		for _, e := range edges {
			if e.Type == types.EdgeReferences {
				target, err := g.GetNode(ctx, e.To)
				if err == nil && target.Type == types.TypeCommit {
					found = true
					break
				}
			}
		}
		if found {
			break
		}
	}
	if !found {
		t.Error("expected 'references' edge from tag to its commit")
	}
}
