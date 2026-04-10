package main

import (
	"context"
	"fmt"
	"path/filepath"
	"sort"
	"strings"

	"github.com/codewandler/axon/adapters/sqlite"
	"github.com/codewandler/axon/aql"
	"github.com/codewandler/axon/graph"
	"github.com/codewandler/axon/render"
	"github.com/codewandler/axon/types"
)

const defaultPageSize = 50

// edgeGroup represents a group of edges with the same type.
type edgeGroup struct {
	EdgeType string
	Count    int
	Nodes    []*graph.Node
	Loaded   bool // whether nodes have been loaded
}

// parentEdges are edge types that represent "upward" relationships (shown in left pane).
var parentEdges = map[string]bool{
	"contained_by": true,
	"belongs_to":   true,
}

// siblingData holds the parent node and its children (siblings of center).
type siblingData struct {
	Parent   *graph.Node   // the parent node (highlighted in left pane)
	Siblings []*graph.Node // all children of parent (siblings including center)
}

// edgeData holds all edge information for a node, split by semantic direction.
type edgeData struct {
	// Parents: edges going "up" (contained_by, belongs_to) — shown in left pane
	Parents []*edgeGroup
	// Children: edges going "down/out" (contains, has, located_at, etc.) — shown in right pane
	Children []*edgeGroup
	// Siblings: parent's children, for the left pane display
	SiblingData *siblingData
}

// historyEntry stores a navigation state for back/forward.
type historyEntry struct {
	Node *graph.Node
}

// nodeFilter represents a user-defined filter on target nodes.
type nodeFilter struct {
	Field    string // e.g. "name", "type", "data.ext"
	Operator string // e.g. "GLOB", "=", "!=", "LIKE", ">", "<"
	Value    string // the filter value
}

// navigator manages the focused node, edge loading, and navigation history.
type navigator struct {
	ctx     context.Context
	storage *sqlite.Storage
	graph   *graph.Graph

	center       *graph.Node
	edges        edgeData
	history      []historyEntry
	depth        int            // zoom level (1 = direct edges)
	filter       *nodeFilter    // optional text filter on target nodes
	categoryExpr aql.Expression // optional category filter expression (pre-built)
}

func newNavigator(ctx context.Context, storage *sqlite.Storage, g *graph.Graph) *navigator {
	return &navigator{
		ctx:     ctx,
		storage: storage,
		graph:   g,
		depth:   1,
	}
}

// SetCenter sets the focused node and loads its edges.
func (n *navigator) SetCenter(node *graph.Node) error {
	n.center = node
	return n.loadEdges()
}

// NavigateTo pushes current state to history and sets a new center.
func (n *navigator) NavigateTo(node *graph.Node) error {
	if n.center != nil {
		n.history = append(n.history, historyEntry{Node: n.center})
	}
	return n.SetCenter(node)
}

// GoBack pops the last history entry and restores it.
func (n *navigator) GoBack() error {
	if len(n.history) == 0 {
		return nil
	}
	last := n.history[len(n.history)-1]
	n.history = n.history[:len(n.history)-1]
	return n.SetCenter(last.Node)
}

// CanGoBack returns whether there's history to go back to.
func (n *navigator) CanGoBack() bool {
	return len(n.history) > 0
}

// Breadcrumbs returns display names from history + current node.
func (n *navigator) Breadcrumbs() []string {
	crumbs := make([]string, 0, len(n.history)+1)
	for _, h := range n.history {
		crumbs = append(crumbs, render.GetDisplayName(h.Node))
	}
	if n.center != nil {
		crumbs = append(crumbs, render.GetDisplayName(n.center))
	}
	return crumbs
}

// ZoomIn increases the depth level.
func (n *navigator) ZoomIn() error {
	n.depth++
	if n.depth > 5 {
		n.depth = 5
	}
	return n.loadEdges()
}

// ZoomOut decreases the depth level.
func (n *navigator) ZoomOut() error {
	if n.depth > 1 {
		n.depth--
		return n.loadEdges()
	}
	return nil
}

// SetFilter sets a text filter and optional category expression, then reloads edges.
func (n *navigator) SetFilter(field, operator, value string, catExpr aql.Expression) error {
	if value != "" {
		n.filter = &nodeFilter{Field: field, Operator: operator, Value: value}
	} else {
		n.filter = nil
	}
	n.categoryExpr = catExpr
	return n.loadEdges()
}

// ClearFilter removes all filters and reloads edges.
func (n *navigator) ClearFilter() error {
	n.filter = nil
	n.categoryExpr = nil
	return n.loadEdges()
}

// LoadMoreNodes loads the next page of nodes for an edge group.
func (n *navigator) LoadMoreNodes(isParent bool, groupIdx int) error {
	var groups []*edgeGroup
	if isParent {
		groups = n.edges.Parents
	} else {
		groups = n.edges.Children
	}
	if groupIdx >= len(groups) {
		return nil
	}

	group := groups[groupIdx]
	return n.loadGroupNodes(group, len(group.Nodes))
}

