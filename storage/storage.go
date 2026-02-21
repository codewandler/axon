package storage

import "errors"

// Common errors returned by storage implementations.
var (
	ErrNodeNotFound = errors.New("node not found")
	ErrEdgeNotFound = errors.New("edge not found")
)
