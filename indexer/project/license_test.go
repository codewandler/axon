package project

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/codewandler/axon/graph"
	"github.com/codewandler/axon/indexer"
	"github.com/codewandler/axon/types"
)

// MIT licence header fragment
const mitLicense = `MIT License

Copyright (c) 2024 Example Author

Permission is hereby granted, free of charge, to any person obtaining a copy
of this software and associated documentation files (the "Software"), to deal
in the Software without restriction, including without limitation the rights
to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
copies of the Software, and to permit persons to whom the Software is
furnished to do so, subject to the following conditions:
`

// Apache-2.0 licence header fragment
const apacheLicense = `                                 Apache License
                           Version 2.0, January 2004
                        http://www.apache.org/licenses/

TERMS AND CONDITIONS FOR USE, REPRODUCTION, AND DISTRIBUTION

Apache License, Version 2.0
`

// GPL-3.0-only header
const gpl3License = `                    GNU GENERAL PUBLIC LICENSE
                       Version 3, 29 June 2007

 Copyright (C) 2007 Free Software Foundation, Inc.
`

// GPL-2.0-only header
const gpl2License = `                    GNU GENERAL PUBLIC LICENSE
                       Version 2, June 1991

 Copyright (C) 1989, 1991 Free Software Foundation, Inc.
`

// LGPL-2.1-only header
const lgpl21License = `                  GNU LESSER GENERAL PUBLIC LICENSE
                       Version 2.1, February 1999

 Copyright (C) 1991, 1999 Free Software Foundation, Inc.
`

// BSD-3-Clause header
const bsd3License = `Copyright (c) 2024 Example Author. All rights reserved.

Redistribution and use in source and binary forms, with or without
modification, are permitted provided that the following conditions are met:

1. Redistributions of source code must retain the above copyright notice, this
   list of conditions and the following disclaimer.

2. Neither the name of the copyright holder nor the names of its contributors
   may be used to endorse or promote products derived from this software
   without specific prior written permission.
`

// BSD-2-Clause header (no "neither the name" clause)
const bsd2License = `Copyright (c) 2024 Example Author. All rights reserved.

Redistribution and use in source and binary forms, with or without
modification, are permitted provided that the following conditions are met:

1. Redistributions of source code must retain the above copyright notice, this
   list of conditions and the following disclaimer.

2. Redistributions in binary form must reproduce the above copyright notice,
   this list of conditions and the following disclaimer in the documentation
   and/or other materials provided with the distribution.
`

// ISC header
const iscLicense = `ISC License

Copyright (c) 2024 Example Author

Permission to use, copy, modify, and/or distribute this software for any
purpose with or without fee is hereby granted, provided that the above
copyright notice and this permission notice appear in all copies.
`

// MPL-2.0 header
const mpl2License = `Mozilla Public License, Version 2.0

1. Definitions
...
`

// Unknown licence
const unknownLicense = `This software is proprietary and confidential.
All rights reserved. Unauthorized copying or distribution is prohibited.
`

func setupLicenseGraph(t *testing.T) *graph.Graph {
	t.Helper()
	r := graph.NewRegistry()
	types.RegisterCommonEdges(r)
	types.RegisterFSTypes(r)
	types.RegisterProjectTypes(r)
	s, err := newTestStorage(t)
	if err != nil {
		t.Fatalf("storage setup failed: %v", err)
	}
	return graph.New(s, r)
}

func newTestStorage(t *testing.T) (graph.Storage, error) {
	t.Helper()
	// Use setupGraph's sqlite helper
	g := setupGraph(t)
	return g.Storage(), nil
}

