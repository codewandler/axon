package aql

// NodeTypeRef represents a typed node type constant.
// Can be used in Type.Eq(NodeType.File) for type-safe queries.
type NodeTypeRef struct {
	name string
}

// String returns the node type as a string.
func (n NodeTypeRef) String() string { return n.name }

// NodeType namespace with predefined node types.
var NodeType = struct {
	// Filesystem
	File NodeTypeRef
	Dir  NodeTypeRef
	Link NodeTypeRef

	// VCS
	Repo   NodeTypeRef
	Remote NodeTypeRef
	Branch NodeTypeRef
	Tag    NodeTypeRef

	// Markdown
	Document NodeTypeRef
	Section  NodeTypeRef
	Heading  NodeTypeRef

	// Project
	Project NodeTypeRef
}{
	// Filesystem
	File: NodeTypeRef{"fs:file"},
	Dir:  NodeTypeRef{"fs:dir"},
	Link: NodeTypeRef{"fs:link"},

	// VCS
	Repo:   NodeTypeRef{"vcs:repo"},
	Remote: NodeTypeRef{"vcs:remote"},
	Branch: NodeTypeRef{"vcs:branch"},
	Tag:    NodeTypeRef{"vcs:tag"},

	// Markdown
	Document: NodeTypeRef{"md:document"},
	Section:  NodeTypeRef{"md:section"},
	Heading:  NodeTypeRef{"md:heading"},

	// Project
	Project: NodeTypeRef{"project:root"},
}

// NodeTypeOf creates a custom node type reference.
// Use this for node types not in the predefined NodeType namespace.
func NodeTypeOf(t string) NodeTypeRef {
	return NodeTypeRef{name: t}
}
