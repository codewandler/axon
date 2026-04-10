package main

import (
	"context"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/codewandler/axon/adapters/sqlite"
	"github.com/codewandler/axon/aql"
	"github.com/codewandler/axon/graph"
	"github.com/codewandler/axon/render"
	"github.com/codewandler/axon/types"
)

func TestNavigationBenchmark(t *testing.T) {
	runBenchmark()
}

func TestGlamourColdStart(t *testing.T) {
	// Test the FIRST glamour render (cold start) vs subsequent renders
	md := "# Hello\n\nThis is a test.\n\n```go\nfmt.Println(\"hello\")\n```\n"

	t0 := time.Now()
	r1 := renderGlamour(md, 80)
	d0 := time.Since(t0)
	fmt.Printf("Glamour cold start: %d bytes (%s)\n", len(r1), d0)

	t1 := time.Now()
	r2 := renderGlamour(md, 80)
	d1 := time.Since(t1)
	fmt.Printf("Glamour warm:       %d bytes (%s)\n", len(r2), d1)

	t2 := time.Now()
	r3 := renderGlamour(md, 80)
	d2 := time.Since(t2)
	fmt.Printf("Glamour warm 2:     %d bytes (%s)\n", len(r3), d2)

	// Now test with AGENTS.md-sized content
	bigMD := strings.Repeat("# Section\n\nSome paragraph text here with **bold** and `code`.\n\n```go\nfunc main() {\n\tfmt.Println(\"hello\")\n}\n```\n\n", 50)
	t3 := time.Now()
	r4 := renderGlamour(bigMD, 80)
	d3 := time.Since(t3)
	fmt.Printf("Glamour big (%d bytes in): %d bytes out (%s)\n", len(bigMD), len(r4), d3)
}

func TestFullNavigationTiming(t *testing.T) {
	home, _ := os.UserHomeDir()
	s, err := sqlite.New(home + "/.axon/graph.db")
	if err != nil {
		t.Skip("No database")
	}
	defer s.Close()

	ctx := context.Background()
	registry := graph.NewRegistry()
	types.RegisterCommonEdges(registry)
	types.RegisterFSTypes(registry)
	types.RegisterVCSTypes(registry)
	types.RegisterMarkdownTypes(registry)
	g := graph.New(s, registry)

	nav := newNavigator(ctx, s, g)

	// Start from axon dir
	uri := types.PathToURI("/home/timo/projects/axon")
	cwdNode, err := s.GetNodeByURI(ctx, uri)
	if err != nil {
		t.Skip("axon dir not indexed")
	}

	t0 := time.Now()
	err = nav.SetCenter(cwdNode)
	d0 := time.Since(t0)
	fmt.Printf("SetCenter (axon dir, %d out groups): %s\n", len(nav.edges.Children), d0)

	// Find AGENTS.md in children
	var agentsMD *graph.Node
	for _, g := range nav.edges.Children {
		for _, n := range g.Nodes {
			if render.GetDisplayName(n) == "AGENTS.md" {
				agentsMD = n
				break
			}
		}
	}
	if agentsMD == nil {
		t.Skip("AGENTS.md not in children")
	}

	// Navigate to AGENTS.md - this is the full "click" path
	t1 := time.Now()
	err = nav.NavigateTo(agentsMD)
	d1 := time.Since(t1)
	fmt.Printf("NavigateTo AGENTS.md: %s\n", d1)

	// Now simulate the preview resolve that happens in refreshPreview
	t2 := time.Now()
	preview := resolvePreview(ctx, g, nav.center, 80)
	d2 := time.Since(t2)
	fmt.Printf("resolvePreview: %d bytes (%s)\n", len(preview), d2)

	fmt.Printf("TOTAL click-to-render: %s\n", d1+d2)

	// Navigate to the huge manager.md
	hugeQ := aql.Nodes.SelectStar().
		Where(aql.ID.Eq("WNFlIK0Vx2wRlJfRcSQjPA")).
		Limit(1).Build()
	hugeResult, _ := s.Query(ctx, hugeQ)
	if len(hugeResult.Nodes) > 0 {
		t3 := time.Now()
		err = nav.NavigateTo(hugeResult.Nodes[0])
		d3 := time.Since(t3)
		fmt.Printf("\nNavigateTo manager.md (970KB): %s\n", d3)

		t4 := time.Now()
		preview2 := resolvePreview(ctx, g, nav.center, 80)
		d4 := time.Since(t4)
		fmt.Printf("resolvePreview: %d bytes (%s)\n", len(preview2), d4)

		fmt.Printf("TOTAL click-to-render: %s\n", d3+d4)
	}

	// Test navigating to a directory with MANY children
	fmt.Println("\n--- Navigate to dir with 24k children ---")
	bigQ := aql.Nodes.SelectStar().
		Where(aql.ID.Eq("iNr6HLeUlLg36Dnh_YQLlQ")).
		Limit(1).Build()
	bigResult, _ := s.Query(ctx, bigQ)
	if len(bigResult.Nodes) > 0 {
		t5 := time.Now()
		err = nav.NavigateTo(bigResult.Nodes[0])
		d5 := time.Since(t5)
		fmt.Printf("NavigateTo 24k-child dir: %s\n", d5)
	}
}