// setupLicenseDir creates a temp directory with dir + optional project:root node.
func setupLicenseDir(t *testing.T, ctx context.Context, g *graph.Graph, emitter indexer.Emitter, withProject bool) string {
	t.Helper()
	dir := t.TempDir()

	dirURI := types.PathToURI(dir)
	dirNode := graph.NewNode(types.TypeDir).
		WithURI(dirURI).
		WithKey(dir).
		WithName(filepath.Base(dir))
	if err := emitter.EmitNode(ctx, dirNode); err != nil {
		t.Fatalf("failed to emit dir node: %v", err)
	}

	if withProject {
		projectData := types.ProjectData{Language: types.LangGo, Name: "example.com/mymod"}
		projectNode := graph.NewNode(types.TypeProject).
			WithURI(types.ProjectPathToURI(dir)).
			WithKey(dir).
			WithName("example.com/mymod").
			WithData(projectData)
		if err := emitter.EmitNode(ctx, projectNode); err != nil {
			t.Fatalf("failed to emit project node: %v", err)
		}
	}

	if err := g.Storage().Flush(ctx); err != nil {
		t.Fatalf("Flush failed: %v", err)
	}
	return dir
}

// indexLicenseFile creates a LICENSE file and triggers the indexer.
func indexLicenseFile(t *testing.T, ctx context.Context, g *graph.Graph, emitter indexer.Emitter, dir, filename, content string) {
	t.Helper()
	path := filepath.Join(dir, filename)
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatalf("failed to write %s: %v", filename, err)
	}

	fileNode := graph.NewNode(types.TypeFile).
		WithURI(types.PathToURI(path)).
		WithKey(path).
		WithName(filename)
	if err := emitter.EmitNode(ctx, fileNode); err != nil {
		t.Fatalf("failed to emit file node: %v", err)
	}
	if err := g.Storage().Flush(ctx); err != nil {
		t.Fatalf("Flush failed: %v", err)
	}

	idx := NewLicenseIndexer()
	ictx := &indexer.Context{
		Root:       types.PathToURI(dir),
		Generation: "gen-1",
		Graph:      g,
		Emitter:    emitter,
	}
	event := indexer.Event{
		Type:     indexer.EventEntryVisited,
		URI:      types.PathToURI(path),
		Path:     path,
		Name:     filename,
		NodeType: types.TypeFile,
		NodeID:   fileNode.ID,
		Node:     fileNode,
	}
	if err := idx.HandleEvent(ctx, ictx, event); err != nil {
		t.Fatalf("HandleEvent failed: %v", err)
	}
	if err := g.Storage().Flush(ctx); err != nil {
		t.Fatalf("Flush failed: %v", err)
	}
}

// getLicenseNodes returns all project:license nodes in the graph.
func getLicenseNodes(t *testing.T, ctx context.Context, g *graph.Graph) []*graph.Node {
	t.Helper()
	nodes, err := g.FindNodes(ctx, graph.NodeFilter{Type: types.TypeLicense}, graph.QueryOptions{})
	if err != nil {
		t.Fatalf("FindNodes failed: %v", err)
	}
	return nodes
}

func assertLicenseNode(t *testing.T, nodes []*graph.Node, wantSPDX, wantConfidence string) *graph.Node {
	t.Helper()
	if len(nodes) != 1 {
		t.Fatalf("expected 1 license node, got %d", len(nodes))
	}
	n := nodes[0]
	data, ok := n.Data.(map[string]any)
	if !ok {
		t.Fatalf("expected license data as map, got %T", n.Data)
	}
	gotSPDX, _ := data["spdx_id"].(string)
	gotConf, _ := data["confidence"].(string)
	if gotSPDX != wantSPDX {
		t.Errorf("spdx_id: want %q, got %q", wantSPDX, gotSPDX)
	}
	if gotConf != wantConfidence {
		t.Errorf("confidence: want %q, got %q", wantConfidence, gotConf)
	}
	return n
}

// --- SPDX detection tests ---

func TestLicenseIndexer_MIT(t *testing.T) {
	ctx := context.Background()
	g := setupLicenseGraph(t)
	emitter := indexer.NewGraphEmitter(g, "gen-1")
	dir := setupLicenseDir(t, ctx, g, emitter, false)
	indexLicenseFile(t, ctx, g, emitter, dir, "LICENSE", mitLicense)
	assertLicenseNode(t, getLicenseNodes(t, ctx, g), "MIT", "high")
}

