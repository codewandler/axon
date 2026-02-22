// Package project provides an indexer for detecting project types from manifest files.
//
// The indexer triggers on common manifest files and extracts:
//   - Project type (language/ecosystem)
//   - Project name
//   - Project version
//   - Dependency count
//
// Supported manifest files:
//   - go.mod (Go)
//   - package.json (Node.js)
//   - Cargo.toml (Rust)
//   - pyproject.toml, setup.py (Python)
//   - pom.xml, build.gradle (Java)
//   - Gemfile (Ruby)
//   - composer.json (PHP)
//
// Node types emitted:
//   - project:root - A project root detected from manifest files
//
// Edge relationships:
//   - project -[located_at]-> directory
//
// The indexer also tags directory nodes with project labels (e.g., project:go).
package project

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/BurntSushi/toml"
	"golang.org/x/mod/modfile"

	"github.com/codewandler/axon/graph"
	"github.com/codewandler/axon/indexer"
	"github.com/codewandler/axon/types"
)

// manifestInfo describes a manifest file and how to process it.
type manifestInfo struct {
	Name     string // Filename (e.g., "go.mod")
	Language string // Language identifier
	Label    string // Label to apply to directory
}

// manifestFiles maps filenames to their manifest info.
var manifestFiles = map[string]manifestInfo{
	"go.mod":         {Name: "go.mod", Language: types.LangGo, Label: types.LabelProjectGo},
	"package.json":   {Name: "package.json", Language: types.LangNode, Label: types.LabelProjectNode},
	"Cargo.toml":     {Name: "Cargo.toml", Language: types.LangRust, Label: types.LabelProjectRust},
	"pyproject.toml": {Name: "pyproject.toml", Language: types.LangPython, Label: types.LabelProjectPython},
	"setup.py":       {Name: "setup.py", Language: types.LangPython, Label: types.LabelProjectPython},
	"pom.xml":        {Name: "pom.xml", Language: types.LangJava, Label: types.LabelProjectJava},
	"build.gradle":   {Name: "build.gradle", Language: types.LangJava, Label: types.LabelProjectJava},
	"Gemfile":        {Name: "Gemfile", Language: types.LangRuby, Label: types.LabelProjectRuby},
	"composer.json":  {Name: "composer.json", Language: types.LangPHP, Label: types.LabelProjectPHP},
}

// Indexer detects project types from manifest files.
type Indexer struct{}

// New creates a new project detector indexer.
func New() *Indexer {
	return &Indexer{}
}

func (i *Indexer) Name() string {
	return "project"
}

func (i *Indexer) Schemes() []string {
	return []string{"project+file"}
}

func (i *Indexer) Handles(uri string) bool {
	return strings.HasPrefix(uri, "project+file://")
}

func (i *Indexer) Subscriptions() []indexer.Subscription {
	subs := make([]indexer.Subscription, 0, len(manifestFiles)*2)
	for name := range manifestFiles {
		// Subscribe to manifest file visits
		subs = append(subs, indexer.Subscription{
			EventType: indexer.EventEntryVisited,
			NodeType:  types.TypeFile,
			Name:      name,
		})
		// Subscribe to manifest file deletions for cleanup
		subs = append(subs, indexer.Subscription{
			EventType: indexer.EventNodeDeleting,
			NodeType:  types.TypeFile,
			Name:      name,
		})
	}
	return subs
}

func (i *Indexer) Index(ctx context.Context, ictx *indexer.Context) error {
	// Project indexer is event-driven only, direct invocation is a no-op
	return nil
}

func (i *Indexer) HandleEvent(ctx context.Context, ictx *indexer.Context, event indexer.Event) error {
	info, ok := manifestFiles[event.Name]
	if !ok {
		return nil
	}

	// Handle deletion - clean up project nodes
	if event.Type == indexer.EventNodeDeleting {
		return i.cleanup(ctx, ictx, event.Path, info)
	}

	// Handle visit - create project node
	return i.indexProject(ctx, ictx, event, info)
}