// loadEdges loads edge type counts and first page of nodes for each group.
// All edges are loaded from the outgoing direction (from_id = center),
// then split semantically: parent edges (contained_by, belongs_to) go left,
// everything else (contains, has, located_at, etc.) goes right.
func (n *navigator) loadEdges() error {
	if n.center == nil {
		return nil
	}

	// Load ALL outgoing edge counts (from_id = center)
	counts, err := n.loadEdgeCounts(n.center.ID)
	if err != nil {
		return err
	}

	// Split into parent vs child groups
	n.edges.Parents = make([]*edgeGroup, 0)
	n.edges.Children = make([]*edgeGroup, 0)

	for edgeType, count := range counts {
		group := &edgeGroup{
			EdgeType: edgeType,
			Count:    count,
		}
		if parentEdges[edgeType] {
			n.edges.Parents = append(n.edges.Parents, group)
		} else {
			n.edges.Children = append(n.edges.Children, group)
		}
	}

	sort.Slice(n.edges.Parents, func(i, j int) bool {
		return n.edges.Parents[i].EdgeType < n.edges.Parents[j].EdgeType
	})
	sort.Slice(n.edges.Children, func(i, j int) bool {
		return n.edges.Children[i].EdgeType < n.edges.Children[j].EdgeType
	})

	// Load first page of nodes for each group
	for _, g := range n.edges.Parents {
		if err := n.loadGroupNodes(g, 0); err != nil {
			return err
		}
	}
	for _, g := range n.edges.Children {
		if err := n.loadGroupNodes(g, 0); err != nil {
			return err
		}
	}

	// Load siblings (parent's children) for left pane
	n.edges.SiblingData = nil
	if err := n.loadSiblings(); err != nil {
		// Non-fatal: left pane just stays empty
		_ = err
	}

	return nil
}

// loadSiblings finds the parent node via contained_by/belongs_to, then loads
// all children of that parent (siblings of center).
func (n *navigator) loadSiblings() error {
	// Find the parent node — first parent group node
	var parentNode *graph.Node
	for _, g := range n.edges.Parents {
		if len(g.Nodes) > 0 {
			parentNode = g.Nodes[0]
			break
		}
	}
	if parentNode == nil {
		return nil // no parent (root node)
	}

	// Load children of the parent using the "contains" edge
	centerN := aql.N("parent").Build()
	targetN := aql.N("sibling").Build()
	pattern := aql.Pat(centerN).
		To(aql.EdgeTypeOf("contains").ToEdgePattern(), targetN).
		Build()

	q := aql.Select(aql.Var("sibling")).
		FromPattern(pattern).
		Where(aql.Var("parent").Field("id").Eq(parentNode.ID)).
		OrderBy(aql.Var("sibling").Field("name")).
		Limit(defaultPageSize).
		Build()

	result, err := n.storage.Query(n.ctx, q)
	if err != nil {
		// Fallback: try "has" edge
		pattern2 := aql.Pat(centerN).
			To(aql.EdgeTypeOf("has").ToEdgePattern(), targetN).
			Build()
		q2 := aql.Select(aql.Var("sibling")).
			FromPattern(pattern2).
			Where(aql.Var("parent").Field("id").Eq(parentNode.ID)).
			OrderBy(aql.Var("sibling").Field("name")).
			Limit(defaultPageSize).
			Build()
		result, err = n.storage.Query(n.ctx, q2)
		if err != nil {
			return err
		}
	}

	n.edges.SiblingData = &siblingData{
		Parent:   parentNode,
		Siblings: result.Nodes,
	}
	return nil
}

// loadEdgeCounts returns edge type → count for all outgoing edges from a node.
func (n *navigator) loadEdgeCounts(nodeID string) (map[string]int, error) {
	q := aql.Edges.Select(aql.Type, aql.Count()).
		Where(aql.FromID.Eq(nodeID)).
		GroupBy(aql.Type).
		Build()

	result, err := n.storage.Query(n.ctx, q)
	if err != nil {
		return nil, err
	}

	m := make(map[string]int, len(result.Counts))
	for _, item := range result.Counts {
		m[item.Name] = item.Count
	}
	return m, nil
}

// loadGroupNodes loads a page of nodes for an edge group using pattern queries.
// All edges are outgoing from center (from_id = center.id).
func (n *navigator) loadGroupNodes(group *edgeGroup, offset int) error {
	edgePattern := aql.EdgeTypeOf(group.EdgeType).ToEdgePattern()

	if n.depth > 1 {
		edgePattern = aql.EdgeTypeOf(group.EdgeType).WithHops(1, n.depth)
	}

	centerNode := aql.N("center").Build()
	targetNode := aql.N("target").Build()

	pattern := aql.Pat(centerNode).To(edgePattern, targetNode).Build()

	// Build WHERE clause: always filter by center ID
	where := n.buildWhereClause()

	q := aql.Select(aql.Var("target")).
		FromPattern(pattern).
		Where(where).
		OrderBy(aql.Var("target").Field("name")).
		Limit(defaultPageSize).
		Offset(offset).
		Build()

	result, err := n.storage.Query(n.ctx, q)
	if err != nil {
		// Fallback: use direct graph API for simple cases
		return n.loadGroupNodesDirect(group)
	}

	if offset == 0 {
		group.Nodes = result.Nodes
	} else {
		group.Nodes = append(group.Nodes, result.Nodes...)
	}
	group.Loaded = true

	return nil
}