func TestLicenseIndexer_Apache(t *testing.T) {
	ctx := context.Background()
	g := setupLicenseGraph(t)
	emitter := indexer.NewGraphEmitter(g, "gen-1")
	dir := setupLicenseDir(t, ctx, g, emitter, false)
	indexLicenseFile(t, ctx, g, emitter, dir, "LICENSE", apacheLicense)
	assertLicenseNode(t, getLicenseNodes(t, ctx, g), "Apache-2.0", "high")
}

func TestLicenseIndexer_GPL3(t *testing.T) {
	ctx := context.Background()
	g := setupLicenseGraph(t)
	emitter := indexer.NewGraphEmitter(g, "gen-1")
	dir := setupLicenseDir(t, ctx, g, emitter, false)
	indexLicenseFile(t, ctx, g, emitter, dir, "LICENSE", gpl3License)
	assertLicenseNode(t, getLicenseNodes(t, ctx, g), "GPL-3.0-only", "high")
}

func TestLicenseIndexer_GPL2(t *testing.T) {
	ctx := context.Background()
	g := setupLicenseGraph(t)
	emitter := indexer.NewGraphEmitter(g, "gen-1")
	dir := setupLicenseDir(t, ctx, g, emitter, false)
	indexLicenseFile(t, ctx, g, emitter, dir, "LICENSE", gpl2License)
	assertLicenseNode(t, getLicenseNodes(t, ctx, g), "GPL-2.0-only", "high")
}

func TestLicenseIndexer_LGPL21(t *testing.T) {
	ctx := context.Background()
	g := setupLicenseGraph(t)
	emitter := indexer.NewGraphEmitter(g, "gen-1")
	dir := setupLicenseDir(t, ctx, g, emitter, false)
	indexLicenseFile(t, ctx, g, emitter, dir, "COPYING", lgpl21License)
	assertLicenseNode(t, getLicenseNodes(t, ctx, g), "LGPL-2.1-only", "high")
}

func TestLicenseIndexer_BSD3(t *testing.T) {
	ctx := context.Background()
	g := setupLicenseGraph(t)
	emitter := indexer.NewGraphEmitter(g, "gen-1")
	dir := setupLicenseDir(t, ctx, g, emitter, false)
	indexLicenseFile(t, ctx, g, emitter, dir, "LICENSE", bsd3License)
	assertLicenseNode(t, getLicenseNodes(t, ctx, g), "BSD-3-Clause", "high")
}

func TestLicenseIndexer_BSD2(t *testing.T) {
	ctx := context.Background()
	g := setupLicenseGraph(t)
	emitter := indexer.NewGraphEmitter(g, "gen-1")
	dir := setupLicenseDir(t, ctx, g, emitter, false)
	indexLicenseFile(t, ctx, g, emitter, dir, "LICENSE", bsd2License)
	assertLicenseNode(t, getLicenseNodes(t, ctx, g), "BSD-2-Clause", "high")
}

func TestLicenseIndexer_ISC(t *testing.T) {
	ctx := context.Background()
	g := setupLicenseGraph(t)
	emitter := indexer.NewGraphEmitter(g, "gen-1")
	dir := setupLicenseDir(t, ctx, g, emitter, false)
	indexLicenseFile(t, ctx, g, emitter, dir, "LICENSE.txt", iscLicense)
	assertLicenseNode(t, getLicenseNodes(t, ctx, g), "ISC", "high")
}

func TestLicenseIndexer_MPL2(t *testing.T) {
	ctx := context.Background()
	g := setupLicenseGraph(t)
	emitter := indexer.NewGraphEmitter(g, "gen-1")
	dir := setupLicenseDir(t, ctx, g, emitter, false)
	indexLicenseFile(t, ctx, g, emitter, dir, "LICENSE.md", mpl2License)
	assertLicenseNode(t, getLicenseNodes(t, ctx, g), "MPL-2.0", "high")
}

func TestLicenseIndexer_Unknown(t *testing.T) {
	ctx := context.Background()
	g := setupLicenseGraph(t)
	emitter := indexer.NewGraphEmitter(g, "gen-1")
	dir := setupLicenseDir(t, ctx, g, emitter, false)
	indexLicenseFile(t, ctx, g, emitter, dir, "LICENCE", unknownLicense)
	assertLicenseNode(t, getLicenseNodes(t, ctx, g), "", "unknown")
}

