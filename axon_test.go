package axon

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/codewandler/axon/graph"
	"github.com/codewandler/axon/indexer"
	"github.com/codewandler/axon/types"
)

func setupTestDir(t *testing.T) string {
	t.Helper()

	dir := t.TempDir()

	// Create structure:
	// dir/
	//   file1.txt
	//   subdir/
	//     file2.txt
	//   .git/        (should be ignored)
	//     config

	if err := os.WriteFile(filepath.Join(dir, "file1.txt"), []byte("hello"), 0644); err != nil {
		t.Fatal(err)
	}

	subdir := filepath.Join(dir, "subdir")
	if err := os.Mkdir(subdir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(subdir, "file2.txt"), []byte("world"), 0644); err != nil {
		t.Fatal(err)
	}

	gitDir := filepath.Join(dir, ".git")
	if err := os.Mkdir(gitDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(gitDir, "config"), []byte("[core]"), 0644); err != nil {
		t.Fatal(err)
	}

	return dir
}

func TestAxonNew(t *testing.T) {
	dir := setupTestDir(t)

	ax, err := New(Config{Dir: dir})
	if err != nil {
		t.Fatalf("New failed: %v", err)
	}

	if ax.Graph() == nil {
		t.Error("Graph() returned nil")
	}
}

func TestAxonIndex(t *testing.T) {
	ctx := context.Background()
	dir := setupTestDir(t)

	ax, err := New(Config{Dir: dir})
	if err != nil {
		t.Fatalf("New failed: %v", err)
	}

	result, err := ax.Index(ctx, "")
	if err != nil {
		t.Fatalf("Index failed: %v", err)
	}

	// Should have file1.txt, file2.txt = 2 files
	if result.Files != 2 {
		t.Errorf("expected 2 files, got %d", result.Files)
	}

	// Should have dir, subdir, .git = 3 directories
	// (ignored directories are indexed as nodes so we can detect their deletion,
	// but their contents are skipped)
	if result.Directories != 3 {
		t.Errorf("expected 3 directories, got %d", result.Directories)
	}

	// .git directory should exist as a node (for deletion detection)
	nodes, err := ax.Graph().FindNodes(ctx, graph.NodeFilter{URIPrefix: types.PathToURI(filepath.Join(dir, ".git"))}, graph.QueryOptions{})
	if err != nil {
		t.Fatalf("FindNodes failed: %v", err)
	}
	if len(nodes) != 1 {
		t.Errorf("expected .git directory node, found %d nodes", len(nodes))
	}

	// But .git contents should not be indexed (check for a file that would be inside)
	allNodes, err := ax.Graph().FindNodes(ctx, graph.NodeFilter{}, graph.QueryOptions{})
	if err != nil {
		t.Fatalf("FindNodes failed: %v", err)
	}
	for _, n := range allNodes {
		path := types.URIToPath(n.URI)
		if strings.Contains(path, ".git/") {
			t.Errorf("expected .git contents to be skipped, found: %s", path)
		}
	}
}

func TestAxonReindex(t *testing.T) {
	ctx := context.Background()
	dir := setupTestDir(t)

	ax, err := New(Config{Dir: dir})
	if err != nil {
		t.Fatalf("New failed: %v", err)
	}

	// First index
	_, err = ax.Index(ctx, "")
	if err != nil {
		t.Fatalf("First Index failed: %v", err)
	}

	// Add a new file
	if err := os.WriteFile(filepath.Join(dir, "newfile.txt"), []byte("new"), 0644); err != nil {
		t.Fatal(err)
	}

	// Remove an existing file
	if err := os.Remove(filepath.Join(dir, "file1.txt")); err != nil {
		t.Fatal(err)
	}

	// Reindex
	result, err := ax.Index(ctx, "")
	if err != nil {
		t.Fatalf("Reindex failed: %v", err)
	}

	// Should have newfile.txt, file2.txt = 2 files
	if result.Files != 2 {
		t.Errorf("expected 2 files after reindex, got %d", result.Files)
	}

	// Should have removed 1 stale node (file1.txt)
	if result.StaleRemoved < 1 {
		t.Errorf("expected at least 1 stale entry removed, got %d", result.StaleRemoved)
	}

	// file1.txt should no longer exist
	_, err = ax.Graph().GetNodeByURI(ctx, types.PathToURI(filepath.Join(dir, "file1.txt")))
	if err == nil {
		t.Error("file1.txt should have been removed from graph")
	}

	// newfile.txt should exist
	_, err = ax.Graph().GetNodeByURI(ctx, types.PathToURI(filepath.Join(dir, "newfile.txt")))
	if err != nil {
		t.Error("newfile.txt should exist in graph")
	}
}