// buildWhereClause constructs the WHERE expression with center ID, text filter, and category filter.
func (n *navigator) buildWhereClause() aql.Expression {
	centerExpr := aql.Var("center").Field("id").Eq(n.center.ID)

	var parts []aql.Expression
	parts = append(parts, centerExpr)

	// Text filter
	if n.filter != nil && n.filter.Value != "" {
		if fe := n.buildFilterExpr(); fe != nil {
			parts = append(parts, fe)
		}
	}

	// Category filter
	if n.categoryExpr != nil {
		parts = append(parts, n.categoryExpr)
	}

	if len(parts) == 1 {
		return centerExpr
	}
	return aql.And(parts...)
}

// buildFilterExpr constructs a filter expression for the target node based on user filter.
func (n *navigator) buildFilterExpr() aql.Expression {
	if n.filter == nil || n.filter.Value == "" {
		return nil
	}

	field := n.filter.Field
	op := n.filter.Operator
	val := n.filter.Value

	// Resolve the field: "data.ext" → Var("target").DataField("ext"), else Var("target").Field(field)
	parts := strings.SplitN(field, ".", 2)
	isData := parts[0] == "data" && len(parts) == 2

	switch op {
	case "GLOB":
		if isData {
			return aql.Var("target").DataField(parts[1]).Glob(val)
		}
		return aql.Var("target").Field(field).Glob(val)
	case "LIKE":
		if isData {
			return aql.Var("target").DataField(parts[1]).Like(val)
		}
		return aql.Var("target").Field(field).Like(val)
	case "=":
		if isData {
			return aql.Var("target").DataField(parts[1]).Eq(val)
		}
		return aql.Var("target").Field(field).Eq(val)
	case "!=":
		if isData {
			return aql.Var("target").DataField(parts[1]).Ne(val)
		}
		return aql.Var("target").Field(field).Ne(val)
	case ">":
		if isData {
			return aql.Var("target").DataField(parts[1]).Gt(val)
		}
		return aql.Var("target").Field(field).Gt(val)
	case "<":
		if isData {
			return aql.Var("target").DataField(parts[1]).Lt(val)
		}
		return aql.Var("target").Field(field).Lt(val)
	}
	return nil
}

// loadGroupNodesDirect is a fallback that uses the graph API directly.
func (n *navigator) loadGroupNodesDirect(group *edgeGroup) error {
	edges, err := n.graph.GetEdgesFrom(n.ctx, n.center.ID)
	if err != nil {
		return err
	}

	nodes := make([]*graph.Node, 0)
	count := 0
	for _, e := range edges {
		if e.Type != group.EdgeType {
			continue
		}
		if count >= defaultPageSize {
			break
		}
		node, err := n.graph.GetNode(n.ctx, e.To)
		if err != nil {
			continue
		}
		nodes = append(nodes, node)
		count++
	}

	sort.Slice(nodes, func(i, j int) bool {
		return render.GetDisplayName(nodes[i]) < render.GetDisplayName(nodes[j])
	})

	group.Nodes = nodes
	group.Loaded = true
	return nil
}

// resolveStartNode resolves the starting node from a path/ID argument or CWD.
func resolveStartNode(ctx context.Context, storage *sqlite.Storage, g *graph.Graph, arg string, cwd string) (*graph.Node, error) {
	// If an argument was provided, try it as a path first, then as an ID prefix
	if arg != "" {
		// Try as file path
		uri := types.PathToURI(arg)
		if node, err := storage.GetNodeByURI(ctx, uri); err == nil {
			return node, nil
		}

		// Try as node ID prefix (GLOB query)
		q := aql.Nodes.SelectStar().
			Where(aql.ID.Glob(arg + "*")).
			Limit(1).
			Build()
		result, err := storage.Query(ctx, q)
		if err == nil && len(result.Nodes) == 1 {
			return result.Nodes[0], nil
		}

		// Try as URI directly
		if node, err := storage.GetNodeByURI(ctx, arg); err == nil {
			return node, nil
		}
	}

	// Default: CWD
	uri := types.PathToURI(cwd)
	node, err := storage.GetNodeByURI(ctx, uri)
	if err != nil {
		// CWD not in graph - try to find nearest parent that is
		return findNearestIndexedParent(ctx, storage, cwd)
	}
	return node, nil
}

// findNearestIndexedParent walks up from path to find the nearest indexed directory.
func findNearestIndexedParent(ctx context.Context, storage *sqlite.Storage, path string) (*graph.Node, error) {
	dir := filepath.Dir(path)
	for {
		uri := types.PathToURI(dir)
		if node, err := storage.GetNodeByURI(ctx, uri); err == nil {
			return node, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			// Reached filesystem root
			return nil, fmt.Errorf("no indexed parent found for %s", path)
		}
		dir = parent
	}
}
