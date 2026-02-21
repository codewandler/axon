package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
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
)

// Storage is a SQLite implementation of the graph.Storage interface.
// It buffers writes and flushes them in batches for performance.
type Storage struct {
	db   *sql.DB
	path string

	// Write buffer
	mu         sync.Mutex
	nodeBuffer []*graph.Node
	edgeBuffer []*graph.Edge
	lastFlush  time.Time
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
		db:        db,
		path:      path,
		lastFlush: time.Now(),
	}
	if err := s.init(); err != nil {
		db.Close()
		return nil, err
	}

	return s, nil
}

// Close flushes any pending writes and closes the database connection.
func (s *Storage) Close() error {
	if err := s.Flush(context.Background()); err != nil {
		s.db.Close()
		return err
	}
	return s.db.Close()
}

func (s *Storage) init() error {
	// Enable WAL mode and performance settings
	_, err := s.db.Exec(`
		PRAGMA journal_mode=WAL;
		PRAGMA synchronous=NORMAL;
		PRAGMA cache_size=10000;
		PRAGMA temp_store=MEMORY;
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
	s.mu.Lock()
	defer s.mu.Unlock()

	// Copy the node to avoid external mutation
	nodeCopy := *node
	s.nodeBuffer = append(s.nodeBuffer, &nodeCopy)

	if s.shouldFlush() {
		return s.flushLocked(ctx)
	}
	return nil
}

// shouldFlush returns true if the buffer should be flushed.
// Must be called with mu held.
func (s *Storage) shouldFlush() bool {
	total := len(s.nodeBuffer) + len(s.edgeBuffer)
	return total >= flushThreshold || time.Since(s.lastFlush) > flushInterval
}

// Flush writes any buffered data to the database.
func (s *Storage) Flush(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.flushLocked(ctx)
}

// flushLocked writes buffered data to the database.
// Must be called with mu held.
func (s *Storage) flushLocked(ctx context.Context) error {
	if len(s.nodeBuffer) == 0 && len(s.edgeBuffer) == 0 {
		return nil
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	// Prepare statements for batch insert
	nodeStmt, err := tx.PrepareContext(ctx, `
		INSERT INTO nodes (id, type, uri, key, data, generation, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET
			type = excluded.type,
			uri = excluded.uri,
			key = excluded.key,
			data = excluded.data,
			generation = excluded.generation,
			updated_at = excluded.updated_at
	`)
	if err != nil {
		return err
	}
	defer nodeStmt.Close()

	edgeStmt, err := tx.PrepareContext(ctx, `
		INSERT INTO edges (id, type, from_id, to_id, data, generation, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET
			type = excluded.type,
			from_id = excluded.from_id,
			to_id = excluded.to_id,
			data = excluded.data,
			generation = excluded.generation
	`)
	if err != nil {
		return err
	}
	defer edgeStmt.Close()

	// Insert nodes
	for _, node := range s.nodeBuffer {
		data, err := json.Marshal(node.Data)
		if err != nil {
			return err
		}
		_, err = nodeStmt.ExecContext(ctx,
			node.ID, node.Type, node.URI, node.Key, string(data), node.Generation,
			node.CreatedAt.Format(time.RFC3339), node.UpdatedAt.Format(time.RFC3339))
		if err != nil {
			return err
		}
	}

	// Insert edges
	for _, edge := range s.edgeBuffer {
		var data string
		if edge.Data != nil {
			dataBytes, err := json.Marshal(edge.Data)
			if err != nil {
				return err
			}
			data = string(dataBytes)
		}
		_, err = edgeStmt.ExecContext(ctx,
			edge.ID, edge.Type, edge.From, edge.To, data, edge.Generation,
			edge.CreatedAt.Format(time.RFC3339))
		if err != nil {
			return err
		}
	}

	if err := tx.Commit(); err != nil {
		return err
	}

	// Clear buffers
	s.nodeBuffer = s.nodeBuffer[:0]
	s.edgeBuffer = s.edgeBuffer[:0]
	s.lastFlush = time.Now()

	return nil
}

func (s *Storage) GetNode(ctx context.Context, id string) (*graph.Node, error) {
	// Flush buffer first to ensure we read the latest data
	if err := s.Flush(ctx); err != nil {
		return nil, err
	}

	row := s.db.QueryRowContext(ctx, `
		SELECT id, type, uri, key, data, generation, root, created_at, updated_at
		FROM nodes WHERE id = ?
	`, id)
	return s.scanNode(row)
}

func (s *Storage) GetNodeByURI(ctx context.Context, uri string) (*graph.Node, error) {
	if err := s.Flush(ctx); err != nil {
		return nil, err
	}

	row := s.db.QueryRowContext(ctx, `
		SELECT id, type, uri, key, data, generation, root, created_at, updated_at
		FROM nodes WHERE uri = ?
	`, uri)
	return s.scanNode(row)
}

func (s *Storage) GetNodeByKey(ctx context.Context, nodeType, key string) (*graph.Node, error) {
	if err := s.Flush(ctx); err != nil {
		return nil, err
	}

	row := s.db.QueryRowContext(ctx, `
		SELECT id, type, uri, key, data, generation, root, created_at, updated_at
		FROM nodes WHERE type = ? AND key = ?
	`, nodeType, key)
	return s.scanNode(row)
}

func (s *Storage) scanNode(row *sql.Row) (*graph.Node, error) {
	var node graph.Node
	var dataStr, createdAt, updatedAt string
	var uri, key, generation, root sql.NullString

	err := row.Scan(&node.ID, &node.Type, &uri, &key, &dataStr, &generation, &root, &createdAt, &updatedAt)
	if err == sql.ErrNoRows {
		return nil, storage.ErrNodeNotFound
	}
	if err != nil {
		return nil, err
	}

	node.URI = uri.String
	node.Key = key.String
	node.Generation = generation.String
	_ = root // Column still exists in schema but no longer used

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
	s.mu.Lock()
	defer s.mu.Unlock()

	// Copy the edge to avoid external mutation
	edgeCopy := *edge
	s.edgeBuffer = append(s.edgeBuffer, &edgeCopy)

	if s.shouldFlush() {
		return s.flushLocked(ctx)
	}
	return nil
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

func (s *Storage) FindNodes(ctx context.Context, filter graph.NodeFilter) ([]*graph.Node, error) {
	if err := s.Flush(ctx); err != nil {
		return nil, err
	}

	query := `SELECT id, type, uri, key, data, generation, root, created_at, updated_at FROM nodes WHERE 1=1`
	var args []any

	if filter.Type != "" {
		query += ` AND type = ?`
		args = append(args, filter.Type)
	}
	if filter.URIPrefix != "" {
		query += ` AND uri LIKE ?`
		args = append(args, filter.URIPrefix+"%")
	}

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var nodes []*graph.Node
	for rows.Next() {
		var node graph.Node
		var dataStr, createdAt, updatedAt string
		var uri, key, generation, root sql.NullString

		err := rows.Scan(&node.ID, &node.Type, &uri, &key, &dataStr, &generation, &root, &createdAt, &updatedAt)
		if err != nil {
			return nil, err
		}

		node.URI = uri.String
		node.Key = key.String
		node.Generation = generation.String
		_ = root // Column still exists in schema but no longer used

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

func (s *Storage) FindStaleByURIPrefix(ctx context.Context, uriPrefix, currentGen string) ([]*graph.Node, error) {
	if err := s.Flush(ctx); err != nil {
		return nil, err
	}

	rows, err := s.db.QueryContext(ctx, `
		SELECT id, type, uri, key, data, generation, root, created_at, updated_at
		FROM nodes WHERE uri LIKE ? AND generation != ?
	`, uriPrefix+"%", currentGen)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var nodes []*graph.Node
	for rows.Next() {
		var node graph.Node
		var dataStr, createdAt, updatedAt string
		var uri, key, generation, root sql.NullString

		err := rows.Scan(&node.ID, &node.Type, &uri, &key, &dataStr, &generation, &root, &createdAt, &updatedAt)
		if err != nil {
			return nil, err
		}

		node.URI = uri.String
		node.Key = key.String
		node.Generation = generation.String

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

// URIPrefix helper for building LIKE patterns
func URIPrefix(uri string) string {
	if strings.HasSuffix(uri, "/") {
		return uri
	}
	return uri + "/"
}
