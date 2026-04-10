package sqlite

import (
	"context"
	"database/sql"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"log"
	"math"
	"net/url"
	"sort"
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

	// Flush error tracking: set by flushBatch when a batch write permanently fails.
	// Read by Flush() callers after the flush completes.
	flushMu      sync.Mutex
	lastFlushErr error
}

var memoryDBCounter uint64

// perConnectionPragmas are applied on every new connection via the DSN
// _pragma parameter. This is critical because database/sql uses a connection
// pool and PRAGMAs are per-connection settings — setting them in init() only
// affects one connection while other pooled connections use SQLite defaults.
var perConnectionPragmas = []string{
	"busy_timeout(30000)",
	"journal_mode(WAL)",
	"synchronous(NORMAL)",
	"cache_size(10000)",
	"temp_store(MEMORY)",
}

// buildDSN constructs a modernc.org/sqlite DSN with per-connection PRAGMAs.
func buildDSN(path string) string {
	if path == ":memory:" {
		id := atomic.AddUint64(&memoryDBCounter, 1)
		base := fmt.Sprintf("file:memdb%d?mode=memory&cache=shared", id)
		for _, p := range perConnectionPragmas {
			base += "&_pragma=" + url.QueryEscape(p)
		}
		return base
	}

	// For file-based databases, build a URI DSN so we can append query params.
	qs := url.Values{}
	for _, p := range perConnectionPragmas {
		qs.Add("_pragma", p)
	}
	return "file:" + path + "?" + qs.Encode()
}

// New creates a new SQLite storage at the given path.
// The database file will be created if it doesn't exist.
// For in-memory databases, use ":memory:" as the path.
func New(path string) (*Storage, error) {
	dsn := buildDSN(path)

	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("opening database %s: %w", path, err)
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
		return nil, fmt.Errorf("initializing database: %w", err)
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
	// Use a timeout context for initialization to prevent hangs
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// PRAGMAs (busy_timeout, journal_mode, synchronous, etc.) are set
	// per-connection via DSN _pragma parameters in buildDSN().
	// This ensures every connection from the database/sql pool gets
	// the same settings, not just the first one.

	// Run migrations
	if err := s.migrate(ctx); err != nil {
		return fmt.Errorf("running migrations: %w", err)
	}
	return nil
}