func TestAxonCustomIgnore(t *testing.T) {
	ctx := context.Background()
	dir := setupTestDir(t)

	// Custom ignore list: ignore subdir but not the defaults.
	// With the new behaviour, dot-prefixed paths are no longer blanket-excluded;
	// .git is only excluded when it appears in the ignore/exclude list.
	// Here we set a custom FSIgnore that contains only "subdir", so .git is NOT
	// excluded and .git/config will be indexed as a regular file.
	ax, err := New(Config{
		Dir:      dir,
		FSIgnore: []string{"subdir"},
	})
	if err != nil {
		t.Fatalf("New failed: %v", err)
	}

	result, err := ax.Index(ctx, "")
	if err != nil {
		t.Fatalf("Index failed: %v", err)
	}

	// Should have file1.txt + .git/config = 2 files
	// subdir is explicitly ignored so file2.txt is skipped.
	// .git is NOT auto-excluded when a custom FSIgnore is set (no defaults applied).
	if result.Files != 2 {
		t.Errorf("expected 2 files, got %d", result.Files)
	}

	// subdir should exist as a node (for deletion detection) but contents skipped
	nodes, err := ax.Graph().FindNodes(ctx, graph.NodeFilter{URIPrefix: types.PathToURI(filepath.Join(dir, "subdir"))}, graph.QueryOptions{})
	if err != nil {
		t.Fatalf("FindNodes failed: %v", err)
	}
	if len(nodes) != 1 {
		t.Errorf("expected subdir directory node only, found %d nodes", len(nodes))
	}

	// Check that file2.txt inside subdir is NOT indexed
	for _, n := range nodes {
		if filepath.Base(types.URIToPath(n.URI)) == "file2.txt" {
			t.Errorf("expected subdir contents to be skipped")
		}
	}
}

// TestAxonIndex_Silent verifies that a plain Index() call produces no output
// on stdout or stderr. Any accidental fmt.Print / log.Print inside the library
// would be caught here.
func TestAxonIndex_Silent(t *testing.T) {
	ctx := context.Background()
	dir := setupTestDir(t)

	ax, err := New(Config{Dir: dir})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	rOut, wOut, _ := os.Pipe()
	rErr, wErr, _ := os.Pipe()
	oldOut, oldErr := os.Stdout, os.Stderr
	os.Stdout, os.Stderr = wOut, wErr

	_, indexErr := ax.Index(ctx, "")

	wOut.Close()
	wErr.Close()
	os.Stdout, os.Stderr = oldOut, oldErr

	var bufOut, bufErr strings.Builder
	io.Copy(&bufOut, rOut) //nolint:errcheck
	io.Copy(&bufErr, rErr) //nolint:errcheck
	rOut.Close()
	rErr.Close()

	if indexErr != nil {
		t.Fatalf("Index: %v", indexErr)
	}
	if got := bufOut.String(); got != "" {
		t.Errorf("Index() wrote to stdout: %q", got)
	}
	if got := bufErr.String(); got != "" {
		t.Errorf("Index() wrote to stderr: %q", got)
	}
}