// TestLicenseIndexer_LICENCE_variant checks the British spelling variant is handled.
func TestLicenseIndexer_LICENCEVariant(t *testing.T) {
	ctx := context.Background()
	g := setupLicenseGraph(t)
	emitter := indexer.NewGraphEmitter(g, "gen-1")
	dir := setupLicenseDir(t, ctx, g, emitter, false)
	indexLicenseFile(t, ctx, g, emitter, dir, "LICENCE.txt", mitLicense)
	assertLicenseNode(t, getLicenseNodes(t, ctx, g), "MIT", "high")
}

// TestLicenseIndexer_EdgeToProjectRoot checks has edge when project:root exists.
func TestLicenseIndexer_EdgeToProjectRoot(t *testing.T) {
	ctx := context.Background()
	g := setupLicenseGraph(t)
	emitter := indexer.NewGraphEmitter(g, "gen-1")
	dir := setupLicenseDir(t, ctx, g, emitter, true) // create project:root
	indexLicenseFile(t, ctx, g, emitter, dir, "LICENSE", mitLicense)

	nodes := getLicenseNodes(t, ctx, g)
	if len(nodes) != 1 {
		t.Fatalf("expected 1 license node, got %d", len(nodes))
	}
	licNode := nodes[0]

	// The license node should have a belongs_to edge pointing to project:root
	edges, err := g.GetEdgesFrom(ctx, licNode.ID)
	if err != nil {
		t.Fatalf("GetEdgesFrom failed: %v", err)
	}

	expectedOwnerID := graph.IDFromURI(types.ProjectPathToURI(dir))
	var foundBelongsTo bool
	for _, e := range edges {
		if e.Type == types.EdgeBelongsTo && e.To == expectedOwnerID {
			foundBelongsTo = true
		}
	}
	if !foundBelongsTo {
		t.Errorf("expected belongs_to edge from license node to project:root node (ID=%s)", expectedOwnerID)
	}

	// project:root should have a has edge to license
	projEdges, err := g.GetEdgesFrom(ctx, expectedOwnerID)
	if err != nil {
		t.Fatalf("GetEdgesFrom project root failed: %v", err)
	}
	var foundHas bool
	for _, e := range projEdges {
		if e.Type == types.EdgeHas && e.To == licNode.ID {
			foundHas = true
		}
	}
	if !foundHas {
		t.Errorf("expected has edge from project:root to license node")
	}
}

// TestLicenseIndexer_EdgeToDir checks fallback has edge to fs:dir when no project:root.
func TestLicenseIndexer_EdgeToDir(t *testing.T) {
	ctx := context.Background()
	g := setupLicenseGraph(t)
	emitter := indexer.NewGraphEmitter(g, "gen-1")
	dir := setupLicenseDir(t, ctx, g, emitter, false) // no project:root
	indexLicenseFile(t, ctx, g, emitter, dir, "LICENSE", mitLicense)

	nodes := getLicenseNodes(t, ctx, g)
	if len(nodes) != 1 {
		t.Fatalf("expected 1 license node, got %d", len(nodes))
	}
	licNode := nodes[0]

	expectedDirID := graph.IDFromURI(types.PathToURI(dir))
	edges, err := g.GetEdgesFrom(ctx, licNode.ID)
	if err != nil {
		t.Fatalf("GetEdgesFrom failed: %v", err)
	}

	var foundBelongsTo bool
	for _, e := range edges {
		if e.Type == types.EdgeBelongsTo && e.To == expectedDirID {
			foundBelongsTo = true
		}
	}
	if !foundBelongsTo {
		t.Errorf("expected belongs_to edge from license node to fs:dir node (ID=%s)", expectedDirID)
	}
}

