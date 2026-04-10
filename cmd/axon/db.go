package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/codewandler/axon"
	"github.com/codewandler/axon/adapters/sqlite"
)

const (
	axonDir    = ".axon"
	axonDBFile = "graph.db"
)

// ErrNoDatabase is returned when no database can be found.
var ErrNoDatabase = errors.New("no database found in current directory. Run 'axon init' first, or use --global to search parent directories")

// DBLocation contains information about a resolved database location.
type DBLocation struct {
	// Path is the full path to the database file.
	Path string
	// Dir is the directory containing the database file.
	Dir string
	// IsGlobal indicates if this is the global database (~/.axon).
	IsGlobal bool
	// Created indicates if the directory was created (for init).
	Created bool
}

// findDB searches for an existing database starting from startPath.
// It walks up the directory tree looking for .axon/graph.db, then falls back to ~/.axon/graph.db.
// Returns ErrNoDatabase if no database is found.
func findDB(startPath string) (*DBLocation, error) {
	// Walk up from startPath
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
			// Reached root, try global
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

// resolveDB resolves the database location based on flags.
//
// Parameters:
//   - dbDir: explicit --db-dir flag value (empty = not set)
//   - global: --global flag; walk up the directory tree then fall back to ~/.axon
//   - startPath: directory to start lookup from (CWD for most commands)
//   - forWrite: if true, create the directory when it doesn't exist
//
// Precedence:
//  1. --db-dir flag (explicit directory)
//  2. --global flag (walk up from startPath, then ~/.axon fallback)
//  3. Default: use startPath/.axon directly — no traversal
func resolveDB(dbDir string, global bool, startPath string, forWrite bool) (*DBLocation, error) {
	// 1. Explicit --db-dir flag
	if dbDir != "" {
		absDir, err := filepath.Abs(dbDir)
		if err != nil {
			return nil, err
		}
		dbPath := filepath.Join(absDir, axonDBFile)

		if forWrite {
			if err := os.MkdirAll(absDir, 0755); err != nil {
				return nil, err
			}
		}

		return &DBLocation{
			Path:     dbPath,
			Dir:      absDir,
			IsGlobal: false,
		}, nil
	}

	absPath, err := filepath.Abs(startPath)
	if err != nil {
		return nil, err
	}

	// 2. --global: walk up the directory tree, then fall back to ~/.axon
	if global {
		loc, err := findDB(absPath)
		if err == nil {
			return loc, nil
		}

		if forWrite {
			homeDir, err := os.UserHomeDir()
			if err != nil {
				return nil, err
			}
			globalDir := filepath.Join(homeDir, axonDir)
			dbPath := filepath.Join(globalDir, axonDBFile)
			created := false
			if _, err := os.Stat(globalDir); os.IsNotExist(err) {
				if err := os.MkdirAll(globalDir, 0755); err != nil {
					return nil, err
				}
				created = true
			}
			return &DBLocation{Path: dbPath, Dir: globalDir, IsGlobal: true, Created: created}, nil
		}

		return nil, ErrNoDatabase
	}

	// 3. Default: use startPath/.axon directly — no traversal
	localDir := filepath.Join(absPath, axonDir)
	dbPath := filepath.Join(localDir, axonDBFile)

	if forWrite {
		created := false
		if _, err := os.Stat(localDir); os.IsNotExist(err) {
			if err := os.MkdirAll(localDir, 0755); err != nil {
				return nil, err
			}
			created = true
		}
		return &DBLocation{Path: dbPath, Dir: localDir, IsGlobal: false, Created: created}, nil
	}

	if _, err := os.Stat(dbPath); err != nil {
		return nil, ErrNoDatabase
	}
	return &DBLocation{Path: dbPath, Dir: localDir, IsGlobal: false}, nil
}

// CommandContext holds common resources for CLI commands.
// Use openDB() to create one. Always call Close() when done.
type CommandContext struct {
	// Ctx is a background context for the command.
	Ctx context.Context
	// Cwd is the current working directory.
	Cwd string
	// DBLoc contains database location information.
	DBLoc *DBLocation
	// Storage is the SQLite storage instance.
	Storage *sqlite.Storage
	// ax is the Axon instance (optional, created lazily).
	ax *axon.Axon
}

// Axon returns the Axon instance, creating it lazily if needed.
func (c *CommandContext) Axon() (*axon.Axon, error) {
	if c.ax != nil {
		return c.ax, nil
	}
	ax, err := axon.New(axon.Config{
		Dir:     c.Cwd,
		Storage: c.Storage,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create axon instance: %w", err)
	}
	c.ax = ax
	return ax, nil
}

// Close closes the storage connection.
func (c *CommandContext) Close() error {
	if c.Storage != nil {
		return c.Storage.Close()
	}
	return nil
}

// openDB opens the database and returns a CommandContext.
// The caller must call Close() when done (typically via defer).
//
// Parameters:
//   - forWrite: if true, creates the database directory if needed
//
// Example:
//
//	cmdCtx, err := openDB(false)
//	if err != nil {
//	    return err
//	}
//	defer cmdCtx.Close()
func openDB(forWrite bool) (*CommandContext, error) {
	ctx := context.Background()

	cwd, err := os.Getwd()
	if err != nil {
		return nil, fmt.Errorf("failed to get current directory: %w", err)
	}

	dbLoc, err := resolveDB(flagDBDir, flagGlobal, cwd, forWrite)
	if err != nil {
		return nil, err
	}

	storage, err := sqlite.New(dbLoc.Path)
	if err != nil {
		return nil, fmt.Errorf("failed to open database: %w", err)
	}

	return &CommandContext{
		Ctx:     ctx,
		Cwd:     cwd,
		DBLoc:   dbLoc,
		Storage: storage,
	}, nil
}
