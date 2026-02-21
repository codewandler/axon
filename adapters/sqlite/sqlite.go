package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	_ "modernc.org/sqlite"

	"github.com/codewandler/axon/graph"
	"github.com/codewandler/axon/storage"
)

// Ensure Storage implements graph.Storage.
var _ graph.Storage = (*Storage)(nil)

const (
	// flushThreshold is the number of buffered items before auto-flush.
	flushThreshold = 5000
	// flushInterval is the maximum time between flushes.
	flushInterval = 100 * time.Millisecond
	// writeOpBufferSize is the size of the write operation channel.
	writeOpBufferSize = 10000
)

// writeOp represents a write operation (either node or edge).
// Only one of Node or Edge is set.
type writeOp struct {
	Node *graph.Node
	Edge *graph.Edge
}

// Storage is a SQLite implementation of the graph.Storage interface.
// It buffers writes via a channel and flushes them in batches for performance.
type Storage struct {
	db   *sql.DB
	path string

	// Write buffer channel - single channel maintains order
	writeCh    chan writeOp
	flushDone  chan struct{} // Signal to wait for flush completion
	closeCh    chan struct{} // Signal to stop the flush loop
	closeOnce  sync.Once
	flushReqCh chan chan struct{} // Channel for flush requests (sends completion signal back)
	pendingOps atomic.Int64       // Count of pending operations in channel
}

var memoryDBCounter uint64

// New creates a new SQLite storage at the given path.
// The database file will be created if it doesn't exist.
// For in-memory databases, use ":memory:" as the path.
func New(path string) (*Storage, error) {
	// For in-memory databases, use shared cache mode so all connections
	// see the same database. Without this, each connection gets its own
	// empty database. We use a unique name per instance to isolate tests.
	dsn := path
	if path == ":memory:" {
		id := atomic.AddUint64(&memoryDBCounter, 1)
		dsn = fmt.Sprintf("file:memdb%d?mode=memory&cache=shared", id)
	}

	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, err
	}

	// For in-memory with shared cache, we need to keep at least one connection
	// open at all times, otherwise the database is deleted when all connections close.
	if path == ":memory:" {
		db.SetMaxIdleConns(1)
		db.SetConnMaxLifetime(0)
	}

	s := &Storage{
		db:         db,
		path:       path,
		writeCh:    make(chan writeOp, writeOpBufferSize),
		flushDone:  make(chan struct{}),
		closeCh:    make(chan struct{}),
		flushReqCh: make(chan chan struct{}, 1),
	}

	if err := s.init(); err != nil {
		db.Close()
		return nil, err
	}

	// Start background flush loop
	go s.flushLoop()

	return s, nil
}

// Close flushes any pending writes and closes the database connection.
func (s *Storage) Close() error {
	s.closeOnce.Do(func() {
		close(s.closeCh)
	})
	// Wait for final flush
	<-s.flushDone
	return s.db.Close()
}

func (s *Storage) init() error {
	// Enable WAL mode and performance settings
	_, err := s.db.Exec(`
		PRAGMA journal_mode=WAL;
		PRAGMA synchronous=NORMAL;
		PRAGMA cache_size=10000;
		PRAGMA temp_store=MEMORY;
		PRAGMA busy_timeout=30000;
	`)
	if err != nil {
		return err
	}

	// Run migrations
	return s.migrate()
}

