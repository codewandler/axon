package types

import "github.com/codewandler/axon/graph"

// Common Edge Types
//
// Edge Type Guidelines:
//
// 1. STRUCTURAL vs LOGICAL relationships:
//    - `contains` / `contained_by`: Structural containment (physical container ↔ item)
//      Example: directory contains file, directory contains subdirectory
//    - `has` / `belongs_to`: Logical ownership (owner ↔ owned)
//      Example: repo has branch, document has section
//
// 2. BIDIRECTIONAL relationships:
//    When modeling hierarchies, create both directions for efficient querying:
//    - `contains` / `contained_by`: structural (dir ↔ file)
//    - `has` / `belongs_to`: ownership (repo ↔ branch)
//    Use the Emitter helpers: EmitContainment() and EmitOwnership()
//
// 3. SCOPED edge types:
//    Only create domain-scoped edges (e.g., `git::is_submodule_of`) when
//    the relationship has unique semantics that cannot be expressed with
//    generic edges. The node types already provide domain context.
//
// 4. QUERY patterns:
//    - "What does X contain?" → GetEdgesFrom(X.ID) where edge.Type == "contains"
//    - "What is X contained by?" → GetEdgesFrom(X.ID) where edge.Type == "contained_by"
//    - "What does X have?" → GetEdgesFrom(X.ID) where edge.Type == "has"
//    - "What does X belong to?" → GetEdgesFrom(X.ID) where edge.Type == "belongs_to"
//    - Filter by target node type for specificity (e.g., "all tags of repo")

const (
	// Structural containment: container → item
	// Used for physical/structural hierarchies (directories, nested structures)
	// Inverse: contained_by
	EdgeContains = "contains"

	// Inverse of contains: item → container
	// Used to query "what contains this item"
	EdgeContainedBy = "contained_by"

	// Logical ownership: owner → owned
	// Used when parent logically owns children (repo→branch, doc→section, file→parsed content)
	// Inverse: belongs_to
	EdgeHas = "has"

	// Inverse of has: owned → owner
	// Used when child cannot exist without parent (branch→repo, section→doc)
	EdgeBelongsTo = "belongs_to"

	// Cross-domain physical location: resource → location
	// Used when a resource is located at a filesystem location
	// Example: vcs:repo → fs:dir
	EdgeLocatedAt = "located_at"

	// Explicit hyperlink/reference: source → target
	// Used for explicit links in documents (markdown links, hyperlinks)
	EdgeLinksTo = "links_to"

	// Soft cross-reference: source → target
	// Used for implicit or inferred references between nodes
	EdgeReferences = "references"

	// Dependency relationship: dependent → dependency
	// Used for build/runtime dependencies between modules
	EdgeDependsOn = "depends_on"

	// Import relationship: importer → imported
	// Used for code imports/includes
	EdgeImports = "imports"

	// Symbol definition: definer → symbol
	// Used when a file/module defines a symbol
	EdgeDefines = "defines"

	// Implementation relationship: struct → interface
	// Used when a struct type implements an interface
	EdgeImplements = "implements"

	// Test relationship: test package → source package
	// Used when a test package tests a source package
	EdgeTests = "tests"

	// Commit DAG: parent commit → child commit
	EdgeParentOf = "parent_of"

	// File modification: commit → file
	EdgeModifies = "modifies"
)

// RegisterCommonEdges registers the common edge types that are used across domains.
// These are generic edges that don't have specific FromTypes/ToTypes constraints.
func RegisterCommonEdges(r *graph.Registry) {
	// Structural containment pair
	r.RegisterEdgeType(graph.EdgeSpec{
		Type:        EdgeContains,
		Description: "Structural containment (container contains item)",
	})

	r.RegisterEdgeType(graph.EdgeSpec{
		Type:        EdgeContainedBy,
		Description: "Inverse of contains (item contained by container)",
	})

	// Logical ownership pair
	r.RegisterEdgeType(graph.EdgeSpec{
		Type:        EdgeHas,
		Description: "Logical ownership (owner has owned)",
	})

	r.RegisterEdgeType(graph.EdgeSpec{
		Type:        EdgeBelongsTo,
		Description: "Inverse of has (owned belongs to owner)",
	})

	// Location
	r.RegisterEdgeType(graph.EdgeSpec{
		Type:        EdgeLocatedAt,
		Description: "Physical location (resource located at filesystem location)",
	})

	// Links and references
	r.RegisterEdgeType(graph.EdgeSpec{
		Type:        EdgeLinksTo,
		Description: "Explicit hyperlink (source links to target)",
	})

	r.RegisterEdgeType(graph.EdgeSpec{
		Type:        EdgeReferences,
		Description: "Soft cross-reference between nodes",
	})

	// Dependencies and imports
	r.RegisterEdgeType(graph.EdgeSpec{
		Type:        EdgeDependsOn,
		Description: "Dependency relationship (dependent depends on dependency)",
	})

	r.RegisterEdgeType(graph.EdgeSpec{
		Type:        EdgeImports,
		Description: "Import relationship (importer imports imported)",
	})

	r.RegisterEdgeType(graph.EdgeSpec{
		Type:        EdgeDefines,
		Description: "Symbol definition (definer defines symbol)",
	})

	r.RegisterEdgeType(graph.EdgeSpec{
		Type:        EdgeImplements,
		Description: "Struct implements interface",
	})

	r.RegisterEdgeType(graph.EdgeSpec{
		Type:        EdgeTests,
		Description: "Test package tests source package",
	})

	r.RegisterEdgeType(graph.EdgeSpec{
		Type:        EdgeParentOf,
		Description: "Commit DAG parent-to-child relationship",
	})

	r.RegisterEdgeType(graph.EdgeSpec{
		Type:        EdgeModifies,
		Description: "Commit modified a file",
	})
}
