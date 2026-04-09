package tagger

import (
	"context"
	"path/filepath"
	"strings"

	"github.com/codewandler/axon/graph"
	"github.com/codewandler/axon/indexer"
	"github.com/codewandler/axon/types"
)

// Rule defines a labeling rule based on file patterns.
type Rule struct {
	// Type filters by exact node type (e.g., "fs:file"). Empty matches all types.
	Type string

	// NamePattern filters by glob pattern on the filename (e.g., "*.yml", "AGENTS.md").
	// Uses filepath.Match syntax. Empty matches all names.
	NamePattern string

	// PathPattern filters by glob pattern on the full path (e.g., ".github/workflows/*").
	// Supports ** for recursive matching. Empty matches all paths.
	PathPattern string

	// Labels are the labels to apply when this rule matches.
	Labels []string
}

// Matches returns true if the rule matches the given node and path.
func (r Rule) Matches(nodeType, name, path string) bool {
	// Type filter
	if r.Type != "" && r.Type != nodeType {
		return false
	}

	// Name pattern filter
	if r.NamePattern != "" {
		matched, _ := filepath.Match(r.NamePattern, name)
		if !matched {
			return false
		}
	}

	// Path pattern filter (with ** support)
	if r.PathPattern != "" {
		if !matchPathPattern(r.PathPattern, path) {
			return false
		}
	}

	return true
}

// matchPathPattern matches a path against a pattern that may contain **.
// ** matches zero or more directory components.
func matchPathPattern(pattern, path string) bool {
	// Normalize separators
	pattern = filepath.ToSlash(pattern)
	path = filepath.ToSlash(path)

	// Split into segments
	patternParts := strings.Split(pattern, "/")
	pathParts := strings.Split(path, "/")

	return matchParts(patternParts, pathParts)
}

// matchParts recursively matches pattern segments against path segments.
func matchParts(pattern, path []string) bool {
	for len(pattern) > 0 && len(path) > 0 {
		p := pattern[0]

		if p == "**" {
			// ** matches zero or more path segments
			// Try matching remaining pattern at each position
			for i := 0; i <= len(path); i++ {
				if matchParts(pattern[1:], path[i:]) {
					return true
				}
			}
			return false
		}

		// Regular glob match on this segment
		matched, _ := filepath.Match(p, path[0])
		if !matched {
			return false
		}

		pattern = pattern[1:]
		path = path[1:]
	}

	// Pattern exhausted - check if all ** or empty
	for _, p := range pattern {
		if p != "**" {
			return false
		}
	}

	return true
}

// Config holds configuration for the tagger indexer.
type Config struct {
	// Rules are the labeling rules to apply.
	// If empty, DefaultRules is used.
	Rules []Rule
}

