package axon

import (
	"context"
	"fmt"

	"github.com/codewandler/axon/graph"
)

// NeighborResult is one entry returned by Neighbors: the connected node together
// with the edge that links it to the origin node.
type NeighborResult struct {
	// Node is the connected graph node.
	Node *graph.Node

	// EdgeType is the type of the edge connecting origin and Node.
	EdgeType string

	// Direction is "in" when the edge points from Node toward the origin,
	// or "out" when it points from the origin toward Node.
	Direction string

	// EdgeID is the internal ID of the connecting edge.
	EdgeID string
}

// NeighborsOptions configures a call to (*Axon).Neighbors.
type NeighborsOptions struct {
	// Direction controls which edges to follow.
	// Accepted values: "in", "out", "both". Default (empty string) is "both".
	Direction string

	// EdgeTypes restricts results to edges whose type matches one of these
	// values. When nil or empty, all edge types are included.
	EdgeTypes []string

	// Max caps the number of results returned. 0 means no limit.
	Max int
}

// Neighbors returns the immediate neighbors of the node identified by uri,
// following edges in the requested direction.
//
// uri is the node URI (e.g. "file:///path/to/file.go" or "go:func:pkg.Func").
// Direction defaults to "both" when opts.Direction is empty.
//
// Results are ordered: outgoing edges first, then incoming, as returned by the
// storage layer (no additional sorting is applied). When opts.Max > 0 the
// result slice is truncated to that length after filtering.
//
// Returns a non-nil error if the node cannot be found or the storage layer
// fails. An isolated node (no matching edges) returns an empty slice without
// an error.
func (a *Axon) Neighbors(ctx context.Context, uri string, opts NeighborsOptions) ([]*NeighborResult, error) {
	// Resolve direction default.
	dir := opts.Direction
	if dir == "" {
		dir = "both"
	}

	// Build an O(1) edge-type filter set; nil means "accept all types".
	var edgeTypeSet map[string]bool
	if len(opts.EdgeTypes) > 0 {
		edgeTypeSet = make(map[string]bool, len(opts.EdgeTypes))
		for _, t := range opts.EdgeTypes {
			edgeTypeSet[t] = true
		}
	}

	// Look up the origin node.
	origin, err := a.storage.GetNodeByURI(ctx, uri)
	if err != nil {
		return nil, fmt.Errorf("neighbors: resolve uri %q: %w", uri, err)
	}

	var results []*NeighborResult

	// Collect outgoing edges (origin → neighbor).
	if dir == "out" || dir == "both" {
		outEdges, err := a.storage.GetEdgesFrom(ctx, origin.ID)
		if err != nil {
			return nil, fmt.Errorf("neighbors: get outgoing edges: %w", err)
		}
		for _, e := range outEdges {
			if edgeTypeSet != nil && !edgeTypeSet[e.Type] {
				continue
			}
			neighbor, err := a.storage.GetNode(ctx, e.To)
			if err != nil {
				// Orphaned edge — skip silently.
				continue
			}
			results = append(results, &NeighborResult{
				Node:      neighbor,
				EdgeType:  e.Type,
				Direction: "out",
				EdgeID:    e.ID,
			})
		}
	}

	// Collect incoming edges (neighbor → origin).
	if dir == "in" || dir == "both" {
		inEdges, err := a.storage.GetEdgesTo(ctx, origin.ID)
		if err != nil {
			return nil, fmt.Errorf("neighbors: get incoming edges: %w", err)
		}
		for _, e := range inEdges {
			if edgeTypeSet != nil && !edgeTypeSet[e.Type] {
				continue
			}
			neighbor, err := a.storage.GetNode(ctx, e.From)
			if err != nil {
				// Orphaned edge — skip silently.
				continue
			}
			results = append(results, &NeighborResult{
				Node:      neighbor,
				EdgeType:  e.Type,
				Direction: "in",
				EdgeID:    e.ID,
			})
		}
	}

	// Apply max limit.
	if opts.Max > 0 && len(results) > opts.Max {
		results = results[:opts.Max]
	}

	return results, nil
}
