package sqlite

import (
	"context"
	"fmt"
	"sort"

	"github.com/codewandler/axon/graph"
)

// DescribeSchema implements graph.Describer for the SQLite storage backend.
// It executes up to three raw SQL queries:
//  1. Node types and counts.
//  2. Edge type / from-type / to-type triples and counts (JOIN across nodes).
//  3. (Only when includeFields=true) Distinct JSON object keys per node type,
//     sampled at up to 500 nodes per type.
func (s *Storage) DescribeSchema(ctx context.Context, includeFields bool) (*graph.SchemaDescription, error) {
	nodeTypes, err := s.describeNodeTypes(ctx)
	if err != nil {
		return nil, fmt.Errorf("describe node types: %w", err)
	}

	if includeFields {
		for i := range nodeTypes {
			fields, err := s.describeNodeFields(ctx, nodeTypes[i].Type)
			if err != nil {
				return nil, fmt.Errorf("describe fields for %s: %w", nodeTypes[i].Type, err)
			}
			nodeTypes[i].Fields = fields
		}
	}

	edgeTypes, err := s.describeEdgeTypes(ctx)
	if err != nil {
		return nil, fmt.Errorf("describe edge types: %w", err)
	}

	return &graph.SchemaDescription{
		NodeTypes: nodeTypes,
		EdgeTypes: edgeTypes,
	}, nil
}

// describeNodeTypes returns all node types with their total counts, ordered by
// count descending.
func (s *Storage) describeNodeTypes(ctx context.Context) ([]graph.NodeTypeInfo, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT type, COUNT(*) AS cnt
		FROM nodes
		GROUP BY type
		ORDER BY cnt DESC
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var result []graph.NodeTypeInfo
	for rows.Next() {
		var info graph.NodeTypeInfo
		if err := rows.Scan(&info.Type, &info.Count); err != nil {
			return nil, err
		}
		result = append(result, info)
	}
	return result, rows.Err()
}

// describeNodeFields returns the distinct top-level JSON keys present in the
// `data` column for nodes of the given type.  It samples at most 500 nodes to
// keep the query cost bounded even on large graphs.  Keys are returned in
// sorted order.
func (s *Storage) describeNodeFields(ctx context.Context, nodeType string) ([]string, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT DISTINCT je.key
		FROM (
			SELECT data FROM nodes WHERE type = ? AND data IS NOT NULL LIMIT 500
		) n,
		json_each(n.data) je
		ORDER BY je.key
	`, nodeType)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var fields []string
	for rows.Next() {
		var key string
		if err := rows.Scan(&key); err != nil {
			return nil, err
		}
		fields = append(fields, key)
	}
	return fields, rows.Err()
}

// describeEdgeTypes returns all edge types with their total counts and the
// distinct from-type / to-type pairs for each edge type.  Results are ordered
// by total count descending; connections within each edge type are ordered by
// count descending.
func (s *Storage) describeEdgeTypes(ctx context.Context) ([]graph.EdgeTypeInfo, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT e.type, nf.type AS from_type, nt.type AS to_type, COUNT(*) AS cnt
		FROM edges e
		JOIN nodes nf ON e.from_id = nf.id
		JOIN nodes nt ON e.to_id   = nt.id
		GROUP BY e.type, nf.type, nt.type
		ORDER BY e.type, cnt DESC
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	// Accumulate connections per edge type, preserving insertion order.
	type edgeKey = string
	indexMap := make(map[edgeKey]int) // edge type → index in result slice
	var result []graph.EdgeTypeInfo

	for rows.Next() {
		var edgeType, fromType, toType string
		var cnt int
		if err := rows.Scan(&edgeType, &fromType, &toType, &cnt); err != nil {
			return nil, err
		}

		idx, exists := indexMap[edgeType]
		if !exists {
			result = append(result, graph.EdgeTypeInfo{
				Type:        edgeType,
				Connections: []graph.EdgeConnection{},
			})
			idx = len(result) - 1
			indexMap[edgeType] = idx
		}
		result[idx].Count += cnt
		result[idx].Connections = append(result[idx].Connections, graph.EdgeConnection{
			From:  fromType,
			To:    toType,
			Count: cnt,
		})
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	// Sort edge types by total count descending.
	sort.Slice(result, func(i, j int) bool {
		return result[i].Count > result[j].Count
	})

	return result, nil
}
