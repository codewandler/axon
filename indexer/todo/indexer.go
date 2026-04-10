// Package todo indexes TODO/FIXME/HACK/XXX/NOTE annotation comments from
// source files, emitting one code:todo graph node per matched annotation.
// It is language-agnostic: any text file processed by the FS indexer is a
// candidate; binary files and files larger than maxFileSize are skipped.
package todo

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"os"
	"regexp"
	"strings"

	"github.com/codewandler/axon/graph"
	"github.com/codewandler/axon/indexer"
	"github.com/codewandler/axon/types"
)

// commentPattern matches annotation keywords in common single-line comment styles.
//
// The pattern requires the comment prefix to be at the start of the line
// (possibly after leading whitespace) so that "// TODO" embedded inside
// prose, backtick strings, or string literals is not matched.
//
// Supported prefixes: // (Go/JS/TS/Java/C), # (Python/Shell/YAML/Ruby),
// -- (SQL/Lua), ; (Lisp/Assembly). The optional * handles mid-block /** */ lines.
// Groups: (1) keyword (TODO|FIXME|HACK|XXX|NOTE), (2) annotation text.
var commentPattern = regexp.MustCompile(
	`(?im)^\s*(?:\/\/|#|--|;)\s*\*?\s*(TODO|FIXME|HACK|XXX|NOTE)\b[:.]?\s*(.*)`,
)

// maxFileSize is the largest file we will scan. Files over this limit are skipped.
const maxFileSize = 2 * 1024 * 1024 // 2 MB

// maxNameLen is the maximum length of the node Name field (truncated with "...").
const maxNameLen = 80

// Indexer scans source files for annotation comments and emits code:todo nodes.
type Indexer struct{}

// New returns a new TODO indexer.
func New() *Indexer { return &Indexer{} }

func (i *Indexer) Name() string { return "todo" }

func (i *Indexer) Schemes() []string { return []string{"file+todo"} }

func (i *Indexer) Handles(uri string) bool {
	return strings.HasPrefix(uri, "file+todo://")
}

func (i *Indexer) Subscriptions() []indexer.Subscription {
	return []indexer.Subscription{
		// Scan every file the FS indexer visits.
		{
			EventType: indexer.EventEntryVisited,
			NodeType:  types.TypeFile,
		},
		// Remove todo nodes when the parent file is deleted.
		{
			EventType: indexer.EventNodeDeleting,
			NodeType:  types.TypeFile,
		},
	}
}

// Index is a no-op: the todo indexer is entirely event-driven.
func (i *Indexer) Index(_ context.Context, _ *indexer.Context) error { return nil }

// HandleEvent dispatches incoming events.
func (i *Indexer) HandleEvent(ctx context.Context, ictx *indexer.Context, event indexer.Event) error {
	if event.Type == indexer.EventNodeDeleting {
		return i.cleanup(ctx, ictx, event)
	}
	return i.indexFile(ctx, ictx, event)
}

// cleanup removes all code:todo nodes for the given file.
func (i *Indexer) cleanup(ctx context.Context, ictx *indexer.Context, event indexer.Event) error {
	prefix := types.TodoURIPrefix(event.Path)
	deleted, err := ictx.Graph.Storage().DeleteByURIPrefix(ctx, prefix)
	if deleted > 0 {
		ictx.AddNodesDeleted(int(deleted))
	}
	return err
}

// indexFile scans the file for annotation comments and emits nodes.
func (i *Indexer) indexFile(ctx context.Context, ictx *indexer.Context, event indexer.Event) error {
	path := event.Path
	if path == "" {
		return nil
	}

	// Skip files that are too large.
	fi, err := os.Stat(path)
	if err != nil || fi.Size() > maxFileSize {
		return nil
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return nil // non-fatal: file may have been removed between events
	}

	// Binary detection: if the first 512 bytes contain a null byte, skip.
	probe := data
	if len(probe) > 512 {
		probe = probe[:512]
	}
	if bytes.IndexByte(probe, 0) >= 0 {
		return nil
	}

	// Clear previously indexed annotations for this file so re-indexing
	// correctly reflects edits and deletions.
	prefix := types.TodoURIPrefix(path)
	if _, err := ictx.Graph.Storage().DeleteByURIPrefix(ctx, prefix); err != nil {
		return fmt.Errorf("clearing old todo nodes for %s: %w", path, err)
	}

	fileNodeID := event.NodeID
	scanner := bufio.NewScanner(bytes.NewReader(data))
	lineNum := 0
	prevLine := ""

	for scanner.Scan() {
		lineNum++
		line := scanner.Text()

		if m := commentPattern.FindStringSubmatch(line); m != nil {
			kind := strings.ToUpper(m[1])
			text := strings.TrimSpace(m[2])

			name := kind
			if text != "" {
				name = kind + ": " + text
			}
			if len(name) > maxNameLen {
				name = name[:maxNameLen-3] + "..."
			}

			node := graph.NewNode(types.TypeTodo).
				WithURI(types.TodoURI(path, lineNum)).
				WithKey(fmt.Sprintf("%s:%d", path, lineNum)).
				WithName(name).
				WithData(types.TodoData{
					File:    path,
					Line:    lineNum,
					Kind:    kind,
					Text:    text,
					Context: strings.TrimSpace(prevLine),
				})
			node.AddLabels(strings.ToLower(kind))

			if err := ictx.Emitter.EmitNode(ctx, node); err != nil {
				return fmt.Errorf("emitting todo node at %s:%d: %w", path, lineNum, err)
			}
			if fileNodeID != "" {
				if err := indexer.EmitContainment(ctx, ictx.Emitter, fileNodeID, node.ID); err != nil {
					return fmt.Errorf("emitting todo containment at %s:%d: %w", path, lineNum, err)
				}
			}
		}

		prevLine = line
	}

	return scanner.Err()
}