// migrate runs database migrations to bring schema to current version.
func (s *Storage) migrate() error {
	// Create migrations table if not exists
	_, err := s.db.Exec(`
		CREATE TABLE IF NOT EXISTS schema_version (
			version INTEGER PRIMARY KEY
		);
	`)
	if err != nil {
		return err
	}

	// Get current version
	var version int
	row := s.db.QueryRow(`SELECT COALESCE(MAX(version), 0) FROM schema_version`)
	if err := row.Scan(&version); err != nil {
		return err
	}

	// Define migrations
	migrations := []struct {
		version int
		sql     string
	}{
		{
			version: 1,
			sql: `
				CREATE TABLE IF NOT EXISTS nodes (
					id TEXT PRIMARY KEY,
					type TEXT NOT NULL,
					uri TEXT,
					key TEXT,
					data TEXT,
					generation TEXT,
					root TEXT,
					created_at TEXT,
					updated_at TEXT
				);
				CREATE INDEX IF NOT EXISTS idx_nodes_uri ON nodes(uri);
				CREATE INDEX IF NOT EXISTS idx_nodes_type ON nodes(type);
				CREATE INDEX IF NOT EXISTS idx_nodes_type_key ON nodes(type, key);
				CREATE INDEX IF NOT EXISTS idx_nodes_generation ON nodes(generation);
				CREATE INDEX IF NOT EXISTS idx_nodes_root ON nodes(root);

				CREATE TABLE IF NOT EXISTS edges (
					id TEXT PRIMARY KEY,
					type TEXT NOT NULL,
					from_id TEXT NOT NULL,
					to_id TEXT NOT NULL,
					data TEXT,
					generation TEXT,
					created_at TEXT
				);
				CREATE INDEX IF NOT EXISTS idx_edges_from ON edges(from_id);
				CREATE INDEX IF NOT EXISTS idx_edges_to ON edges(to_id);
				CREATE INDEX IF NOT EXISTS idx_edges_generation ON edges(generation);
			`,
		},
		{
			version: 2,
			sql: `
				-- Add root column to existing nodes table if missing
				ALTER TABLE nodes ADD COLUMN root TEXT;
				CREATE INDEX IF NOT EXISTS idx_nodes_root ON nodes(root);
			`,
		},
		{
			version: 3,
			sql: `
				-- Unique constraint on nodes.uri (only for non-empty URIs)
				-- Partial index allows multiple nodes with empty URI for testing
				CREATE UNIQUE INDEX IF NOT EXISTS idx_nodes_uri_unique ON nodes(uri) WHERE uri != '';
				
				-- Unique constraint on edges (type, from_id, to_id)
				CREATE UNIQUE INDEX IF NOT EXISTS idx_edges_unique ON edges(type, from_id, to_id);
			`,
		},
		{
			version: 4,
			sql: `
				-- Add name column for human-readable node names
				ALTER TABLE nodes ADD COLUMN name TEXT;
				CREATE INDEX IF NOT EXISTS idx_nodes_name ON nodes(name);
			`,
		},
		{
			version: 5,
			sql: `
				-- Add labels column for categorical tagging
				ALTER TABLE nodes ADD COLUMN labels TEXT DEFAULT '[]';
			`,
		},
		{
			version: 6,
			sql: `
				-- Covering index for edge queries that JOIN on from_id and filter by URI
				-- This allows the JOIN lookup to use the index without hitting the table
				CREATE INDEX IF NOT EXISTS idx_nodes_id_uri ON nodes(id, uri);
				
				-- Index for scanning nodes by URI prefix, used by scoped edge queries
				CREATE INDEX IF NOT EXISTS idx_nodes_uri_id ON nodes(uri, id);
			`,
		},
		{
			version: 7,
			sql: `
				-- Track indexing runs for history and stats
				CREATE TABLE IF NOT EXISTS index_runs (
					id INTEGER PRIMARY KEY AUTOINCREMENT,
					started_at TEXT NOT NULL,
					finished_at TEXT NOT NULL,
					duration_ms INTEGER NOT NULL,
					root_path TEXT NOT NULL,
					files_indexed INTEGER NOT NULL,
					dirs_indexed INTEGER NOT NULL,
					repos_indexed INTEGER NOT NULL,
					stale_removed INTEGER NOT NULL,
					generation TEXT NOT NULL
				);
				
				CREATE INDEX IF NOT EXISTS idx_index_runs_finished ON index_runs(finished_at DESC);
			`,
		},
		{
			version: 8,
			sql: `
				-- Index for efficient traversal: find edges by to_id and type
				-- Used for incoming edge lookups and root node detection
				CREATE INDEX IF NOT EXISTS idx_edges_to_id_type ON edges(to_id, type);
			`,
		},
	}

	// Run pending migrations
	for _, m := range migrations {
		if m.version <= version {
			continue
		}

		// For migration 2 (adding root column), check if column already exists
		if m.version == 2 {
			var hasRoot bool
			rows, err := s.db.Query(`PRAGMA table_info(nodes)`)
			if err != nil {
				return err
			}
			for rows.Next() {
				var cid int
				var name, ctype string
				var notnull, pk int
				var dfltValue any
				if err := rows.Scan(&cid, &name, &ctype, &notnull, &dfltValue, &pk); err != nil {
					rows.Close()
					return err
				}
				if name == "root" {
					hasRoot = true
					break
				}
			}
			rows.Close()

			if hasRoot {
				// Column already exists (from migration 1 on fresh db), just record version
				if _, err := s.db.Exec(`INSERT INTO schema_version (version) VALUES (?)`, m.version); err != nil {
					return err
				}
				continue
			}
		}

		if _, err := s.db.Exec(m.sql); err != nil {
			return err
		}
		if _, err := s.db.Exec(`INSERT INTO schema_version (version) VALUES (?)`, m.version); err != nil {
			return err
		}
	}

	return nil
}

func (s *Storage) PutNode(ctx context.Context, node *graph.Node) error {
	// Copy the node to avoid external mutation
	nodeCopy := *node
	s.pendingOps.Add(1)
	select {
	case s.writeCh <- writeOp{Node: &nodeCopy}:
		return nil
	case <-s.closeCh:
		s.pendingOps.Add(-1)
		return fmt.Errorf("storage closed")
	case <-ctx.Done():
		s.pendingOps.Add(-1)
		return ctx.Err()
	}
}

// Flush writes any buffered data to the database.
// This blocks until all pending writes are flushed.
// If there are no pending writes, this returns immediately.
func (s *Storage) Flush(ctx context.Context) error {
	// Fast path: no pending writes, nothing to flush
	if s.pendingOps.Load() == 0 {
		return nil
	}

	// Request a flush and wait for completion
	done := make(chan struct{})
	select {
	case s.flushReqCh <- done:
		// Request sent, wait for completion
		select {
		case <-done:
			return nil
		case <-ctx.Done():
			return ctx.Err()
		case <-s.closeCh:
			return nil
		}
	case <-ctx.Done():
		return ctx.Err()
	case <-s.closeCh:
		return nil
	}
}

// flushLoop runs in a background goroutine and handles batched writes.
func (s *Storage) flushLoop() {
	defer close(s.flushDone)

	batch := make([]writeOp, 0, flushThreshold)
	ticker := time.NewTicker(flushInterval)
	defer ticker.Stop()

	// Track pending flush requests to signal when done
	var pendingFlushReqs []chan struct{}

	flushAndSignal := func() {
		if len(batch) > 0 {
			s.flushBatch(batch)
			s.pendingOps.Add(-int64(len(batch)))
			batch = batch[:0]
		}
		// Signal all pending flush requests
		for _, done := range pendingFlushReqs {
			close(done)
		}
		pendingFlushReqs = pendingFlushReqs[:0]
	}

	for {
		select {
		case op, ok := <-s.writeCh:
			if !ok {
				// Channel closed, flush remaining and exit
				flushAndSignal()
				return
			}
			batch = append(batch, op)
			if len(batch) >= flushThreshold {
				flushAndSignal()
			}

		case done := <-s.flushReqCh:
			// Flush request received - drain channel and flush immediately
			pendingFlushReqs = append(pendingFlushReqs, done)
		drainLoop:
			for {
				select {
				case op := <-s.writeCh:
					batch = append(batch, op)
				default:
					break drainLoop
				}
			}
			flushAndSignal()

		case <-ticker.C:
			// Time-based flush for batching efficiency
			if len(batch) > 0 {
				flushAndSignal()
			}

		case <-s.closeCh:
			// Drain remaining writes from channel
			for {
				select {
				case op := <-s.writeCh:
					batch = append(batch, op)
				default:
					flushAndSignal()
					return
				}
			}
		}
	}
}