// DefaultRules provides a sensible set of labeling rules.
var DefaultRules = []Rule{
	// Agent instructions
	{Type: types.TypeFile, NamePattern: "AGENTS.md", Labels: []string{"agent:instructions"}},
	{Type: types.TypeFile, NamePattern: "CLAUDE.md", Labels: []string{"agent:instructions"}},
	{Type: types.TypeFile, NamePattern: "CURSOR.md", Labels: []string{"agent:instructions"}},
	{Type: types.TypeFile, NamePattern: ".cursorrules", Labels: []string{"agent:instructions"}},
	{Type: types.TypeFile, NamePattern: ".clauderules", Labels: []string{"agent:instructions"}},
	{Type: types.TypeFile, NamePattern: "COPILOT.md", Labels: []string{"agent:instructions"}},
	{Type: types.TypeFile, NamePattern: ".github/copilot-instructions.md", Labels: []string{"agent:instructions"}},

	// CI/CD - GitLab
	{Type: types.TypeFile, NamePattern: ".gitlab-ci.yml", Labels: []string{"ci:config", "ci:gitlab"}},
	{Type: types.TypeFile, NamePattern: ".gitlab-ci.yaml", Labels: []string{"ci:config", "ci:gitlab"}},

	// CI/CD - Jenkins
	{Type: types.TypeFile, NamePattern: "Jenkinsfile", Labels: []string{"ci:config", "ci:jenkins"}},

	// CI/CD - GitHub Actions
	{Type: types.TypeFile, PathPattern: ".github/workflows/*.yml", Labels: []string{"ci:config", "ci:github"}},
	{Type: types.TypeFile, PathPattern: ".github/workflows/*.yaml", Labels: []string{"ci:config", "ci:github"}},

	// CI/CD - CircleCI
	{Type: types.TypeFile, PathPattern: ".circleci/config.yml", Labels: []string{"ci:config", "ci:circleci"}},

	// Containers - Dockerfile
	{Type: types.TypeFile, NamePattern: "Dockerfile", Labels: []string{"container:dockerfile"}},
	{Type: types.TypeFile, NamePattern: "Dockerfile.*", Labels: []string{"container:dockerfile"}},
	{Type: types.TypeFile, NamePattern: "*.Dockerfile", Labels: []string{"container:dockerfile"}},

	// Containers - Compose
	{Type: types.TypeFile, NamePattern: "docker-compose.yml", Labels: []string{"container:compose"}},
	{Type: types.TypeFile, NamePattern: "docker-compose.yaml", Labels: []string{"container:compose"}},
	{Type: types.TypeFile, NamePattern: "compose.yml", Labels: []string{"container:compose"}},
	{Type: types.TypeFile, NamePattern: "compose.yaml", Labels: []string{"container:compose"}},

	// Kubernetes - Helm
	{Type: types.TypeFile, NamePattern: "Chart.yaml", Labels: []string{"k8s:helm"}},
	{Type: types.TypeFile, NamePattern: "values.yaml", Labels: []string{"k8s:helm"}},
	{Type: types.TypeFile, NamePattern: "values.yml", Labels: []string{"k8s:helm"}},
	{Type: types.TypeFile, PathPattern: "templates/*.yaml", Labels: []string{"k8s:helm"}},
	{Type: types.TypeFile, PathPattern: "templates/*.yml", Labels: []string{"k8s:helm"}},

	// Kubernetes - Manifests (in k8s directories)
	{Type: types.TypeFile, PathPattern: "**/k8s/**/*.yaml", Labels: []string{"k8s:manifest"}},
	{Type: types.TypeFile, PathPattern: "**/k8s/**/*.yml", Labels: []string{"k8s:manifest"}},
	{Type: types.TypeFile, PathPattern: "**/kubernetes/**/*.yaml", Labels: []string{"k8s:manifest"}},
	{Type: types.TypeFile, PathPattern: "**/kubernetes/**/*.yml", Labels: []string{"k8s:manifest"}},

	// Build configs - Go
	{Type: types.TypeFile, NamePattern: "go.mod", Labels: []string{"build:config", "lang:go"}},
	{Type: types.TypeFile, NamePattern: "go.sum", Labels: []string{"build:lockfile", "lang:go"}},

	// Build configs - Node.js
	{Type: types.TypeFile, NamePattern: "package.json", Labels: []string{"build:config", "lang:javascript"}},
	{Type: types.TypeFile, NamePattern: "package-lock.json", Labels: []string{"build:lockfile", "lang:javascript"}},
	{Type: types.TypeFile, NamePattern: "yarn.lock", Labels: []string{"build:lockfile", "lang:javascript"}},
	{Type: types.TypeFile, NamePattern: "pnpm-lock.yaml", Labels: []string{"build:lockfile", "lang:javascript"}},
	{Type: types.TypeFile, NamePattern: "bun.lockb", Labels: []string{"build:lockfile", "lang:javascript"}},

	// Build configs - Rust
	{Type: types.TypeFile, NamePattern: "Cargo.toml", Labels: []string{"build:config", "lang:rust"}},
	{Type: types.TypeFile, NamePattern: "Cargo.lock", Labels: []string{"build:lockfile", "lang:rust"}},

	// Build configs - Python
	{Type: types.TypeFile, NamePattern: "pyproject.toml", Labels: []string{"build:config", "lang:python"}},
	{Type: types.TypeFile, NamePattern: "setup.py", Labels: []string{"build:config", "lang:python"}},
	{Type: types.TypeFile, NamePattern: "setup.cfg", Labels: []string{"build:config", "lang:python"}},
	{Type: types.TypeFile, NamePattern: "requirements.txt", Labels: []string{"build:lockfile", "lang:python"}},
	{Type: types.TypeFile, NamePattern: "requirements*.txt", Labels: []string{"build:lockfile", "lang:python"}},
	{Type: types.TypeFile, NamePattern: "Pipfile", Labels: []string{"build:config", "lang:python"}},
	{Type: types.TypeFile, NamePattern: "Pipfile.lock", Labels: []string{"build:lockfile", "lang:python"}},
	{Type: types.TypeFile, NamePattern: "poetry.lock", Labels: []string{"build:lockfile", "lang:python"}},

	// Build configs - Ruby
	{Type: types.TypeFile, NamePattern: "Gemfile", Labels: []string{"build:config", "lang:ruby"}},
	{Type: types.TypeFile, NamePattern: "Gemfile.lock", Labels: []string{"build:lockfile", "lang:ruby"}},

	// Build configs - Java/Kotlin
	{Type: types.TypeFile, NamePattern: "pom.xml", Labels: []string{"build:config", "lang:java"}},
	{Type: types.TypeFile, NamePattern: "build.gradle", Labels: []string{"build:config", "lang:java"}},
	{Type: types.TypeFile, NamePattern: "build.gradle.kts", Labels: []string{"build:config", "lang:kotlin"}},
	{Type: types.TypeFile, NamePattern: "settings.gradle", Labels: []string{"build:config", "lang:java"}},
	{Type: types.TypeFile, NamePattern: "settings.gradle.kts", Labels: []string{"build:config", "lang:kotlin"}},

	// Build tools
	{Type: types.TypeFile, NamePattern: "Makefile", Labels: []string{"build:makefile"}},
	{Type: types.TypeFile, NamePattern: "GNUmakefile", Labels: []string{"build:makefile"}},
	{Type: types.TypeFile, NamePattern: "justfile", Labels: []string{"build:justfile"}},
	{Type: types.TypeFile, NamePattern: "Justfile", Labels: []string{"build:justfile"}},
	{Type: types.TypeFile, NamePattern: "Taskfile.yml", Labels: []string{"build:taskfile"}},
	{Type: types.TypeFile, NamePattern: "Taskfile.yaml", Labels: []string{"build:taskfile"}},

	// Documentation
	{Type: types.TypeFile, NamePattern: "README*", Labels: []string{"docs:readme"}},
	{Type: types.TypeFile, NamePattern: "readme*", Labels: []string{"docs:readme"}},
	{Type: types.TypeFile, NamePattern: "CHANGELOG*", Labels: []string{"docs:changelog"}},
	{Type: types.TypeFile, NamePattern: "changelog*", Labels: []string{"docs:changelog"}},
	{Type: types.TypeFile, NamePattern: "HISTORY*", Labels: []string{"docs:changelog"}},
	{Type: types.TypeFile, NamePattern: "LICENSE*", Labels: []string{"docs:license"}},
	{Type: types.TypeFile, NamePattern: "license*", Labels: []string{"docs:license"}},
	{Type: types.TypeFile, NamePattern: "COPYING*", Labels: []string{"docs:license"}},
	{Type: types.TypeFile, NamePattern: "CONTRIBUTING*", Labels: []string{"docs:contributing"}},
	{Type: types.TypeFile, NamePattern: "CODE_OF_CONDUCT*", Labels: []string{"docs:code-of-conduct"}},
	{Type: types.TypeFile, NamePattern: "SECURITY*", Labels: []string{"docs:security"}},

	// Editor/IDE configs
	{Type: types.TypeFile, NamePattern: ".editorconfig", Labels: []string{"editor:config"}},
	{Type: types.TypeFile, NamePattern: ".prettierrc*", Labels: []string{"editor:formatter"}},
	{Type: types.TypeFile, NamePattern: "prettier.config.*", Labels: []string{"editor:formatter"}},
	{Type: types.TypeFile, NamePattern: ".eslintrc*", Labels: []string{"editor:linter"}},
	{Type: types.TypeFile, NamePattern: "eslint.config.*", Labels: []string{"editor:linter"}},
	{Type: types.TypeFile, NamePattern: "tsconfig.json", Labels: []string{"lang:typescript"}},
	{Type: types.TypeFile, NamePattern: "tsconfig.*.json", Labels: []string{"lang:typescript"}},
	{Type: types.TypeFile, NamePattern: "jsconfig.json", Labels: []string{"lang:javascript"}},

	// Git configs
	{Type: types.TypeFile, NamePattern: ".gitignore", Labels: []string{"git:config"}},
	{Type: types.TypeFile, NamePattern: ".gitattributes", Labels: []string{"git:config"}},
	{Type: types.TypeFile, NamePattern: ".gitmodules", Labels: []string{"git:config"}},

	// Test files - language is already apparent from extension, only add test:file
	{Type: types.TypeFile, NamePattern: "*_test.go", Labels: []string{"test:file"}},
	{Type: types.TypeFile, NamePattern: "*.test.js", Labels: []string{"test:file"}},
	{Type: types.TypeFile, NamePattern: "*.spec.js", Labels: []string{"test:file"}},
	{Type: types.TypeFile, NamePattern: "*.test.ts", Labels: []string{"test:file"}},
	{Type: types.TypeFile, NamePattern: "*.spec.ts", Labels: []string{"test:file"}},
	{Type: types.TypeFile, NamePattern: "*.test.tsx", Labels: []string{"test:file"}},
	{Type: types.TypeFile, NamePattern: "*.spec.tsx", Labels: []string{"test:file"}},
	{Type: types.TypeFile, NamePattern: "test_*.py", Labels: []string{"test:file"}},
	{Type: types.TypeFile, NamePattern: "*_test.py", Labels: []string{"test:file"}},
	{Type: types.TypeFile, PathPattern: "**/tests/*.rs", Labels: []string{"test:file"}},
	{Type: types.TypeFile, NamePattern: "*_spec.rb", Labels: []string{"test:file"}},
	{Type: types.TypeFile, NamePattern: "*_test.rb", Labels: []string{"test:file"}},
}