// TestAxonWatch_Silent verifies Watch() with no callbacks and no ShowProgress
// produces zero output — the initial index and the library internals are silent.
func TestAxonWatch_Silent(t *testing.T) {
	dir := setupTestDir(t)

	ax, err := New(Config{Dir: dir})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	rOut, wOut, _ := os.Pipe()
	rErr, wErr, _ := os.Pipe()
	oldOut, oldErr := os.Stdout, os.Stderr
	os.Stdout, os.Stderr = wOut, wErr

	// Cancel immediately after the initial index so Watch returns quickly.
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	errCh := make(chan error, 1)
	go func() {
		errCh <- ax.Watch(ctx, dir, WatchOptions{
			// OnReady and OnReindex deliberately nil — no output expected.
		})
	}()
	// Give the initial index time to complete then cancel.
	// (setupTestDir is tiny, so 500 ms is generous.)
	select {
	case err := <-errCh:
		// Watch exited before we cancelled — unexpected but non-fatal.
		if err != nil && err != context.Canceled {
			t.Logf("Watch returned early: %v", err)
		}
	case <-waitForInitDone(ax, dir, 2*time.Second):
		cancel()
		<-errCh
	}

	wOut.Close()
	wErr.Close()
	os.Stdout, os.Stderr = oldOut, oldErr

	var bufOut, bufErr strings.Builder
	io.Copy(&bufOut, rOut) //nolint:errcheck
	io.Copy(&bufErr, rErr) //nolint:errcheck
	rOut.Close()
	rErr.Close()

	if got := bufOut.String(); got != "" {
		t.Errorf("Watch() wrote to stdout: %q", got)
	}
	if got := bufErr.String(); got != "" {
		t.Errorf("Watch() wrote to stderr: %q", got)
	}
}

// waitForInitDone returns a channel that closes once the initial index has
// populated at least one node in the graph — used to time Watch cancellation.
func waitForInitDone(ax *Axon, dir string, timeout time.Duration) <-chan struct{} {
	ch := make(chan struct{})
	go func() {
		deadline := time.Now().Add(timeout)
		for time.Now().Before(deadline) {
			nodes, _ := ax.Graph().FindNodes(context.Background(),
				graph.NodeFilter{URIPrefix: types.PathToURI(dir)},
				graph.QueryOptions{Limit: 1})
			if len(nodes) > 0 {
				close(ch)
				return
			}
			time.Sleep(50 * time.Millisecond)
		}
		close(ch) // timed out — let caller proceed anyway
	}()
	return ch
}

// TestAxonIndex_ShowProgress verifies that ShowProgress: true writes lines to
// stderr and nothing to stdout.
func TestAxonIndex_ShowProgress(t *testing.T) {
	ctx := context.Background()
	dir := setupTestDir(t)

	ax, err := New(Config{Dir: dir})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	rOut, wOut, _ := os.Pipe()
	rErr, wErr, _ := os.Pipe()
	oldOut, oldErr := os.Stdout, os.Stderr
	os.Stdout, os.Stderr = wOut, wErr

	_, indexErr := ax.IndexWithOptions(ctx, IndexOptions{ShowProgress: true})

	wOut.Close()
	wErr.Close()
	os.Stdout, os.Stderr = oldOut, oldErr

	var bufOut, bufErr strings.Builder
	io.Copy(&bufOut, rOut) //nolint:errcheck
	io.Copy(&bufErr, rErr) //nolint:errcheck
	rOut.Close()
	rErr.Close()

	if indexErr != nil {
		t.Fatalf("Index: %v", indexErr)
	}
	if got := bufOut.String(); got != "" {
		t.Errorf("ShowProgress wrote to stdout: %q", got)
	}
	if got := bufErr.String(); !strings.Contains(got, "[axon]") {
		t.Errorf("expected [axon] lines on stderr, got: %q", got)
	}
}

func TestAxonDeleteByPath(t *testing.T) {
	ctx := context.Background()
	dir := setupTestDir(t)

	ax, err := New(Config{Dir: dir})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if _, err := ax.Index(ctx, ""); err != nil {
		t.Fatalf("Index: %v", err)
	}

	filePath := filepath.Join(dir, "file1.txt")
	fileURI := types.PathToURI(filePath)

	// Verify the file node exists before deletion.
	if _, err := ax.Graph().GetNodeByURI(ctx, fileURI); err != nil {
		t.Fatalf("file1.txt not found before DeleteByPath: %v", err)
	}

	// Delete it from the graph.
	if err := ax.DeleteByPath(ctx, filePath); err != nil {
		t.Fatalf("DeleteByPath: %v", err)
	}

	// Node must be gone.
	if _, err := ax.Graph().GetNodeByURI(ctx, fileURI); err == nil {
		t.Error("file1.txt should have been removed by DeleteByPath")
	}
}