// flushBatch writes a batch of operations to the database.
// Errors are logged since this runs asynchronously and cannot return errors to callers.
func (s *Storage) flushBatch(batch []writeOp) {
	if len(batch) == 0 {
		return
	}

	ctx := context.Background()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		log.Printf("sqlite: failed to begin transaction: %v", err)
		return
	}
	defer tx.Rollback()

	// Prepare statements for batch insert
	nodeStmt, err := tx.PrepareContext(ctx, `
		INSERT INTO nodes (id, type, uri, key, name, labels, data, generation, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET
			type = excluded.type,
			uri = excluded.uri,
			key = excluded.key,
			name = excluded.name,
			labels = excluded.labels,
			data = excluded.data,
			generation = excluded.generation,
			updated_at = excluded.updated_at
	`)
	if err != nil {
		log.Printf("sqlite: failed to prepare node statement: %v", err)
		return
	}
	defer nodeStmt.Close()

	edgeStmt, err := tx.PrepareContext(ctx, `
		INSERT INTO edges (id, type, from_id, to_id, data, generation, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(type, from_id, to_id) DO UPDATE SET
			data = excluded.data,
			generation = excluded.generation
	`)
	if err != nil {
		log.Printf("sqlite: failed to prepare edge statement: %v", err)
		return
	}
	defer edgeStmt.Close()

	// Process operations in order, abort batch on first error
	var batchErr error
	for _, op := range batch {
		if op.Node != nil {
			data, err := json.Marshal(op.Node.Data)
			if err != nil {
				log.Printf("sqlite: failed to marshal node data for %s: %v", op.Node.ID, err)
				batchErr = err
				break
			}
			labels, err := json.Marshal(op.Node.Labels)
			if err != nil {
				log.Printf("sqlite: failed to marshal node labels for %s: %v", op.Node.ID, err)
				batchErr = err
				break
			}
			_, err = nodeStmt.ExecContext(ctx,
				op.Node.ID, op.Node.Type, op.Node.URI, op.Node.Key, op.Node.Name,
				string(labels), string(data), op.Node.Generation,
				op.Node.CreatedAt.Format(time.RFC3339), op.Node.UpdatedAt.Format(time.RFC3339))
			if err != nil {
				log.Printf("sqlite: failed to insert node %s: %v", op.Node.ID, err)
				batchErr = err
				break
			}
		}
		if op.Edge != nil {
			var data string
			if op.Edge.Data != nil {
				dataBytes, err := json.Marshal(op.Edge.Data)
				if err != nil {
					log.Printf("sqlite: failed to marshal edge data for %s: %v", op.Edge.ID, err)
					batchErr = err
					break
				}
				data = string(dataBytes)
			}
			_, err = edgeStmt.ExecContext(ctx,
				op.Edge.ID, op.Edge.Type, op.Edge.From, op.Edge.To, data, op.Edge.Generation,
				op.Edge.CreatedAt.Format(time.RFC3339))
			if err != nil {
				log.Printf("sqlite: failed to insert edge %s: %v", op.Edge.ID, err)
				batchErr = err
				break
			}
		}
	}

	// If any operation failed, rollback (via defer) instead of committing
	if batchErr != nil {
		log.Printf("sqlite: batch write failed, rolling back %d operations", len(batch))
		return
	}

	if err := tx.Commit(); err != nil {
		log.Printf("sqlite: failed to commit batch of %d operations: %v", len(batch), err)
	}
}

func (s *Storage) GetNode(ctx context.Context, id string) (*graph.Node, error) {
	// Flush buffer first to ensure we read the latest data
	if err := s.Flush(ctx); err != nil {
		return nil, err
	}

	row := s.db.QueryRowContext(ctx, `
		SELECT id, type, uri, key, name, labels, data, generation, root, created_at, updated_at
		FROM nodes WHERE id = ?
	`, id)
	return s.scanNode(row)
}

func (s *Storage) GetNodeByURI(ctx context.Context, uri string) (*graph.Node, error) {
	if err := s.Flush(ctx); err != nil {
		return nil, err
	}

	row := s.db.QueryRowContext(ctx, `
		SELECT id, type, uri, key, name, labels, data, generation, root, created_at, updated_at
		FROM nodes WHERE uri = ?
	`, uri)
	return s.scanNode(row)
}

func (s *Storage) GetNodeByKey(ctx context.Context, nodeType, key string) (*graph.Node, error) {
	if err := s.Flush(ctx); err != nil {
		return nil, err
	}

	row := s.db.QueryRowContext(ctx, `
		SELECT id, type, uri, key, name, labels, data, generation, root, created_at, updated_at
		FROM nodes WHERE type = ? AND key = ?
	`, nodeType, key)
	return s.scanNode(row)
}

func (s *Storage) scanNode(row *sql.Row) (*graph.Node, error) {
	var node graph.Node
	var labelsStr, dataStr, createdAt, updatedAt string
	var uri, key, name, generation, root sql.NullString

	err := row.Scan(&node.ID, &node.Type, &uri, &key, &name, &labelsStr, &dataStr, &generation, &root, &createdAt, &updatedAt)
	if err == sql.ErrNoRows {
		return nil, storage.ErrNodeNotFound
	}
	if err != nil {
		return nil, err
	}

	node.URI = uri.String
	node.Key = key.String
	node.Name = name.String
	node.Generation = generation.String
	_ = root // Column still exists in schema but no longer used

	if labelsStr != "" && labelsStr != "[]" {
		if err := json.Unmarshal([]byte(labelsStr), &node.Labels); err != nil {
			return nil, err
		}
	}

	if dataStr != "" {
		var data any
		if err := json.Unmarshal([]byte(dataStr), &data); err != nil {
			return nil, err
		}
		node.Data = data
	}

	node.CreatedAt, _ = time.Parse(time.RFC3339, createdAt)
	node.UpdatedAt, _ = time.Parse(time.RFC3339, updatedAt)

	return &node, nil
}

