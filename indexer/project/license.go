// Package project provides detection of project:license nodes from LICENSE/COPYING files.
// This file implements the LicenseIndexer which reads licence-file headers and
// emits structured project:license nodes with SPDX identifiers.
package project

import (
	"context"
	"os"
	"path/filepath"
	"strings"

	"github.com/codewandler/axon/graph"
	"github.com/codewandler/axon/indexer"
	"github.com/codewandler/axon/types"
)

// spdxEntry describes one SPDX licence identifier and the header fragments used
// to detect it. All Required fragments must appear (case-insensitive AND);
// none of the Exclude fragments may appear.
type spdxEntry struct {
	ID       string
	Required []string
	Exclude  []string
}

// spdxTable is matched top-to-bottom; order matters for GPL-3 vs GPL-2.
var spdxTable = []spdxEntry{
	{
		ID:       "MIT",
		Required: []string{"permission is hereby granted, free of charge"},
	},
	{
		ID:       "Apache-2.0",
		Required: []string{"apache license, version 2.0"},
	},
	{
		// GPL-3 must be checked before GPL-2 because both say "GNU GENERAL PUBLIC LICENSE"
		ID:       "GPL-3.0-only",
		Required: []string{"gnu general public license", "version 3"},
	},
	{
		ID:       "GPL-2.0-only",
		Required: []string{"gnu general public license", "version 2"},
	},
	{
		ID:       "LGPL-2.1-only",
		Required: []string{"gnu lesser general public license", "version 2.1"},
	},
	{
		// BSD-3 includes a "neither the name" non-endorsement clause; BSD-2 does not.
		ID:       "BSD-3-Clause",
		Required: []string{"redistribution and use in source and binary forms", "neither the name"},
	},
	{
		ID:      "BSD-2-Clause",
		Required: []string{"redistribution and use in source and binary forms"},
		Exclude:  []string{"neither the name"},
	},
	{
		ID:       "ISC",
		Required: []string{"permission to use, copy, modify, and/or distribute"},
	},
	{
		ID:       "MPL-2.0",
		Required: []string{"mozilla public license, version 2.0"},
	},
}


// headerReadLimit is the maximum number of bytes read from a licence file for
// detection. 1 KiB is sufficient for any standard licence header.
const headerReadLimit = 1024

// LicenseIndexer detects software licences from LICENSE/LICENCE/COPYING files
// and emits project:license nodes.
type LicenseIndexer struct{}

// NewLicenseIndexer creates a new LicenseIndexer.
func NewLicenseIndexer() *LicenseIndexer {
	return &LicenseIndexer{}
}

func (i *LicenseIndexer) Name() string { return "license" }

func (i *LicenseIndexer) Schemes() []string { return []string{"license+file"} }

func (i *LicenseIndexer) Handles(uri string) bool {
	return strings.HasPrefix(uri, "license+file://")
}

func (i *LicenseIndexer) Subscriptions() []indexer.Subscription {
	return []indexer.Subscription{
		// Visit subscriptions — glob patterns cover all variants
		{EventType: indexer.EventEntryVisited, NodeType: types.TypeFile, Pattern: "LICENSE*"},
		{EventType: indexer.EventEntryVisited, NodeType: types.TypeFile, Pattern: "LICENCE*"},
		{EventType: indexer.EventEntryVisited, NodeType: types.TypeFile, Name: "COPYING"},
		// Deletion subscriptions — clean up when file is removed
		{EventType: indexer.EventNodeDeleting, NodeType: types.TypeFile, Pattern: "LICENSE*"},
		{EventType: indexer.EventNodeDeleting, NodeType: types.TypeFile, Pattern: "LICENCE*"},
		{EventType: indexer.EventNodeDeleting, NodeType: types.TypeFile, Name: "COPYING"},
	}
}

// Index is a no-op; this indexer is event-driven only.
func (i *LicenseIndexer) Index(_ context.Context, _ *indexer.Context) error { return nil }

// HandleEvent dispatches to indexLicense or cleanup depending on the event type.
func (i *LicenseIndexer) HandleEvent(ctx context.Context, ictx *indexer.Context, event indexer.Event) error {
	if event.Type == indexer.EventNodeDeleting {
		return i.cleanup(ctx, ictx, event.Path)
	}
	return i.indexLicense(ctx, ictx, event)
}

// indexLicense reads the licence file, detects the SPDX identifier, and emits a
// project:license node connected via has/belongs_to edges.
func (i *LicenseIndexer) indexLicense(ctx context.Context, ictx *indexer.Context, event indexer.Event) error {
	spdxID, confidence := detectLicense(event.Path)

	nodeName := spdxID
	if nodeName == "" {
		nodeName = "unknown"
	}

	licData := types.LicenseData{
		SPDXID:     spdxID,
		Confidence: confidence,
		File:       event.Path,
	}

	licURI := types.LicensePathToURI(event.Path)
	licNode := graph.NewNode(types.TypeLicense).
		WithURI(licURI).
		WithKey(event.Path).
		WithName(nodeName).
		WithData(licData)

	if err := ictx.Emitter.EmitNode(ctx, licNode); err != nil {
		return err
	}

	// Attach to the nearest project:root node, or fall back to fs:dir.
	dir := filepath.Dir(event.Path)
	ownerID := i.findOwnerID(ctx, ictx, dir)

	return indexer.EmitOwnership(ctx, ictx.Emitter, ownerID, licNode.ID)
}

// findOwnerID returns the ID of the project:root node for dir if one exists,
// otherwise falls back to the fs:dir node ID.
func (i *LicenseIndexer) findOwnerID(ctx context.Context, ictx *indexer.Context, dir string) string {
	projectURI := types.ProjectPathToURI(dir)
	projectID := graph.IDFromURI(projectURI)

	projectNode, err := ictx.Graph.Storage().GetNode(ctx, projectID)
	if err == nil && projectNode != nil && projectNode.Type == types.TypeProject {
		return projectID
	}

	// Fallback: attach to the directory node
	return graph.IDFromURI(types.PathToURI(dir))
}

// cleanup deletes the project:license node associated with the given file path.
func (i *LicenseIndexer) cleanup(ctx context.Context, ictx *indexer.Context, path string) error {
	licURI := types.LicensePathToURI(path)
	deleted, err := ictx.Graph.Storage().DeleteByURIPrefix(ctx, licURI)
	if deleted > 0 {
		ictx.AddNodesDeleted(deleted)
	}
	return err
}

// detectLicense reads the first headerReadLimit bytes of a file and matches
// against the SPDX table. Returns the SPDX ID and confidence level.
func detectLicense(path string) (spdxID, confidence string) {
	f, err := os.Open(path)
	if err != nil {
		return "", "unknown"
	}
	defer f.Close()

	buf := make([]byte, headerReadLimit)
	n, _ := f.Read(buf)
	header := strings.ToLower(string(buf[:n]))

	for _, entry := range spdxTable {
		if matchSPDX(header, entry) {
			return entry.ID, "high"
		}
	}
	return "", "unknown"
}

// matchSPDX returns true if all Required fragments are present and no Exclude
// fragments are present in the (already lowercased) header.
func matchSPDX(header string, entry spdxEntry) bool {
	for _, req := range entry.Required {
		if !strings.Contains(header, strings.ToLower(req)) {
			return false
		}
	}
	for _, exc := range entry.Exclude {
		if strings.Contains(header, strings.ToLower(exc)) {
			return false
		}
	}
	return true
}