// indexProject creates a project node from a manifest file.
func (i *Indexer) indexProject(ctx context.Context, ictx *indexer.Context, event indexer.Event, info manifestInfo) error {
	projectDir := filepath.Dir(event.Path)

	// Parse the manifest file to extract metadata
	data, err := i.parseManifest(event.Path, info.Language)
	if err != nil {
		// If parsing fails, still create a project node with just the language
		data = types.ProjectData{Language: info.Language}
	}

	// Create project node
	projectURI := types.ProjectPathToURI(projectDir)
	projectNode := graph.NewNode(types.TypeProject).
		WithURI(projectURI).
		WithKey(projectDir).
		WithName(data.Name).
		WithData(data)

	// If name is empty, use the directory name
	if projectNode.Name == "" {
		projectNode.Name = filepath.Base(projectDir)
	}

	if err := ictx.Emitter.EmitNode(ctx, projectNode); err != nil {
		return err
	}

	// Link project to directory
	dirURI := types.PathToURI(projectDir)
	dirID := graph.IDFromURI(dirURI)
	edge := graph.NewEdge(types.EdgeLocatedAt, projectNode.ID, dirID)
	if err := ictx.Emitter.EmitEdge(ctx, edge); err != nil {
		return err
	}

	// Tag the directory node with the project label
	if event.Node != nil {
		// Get the parent directory node
		dirNode, err := ictx.Graph.Storage().GetNode(ctx, dirID)
		if err == nil && dirNode != nil {
			return i.tagNode(ctx, ictx, dirNode, info.Label)
		}
	}

	return nil
}

// tagNode adds a label to a node if not already present.
func (i *Indexer) tagNode(ctx context.Context, ictx *indexer.Context, node *graph.Node, label string) error {
	if node == nil {
		return nil
	}

	// Check if label already present
	for _, l := range node.Labels {
		if l == label {
			return nil
		}
	}

	node.Labels = append(node.Labels, label)
	return ictx.Emitter.EmitNode(ctx, node)
}

// cleanup removes project nodes when a manifest file is deleted.
func (i *Indexer) cleanup(ctx context.Context, ictx *indexer.Context, manifestPath string, info manifestInfo) error {
	projectDir := filepath.Dir(manifestPath)
	projectURI := types.ProjectPathToURI(projectDir)

	// Delete the project node
	deleted, err := ictx.Graph.Storage().DeleteByURIPrefix(ctx, projectURI)
	if deleted > 0 {
		ictx.AddNodesDeleted(deleted)
	}
	return err
}

// parseManifest extracts project metadata from a manifest file.
func (i *Indexer) parseManifest(path string, language string) (types.ProjectData, error) {
	data := types.ProjectData{Language: language}

	switch language {
	case types.LangGo:
		return i.parseGoMod(path)
	case types.LangNode:
		return i.parsePackageJSON(path)
	case types.LangRust:
		return i.parseCargoToml(path)
	case types.LangPython:
		if strings.HasSuffix(path, "pyproject.toml") {
			return i.parsePyprojectToml(path)
		}
		// setup.py is harder to parse, just return language
		return data, nil
	case types.LangJava:
		// pom.xml and build.gradle are complex, just return language for now
		return data, nil
	case types.LangRuby:
		// Gemfile doesn't contain project name/version directly
		return data, nil
	case types.LangPHP:
		return i.parseComposerJSON(path)
	}

	return data, nil
}

// parseGoMod extracts metadata from go.mod.
func (i *Indexer) parseGoMod(path string) (types.ProjectData, error) {
	data := types.ProjectData{Language: types.LangGo}

	content, err := os.ReadFile(path)
	if err != nil {
		return data, err
	}

	modFile, err := modfile.Parse(path, content, nil)
	if err != nil {
		return data, err
	}

	if modFile.Module != nil {
		data.Name = modFile.Module.Mod.Path
	}

	// Count direct dependencies
	data.DepCount = len(modFile.Require)

	return data, nil
}