func (s *Storage) DeleteNode(ctx context.Context, id string) error {
	if err := s.Flush(ctx); err != nil {
		return err
	}

	result, err := s.db.ExecContext(ctx, `DELETE FROM nodes WHERE id = ?`, id)
	if err != nil {
		return err
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if rows == 0 {
		return storage.ErrNodeNotFound
	}
	return nil
}

func (s *Storage) PutEdge(ctx context.Context, edge *graph.Edge) error {
	// Copy the edge to avoid external mutation
	edgeCopy := *edge
	s.pendingOps.Add(1)
	select {
	case s.writeCh <- writeOp{Edge: &edgeCopy}:
		return nil
	case <-s.closeCh:
		s.pendingOps.Add(-1)
		return fmt.Errorf("storage closed")
	case <-ctx.Done():
		s.pendingOps.Add(-1)
		return ctx.Err()
	}
}

func (s *Storage) GetEdge(ctx context.Context, id string) (*graph.Edge, error) {
	if err := s.Flush(ctx); err != nil {
		return nil, err
	}

	row := s.db.QueryRowContext(ctx, `
		SELECT id, type, from_id, to_id, data, generation, created_at
		FROM edges WHERE id = ?
	`, id)

	var edge graph.Edge
	var createdAt string
	var data, generation sql.NullString

	err := row.Scan(&edge.ID, &edge.Type, &edge.From, &edge.To, &data, &generation, &createdAt)
	if err == sql.ErrNoRows {
		return nil, storage.ErrEdgeNotFound
	}
	if err != nil {
		return nil, err
	}

	edge.Generation = generation.String

	if data.Valid && data.String != "" {
		var d any
		if err := json.Unmarshal([]byte(data.String), &d); err != nil {
			return nil, err
		}
		edge.Data = d
	}

	edge.CreatedAt, _ = time.Parse(time.RFC3339, createdAt)

	return &edge, nil
}

func (s *Storage) DeleteEdge(ctx context.Context, id string) error {
	if err := s.Flush(ctx); err != nil {
		return err
	}

	result, err := s.db.ExecContext(ctx, `DELETE FROM edges WHERE id = ?`, id)
	if err != nil {
		return err
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if rows == 0 {
		return storage.ErrEdgeNotFound
	}
	return nil
}

func (s *Storage) GetEdgesFrom(ctx context.Context, nodeID string) ([]*graph.Edge, error) {
	if err := s.Flush(ctx); err != nil {
		return nil, err
	}

	rows, err := s.db.QueryContext(ctx, `
		SELECT id, type, from_id, to_id, data, generation, created_at
		FROM edges WHERE from_id = ?
	`, nodeID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return s.scanEdges(rows)
}

func (s *Storage) GetEdgesTo(ctx context.Context, nodeID string) ([]*graph.Edge, error) {
	if err := s.Flush(ctx); err != nil {
		return nil, err
	}

	rows, err := s.db.QueryContext(ctx, `
		SELECT id, type, from_id, to_id, data, generation, created_at
		FROM edges WHERE to_id = ?
	`, nodeID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return s.scanEdges(rows)
}

// Traverse walks the graph from seed nodes using BFS, yielding visited nodes via channel.
func (s *Storage) Traverse(ctx context.Context, opts graph.TraverseOptions) (<-chan graph.TraverseResult, error) {
	if err := s.Flush(ctx); err != nil {
		return nil, err
	}

	// Find seed nodes
	seedNodes, err := s.FindNodes(ctx, opts.Seed, graph.QueryOptions{})
	if err != nil {
		return nil, err
	}

	results := make(chan graph.TraverseResult, 100)

	go func() {
		defer close(results)

		// Track visited nodes to avoid cycles
		visited := make(map[string]bool)

		// BFS queue: (nodeID, depth, edge that led here)
		type queueItem struct {
			nodeID string
			depth  int
			via    *graph.Edge
		}
		queue := make([]queueItem, 0, len(seedNodes))

		// Initialize queue with seed nodes
		for _, node := range seedNodes {
			if visited[node.ID] {
				continue
			}
			visited[node.ID] = true
			queue = append(queue, queueItem{nodeID: node.ID, depth: 0, via: nil})

			// Yield seed node if it passes the filter
			if s.nodeMatchesFilter(node, opts.NodeFilter) {
				select {
				case <-ctx.Done():
					results <- graph.TraverseResult{Err: ctx.Err()}
					return
				case results <- graph.TraverseResult{Node: node, Depth: 0, Via: nil}:
				}
			}
		}

		// BFS traversal
		for len(queue) > 0 {
			item := queue[0]
			queue = queue[1:]

			// Check depth limit
			if opts.MaxDepth > 0 && item.depth >= opts.MaxDepth {
				continue
			}

			// Find edges to follow based on EdgeFilters
			edges, err := s.findEdgesToFollow(ctx, item.nodeID, opts.EdgeFilters)
			if err != nil {
				results <- graph.TraverseResult{Err: err}
				return
			}

			for _, edge := range edges {
				// Determine target node ID based on edge direction
				var targetID string
				if edge.From == item.nodeID {
					targetID = edge.To
				} else {
					targetID = edge.From
				}

				if visited[targetID] {
					continue
				}
				visited[targetID] = true

				// Get the target node
				targetNode, err := s.GetNode(ctx, targetID)
				if err != nil {
					// Node might have been deleted - skip
					continue
				}

				// Add to queue for further traversal
				queue = append(queue, queueItem{
					nodeID: targetID,
					depth:  item.depth + 1,
					via:    edge,
				})

				// Yield if node passes the filter
				if s.nodeMatchesFilter(targetNode, opts.NodeFilter) {
					select {
					case <-ctx.Done():
						results <- graph.TraverseResult{Err: ctx.Err()}
						return
					case results <- graph.TraverseResult{
						Node:  targetNode,
						Depth: item.depth + 1,
						Via:   edge,
					}:
					}
				}
			}
		}
	}()

	return results, nil
}

// findEdgesToFollow returns edges from nodeID matching any of the EdgeFilters.
func (s *Storage) findEdgesToFollow(ctx context.Context, nodeID string, filters []graph.EdgeFilter) ([]*graph.Edge, error) {
	if len(filters) == 0 {
		// Default: follow all outgoing edges
		return s.GetEdgesFrom(ctx, nodeID)
	}

	var allEdges []*graph.Edge
	seen := make(map[string]bool)

	for _, filter := range filters {
		var edges []*graph.Edge
		var err error

		direction := filter.Direction
		if direction == "" {
			direction = "outgoing"
		}

		// Get edges based on direction
		switch direction {
		case "outgoing":
			edges, err = s.getEdgesFromWithTypes(ctx, nodeID, filter.Types, filter.Type)
		case "incoming":
			edges, err = s.getEdgesToWithTypes(ctx, nodeID, filter.Types, filter.Type)
		case "both":
			outEdges, err1 := s.getEdgesFromWithTypes(ctx, nodeID, filter.Types, filter.Type)
			inEdges, err2 := s.getEdgesToWithTypes(ctx, nodeID, filter.Types, filter.Type)
			if err1 != nil {
				return nil, err1
			}
			if err2 != nil {
				return nil, err2
			}
			edges = append(outEdges, inEdges...)
		default:
			edges, err = s.getEdgesFromWithTypes(ctx, nodeID, filter.Types, filter.Type)
		}

		if err != nil {
			return nil, err
		}

		// Deduplicate edges
		for _, e := range edges {
			if !seen[e.ID] {
				seen[e.ID] = true
				allEdges = append(allEdges, e)
			}
		}
	}

	return allEdges, nil
}

// getEdgesFromWithTypes gets outgoing edges optionally filtered by type(s).
func (s *Storage) getEdgesFromWithTypes(ctx context.Context, nodeID string, types []string, singleType string) ([]*graph.Edge, error) {
	query := `SELECT id, type, from_id, to_id, data, generation, created_at FROM edges WHERE from_id = ?`
	args := []any{nodeID}

	query, args = s.appendEdgeTypeFilter(query, args, types, singleType)

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return s.scanEdges(rows)
}

// getEdgesToWithTypes gets incoming edges optionally filtered by type(s).
func (s *Storage) getEdgesToWithTypes(ctx context.Context, nodeID string, types []string, singleType string) ([]*graph.Edge, error) {
	query := `SELECT id, type, from_id, to_id, data, generation, created_at FROM edges WHERE to_id = ?`
	args := []any{nodeID}

	query, args = s.appendEdgeTypeFilter(query, args, types, singleType)

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return s.scanEdges(rows)
}

// appendEdgeTypeFilter appends type filter conditions to query.
func (s *Storage) appendEdgeTypeFilter(query string, args []any, types []string, singleType string) (string, []any) {
	if len(types) > 0 {
		placeholders := make([]string, len(types))
		for i, t := range types {
			placeholders[i] = "?"
			args = append(args, t)
		}
		query += ` AND type IN (` + strings.Join(placeholders, ", ") + `)`
	} else if singleType != "" {
		query += ` AND type = ?`
		args = append(args, singleType)
	}
	return query, args
}

// nodeMatchesFilter checks if a node matches the given filter.
// An empty filter matches all nodes.
func (s *Storage) nodeMatchesFilter(node *graph.Node, filter graph.NodeFilter) bool {
	if filter.Type != "" && node.Type != filter.Type {
		return false
	}
	if filter.TypePattern != "" && !globMatch(filter.TypePattern, node.Type) {
		return false
	}
	if filter.URIPrefix != "" && !strings.HasPrefix(node.URI, filter.URIPrefix) {
		return false
	}
	if filter.Name != "" && node.Name != filter.Name {
		return false
	}
	if filter.NamePattern != "" && !globMatch(filter.NamePattern, node.Name) {
		return false
	}
	if len(filter.Labels) > 0 {
		hasLabel := false
		for _, filterLabel := range filter.Labels {
			for _, nodeLabel := range node.Labels {
				if nodeLabel == filterLabel {
					hasLabel = true
					break
				}
			}
			if hasLabel {
				break
			}
		}
		if !hasLabel {
			return false
		}
	}
	if len(filter.Extensions) > 0 {
		hasExt := false
		for _, ext := range filter.Extensions {
			if strings.HasSuffix(node.Name, "."+ext) {
				hasExt = true
				break
			}
		}
		if !hasExt {
			return false
		}
	}
	if len(filter.NodeIDs) > 0 {
		hasID := false
		for _, id := range filter.NodeIDs {
			if node.ID == id {
				hasID = true
				break
			}
		}
		if !hasID {
			return false
		}
	}
	// Note: Root filter is handled at query level, not here
	return true
}

// globMatch provides simple glob matching (*, ?).
func globMatch(pattern, s string) bool {
	// Simple implementation - could use filepath.Match but it has different semantics
	// For now, convert glob to a simple check
	if pattern == "*" {
		return true
	}
	if !strings.Contains(pattern, "*") && !strings.Contains(pattern, "?") {
		return pattern == s
	}
	// Use filepath.Match for proper glob semantics
	matched, _ := filepath.Match(pattern, s)
	return matched
}

func (s *Storage) scanEdges(rows *sql.Rows) ([]*graph.Edge, error) {
	var edges []*graph.Edge
	for rows.Next() {
		var edge graph.Edge
		var createdAt string
		var data, generation sql.NullString

		err := rows.Scan(&edge.ID, &edge.Type, &edge.From, &edge.To, &data, &generation, &createdAt)
		if err != nil {
			return nil, err
		}

		edge.Generation = generation.String

		if data.Valid && data.String != "" {
			var d any
			if err := json.Unmarshal([]byte(data.String), &d); err != nil {
				return nil, err
			}
			edge.Data = d
		}

		edge.CreatedAt, _ = time.Parse(time.RFC3339, createdAt)
		edges = append(edges, &edge)
	}
	return edges, rows.Err()
}

func (s *Storage) FindNodes(ctx context.Context, filter graph.NodeFilter, opts graph.QueryOptions) ([]*graph.Node, error) {
	if err := s.Flush(ctx); err != nil {
		return nil, err
	}

	query := `SELECT id, type, uri, key, name, labels, data, generation, root, created_at, updated_at FROM nodes WHERE 1=1`
	args := s.buildNodeFilterArgs(&query, filter)

	// ORDER BY
	query += s.buildOrderBy(opts, "name", "updated_at", "type")

	// LIMIT
	if opts.Limit > 0 {
		query += ` LIMIT ?`
		args = append(args, opts.Limit)
	}

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var nodes []*graph.Node
	for rows.Next() {
		var node graph.Node
		var labelsStr, dataStr, createdAt, updatedAt string
		var uri, key, name, generation, root sql.NullString

		err := rows.Scan(&node.ID, &node.Type, &uri, &key, &name, &labelsStr, &dataStr, &generation, &root, &createdAt, &updatedAt)
		if err != nil {
			return nil, err
		}

		node.URI = uri.String
		node.Key = key.String
		node.Name = name.String
		node.Generation = generation.String
		_ = root // Column still exists in schema but no longer used

		if labelsStr != "" && labelsStr != "[]" {
			if err := json.Unmarshal([]byte(labelsStr), &node.Labels); err != nil {
				return nil, err
			}
		}

		if dataStr != "" {
			var data any
			if err := json.Unmarshal([]byte(dataStr), &data); err != nil {
				return nil, err
			}
			node.Data = data
		}

		node.CreatedAt, _ = time.Parse(time.RFC3339, createdAt)
		node.UpdatedAt, _ = time.Parse(time.RFC3339, updatedAt)
		nodes = append(nodes, &node)
	}

	return nodes, rows.Err()
}

// buildNodeFilterArgs appends WHERE conditions for NodeFilter to query and returns args.
func (s *Storage) buildNodeFilterArgs(query *string, filter graph.NodeFilter) []any {
	var args []any

	if filter.Type != "" {
		*query += ` AND type = ?`
		args = append(args, filter.Type)
	}
	if filter.TypePattern != "" {
		*query += ` AND type GLOB ?`
		args = append(args, filter.TypePattern)
	}
	if filter.URIPrefix != "" {
		*query += ` AND uri LIKE ?`
		args = append(args, filter.URIPrefix+"%")
	}
	if filter.Name != "" {
		*query += ` AND name = ?`
		args = append(args, filter.Name)
	}
	if filter.NamePattern != "" {
		*query += ` AND name GLOB ?`
		args = append(args, filter.NamePattern)
	}
	// Labels filter: OR logic - node must have at least one of the labels
	if len(filter.Labels) > 0 {
		labelConditions := make([]string, len(filter.Labels))
		for i, label := range filter.Labels {
			labelConditions[i] = `labels LIKE ?`
			args = append(args, `%"`+label+`"%`)
		}
		*query += ` AND (` + strings.Join(labelConditions, " OR ") + `)`
	}
	// Extensions filter: OR logic - node name must end with one of the extensions
	if len(filter.Extensions) > 0 {
		extConditions := make([]string, len(filter.Extensions))
		for i, ext := range filter.Extensions {
			extConditions[i] = `name LIKE ?`
			args = append(args, "%."+ext)
		}
		*query += ` AND (` + strings.Join(extConditions, " OR ") + `)`
	}

	// NodeIDs filter: OR logic - node id must be one of the specified IDs
	if len(filter.NodeIDs) > 0 {
		placeholders := make([]string, len(filter.NodeIDs))
		for i, id := range filter.NodeIDs {
			placeholders[i] = "?"
			args = append(args, id)
		}
		*query += ` AND id IN (` + strings.Join(placeholders, ", ") + `)`
	}

	// Root filter: only nodes with no incoming containment edges
	if filter.Root {
		*query += ` AND NOT EXISTS (
			SELECT 1 FROM edges e 
			WHERE e.to_id = nodes.id 
			AND e.type IN ('contains', 'located_at')
		)`
	}

	return args
}

// buildOrderBy builds ORDER BY clause based on QueryOptions.
// validColumns maps OrderBy values to actual column names.
func (s *Storage) buildOrderBy(opts graph.QueryOptions, validColumns ...string) string {
	if opts.OrderBy == "" {
		return ""
	}

	// Map of allowed OrderBy values to column names
	columnMap := make(map[string]string)
	for _, col := range validColumns {
		columnMap[col] = col
	}
	// Common aliases
	columnMap["updated"] = "updated_at"
	columnMap["created"] = "created_at"

	col, ok := columnMap[opts.OrderBy]
	if !ok {
		return ""
	}

	order := "ASC"
	if opts.Desc {
		order = "DESC"
	}
	return fmt.Sprintf(` ORDER BY %s %s`, col, order)
}

func (s *Storage) CountNodes(ctx context.Context, filter graph.NodeFilter, opts graph.QueryOptions) (map[string]int, error) {
	if err := s.Flush(ctx); err != nil {
		return nil, err
	}

	result := make(map[string]int)

	switch opts.GroupBy {
	case "type":
		query := `SELECT type, COUNT(*) FROM nodes WHERE 1=1`
		args := s.buildNodeFilterArgs(&query, filter)
		query += ` GROUP BY type`
		query += s.buildCountOrderBy(opts)
		if opts.Limit > 0 {
			query += ` LIMIT ?`
			args = append(args, opts.Limit)
		}

		rows, err := s.db.QueryContext(ctx, query, args...)
		if err != nil {
			return nil, err
		}
		defer rows.Close()

		for rows.Next() {
			var typeName string
			var count int
			if err := rows.Scan(&typeName, &count); err != nil {
				return nil, err
			}
			result[typeName] = count
		}
		return result, rows.Err()

	case "label":
		// Use json_each to extract labels from JSON array
		// Filter out nodes with empty/null labels arrays
		query := `SELECT j.value, COUNT(*) FROM nodes, json_each(nodes.labels) AS j WHERE j.value IS NOT NULL`
		args := s.buildNodeFilterArgs(&query, filter)
		query += ` GROUP BY j.value`
		query += s.buildCountOrderBy(opts)
		if opts.Limit > 0 {
			query += ` LIMIT ?`
			args = append(args, opts.Limit)
		}

		rows, err := s.db.QueryContext(ctx, query, args...)
		if err != nil {
			return nil, err
		}
		defer rows.Close()

		for rows.Next() {
			var label string
			var count int
			if err := rows.Scan(&label, &count); err != nil {
				return nil, err
			}
			result[label] = count
		}
		return result, rows.Err()

	case "extension":
		// Extract extension from data JSON, only for fs:file nodes
		query := `SELECT json_extract(data, '$.ext'), COUNT(*) FROM nodes WHERE type = 'fs:file' AND json_extract(data, '$.ext') IS NOT NULL AND json_extract(data, '$.ext') != ''`
		args := s.buildNodeFilterArgs(&query, filter)
		query += ` GROUP BY json_extract(data, '$.ext')`
		query += s.buildCountOrderBy(opts)
		if opts.Limit > 0 {
			query += ` LIMIT ?`
			args = append(args, opts.Limit)
		}

		rows, err := s.db.QueryContext(ctx, query, args...)
		if err != nil {
			return nil, err
		}
		defer rows.Close()

		for rows.Next() {
			var ext string
			var count int
			if err := rows.Scan(&ext, &count); err != nil {
				return nil, err
			}
			result[ext] = count
		}
		return result, rows.Err()

	default:
		// No grouping - just total count
		query := `SELECT COUNT(*) FROM nodes WHERE 1=1`
		args := s.buildNodeFilterArgs(&query, filter)

		var count int
		if err := s.db.QueryRowContext(ctx, query, args...).Scan(&count); err != nil {
			return nil, err
		}
		result[""] = count
		return result, nil
	}
}

func (s *Storage) CountEdges(ctx context.Context, filter graph.EdgeFilter, opts graph.QueryOptions) (map[string]int, error) {
	if err := s.Flush(ctx); err != nil {
		return nil, err
	}

	result := make(map[string]int)

	// Build query with optional JOINs for From/To filters
	var query string
	var args []any

	// Determine if we need JOINs
	needFromJoin := filter.From != nil
	needToJoin := filter.To != nil

	// When filtering by From node (common case for scoped queries),
	// restructure query to start from nodes table with index hints.
	// This allows SQLite to use idx_nodes_uri for URI prefix filtering
	// and idx_edges_from for the join, which is much faster.
	if needFromJoin && filter.From.URIPrefix != "" {
		// Optimized query: start from nodes, use index hints
		switch opts.GroupBy {
		case "type":
			query = `SELECT e.type, COUNT(*) FROM nodes nf INDEXED BY idx_nodes_uri`
			query += ` JOIN edges e INDEXED BY idx_edges_from ON e.from_id = nf.id`
		default:
			query = `SELECT COUNT(*) FROM nodes nf INDEXED BY idx_nodes_uri`
			query += ` JOIN edges e INDEXED BY idx_edges_from ON e.from_id = nf.id`
		}

		if needToJoin {
			query += ` JOIN nodes nt ON e.to_id = nt.id`
		}

		query += ` WHERE 1=1`

		// From node filter (applied to nf)
		args = append(args, s.buildNodeFilterArgsWithPrefix(&query, *filter.From, "nf")...)

		// Edge type filter
		if filter.Type != "" {
			query += ` AND e.type = ?`
			args = append(args, filter.Type)
		}

		// To node filter
		if filter.To != nil {
			args = append(args, s.buildNodeFilterArgsWithPrefix(&query, *filter.To, "nt")...)
		}
	} else {
		// Standard query starting from edges
		switch opts.GroupBy {
		case "type":
			query = `SELECT e.type, COUNT(*) FROM edges e`
		default:
			query = `SELECT COUNT(*) FROM edges e`
		}

		if needFromJoin {
			query += ` JOIN nodes nf ON e.from_id = nf.id`
		}
		if needToJoin {
			query += ` JOIN nodes nt ON e.to_id = nt.id`
		}

		query += ` WHERE 1=1`

		// Edge type filter
		if filter.Type != "" {
			query += ` AND e.type = ?`
			args = append(args, filter.Type)
		}

		// From node filter
		if filter.From != nil {
			args = append(args, s.buildNodeFilterArgsWithPrefix(&query, *filter.From, "nf")...)
		}

		// To node filter
		if filter.To != nil {
			args = append(args, s.buildNodeFilterArgsWithPrefix(&query, *filter.To, "nt")...)
		}
	}

	if opts.GroupBy == "type" {
		query += ` GROUP BY e.type`
		query += s.buildCountOrderBy(opts)
		if opts.Limit > 0 {
			query += ` LIMIT ?`
			args = append(args, opts.Limit)
		}

		rows, err := s.db.QueryContext(ctx, query, args...)
		if err != nil {
			return nil, err
		}
		defer rows.Close()

		for rows.Next() {
			var edgeType string
			var count int
			if err := rows.Scan(&edgeType, &count); err != nil {
				return nil, err
			}
			result[edgeType] = count
		}
		return result, rows.Err()
	}

	// No grouping - just total count
	var count int
	if err := s.db.QueryRowContext(ctx, query, args...).Scan(&count); err != nil {
		return nil, err
	}
	result[""] = count
	return result, nil
}

// buildNodeFilterArgsWithPrefix is like buildNodeFilterArgs but uses a table prefix for JOINs.
func (s *Storage) buildNodeFilterArgsWithPrefix(query *string, filter graph.NodeFilter, prefix string) []any {
	var args []any

	if filter.Type != "" {
		*query += fmt.Sprintf(` AND %s.type = ?`, prefix)
		args = append(args, filter.Type)
	}
	if filter.TypePattern != "" {
		*query += fmt.Sprintf(` AND %s.type GLOB ?`, prefix)
		args = append(args, filter.TypePattern)
	}
	if filter.URIPrefix != "" {
		*query += fmt.Sprintf(` AND %s.uri LIKE ?`, prefix)
		args = append(args, filter.URIPrefix+"%")
	}
	if filter.Name != "" {
		*query += fmt.Sprintf(` AND %s.name = ?`, prefix)
		args = append(args, filter.Name)
	}
	if filter.NamePattern != "" {
		*query += fmt.Sprintf(` AND %s.name GLOB ?`, prefix)
		args = append(args, filter.NamePattern)
	}
	if len(filter.Labels) > 0 {
		labelConditions := make([]string, len(filter.Labels))
		for i, label := range filter.Labels {
			labelConditions[i] = fmt.Sprintf(`%s.labels LIKE ?`, prefix)
			args = append(args, `%"`+label+`"%`)
		}
		*query += ` AND (` + strings.Join(labelConditions, " OR ") + `)`
	}
	if len(filter.Extensions) > 0 {
		extConditions := make([]string, len(filter.Extensions))
		for i, ext := range filter.Extensions {
			extConditions[i] = fmt.Sprintf(`%s.name LIKE ?`, prefix)
			args = append(args, "%."+ext)
		}
		*query += ` AND (` + strings.Join(extConditions, " OR ") + `)`
	}

	return args
}

// buildCountOrderBy builds ORDER BY for count queries.
func (s *Storage) buildCountOrderBy(opts graph.QueryOptions) string {
	if opts.OrderBy == "" {
		return ""
	}

	order := "ASC"
	if opts.Desc {
		order = "DESC"
	}

	switch opts.OrderBy {
	case "count":
		return fmt.Sprintf(` ORDER BY COUNT(*) %s`, order)
	case "name":
		return fmt.Sprintf(` ORDER BY 1 %s`, order) // Column 1 is the group key
	default:
		return ""
	}
}

func (s *Storage) FindStaleByURIPrefix(ctx context.Context, uriPrefix, currentGen string) ([]*graph.Node, error) {
	if err := s.Flush(ctx); err != nil {
		return nil, err
	}

	rows, err := s.db.QueryContext(ctx, `
		SELECT id, type, uri, key, name, labels, data, generation, root, created_at, updated_at
		FROM nodes WHERE uri LIKE ? AND generation != ?
	`, uriPrefix+"%", currentGen)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var nodes []*graph.Node
	for rows.Next() {
		var node graph.Node
		var labelsStr, dataStr, createdAt, updatedAt string
		var uri, key, name, generation, root sql.NullString

		err := rows.Scan(&node.ID, &node.Type, &uri, &key, &name, &labelsStr, &dataStr, &generation, &root, &createdAt, &updatedAt)
		if err != nil {
			return nil, err
		}

		node.URI = uri.String
		node.Key = key.String
		node.Name = name.String
		node.Generation = generation.String

		if labelsStr != "" && labelsStr != "[]" {
			if err := json.Unmarshal([]byte(labelsStr), &node.Labels); err != nil {
				return nil, err
			}
		}

		if dataStr != "" {
			var data any
			if err := json.Unmarshal([]byte(dataStr), &data); err != nil {
				return nil, err
			}
			node.Data = data
		}

		node.CreatedAt, _ = time.Parse(time.RFC3339, createdAt)
		node.UpdatedAt, _ = time.Parse(time.RFC3339, updatedAt)
		nodes = append(nodes, &node)
	}

	return nodes, rows.Err()
}

func (s *Storage) DeleteStaleByURIPrefix(ctx context.Context, uriPrefix, currentGen string) (int, error) {
	if err := s.Flush(ctx); err != nil {
		return 0, err
	}

	result, err := s.db.ExecContext(ctx, `
		DELETE FROM nodes WHERE uri LIKE ? AND generation != ?
	`, uriPrefix+"%", currentGen)
	if err != nil {
		return 0, err
	}
	rows, err := result.RowsAffected()
	return int(rows), err
}

func (s *Storage) DeleteByURIPrefix(ctx context.Context, uriPrefix string) (int, error) {
	if err := s.Flush(ctx); err != nil {
		return 0, err
	}

	result, err := s.db.ExecContext(ctx, `
		DELETE FROM nodes WHERE uri LIKE ?
	`, uriPrefix+"%")
	if err != nil {
		return 0, err
	}
	rows, err := result.RowsAffected()
	return int(rows), err
}

func (s *Storage) DeleteStaleEdges(ctx context.Context, currentGen string) (int, error) {
	if err := s.Flush(ctx); err != nil {
		return 0, err
	}

	result, err := s.db.ExecContext(ctx, `
		DELETE FROM edges WHERE generation != ?
	`, currentGen)
	if err != nil {
		return 0, err
	}
	rows, err := result.RowsAffected()
	return int(rows), err
}

func (s *Storage) DeleteOrphanedEdges(ctx context.Context) (int, error) {
	if err := s.Flush(ctx); err != nil {
		return 0, err
	}

	result, err := s.db.ExecContext(ctx, `
		DELETE FROM edges 
		WHERE from_id NOT IN (SELECT id FROM nodes) 
		   OR to_id NOT IN (SELECT id FROM nodes)
	`)
	if err != nil {
		return 0, err
	}
	rows, err := result.RowsAffected()
	return int(rows), err
}

func (s *Storage) CountOrphanedEdges(ctx context.Context) (int, error) {
	if err := s.Flush(ctx); err != nil {
		return 0, err
	}

	var count int
	err := s.db.QueryRowContext(ctx, `
		SELECT COUNT(*) FROM edges 
		WHERE from_id NOT IN (SELECT id FROM nodes) 
		   OR to_id NOT IN (SELECT id FROM nodes)
	`).Scan(&count)
	return count, err
}

// URIPrefix helper for building LIKE patterns
func URIPrefix(uri string) string {
	if strings.HasSuffix(uri, "/") {
		return uri
	}
	return uri + "/"
}

// RecordIndexRun saves a record of an indexing run.
func (s *Storage) RecordIndexRun(ctx context.Context, run graph.IndexRunRecord) error {
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO index_runs (started_at, finished_at, duration_ms, root_path, files_indexed, dirs_indexed, repos_indexed, stale_removed, generation)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
	`,
		run.StartedAt.Format(time.RFC3339),
		run.FinishedAt.Format(time.RFC3339),
		run.DurationMs,
		run.RootPath,
		run.FilesIndexed,
		run.DirsIndexed,
		run.ReposIndexed,
		run.StaleRemoved,
		run.Generation,
	)
	return err
}

// GetLastIndexRun returns the most recent index run, or nil if none.
func (s *Storage) GetLastIndexRun(ctx context.Context) (*graph.IndexRunRecord, error) {
	var run graph.IndexRunRecord
	var startedAt, finishedAt string

	err := s.db.QueryRowContext(ctx, `
		SELECT id, started_at, finished_at, duration_ms, root_path, files_indexed, dirs_indexed, repos_indexed, stale_removed, generation
		FROM index_runs
		ORDER BY finished_at DESC
		LIMIT 1
	`).Scan(
		&run.ID,
		&startedAt,
		&finishedAt,
		&run.DurationMs,
		&run.RootPath,
		&run.FilesIndexed,
		&run.DirsIndexed,
		&run.ReposIndexed,
		&run.StaleRemoved,
		&run.Generation,
	)

	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}

	run.StartedAt, _ = time.Parse(time.RFC3339, startedAt)
	run.FinishedAt, _ = time.Parse(time.RFC3339, finishedAt)

	return &run, nil
}

// GetDatabasePath returns the path to the database file.
func (s *Storage) GetDatabasePath() string {
	return s.path
}
