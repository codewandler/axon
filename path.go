package axon

import (
	"context"
	"fmt"

	"github.com/codewandler/axon/graph"
)

// PathStep is one node in a path through the knowledge graph, annotated with
// the edge that was followed to arrive at it.
type PathStep struct {
	// Node is the graph node at this position in the path.
	Node *graph.Node

	// EdgeType is the type of the edge that connects the previous node to this
	// one. Empty string for the first step (the origin node).
	EdgeType string

	// Incoming is true when this step was reached by following an incoming
	// edge in reverse (i.e. the underlying edge points FROM this node TOWARD
	// the previous node). False for outgoing edges and for the origin step.
	Incoming bool
}

// Path is an ordered sequence of PathSteps representing a route through the
// knowledge graph from one node to another.
type Path struct {
	Steps []PathStep
}

// Length returns the number of edges in this path (one less than the number
// of steps). Returns 0 for a degenerate single-node path.
func (p *Path) Length() int {
	if len(p.Steps) == 0 {
		return 0
	}
	return len(p.Steps) - 1
}

// PathOptions configures a call to (*Axon).FindPath.
type PathOptions struct {
	// MaxDepth caps the BFS search depth in number of edges. Default: 6.
	MaxDepth int

	// MaxPaths limits the number of distinct paths returned. Default: 3.
	MaxPaths int

	// EdgeTypes restricts traversal to edges of these types only.
	// When nil or empty, all edge types are traversed.
	EdgeTypes []string
}

// pathState is an in-flight BFS entry that tracks the current node, the path
// accumulated so far, and the set of node IDs already visited on *this branch*
// (to prevent cycles without globally marking nodes, which would prevent
// finding multiple distinct paths through shared intermediate nodes).
type pathState struct {
	nodeID string
	steps  []PathStep
	inPath map[string]bool
}

// findPaths is the core bidirectional-BFS engine used by (*Axon).FindPath.
//
// It traverses the graph in both directions (outgoing and incoming edges) from
// fromID, collecting up to maxPaths shortest paths to toID within maxDepth
// edges. Paths are returned in discovery order (shortest first due to BFS).
func findPaths(
	ctx context.Context,
	storage graph.Storage,
	fromID, toID string,
	maxDepth, maxPaths int,
	edgeTypes map[string]bool,
) ([]*Path, error) {
	// Degenerate case: from and to are the same node.
	if fromID == toID {
		return nil, nil
	}

	// Load the origin node.
	originNode, err := storage.GetNode(ctx, fromID)
	if err != nil {
		return nil, fmt.Errorf("load origin node %s: %w", fromID, err)
	}

	// BFS queue seeded with the origin.
	queue := []pathState{
		{
			nodeID: fromID,
			steps:  []PathStep{{Node: originNode}},
			inPath: map[string]bool{fromID: true},
		},
	}

	var results []*Path

	for len(queue) > 0 && len(results) < maxPaths {
		// Dequeue front (FIFO = BFS).
		cur := queue[0]
		queue = queue[1:]

		// Depth guard: steps includes the origin, so edges = len-1.
		if len(cur.steps)-1 >= maxDepth {
			continue
		}

		// Expand outgoing edges (current → neighbor).
		outEdges, err := storage.GetEdgesFrom(ctx, cur.nodeID)
		if err != nil {
			return nil, fmt.Errorf("get edges from %s: %w", cur.nodeID, err)
		}
		for _, e := range outEdges {
			if len(edgeTypes) > 0 && !edgeTypes[e.Type] {
				continue
			}
			neighborID := e.To
			if cur.inPath[neighborID] {
				continue // cycle — skip
			}
			neighborNode, err := storage.GetNode(ctx, neighborID)
			if err != nil {
				continue // orphaned edge — skip
			}

			step := PathStep{Node: neighborNode, EdgeType: e.Type, Incoming: false}

			if neighborID == toID {
				results = append(results, appendPath(cur.steps, step))
				if len(results) >= maxPaths {
					return results, nil
				}
				continue
			}

			newInPath := cloneInPath(cur.inPath)
			newInPath[neighborID] = true
			queue = append(queue, pathState{
				nodeID: neighborID,
				steps:  appendStep(cur.steps, step),
				inPath: newInPath,
			})
		}

		// Expand incoming edges (neighbor → current, traversed in reverse).
		inEdges, err := storage.GetEdgesTo(ctx, cur.nodeID)
		if err != nil {
			return nil, fmt.Errorf("get edges to %s: %w", cur.nodeID, err)
		}
		for _, e := range inEdges {
			if len(edgeTypes) > 0 && !edgeTypes[e.Type] {
				continue
			}
			neighborID := e.From
			if cur.inPath[neighborID] {
				continue // cycle — skip
			}
			neighborNode, err := storage.GetNode(ctx, neighborID)
			if err != nil {
				continue // orphaned edge — skip
			}

			step := PathStep{Node: neighborNode, EdgeType: e.Type, Incoming: true}

			if neighborID == toID {
				results = append(results, appendPath(cur.steps, step))
				if len(results) >= maxPaths {
					return results, nil
				}
				continue
			}

			newInPath := cloneInPath(cur.inPath)
			newInPath[neighborID] = true
			queue = append(queue, pathState{
				nodeID: neighborID,
				steps:  appendStep(cur.steps, step),
				inPath: newInPath,
			})
		}
	}

	return results, nil
}

// appendPath returns a new Path whose steps are the existing steps plus last.
func appendPath(steps []PathStep, last PathStep) *Path {
	all := make([]PathStep, len(steps)+1)
	copy(all, steps)
	all[len(steps)] = last
	return &Path{Steps: all}
}

// appendStep returns a new step slice with last appended. Each BFS branch must
// own its own backing array to avoid aliasing bugs.
func appendStep(steps []PathStep, last PathStep) []PathStep {
	clone := make([]PathStep, len(steps)+1)
	copy(clone, steps)
	clone[len(steps)] = last
	return clone
}

// cloneInPath copies the visited-node set for a new BFS branch.
func cloneInPath(m map[string]bool) map[string]bool {
	clone := make(map[string]bool, len(m)+1)
	for k := range m {
		clone[k] = true
	}
	return clone
}