func runBenchmark() {
	home, _ := os.UserHomeDir()
	s, err := sqlite.New(home + "/.axon/graph.db")
	if err != nil {
		fmt.Println("Error:", err)
		return
	}
	defer s.Close()

	ctx := context.Background()
	registry := graph.NewRegistry()
	types.RegisterCommonEdges(registry)
	types.RegisterFSTypes(registry)
	types.RegisterVCSTypes(registry)
	types.RegisterMarkdownTypes(registry)
	g := graph.New(s, registry)

	// Find AGENTS.md document node
	q := aql.Nodes.SelectStar().
		Where(aql.URI.Eq("file+md:///home/timo/projects/axon/AGENTS.md")).
		Limit(1).
		Build()
	result, err := s.Query(ctx, q)
	if err != nil || len(result.Nodes) == 0 {
		fmt.Println("Can't find AGENTS.md node:", err)
		return
	}
	node := result.Nodes[0]
	fmt.Printf("Target: %s (%s) ID=%s\n\n", render.GetDisplayName(node), node.Type, node.ID)

	// === Step 1: Edge count query ===
	t0 := time.Now()
	eq := aql.Edges.Select(aql.Type, aql.Count()).
		Where(aql.FromID.Eq(node.ID)).
		GroupBy(aql.Type).
		Build()
	edgeResult, err := s.Query(ctx, eq)
	d1 := time.Since(t0)
	if err != nil {
		fmt.Println("Edge count error:", err)
		return
	}
	fmt.Printf("Step 1 - Edge counts: %v (%s)\n", edgeResult.Counts, d1)

	// === Step 2: Load nodes for each edge group ===
	for _, ecItem := range edgeResult.Counts {
		edgeType := ecItem.Name
		count := ecItem.Count
		t2 := time.Now()
		centerN := aql.N("center").Build()
		targetN := aql.N("target").Build()
		pattern := aql.Pat(centerN).To(aql.EdgeTypeOf(edgeType).ToEdgePattern(), targetN).Build()
		nq := aql.Select(aql.Var("target")).
			FromPattern(pattern).
			Where(aql.Var("center").Field("id").Eq(node.ID)).
			OrderBy(aql.Var("target").Field("name")).
			Limit(50).
			Build()
		nodesResult, err := s.Query(ctx, nq)
		d2 := time.Since(t2)
		if err != nil {
			fmt.Printf("Step 2 - Load %s nodes: ERROR %v (%s)\n", edgeType, err, d2)
			// Try fallback
			t2f := time.Now()
			edges, err2 := g.GetEdgesFrom(ctx, node.ID)
			d2f := time.Since(t2f)
			if err2 != nil {
				fmt.Printf("Step 2 - Fallback GetEdgesFrom: ERROR %v (%s)\n", err2, d2f)
				continue
			}
			matchCount := 0
			for _, e := range edges {
				if e.Type == edgeType {
					matchCount++
				}
			}
			fmt.Printf("Step 2 - Fallback GetEdgesFrom: %d edges of type %s (%s)\n", matchCount, edgeType, d2f)
			continue
		}
		fmt.Printf("Step 2 - Load %s: %d/%d nodes (%s)\n", edgeType, len(nodesResult.Nodes), count, d2)
	}

	// === Step 3: Preview generation ===
	t3 := time.Now()
	hasPrev := hasPreviewContent(node)
	d3a := time.Since(t3)
	fmt.Printf("\nStep 3a - hasPreviewContent: %v (%s)\n", hasPrev, d3a)

	if hasPrev {
		t3b := time.Now()
		preview := resolvePreview(ctx, g, node, 80)
		d3b := time.Since(t3b)
		lines := 0
		for _, c := range preview {
			if c == '\n' {
				lines++
			}
		}
		fmt.Printf("Step 3b - resolvePreview: %d lines, %d bytes (%s)\n", lines, len(preview), d3b)
	}

	// === Step 4: Test fs:file .md node (the real TUI path) ===
	fmt.Println("\n--- fs:file .md node (what TUI actually navigates to) ---")
	fq := aql.Nodes.SelectStar().
		Where(aql.URI.Eq("file:///home/timo/projects/axon/AGENTS.md")).
		Limit(1).
		Build()
	fResult, _ := s.Query(ctx, fq)
	if len(fResult.Nodes) > 0 {
		fnode := fResult.Nodes[0]
		fmt.Printf("File node: %s (%s) ID=%s\n", render.GetDisplayName(fnode), fnode.Type, fnode.ID)

		// Edge counts
		tf1 := time.Now()
		feq := aql.Edges.Select(aql.Type, aql.Count()).
			Where(aql.FromID.Eq(fnode.ID)).
			GroupBy(aql.Type).
			Build()
		fEdgeResult, _ := s.Query(ctx, feq)
		df1 := time.Since(tf1)
		fmt.Printf("File edge counts: %v (%s)\n", fEdgeResult.Counts, df1)

		// Load each edge group
		for _, fcItem := range fEdgeResult.Counts {
			edgeType := fcItem.Name
			count := fcItem.Count
			tf2 := time.Now()
			centerN := aql.N("center").Build()
			targetN := aql.N("target").Build()
			pattern := aql.Pat(centerN).To(aql.EdgeTypeOf(edgeType).ToEdgePattern(), targetN).Build()
			nq := aql.Select(aql.Var("target")).
				FromPattern(pattern).
				Where(aql.Var("center").Field("id").Eq(fnode.ID)).
				OrderBy(aql.Var("target").Field("name")).
				Limit(50).
				Build()
			nodesResult, err := s.Query(ctx, nq)
			df2 := time.Since(tf2)
			if err != nil {
				fmt.Printf("File load %s: ERROR %v (%s)\n", edgeType, err, df2)

				// Try fallback
				tf2f := time.Now()
				edges, _ := g.GetEdgesFrom(ctx, fnode.ID)
				df2f := time.Since(tf2f)
				mc := 0
				for _, e := range edges {
					if e.Type == edgeType {
						mc++
					}
				}
				fmt.Printf("File fallback %s: %d edges (%s)\n", edgeType, mc, df2f)
				continue
			}
			fmt.Printf("File load %s: %d/%d (%s)\n", edgeType, len(nodesResult.Nodes), count, df2)
		}

		// Preview
		tf3 := time.Now()
		fHasPrev := hasPreviewContent(fnode)
		df3a := time.Since(tf3)
		fmt.Printf("File hasPreviewContent: %v (%s)\n", fHasPrev, df3a)

		if fHasPrev {
			tf3b := time.Now()
			fPreview := resolvePreview(ctx, g, fnode, 80)
			df3b := time.Since(tf3b)
			fLines := 0
			for _, c := range fPreview {
				if c == '\n' {
					fLines++
				}
			}
			fmt.Printf("File resolvePreview: %d lines, %d bytes (%s)\n", fLines, len(fPreview), df3b)
		}
	}

	// === Step 5: Test a HUGE markdown file ===
	fmt.Println("\n--- Testing huge .md file (970KB manager.md) ---")
	hq := aql.Nodes.SelectStar().
		Where(aql.ID.Eq("WNFlIK0Vx2wRlJfRcSQjPA")).
		Limit(1).
		Build()
	hResult, _ := s.Query(ctx, hq)
	if len(hResult.Nodes) > 0 {
		hnode := hResult.Nodes[0]
		fmt.Printf("Huge node: %s (%s)\n", render.GetDisplayName(hnode), hnode.Type)

		// Edge counts
		th1 := time.Now()
		heq := aql.Edges.Select(aql.Type, aql.Count()).
			Where(aql.FromID.Eq(hnode.ID)).
			GroupBy(aql.Type).
			Build()
		hEdgeResult, _ := s.Query(ctx, heq)
		dh1 := time.Since(th1)
		fmt.Printf("Huge edge counts: %v (%s)\n", hEdgeResult.Counts, dh1)

		// Load each group
		for _, hcItem := range hEdgeResult.Counts {
			edgeType := hcItem.Name
			count := hcItem.Count
			th2 := time.Now()
			centerN := aql.N("center").Build()
			targetN := aql.N("target").Build()
			pattern := aql.Pat(centerN).To(aql.EdgeTypeOf(edgeType).ToEdgePattern(), targetN).Build()
			nq := aql.Select(aql.Var("target")).
				FromPattern(pattern).
				Where(aql.Var("center").Field("id").Eq(hnode.ID)).
				OrderBy(aql.Var("target").Field("name")).
				Limit(50).
				Build()
			nodesResult, err := s.Query(ctx, nq)
			dh2 := time.Since(th2)
			if err != nil {
				fmt.Printf("Huge load %s: ERROR %v (%s)\n", edgeType, err, dh2)
			} else {
				fmt.Printf("Huge load %s: %d/%d (%s)\n", edgeType, len(nodesResult.Nodes), count, dh2)
			}
		}

		// Preview
		th3 := time.Now()
		hPreview := resolvePreview(ctx, g, hnode, 80)
		dh3 := time.Since(th3)
		hLines := 0
		for _, c := range hPreview {
			if c == '\n' {
				hLines++
			}
		}
		fmt.Printf("Huge resolvePreview: %d lines, %d bytes (%s)\n", hLines, len(hPreview), dh3)

		// Break down: read vs render
		th4 := time.Now()
		rawContent := readFilePreview(hnode.URI)
		dh4 := time.Since(th4)
		fmt.Printf("Huge readFilePreview: %d bytes (%s)\n", len(rawContent), dh4)

		th5 := time.Now()
		rendered := renderGlamour(rawContent, 80)
		dh5 := time.Since(th5)
		fmt.Printf("Huge renderGlamour: %d bytes (%s)\n", len(rendered), dh5)
	}

	// === Step 6: Also test a markdown section ===
	fmt.Println("\n--- Now testing a section node ---")
	sq := aql.Nodes.SelectStar().
		Where(aql.And(
			aql.Type.Eq("md:section"),
			aql.URI.Glob("*axon/AGENTS.md*"),
		)).
		Limit(1).
		Build()
	secResult, err := s.Query(ctx, sq)
	if err != nil || len(secResult.Nodes) == 0 {
		fmt.Println("Can't find section node:", err)
		return
	}
	sec := secResult.Nodes[0]
	fmt.Printf("Section: %s (%s) ID=%s\n", render.GetDisplayName(sec), sec.Type, sec.ID)

	// Edge counts for section
	t4 := time.Now()
	seq := aql.Edges.Select(aql.Type, aql.Count()).
		Where(aql.FromID.Eq(sec.ID)).
		GroupBy(aql.Type).
		Build()
	secEdgeResult, err := s.Query(ctx, seq)
	d4 := time.Since(t4)
	fmt.Printf("Section edge counts: %v (%s)\n", secEdgeResult.Counts, d4)

	// Load section children
	for _, scItem := range secEdgeResult.Counts {
		edgeType := scItem.Name
		count := scItem.Count
		t5 := time.Now()
		centerN := aql.N("center").Build()
		targetN := aql.N("target").Build()
		pattern := aql.Pat(centerN).To(aql.EdgeTypeOf(edgeType).ToEdgePattern(), targetN).Build()
		nq := aql.Select(aql.Var("target")).
			FromPattern(pattern).
			Where(aql.Var("center").Field("id").Eq(sec.ID)).
			OrderBy(aql.Var("target").Field("name")).
			Limit(50).
			Build()
		nodesResult, err := s.Query(ctx, nq)
		d5 := time.Since(t5)
		if err != nil {
			fmt.Printf("Section load %s: ERROR %v (%s)\n", edgeType, err, d5)
			continue
		}
		fmt.Printf("Section load %s: %d/%d (%s)\n", edgeType, len(nodesResult.Nodes), count, d5)
	}

	// Section preview
	t6 := time.Now()
	secPreview := resolvePreview(ctx, g, sec, 80)
	d6 := time.Since(t6)
	secLines := 0
	for _, c := range secPreview {
		if c == '\n' {
			secLines++
		}
	}
	fmt.Printf("Section preview: %d lines, %d bytes (%s)\n", secLines, len(secPreview), d6)
}
