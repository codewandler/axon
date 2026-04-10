package axon

import (
	"context"
	"fmt"

	"github.com/codewandler/axon/graph"
)

// Describe returns a schema description of the current graph: every node type
// with its count, every edge type with its count and the from/to node-type
// pairs it connects, and (when includeFields is true) the top-level JSON data
// field names discovered in each node type.
//
// Field discovery samples up to 500 nodes per type so the cost is bounded even
// on large graphs. Pass includeFields=false when you only need type/count
// information and want the fastest possible response.
//
// Describe calls Flush before querying so that any buffered writes are visible
// in the results.
func (a *Axon) Describe(ctx context.Context, includeFields bool) (*graph.SchemaDescription, error) {
	store := a.graph.Storage()
	if err := store.Flush(ctx); err != nil {
		return nil, fmt.Errorf("describe: flush: %w", err)
	}

	d, ok := store.(graph.Describer)
	if !ok {
		return nil, fmt.Errorf("describe: storage backend does not support schema introspection")
	}

	desc, err := d.DescribeSchema(ctx, includeFields)
	if err != nil {
		return nil, fmt.Errorf("describe: %w", err)
	}
	return desc, nil
}