func TestAxonDeleteByPath_Directory(t *testing.T) {
	ctx := context.Background()
	dir := setupTestDir(t)

	ax, err := New(Config{Dir: dir})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if _, err := ax.Index(ctx, ""); err != nil {
		t.Fatalf("Index: %v", err)
	}

	subdirPath := filepath.Join(dir, "subdir")
	file2Path := filepath.Join(dir, "subdir", "file2.txt")

	// Both subdir and its child must exist.
	if _, err := ax.Graph().GetNodeByURI(ctx, types.PathToURI(subdirPath)); err != nil {
		t.Fatalf("subdir not found before DeleteByPath: %v", err)
	}
	if _, err := ax.Graph().GetNodeByURI(ctx, types.PathToURI(file2Path)); err != nil {
		t.Fatalf("file2.txt not found before DeleteByPath: %v", err)
	}

	// Delete the whole subtree.
	if err := ax.DeleteByPath(ctx, subdirPath); err != nil {
		t.Fatalf("DeleteByPath(subdir): %v", err)
	}

	// Both subdir and file2.txt must be gone.
	if _, err := ax.Graph().GetNodeByURI(ctx, types.PathToURI(subdirPath)); err == nil {
		t.Error("subdir should have been removed")
	}
	if _, err := ax.Graph().GetNodeByURI(ctx, types.PathToURI(file2Path)); err == nil {
		t.Error("file2.txt should have been removed (part of deleted subtree)")
	}
}

func TestAxonDeleteByPath_Idempotent(t *testing.T) {
	ctx := context.Background()
	dir := setupTestDir(t)

	ax, err := New(Config{Dir: dir})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if _, err := ax.Index(ctx, ""); err != nil {
		t.Fatalf("Index: %v", err)
	}

	filePath := filepath.Join(dir, "file1.txt")

	// First delete succeeds.
	if err := ax.DeleteByPath(ctx, filePath); err != nil {
		t.Fatalf("first DeleteByPath: %v", err)
	}
	// Second call on a non-existent node must also return nil (idempotent).
	if err := ax.DeleteByPath(ctx, filePath); err != nil {
		t.Errorf("second DeleteByPath should be idempotent, got: %v", err)
	}
}

func TestAxonWatch_FileChange(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping watch integration test in short mode")
	}
	ctx := context.Background()
	dir := setupTestDir(t)

	ax, err := New(Config{Dir: dir})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	reindexed := make(chan string, 5)
	watchCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	errCh := make(chan error, 1)
	go func() {
		errCh <- ax.Watch(watchCtx, dir, WatchOptions{
			IndexOptions: IndexOptions{SkipGC: true},
			Debounce:     50 * time.Millisecond,
			OnReindex: func(path string, result *IndexResult, err error) {
				if err == nil {
					reindexed <- path
				}
			},
		})
	}()

	// Wait for the initial index to populate the graph.
	select {
	case <-waitForInitDone(ax, dir, 5*time.Second):
	case <-time.After(5 * time.Second):
		t.Fatal("initial index timed out")
	}

	// Write a new file — the watcher should pick it up and re-index just that file.
	newFile := filepath.Join(dir, "watch_new.txt")
	if err := os.WriteFile(newFile, []byte("hello watcher"), 0644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	// Expect OnReindex to fire for the new file within a generous timeout.
	select {
	case path := <-reindexed:
		if path != newFile {
			t.Errorf("expected re-index path %s, got %s", newFile, path)
		}
	case <-time.After(3 * time.Second):
		t.Error("timed out waiting for OnReindex after file creation")
	}

	// The new file must now be in the graph.
	if _, err := ax.Graph().GetNodeByURI(ctx, types.PathToURI(newFile)); err != nil {
		t.Errorf("watch_new.txt should be in graph after re-index: %v", err)
	}

	cancel()
	<-errCh
}