// Indexer applies labels to nodes based on configurable rules.
type Indexer struct {
	config Config
}

// New creates a new tagger indexer with the given configuration.
func New(cfg Config) *Indexer {
	if len(cfg.Rules) == 0 {
		cfg.Rules = DefaultRules
	}
	return &Indexer{config: cfg}
}

func (i *Indexer) Name() string {
	return "tagger"
}

func (i *Indexer) Schemes() []string {
	// Tagger works on all schemes via events
	return nil
}

func (i *Indexer) Handles(uri string) bool {
	// Tagger doesn't handle URIs directly, it reacts to events
	return false
}

func (i *Indexer) Subscriptions() []indexer.Subscription {
	// Tagger is now called directly by FS indexer, no event subscriptions needed.
	// This eliminates event channel overhead for high-volume file indexing.
	return nil
}

func (i *Indexer) Index(ctx context.Context, ictx *indexer.Context) error {
	// Tagger is event-driven only, direct invocation is a no-op
	return nil
}

func (i *Indexer) HandleEvent(ctx context.Context, ictx *indexer.Context, event indexer.Event) error {
	// Event carries the node - no DB read needed
	node := event.Node
	if node == nil {
		// Shouldn't happen, but be defensive
		return nil
	}

	// Compute relative path from root for path pattern matching
	rootPath := types.URIToPath(ictx.Root)
	relPath, err := filepath.Rel(rootPath, event.Path)
	if err != nil {
		// Fall back to using name only if relative path fails
		relPath = event.Name
	}

	if i.tagNode(node, event.NodeType, event.Name, relPath) {
		// Emit updated node to storage
		return ictx.Emitter.EmitNode(ctx, node)
	}
	return nil
}

// TagNode applies labels to a node based on matching rules.
// This is the direct call interface for use by other indexers (e.g., FS indexer).
// It modifies the node in-place and returns true if any labels were added.
func (i *Indexer) TagNode(node *graph.Node, nodeType, name, relPath string) bool {
	return i.tagNode(node, nodeType, name, relPath)
}

// tagNode is the internal implementation.
func (i *Indexer) tagNode(node *graph.Node, nodeType, name, relPath string) bool {
	// Collect labels from all matching rules
	var labels []string
	for _, rule := range i.config.Rules {
		if rule.Matches(nodeType, name, relPath) {
			labels = append(labels, rule.Labels...)
		}
	}

	// If no labels matched, nothing to do
	if len(labels) == 0 {
		return false
	}

	// Add labels to the node (deduplicating)
	node.AddLabels(labels...)
	return true
}
