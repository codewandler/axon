package main

import (
	"errors"
	"os"
	"path/filepath"
)

const (
	axonDir    = ".axon"
	axonDBFile = "graph.db"
)

// ErrNoDatabase is returned when no database can be found.
var ErrNoDatabase = errors.New("no database found. Run 'axon init' first")

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

// resolveDB resolves the database location based on flags and auto-lookup.
//
// Parameters:
//   - dbDir: explicit --db-dir flag value (empty = not set)
//   - local: --local flag (use <startPath>/.axon)
//   - startPath: path to start lookup from (usually target path for init, CWD for read commands)
//   - forWrite: if true and no DB found, returns global path and creates dir; if false, returns error
//
// Precedence:
//  1. --db-dir flag (explicit directory)
//  2. --local flag (startPath/.axon)
//  3. Auto-lookup (walk up from startPath, then global)
//  4. If forWrite and not found: create global
func resolveDB(dbDir string, local bool, startPath string, forWrite bool) (*DBLocation, error) {
	// 1. Explicit --db-dir flag
	if dbDir != "" {
		absDir, err := filepath.Abs(dbDir)
		if err != nil {
			return nil, err
		}
		dbPath := filepath.Join(absDir, axonDBFile)

		// For write operations, ensure directory exists
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

	// 2. --local flag
	if local {
		absPath, err := filepath.Abs(startPath)
		if err != nil {
			return nil, err
		}
		localDir := filepath.Join(absPath, axonDir)
		dbPath := filepath.Join(localDir, axonDBFile)

		// For write operations, ensure directory exists
		created := false
		if forWrite {
			if _, err := os.Stat(localDir); os.IsNotExist(err) {
				if err := os.MkdirAll(localDir, 0755); err != nil {
					return nil, err
				}
				created = true
			}
		}

		return &DBLocation{
			Path:     dbPath,
			Dir:      localDir,
			IsGlobal: false,
			Created:  created,
		}, nil
	}

	// 3. Auto-lookup
	absPath, err := filepath.Abs(startPath)
	if err != nil {
		return nil, err
	}

	loc, err := findDB(absPath)
	if err == nil {
		return loc, nil
	}

	// 4. Not found - for write operations, use global
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

		return &DBLocation{
			Path:     dbPath,
			Dir:      globalDir,
			IsGlobal: true,
			Created:  created,
		}, nil
	}

	return nil, ErrNoDatabase
}