func TestAxonWatch_FileDeletion(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping watch integration test in short mode")
	}
	ctx := context.Background()
	dir := setupTestDir(t)

	ax, err := New(Config{Dir: dir})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	watchCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	errCh := make(chan error, 1)
	go func() {
		errCh <- ax.Watch(watchCtx, dir, WatchOptions{
			IndexOptions: IndexOptions{SkipGC: true},
			Debounce:     50 * time.Millisecond,
		})
	}()

	// Wait for the initial index.
	select {
	case <-waitForInitDone(ax, dir, 5*time.Second):
	case <-time.After(5 * time.Second):
		t.Fatal("initial index timed out")
	}

	fileToDelete := filepath.Join(dir, "file1.txt")
	fileURI := types.PathToURI(fileToDelete)

	// Confirm the file is indexed.
	if _, err := ax.Graph().GetNodeByURI(ctx, fileURI); err != nil {
		t.Fatalf("file1.txt should be indexed before deletion: %v", err)
	}

	// Delete the file on disk; the watcher should remove it from the graph.
	if err := os.Remove(fileToDelete); err != nil {
		t.Fatalf("Remove: %v", err)
	}

	// Poll until the node is gone or the deadline is exceeded.
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if _, err := ax.Graph().GetNodeByURI(ctx, fileURI); err != nil {
			// Node is gone — expected.
			cancel()
			<-errCh
			return
		}
		time.Sleep(50 * time.Millisecond)
	}

	t.Error("file1.txt should have been removed from graph after filesystem deletion")
	cancel()
	<-errCh
}

// slowSubscriber is a test indexer that subscribes to fs:file visited events,
// simulates slow I/O by sleeping, and counts the number of events received.
type slowSubscriber struct {
	delay    time.Duration
	received atomic.Int64
}

func (s *slowSubscriber) Name() string        { return "slow-test-subscriber" }
func (s *slowSubscriber) Schemes() []string    { return nil }
func (s *slowSubscriber) Handles(_ string) bool { return false }
func (s *slowSubscriber) Subscriptions() []indexer.Subscription {
	return []indexer.Subscription{
		{EventType: indexer.EventEntryVisited, NodeType: types.TypeFile},
	}
}
func (s *slowSubscriber) Index(_ context.Context, _ *indexer.Context) error { return nil }
func (s *slowSubscriber) HandleEvent(_ context.Context, _ *indexer.Context, _ indexer.Event) error {
	time.Sleep(s.delay)
	s.received.Add(1)
	return nil
}

// TestDispatcher_NoEventsDroppedWhenSubscriberSlow verifies that no events are
// dropped when a slow subscriber cannot keep up with the FS indexer.
// It sets a tiny channel buffer so the subscriber's queue fills quickly,
// then asserts that every file event was delivered.
func TestDispatcher_NoEventsDroppedWhenSubscriberSlow(t *testing.T) {
	// Force a tiny buffer so the subscriber channel fills almost immediately.
	original := eventChannelBuffer
	eventChannelBuffer = 5
	t.Cleanup(func() { eventChannelBuffer = original })

	const fileCount = 20
	dir := t.TempDir()
	for i := range fileCount {
		content := fmt.Sprintf("package p\n// TODO: item %d\nvar _ = %d\n", i, i)
		if err := os.WriteFile(
			filepath.Join(dir, fmt.Sprintf("f%02d.go", i)),
			[]byte(content), 0o644,
		); err != nil {
			t.Fatal(err)
		}
	}

	ax, err := New(Config{Dir: dir, FSExclude: []string{}})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	// Register the slow subscriber after construction so we can inspect its counter.
	sub := &slowSubscriber{delay: 2 * time.Millisecond}
	ax.indexers.Register(sub)

	if _, err := ax.Index(context.Background(), ""); err != nil {
		t.Fatalf("Index: %v", err)
	}

	got := sub.received.Load()
	if got != fileCount {
		t.Errorf("slow subscriber received %d/%d events (events were dropped)", got, fileCount)
	}
}