// TestLicenseIndexer_Cleanup verifies the license node is deleted when the file is removed.
func TestLicenseIndexer_Cleanup(t *testing.T) {
	ctx := context.Background()
	g := setupLicenseGraph(t)
	emitter := indexer.NewGraphEmitter(g, "gen-1")
	dir := setupLicenseDir(t, ctx, g, emitter, false)

	// Create and index
	indexLicenseFile(t, ctx, g, emitter, dir, "LICENSE", mitLicense)
	if len(getLicenseNodes(t, ctx, g)) != 1 {
		t.Fatal("expected 1 license node after indexing")
	}

	// Trigger deletion
	path := filepath.Join(dir, "LICENSE")
	idx := NewLicenseIndexer()
	ictx := &indexer.Context{
		Root:       types.PathToURI(dir),
		Generation: "gen-1",
		Graph:      g,
		Emitter:    emitter,
	}
	deleteEvent := indexer.Event{
		Type:     indexer.EventNodeDeleting,
		URI:      types.PathToURI(path),
		Path:     path,
		Name:     "LICENSE",
		NodeType: types.TypeFile,
	}
	if err := idx.HandleEvent(ctx, ictx, deleteEvent); err != nil {
		t.Fatalf("HandleEvent (delete) failed: %v", err)
	}
	if err := g.Storage().Flush(ctx); err != nil {
		t.Fatalf("Flush failed: %v", err)
	}

	if len(getLicenseNodes(t, ctx, g)) != 0 {
		t.Error("expected 0 license nodes after cleanup")
	}
}

// TestLicenseIndexer_Meta checks Name/Schemes/Handles/Subscriptions.
func TestLicenseIndexer_Meta(t *testing.T) {
	idx := NewLicenseIndexer()

	if idx.Name() != "license" {
		t.Errorf("Name: want 'license', got %q", idx.Name())
	}
	schemes := idx.Schemes()
	if len(schemes) != 1 || schemes[0] != "license+file" {
		t.Errorf("Schemes: want [license+file], got %v", schemes)
	}
	if !idx.Handles("license+file:///path/LICENSE") {
		t.Error("should handle license+file:// URIs")
	}
	if idx.Handles("file:///path/LICENSE") {
		t.Error("should not handle file:// URIs")
	}

	subs := idx.Subscriptions()
	if len(subs) == 0 {
		t.Error("Subscriptions should not be empty")
	}

	// Verify COPYING is covered (exact Name subscription)
	var hasCOPYING bool
	for _, s := range subs {
		if s.Name == "COPYING" {
			hasCOPYING = true
		}
	}
	if !hasCOPYING {
		t.Error("expected subscription for COPYING")
	}
}

// TestLicenseIndexer_NodeName checks that the node Name is set to the SPDX ID.
func TestLicenseIndexer_NodeName(t *testing.T) {
	ctx := context.Background()
	g := setupLicenseGraph(t)
	emitter := indexer.NewGraphEmitter(g, "gen-1")
	dir := setupLicenseDir(t, ctx, g, emitter, false)
	indexLicenseFile(t, ctx, g, emitter, dir, "LICENSE", mitLicense)

	nodes := getLicenseNodes(t, ctx, g)
	if len(nodes) != 1 {
		t.Fatalf("expected 1 license node, got %d", len(nodes))
	}
	if nodes[0].Name != "MIT" {
		t.Errorf("node Name: want 'MIT', got %q", nodes[0].Name)
	}
}

// TestLicenseIndexer_FileFieldStored checks that the file path is stored in data.
func TestLicenseIndexer_FileFieldStored(t *testing.T) {
	ctx := context.Background()
	g := setupLicenseGraph(t)
	emitter := indexer.NewGraphEmitter(g, "gen-1")
	dir := setupLicenseDir(t, ctx, g, emitter, false)
	indexLicenseFile(t, ctx, g, emitter, dir, "LICENSE", mitLicense)

	nodes := getLicenseNodes(t, ctx, g)
	if len(nodes) != 1 {
		t.Fatalf("expected 1 license node, got %d", len(nodes))
	}
	data, ok := nodes[0].Data.(map[string]any)
	if !ok {
		t.Fatalf("expected map data, got %T", nodes[0].Data)
	}
	file, _ := data["file"].(string)
	expectedPath := filepath.Join(dir, "LICENSE")
	if file != expectedPath {
		t.Errorf("file field: want %q, got %q", expectedPath, file)
	}
}