// parsePackageJSON extracts metadata from package.json.
func (i *Indexer) parsePackageJSON(path string) (types.ProjectData, error) {
	data := types.ProjectData{Language: types.LangNode}

	content, err := os.ReadFile(path)
	if err != nil {
		return data, err
	}

	var pkg struct {
		Name            string            `json:"name"`
		Version         string            `json:"version"`
		Dependencies    map[string]string `json:"dependencies"`
		DevDependencies map[string]string `json:"devDependencies"`
	}

	if err := json.Unmarshal(content, &pkg); err != nil {
		return data, err
	}

	data.Name = pkg.Name
	data.Version = pkg.Version
	data.DepCount = len(pkg.Dependencies) + len(pkg.DevDependencies)

	return data, nil
}

// parseCargoToml extracts metadata from Cargo.toml.
func (i *Indexer) parseCargoToml(path string) (types.ProjectData, error) {
	data := types.ProjectData{Language: types.LangRust}

	content, err := os.ReadFile(path)
	if err != nil {
		return data, err
	}

	var cargo struct {
		Package struct {
			Name    string `toml:"name"`
			Version string `toml:"version"`
		} `toml:"package"`
		Dependencies    map[string]interface{} `toml:"dependencies"`
		DevDependencies map[string]interface{} `toml:"dev-dependencies"`
	}

	if err := toml.Unmarshal(content, &cargo); err != nil {
		return data, err
	}

	data.Name = cargo.Package.Name
	data.Version = cargo.Package.Version
	data.DepCount = len(cargo.Dependencies) + len(cargo.DevDependencies)

	return data, nil
}

// parsePyprojectToml extracts metadata from pyproject.toml.
func (i *Indexer) parsePyprojectToml(path string) (types.ProjectData, error) {
	data := types.ProjectData{Language: types.LangPython}

	content, err := os.ReadFile(path)
	if err != nil {
		return data, err
	}

	var pyproject struct {
		Project struct {
			Name         string   `toml:"name"`
			Version      string   `toml:"version"`
			Dependencies []string `toml:"dependencies"`
		} `toml:"project"`
		Tool struct {
			Poetry struct {
				Name         string                 `toml:"name"`
				Version      string                 `toml:"version"`
				Dependencies map[string]interface{} `toml:"dependencies"`
			} `toml:"poetry"`
		} `toml:"tool"`
	}

	if err := toml.Unmarshal(content, &pyproject); err != nil {
		return data, err
	}

	// Try standard project table first
	if pyproject.Project.Name != "" {
		data.Name = pyproject.Project.Name
		data.Version = pyproject.Project.Version
		data.DepCount = len(pyproject.Project.Dependencies)
	} else if pyproject.Tool.Poetry.Name != "" {
		// Fall back to Poetry format
		data.Name = pyproject.Tool.Poetry.Name
		data.Version = pyproject.Tool.Poetry.Version
		data.DepCount = len(pyproject.Tool.Poetry.Dependencies)
	}

	return data, nil
}

// parseComposerJSON extracts metadata from composer.json.
func (i *Indexer) parseComposerJSON(path string) (types.ProjectData, error) {
	data := types.ProjectData{Language: types.LangPHP}

	content, err := os.ReadFile(path)
	if err != nil {
		return data, err
	}

	var composer struct {
		Name       string            `json:"name"`
		Version    string            `json:"version"`
		Require    map[string]string `json:"require"`
		RequireDev map[string]string `json:"require-dev"`
	}

	if err := json.Unmarshal(content, &composer); err != nil {
		return data, err
	}

	data.Name = composer.Name
	data.Version = composer.Version
	data.DepCount = len(composer.Require) + len(composer.RequireDev)

	return data, nil
}

// pomNameRegex extracts artifactId from pom.xml (simple approach).
var pomNameRegex = regexp.MustCompile(`<artifactId>([^<]+)</artifactId>`)
var pomVersionRegex = regexp.MustCompile(`<version>([^<]+)</version>`)