// migrate runs database migrations to bring schema to current version.
func (s *Storage) migrate(ctx context.Context) error {
	// Create migrations table if not exists
	_, err := s.db.ExecContext(ctx, `
		CREATE TABLE IF NOT EXISTS schema_version (
			version INTEGER PRIMARY KEY
		);
	`)
	if err != nil {
		return fmt.Errorf("creating schema_version table: %w", err)
	}

	// Get current version
	var version int
	row := s.db.QueryRowContext(ctx, `SELECT COALESCE(MAX(version), 0) FROM schema_version`)
	if err := row.Scan(&version); err != nil {
		return fmt.Errorf("reading schema version: %w", err)
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
		{
			version: 9,
			sql: `
				-- Index for efficient recursive CTE traversal: find children by from_id and type
				-- Critical for EXISTS patterns with variable-length paths (e.g., descendants of a node)
				-- Without this index, recursive CTEs scan all edges of matching type
				CREATE INDEX IF NOT EXISTS idx_edges_from_type ON edges(from_id, type);
			`,
		},
		{
			version: 10,
			sql: `
				-- Vector embeddings for semantic search
				CREATE TABLE IF NOT EXISTS embeddings (
					node_id TEXT PRIMARY KEY,
					dims    INTEGER NOT NULL,
					data    BLOB NOT NULL
				);
			`,
		},
		{
			version: 11,
			sql: `
				-- TTL support: optional expiry timestamp (unix epoch seconds, nullable)
				ALTER TABLE nodes ADD COLUMN expires_at INTEGER;
				CREATE INDEX IF NOT EXISTS idx_nodes_expires_at ON nodes(expires_at);

				-- View that filters out expired nodes for all read paths
				CREATE VIEW IF NOT EXISTS active_nodes AS
					SELECT * FROM nodes WHERE expires_at IS NULL OR expires_at > unixepoch();
			`,
		},
		{
			version: 12,
			sql: `
				-- TTL support for edges
				ALTER TABLE edges ADD COLUMN expires_at INTEGER;
				CREATE INDEX IF NOT EXISTS idx_edges_expires_at ON edges(expires_at);

				-- View that filters out expired edges for all read paths
				CREATE VIEW IF NOT EXISTS active_edges AS
					SELECT * FROM edges WHERE expires_at IS NULL OR expires_at > unixepoch();
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
			rows, err := s.db.QueryContext(ctx, `PRAGMA table_info(nodes)`)
			if err != nil {
				return fmt.Errorf("checking table info for migration %d: %w", m.version, err)
			}
			for rows.Next() {
				var cid int
				var name, ctype string
				var notnull, pk int
				var dfltValue any
				if err := rows.Scan(&cid, &name, &ctype, &notnull, &dfltValue, &pk); err != nil {
					rows.Close()
					return fmt.Errorf("scanning table info for migration %d: %w", m.version, err)
				}
				if name == "root" {
					hasRoot = true
					break
				}
			}
			rows.Close()

			if hasRoot {
				// Column already exists (from migration 1 on fresh db), just record version
				if _, err := s.db.ExecContext(ctx, `INSERT INTO schema_version (version) VALUES (?)`, m.version); err != nil {
					return fmt.Errorf("recording migration version %d: %w", m.version, err)
				}
				continue
			}
		}

		if _, err := s.db.ExecContext(ctx, m.sql); err != nil {
			return fmt.Errorf("executing migration %d: %w", m.version, err)
		}
		if _, err := s.db.ExecContext(ctx, `INSERT INTO schema_version (version) VALUES (?)`, m.version); err != nil {
			return fmt.Errorf("recording migration version %d: %w", m.version, err)
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
// Returns any error that occurred during the flush (e.g. SQLITE_BUSY after retries).
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
			// Read the error that flushBatch stored before closing done.
			s.flushMu.Lock()
			err := s.lastFlushErr
			s.flushMu.Unlock()
			return err
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
		var flushErr error
		if len(batch) > 0 {
			flushErr = s.flushBatch(batch)
			s.pendingOps.Add(-int64(len(batch)))
			batch = batch[:0]
		}
		// Store the flush result so Flush() callers can read it after waking up.
		s.flushMu.Lock()
		s.lastFlushErr = flushErr
		s.flushMu.Unlock()
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

// isSQLiteBusy reports whether err is a SQLite "database is locked" (SQLITE_BUSY) error.
// Uses string matching so we don't need to import the modernc sqlite package directly.
func isSQLiteBusy(err error) bool {
	return err != nil && strings.Contains(err.Error(), "SQLITE_BUSY")
}

// flushBatch writes a batch of operations to the database.
// On SQLITE_BUSY it retries up to 5 times with exponential backoff.
// With busy_timeout=30s set per-connection via DSN, SQLITE_BUSY should be
// rare (only when another process holds a lock >30s). The retries here are
// a defense-in-depth measure.
// Returns any permanent error so the caller (flushLoop) can propagate it to Flush().
func (s *Storage) flushBatch(batch []writeOp) error {
	if len(batch) == 0 {
		return nil
	}

	const maxRetries = 5
	var lastErr error

	for attempt := 0; attempt < maxRetries; attempt++ {
		if attempt > 0 {
			// Exponential backoff: 100ms, 200ms, 400ms, 800ms
			time.Sleep(time.Duration(100*(1<<(attempt-1))) * time.Millisecond)
		}

		lastErr = s.tryFlushBatch(batch)
		if lastErr == nil {
			return nil
		}
		if !isSQLiteBusy(lastErr) {
			break // non-retryable error
		}
		log.Printf("sqlite: SQLITE_BUSY on batch write attempt %d/%d, retrying...", attempt+1, maxRetries)
	}

	log.Printf("sqlite: batch write failed permanently, rolling back %d operations: %v", len(batch), lastErr)
	return lastErr
}

// tryFlushBatch executes a single batch write attempt inside a transaction.
func (s *Storage) tryFlushBatch(batch []writeOp) error {
	ctx := context.Background()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		if isSQLiteBusy(err) {
			return err
		}
		log.Printf("sqlite: failed to begin transaction: %v", err)
		return err
	}
	defer tx.Rollback()

	// Prepare statements for batch insert
	nodeStmt, err := tx.PrepareContext(ctx, `
		INSERT INTO nodes (id, type, uri, key, name, labels, data, generation, created_at, updated_at, expires_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET
			type = excluded.type,
			uri = excluded.uri,
			key = excluded.key,
			name = excluded.name,
			labels = excluded.labels,
			data = excluded.data,
			generation = excluded.generation,
			updated_at = excluded.updated_at,
			expires_at = excluded.expires_at
	`)
	if err != nil {
		log.Printf("sqlite: failed to prepare node statement: %v", err)
		return err
	}
	defer nodeStmt.Close()

	edgeStmt, err := tx.PrepareContext(ctx, `
		INSERT INTO edges (id, type, from_id, to_id, data, generation, created_at, expires_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(type, from_id, to_id) DO UPDATE SET
			data = excluded.data,
			generation = excluded.generation,
			expires_at = excluded.expires_at
	`)
	if err != nil {
		log.Printf("sqlite: failed to prepare edge statement: %v", err)
		return err
	}
	defer edgeStmt.Close()

	// Process operations in order, abort batch on first error
	var batchErr error
	for _, op := range batch {
		if op.Node != nil {
			// Handle data: use SQL NULL if Data is nil, otherwise marshal to JSON
			var dataStr sql.NullString
			if op.Node.Data != nil {
				data, err := json.Marshal(op.Node.Data)
				if err != nil {
					log.Printf("sqlite: failed to marshal node data for %s: %v", op.Node.ID, err)
					batchErr = err
					break
				}
				dataStr = sql.NullString{String: string(data), Valid: true}
			}

			labels, err := json.Marshal(op.Node.Labels)
			if err != nil {
				log.Printf("sqlite: failed to marshal node labels for %s: %v", op.Node.ID, err)
				batchErr = err
				break
			}
			var nodeCreatedAt, nodeUpdatedAt string
			if op.Node.CreatedAt != nil {
				nodeCreatedAt = op.Node.CreatedAt.Format(time.RFC3339)
			}
			if op.Node.UpdatedAt != nil {
				nodeUpdatedAt = op.Node.UpdatedAt.Format(time.RFC3339)
			}
			var nodeExpiresAt sql.NullInt64
			if op.Node.ExpiresAt != nil {
				nodeExpiresAt = sql.NullInt64{Int64: op.Node.ExpiresAt.Unix(), Valid: true}
			}
			_, err = nodeStmt.ExecContext(ctx,
				op.Node.ID, op.Node.Type, op.Node.URI, op.Node.Key, op.Node.Name,
				string(labels), dataStr, op.Node.Generation,
				nodeCreatedAt, nodeUpdatedAt, nodeExpiresAt)
			if err != nil {
				log.Printf("sqlite: failed to insert node %s: %v", op.Node.ID, err)
				batchErr = err
				break
			}
		}
		if op.Edge != nil {
			// Handle data: use SQL NULL if Data is nil, otherwise marshal to JSON
			var dataStr sql.NullString
			if op.Edge.Data != nil {
				dataBytes, err := json.Marshal(op.Edge.Data)
				if err != nil {
					log.Printf("sqlite: failed to marshal edge data for %s: %v", op.Edge.ID, err)
					batchErr = err
					break
				}
				dataStr = sql.NullString{String: string(dataBytes), Valid: true}
			}
			var edgeCreatedAt string
			if op.Edge.CreatedAt != nil {
				edgeCreatedAt = op.Edge.CreatedAt.Format(time.RFC3339)
			}
			var edgeExpiresAt sql.NullInt64
			if op.Edge.ExpiresAt != nil {
				edgeExpiresAt = sql.NullInt64{Int64: op.Edge.ExpiresAt.Unix(), Valid: true}
			}
			_, err = edgeStmt.ExecContext(ctx,
				op.Edge.ID, op.Edge.Type, op.Edge.From, op.Edge.To, dataStr, op.Edge.Generation,
				edgeCreatedAt, edgeExpiresAt)
			if err != nil {
				log.Printf("sqlite: failed to insert edge %s: %v", op.Edge.ID, err)
				batchErr = err
				break
			}
		}
	}

	// If any operation failed, rollback (via defer) instead of committing
	if batchErr != nil {
		return batchErr
	}

	if err := tx.Commit(); err != nil {
		return err
	}
	return nil
}

func (s *Storage) GetNode(ctx context.Context, id string) (*graph.Node, error) {
	// Flush buffer first to ensure we read the latest data
	if err := s.Flush(ctx); err != nil {
		return nil, err
	}

	row := s.db.QueryRowContext(ctx, `
		SELECT id, type, uri, key, name, labels, data, generation, root, created_at, updated_at, expires_at
		FROM active_nodes WHERE id = ?
	`, id)
	return s.scanNode(row)
}

func (s *Storage) GetNodeByURI(ctx context.Context, uri string) (*graph.Node, error) {
	if err := s.Flush(ctx); err != nil {
		return nil, err
	}

	row := s.db.QueryRowContext(ctx, `
		SELECT id, type, uri, key, name, labels, data, generation, root, created_at, updated_at, expires_at
		FROM active_nodes WHERE uri = ?
	`, uri)
	return s.scanNode(row)
}

func (s *Storage) GetNodeByKey(ctx context.Context, nodeType, key string) (*graph.Node, error) {
	if err := s.Flush(ctx); err != nil {
		return nil, err
	}

	row := s.db.QueryRowContext(ctx, `
		SELECT id, type, uri, key, name, labels, data, generation, root, created_at, updated_at, expires_at
		FROM active_nodes WHERE type = ? AND key = ?
	`, nodeType, key)
	return s.scanNode(row)
}

func (s *Storage) scanNode(row *sql.Row) (*graph.Node, error) {
	var node graph.Node
	var labelsStr, createdAt, updatedAt string
	var uri, key, name, dataStr, generation, root sql.NullString
	var expiresAt sql.NullInt64

	err := row.Scan(&node.ID, &node.Type, &uri, &key, &name, &labelsStr, &dataStr, &generation, &root, &createdAt, &updatedAt, &expiresAt)
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

	if dataStr.Valid && dataStr.String != "" {
		var data any
		if err := json.Unmarshal([]byte(dataStr.String), &data); err != nil {
			return nil, err
		}
		node.Data = data
	}

	if t, err2 := time.Parse(time.RFC3339, createdAt); err2 == nil {
		node.CreatedAt = &t
	}
	if t, err2 := time.Parse(time.RFC3339, updatedAt); err2 == nil {
		node.UpdatedAt = &t
	}
	if expiresAt.Valid {
		t := time.Unix(expiresAt.Int64, 0)
		node.ExpiresAt = &t
	}

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
		SELECT id, type, from_id, to_id, data, generation, created_at, expires_at
		FROM active_edges WHERE id = ?
	`, id)

	var edge graph.Edge
	var createdAt string
	var data, generation sql.NullString
	var expiresAt sql.NullInt64

	err := row.Scan(&edge.ID, &edge.Type, &edge.From, &edge.To, &data, &generation, &createdAt, &expiresAt)
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

	if t, err2 := time.Parse(time.RFC3339, createdAt); err2 == nil {
		edge.CreatedAt = &t
	}
	if expiresAt.Valid {
		t := time.Unix(expiresAt.Int64, 0)
		edge.ExpiresAt = &t
	}

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
		SELECT id, type, from_id, to_id, data, generation, created_at, expires_at
		FROM active_edges WHERE from_id = ?
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
		SELECT id, type, from_id, to_id, data, generation, created_at, expires_at
		FROM active_edges WHERE to_id = ?
	`, nodeID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return s.scanEdges(rows)
}

func (s *Storage) scanEdges(rows *sql.Rows) ([]*graph.Edge, error) {
	var edges []*graph.Edge
	for rows.Next() {
		var edge graph.Edge
		var createdAt string
		var data, generation sql.NullString
		var expiresAt sql.NullInt64

		err := rows.Scan(&edge.ID, &edge.Type, &edge.From, &edge.To, &data, &generation, &createdAt, &expiresAt)
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

		if t, err2 := time.Parse(time.RFC3339, createdAt); err2 == nil {
			edge.CreatedAt = &t
		}
		if expiresAt.Valid {
			t := time.Unix(expiresAt.Int64, 0)
			edge.ExpiresAt = &t
		}
		edges = append(edges, &edge)
	}
	return edges, rows.Err()
}

func (s *Storage) FindNodes(ctx context.Context, filter graph.NodeFilter, opts graph.QueryOptions) ([]*graph.Node, error) {
	filter = filter.Normalize()
	if err := s.Flush(ctx); err != nil {
		return nil, err
	}

	query := `SELECT id, type, uri, key, name, labels, data, generation, root, created_at, updated_at, expires_at FROM active_nodes WHERE 1=1`
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
		var labelsStr, createdAt, updatedAt string
		var uri, key, name, dataStr, generation, root sql.NullString
		var expiresAt sql.NullInt64

		err := rows.Scan(&node.ID, &node.Type, &uri, &key, &name, &labelsStr, &dataStr, &generation, &root, &createdAt, &updatedAt, &expiresAt)
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

		if dataStr.Valid && dataStr.String != "" {
			var data any
			if err := json.Unmarshal([]byte(dataStr.String), &data); err != nil {
				return nil, err
			}
			node.Data = data
		}

		if t, err2 := time.Parse(time.RFC3339, createdAt); err2 == nil {
			node.CreatedAt = &t
		}
		if t, err2 := time.Parse(time.RFC3339, updatedAt); err2 == nil {
			node.UpdatedAt = &t
		}
		if expiresAt.Valid {
			t := time.Unix(expiresAt.Int64, 0)
			node.ExpiresAt = &t
		}
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

	if filter.Generation != "" {
		*query += ` AND generation = ?`
		args = append(args, filter.Generation)
	}

	// Root filter: only nodes with no incoming containment edges
	// Note: We only check 'contains' edges, not 'located_at', because
	// 'located_at' represents "X is located at Y" (e.g., repo at directory),
	// not "Y contains X". The root fs:dir may have repos located at it but
	// is still a structural root of the filesystem tree.
	if filter.Root {
		*query += ` AND NOT EXISTS (
			SELECT 1 FROM edges e 
			WHERE e.to_id = nodes.id 
			AND e.type = 'contains'
		)`
	}

	// ExcludeTypes: node type must not be any of the listed values (OR logic)
	if len(filter.ExcludeTypes) > 0 {
		placeholders := make([]string, len(filter.ExcludeTypes))
		for i, t := range filter.ExcludeTypes {
			placeholders[i] = "?"
			args = append(args, t)
		}
		*query += ` AND type NOT IN (` + strings.Join(placeholders, ", ") + `)`
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
	filter = filter.Normalize()
	if err := s.Flush(ctx); err != nil {
		return nil, err
	}

	result := make(map[string]int)

	switch opts.GroupBy {
	case "type":
		query := `SELECT type, COUNT(*) FROM active_nodes WHERE 1=1`
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
		query := `SELECT j.value, COUNT(*) FROM active_nodes, json_each(active_nodes.labels) AS j WHERE j.value IS NOT NULL`
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
		query := `SELECT json_extract(data, '$.ext'), COUNT(*) FROM active_nodes WHERE type = 'fs:file' AND json_extract(data, '$.ext') IS NOT NULL AND json_extract(data, '$.ext') != ''`
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
		query := `SELECT COUNT(*) FROM active_nodes WHERE 1=1`
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
	// Note: INDEXED BY hints cannot be used with views, so we rely on
	// the query planner to choose optimal indexes automatically.
	if needFromJoin && filter.From.URIPrefix != "" {
		// Optimized query: start from nodes, join edges
		switch opts.GroupBy {
		case "type":
			query = `SELECT e.type, COUNT(*) FROM active_nodes nf`
			query += ` JOIN active_edges e ON e.from_id = nf.id`
		default:
			query = `SELECT COUNT(*) FROM active_nodes nf`
			query += ` JOIN active_edges e ON e.from_id = nf.id`
		}

		if needToJoin {
			query += ` JOIN active_nodes nt ON e.to_id = nt.id`
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
			query = `SELECT e.type, COUNT(*) FROM active_edges e`
		default:
			query = `SELECT COUNT(*) FROM active_edges e`
		}

		if needFromJoin {
			query += ` JOIN active_nodes nf ON e.from_id = nf.id`
		}
		if needToJoin {
			query += ` JOIN active_nodes nt ON e.to_id = nt.id`
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

	if filter.Generation != "" {
		*query += fmt.Sprintf(` AND %s.generation = ?`, prefix)
		args = append(args, filter.Generation)
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
		SELECT id, type, uri, key, name, labels, data, generation, root, created_at, updated_at, expires_at
		FROM nodes WHERE uri LIKE ? AND generation != ?
	`, uriPrefix+"%", currentGen)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var nodes []*graph.Node
	for rows.Next() {
		var node graph.Node
		var labelsStr, createdAt, updatedAt string
		var uri, key, name, dataStr, generation, root sql.NullString
		var expiresAt sql.NullInt64

		err := rows.Scan(&node.ID, &node.Type, &uri, &key, &name, &labelsStr, &dataStr, &generation, &root, &createdAt, &updatedAt, &expiresAt)
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

		if dataStr.Valid && dataStr.String != "" {
			var data any
			if err := json.Unmarshal([]byte(dataStr.String), &data); err != nil {
				return nil, err
			}
			node.Data = data
		}

		if t, err2 := time.Parse(time.RFC3339, createdAt); err2 == nil {
			node.CreatedAt = &t
		}
		if t, err2 := time.Parse(time.RFC3339, updatedAt); err2 == nil {
			node.UpdatedAt = &t
		}
		if expiresAt.Valid {
			t := time.Unix(expiresAt.Int64, 0)
			node.ExpiresAt = &t
		}
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

func (s *Storage) FindOrphanedEdges(ctx context.Context) ([]*graph.Edge, error) {
	if err := s.Flush(ctx); err != nil {
		return nil, err
	}

	rows, err := s.db.QueryContext(ctx, `
		SELECT id, type, from_id, to_id, data, generation, created_at, expires_at
		FROM edges
		WHERE from_id NOT IN (SELECT id FROM nodes)
		   OR to_id   NOT IN (SELECT id FROM nodes)
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return s.scanEdges(rows)
}

// DeleteExpired physically removes all nodes and edges whose ExpiresAt is in
// the past. Returns (nodesDeleted, edgesDeleted, error). Called by "axon gc"
// and the background watch-mode GC ticker.
func (s *Storage) DeleteExpired(ctx context.Context) (int64, int64, error) {
	if err := s.Flush(ctx); err != nil {
		return 0, 0, err
	}

	nr, err := s.db.ExecContext(ctx,
		`DELETE FROM nodes WHERE expires_at IS NOT NULL AND expires_at <= unixepoch()`)
	if err != nil {
		return 0, 0, fmt.Errorf("delete expired nodes: %w", err)
	}
	nodesDeleted, err := nr.RowsAffected()
	if err != nil {
		return 0, 0, fmt.Errorf("delete expired nodes rows affected: %w", err)
	}

	er, err := s.db.ExecContext(ctx,
		`DELETE FROM edges WHERE expires_at IS NOT NULL AND expires_at <= unixepoch()`)
	if err != nil {
		return nodesDeleted, 0, fmt.Errorf("delete expired edges: %w", err)
	}
	edgesDeleted, err := er.RowsAffected()
	if err != nil {
		return nodesDeleted, 0, fmt.Errorf("delete expired edges rows affected: %w", err)
	}

	return nodesDeleted, edgesDeleted, nil
}

// CountExpired returns the number of expired nodes and edges without deleting
// them. Used for dry-run reporting.
func (s *Storage) CountExpired(ctx context.Context) (int64, int64, error) {
	if err := s.Flush(ctx); err != nil {
		return 0, 0, err
	}

	var nodesExpired int64
	if err := s.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM nodes WHERE expires_at IS NOT NULL AND expires_at <= unixepoch()`).Scan(&nodesExpired); err != nil {
		return 0, 0, fmt.Errorf("count expired nodes: %w", err)
	}

	var edgesExpired int64
	if err := s.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM edges WHERE expires_at IS NOT NULL AND expires_at <= unixepoch()`).Scan(&edgesExpired); err != nil {
		return nodesExpired, 0, fmt.Errorf("count expired edges: %w", err)
	}

	return nodesExpired, edgesExpired, nil
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

// GetSchemaVersion returns the current database schema version.
func (s *Storage) GetSchemaVersion(ctx context.Context) (int, error) {
	var version int
	err := s.db.QueryRowContext(ctx, `SELECT COALESCE(MAX(version), 0) FROM schema_version`).Scan(&version)
	if err != nil {
		return 0, err
	}
	return version, nil
}

// PutEmbedding stores a vector embedding for a node.
func (s *Storage) PutEmbedding(ctx context.Context, nodeID string, embedding []float32) error {
	// serialize []float32 to []byte (little-endian)
	buf := make([]byte, len(embedding)*4)
	for i, f := range embedding {
		binary.LittleEndian.PutUint32(buf[i*4:], math.Float32bits(f))
	}
	_, err := s.db.ExecContext(ctx,
		`INSERT OR REPLACE INTO embeddings (node_id, dims, data) VALUES (?, ?, ?)`,
		nodeID, len(embedding), buf)
	return err
}

// GetEmbedding retrieves the vector embedding for a node.
func (s *Storage) GetEmbedding(ctx context.Context, nodeID string) ([]float32, error) {
	var dims int
	var data []byte
	err := s.db.QueryRowContext(ctx,
		`SELECT dims, data FROM embeddings WHERE node_id = ?`, nodeID).
		Scan(&dims, &data)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	embedding := make([]float32, dims)
	for i := range embedding {
		embedding[i] = math.Float32frombits(binary.LittleEndian.Uint32(data[i*4:]))
	}
	return embedding, nil
}

// FindSimilar loads all embeddings, computes cosine similarity, and returns top-k results.
func (s *Storage) FindSimilar(ctx context.Context, query []float32, limit int, filter *graph.NodeFilter) ([]*graph.NodeWithScore, error) {
	if filter != nil {
		norm := filter.Normalize()
		filter = &norm
	}
	// First flush to ensure all nodes are committed
	if err := s.Flush(ctx); err != nil {
		return nil, err
	}

	// Load all embeddings
	rows, err := s.db.QueryContext(ctx, `SELECT node_id, dims, data FROM embeddings`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	type candidate struct {
		nodeID string
		score  float32
	}
	var candidates []candidate

	for rows.Next() {
		var nodeID string
		var dims int
		var data []byte
		if err := rows.Scan(&nodeID, &dims, &data); err != nil {
			return nil, err
		}
		embedding := make([]float32, dims)
		for i := range embedding {
			embedding[i] = math.Float32frombits(binary.LittleEndian.Uint32(data[i*4:]))
		}
		score := cosineSimilarity(query, embedding)
		candidates = append(candidates, candidate{nodeID: nodeID, score: score})
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	// Sort by score descending
	sort.Slice(candidates, func(i, j int) bool {
		return candidates[i].score > candidates[j].score
	})

	// Iterate sorted candidates, apply filter, and stop once limit is reached.
	// (Do NOT truncate before filtering — the filter may discard many candidates.)
	var results []*graph.NodeWithScore
	for _, c := range candidates {
		if limit > 0 && len(results) >= limit {
			break
		}
		node, err := s.GetNode(ctx, c.nodeID)
		if err != nil || node == nil {
			continue
		}
		if filter != nil && !nodeMatchesFilter(node, filter) {
			continue
		}
		results = append(results, &graph.NodeWithScore{Node: node, Score: c.score})
	}

	return results, nil
}

// cosineSimilarity computes the cosine similarity between two vectors.
func cosineSimilarity(a, b []float32) float32 {
	if len(a) != len(b) || len(a) == 0 {
		return 0
	}
	var dot, normA, normB float64
	for i := range a {
		dot += float64(a[i]) * float64(b[i])
		normA += float64(a[i]) * float64(a[i])
		normB += float64(b[i]) * float64(b[i])
	}
	if normA == 0 || normB == 0 {
		return 0
	}
	return float32(dot / (math.Sqrt(normA) * math.Sqrt(normB)))
}

// nodeMatchesFilter returns true if the node satisfies the given filter.
func nodeMatchesFilter(node *graph.Node, filter *graph.NodeFilter) bool {
	if filter.Type != "" && node.Type != filter.Type {
		return false
	}
	for _, excluded := range filter.ExcludeTypes {
		if node.Type == excluded {
			return false
		}
	}
	if filter.URIPrefix != "" && !strings.HasPrefix(node.URI, filter.URIPrefix) {
		return false
	}
	if len(filter.Labels) > 0 {
		found := false
	outer:
		for _, want := range filter.Labels {
			for _, have := range node.Labels {
				if have == want {
					found = true
					break outer
				}
			}
		}
		if !found {
			return false
		}
	}
	if len(filter.Extensions) > 0 {
		ext := ""
		if m, ok := node.Data.(map[string]any); ok {
			ext, _ = m["ext"].(string)
		}
		found := false
		for _, want := range filter.Extensions {
			if ext == want {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}
	return true
}
