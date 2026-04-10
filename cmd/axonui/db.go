package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/codewandler/axon/adapters/sqlite"
	"github.com/codewandler/axon/graph"
	"github.com/codewandler/axon/types"
)

const (
	axonDir    = ".axon"
	axonDBFile = "graph.db"
)

// ErrNoDatabase is returned when no database can be found.
var ErrNoDatabase = errors.New("no database found. Run 'axon init' first")

// DBLocation contains information about a resolved database location.
type DBLocation struct {
	Path     string
	Dir      string
	IsGlobal bool
}

// findDB searches for an existing database starting from startPath.
// It walks up the directory tree looking for .axon/graph.db, then falls back to ~/.axon/graph.db.
func findDB(startPath string) (*DBLocation, error) {
	path := startPath
	for {
		dbPath := filepath.Join(path, axonDir, axonDBFile)
		if _, err := os.Stat(dbPath); err == nil {
			return &DBLocation{
				Path:     dbPath,
				Dir:      filepath.Join(path, axonDir),
				IsGlobal: false,
			}, nil
		}

		parent := filepath.Dir(path)
		if parent == path {
			break
		}
		path = parent
	}

	// Try global database
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return nil, ErrNoDatabase
	}

	globalPath := filepath.Join(homeDir, axonDir, axonDBFile)
	if _, err := os.Stat(globalPath); err == nil {
		return &DBLocation{
			Path:     globalPath,
			Dir:      filepath.Join(homeDir, axonDir),
			IsGlobal: true,
		}, nil
	}

	return nil, ErrNoDatabase
}

// resolveDB resolves the database location based on flags and auto-lookup.
func resolveDB(dbDir string, startPath string) (*DBLocation, error) {
	// Explicit --db-dir flag
	if dbDir != "" {
		absDir, err := filepath.Abs(dbDir)
		if err != nil {
			return nil, err
		}
		dbPath := filepath.Join(absDir, axonDBFile)
		return &DBLocation{
			Path:     dbPath,
			Dir:      absDir,
			IsGlobal: false,
		}, nil
	}

	// Auto-lookup
	absPath, err := filepath.Abs(startPath)
	if err != nil {
		return nil, err
	}

	return findDB(absPath)
}

// appContext holds the application context for the TUI.
type appContext struct {
	Ctx     context.Context
	Cwd     string
	DBLoc   *DBLocation
	Storage *sqlite.Storage
	Graph   *graph.Graph
}

// newAppContext creates a new application context by resolving and opening the database.
func newAppContext(dbDir string) (*appContext, error) {
	ctx := context.Background()

	cwd, err := os.Getwd()
	if err != nil {
		return nil, fmt.Errorf("failed to get current directory: %w", err)
	}

	dbLoc, err := resolveDB(dbDir, cwd)
	if err != nil {
		return nil, err
	}

	storage, err := sqlite.New(dbLoc.Path)
	if err != nil {
		return nil, fmt.Errorf("failed to open database: %w", err)
	}

	// Create graph with registry for read-only access
	registry := graph.NewRegistry()
	types.RegisterCommonEdges(registry)
	types.RegisterFSTypes(registry)
	types.RegisterVCSTypes(registry)
	types.RegisterMarkdownTypes(registry)

	g := graph.New(storage, registry)

	return &appContext{
		Ctx:     ctx,
		Cwd:     cwd,
		DBLoc:   dbLoc,
		Storage: storage,
		Graph:   g,
	}, nil
}

// Close closes the storage connection.
func (a *appContext) Close() error {
	if a.Storage != nil {
		return a.Storage.Close()
	}
	return nil
}
