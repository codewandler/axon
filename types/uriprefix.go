package types

import (
	"os"
	"strings"
)

// URIPrefixForType returns the URI prefix for scoping a node search to a
// specific directory. Each indexer uses a distinct URI scheme, so the prefix
// must match the scheme for the node type family:
//
//   - go:*    → go+file://<dir>
//   - vcs:*   → git+file://<dir>
//   - md:*    → file+md://<dir>
//   - fs:*, * → file://<dir>
//
// workDir is optional. When omitted or empty the current working directory is
// used, so callers that operate in the CWD do not need to supply it:
//
//	// explicit directory
//	prefix := types.URIPrefixForType("go:func", "/home/user/myrepo")
//
//	// infer from CWD
//	prefix := types.URIPrefixForType("go:func")
func URIPrefixForType(nodeType string, workDir ...string) string {
	dir := ""
	if len(workDir) > 0 {
		dir = workDir[0]
	}
	if dir == "" {
		if cwd, err := os.Getwd(); err == nil {
			dir = cwd
		}
	}

	switch {
	case strings.HasPrefix(nodeType, "go:"):
		return "go+file://" + dir
	case strings.HasPrefix(nodeType, "vcs:"):
		return "git+file://" + dir
	case strings.HasPrefix(nodeType, "md:"):
		return "file+md://" + dir
	default:
		return PathToURI(dir) // file://
	}
}
