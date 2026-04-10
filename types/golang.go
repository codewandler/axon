package types

import "github.com/codewandler/axon/graph"

// Go node types
const (
	TypeGoModule    = "go:module"
	TypeGoPackage   = "go:package"
	TypeGoStruct    = "go:struct"
	TypeGoInterface = "go:interface"
	TypeGoFunc      = "go:func"
	TypeGoMethod    = "go:method"
	TypeGoField     = "go:field"
	TypeGoConst     = "go:const"
	TypeGoVar       = "go:var"
	TypeGoRef       = "go:ref" // Reference to a symbol (usage site)
)

// Reference kinds
const (
	RefKindCall   = "call"   // Function/method call
	RefKindType   = "type"   // Type usage (in declaration, signature, etc.)
	RefKindField  = "field"  // Field access
	RefKindValue  = "value"  // Variable/constant reference
	RefKindImport = "import" // Import of a package
)

// Go labels for tagging files
const (
	LabelGoMod = "go:mod"
	LabelGoSum = "go:sum"
)

// ModuleData holds data for a Go module node.
type ModuleData struct {
	Name    string `json:"name"`             // Module path (e.g., "github.com/user/repo")
	GoVer   string `json:"go_ver,omitempty"` // Go version from go.mod
	ModFile string `json:"mod_file"`         // Path to go.mod
}

// PackageData holds data for a Go package node.
type PackageData struct {
	Name        string   `json:"name"`                         // Package name (e.g., "http")
	ImportPath  string   `json:"import_path"`                  // Full import path
	Dir         string   `json:"dir"`                          // Directory containing package
	Doc         string   `json:"doc,omitempty"`                // Package documentation
	IsMain      bool     `json:"is_main,omitempty"`            // True if package main
	IsTest      bool     `json:"is_test,omitempty"`            // True if test package
	NumFiles    int      `json:"num_files,omitempty"`          // Number of Go files
	ImportPaths []string `json:"import_paths,omitempty"`       // direct import paths (intra-module)
	TestFor     string   `json:"test_for,omitempty"`           // import path this test package tests
}

// StructData holds data for a Go struct type node.
type StructData struct {
	Name       string   `json:"name"`                  // Struct name
	Doc        string   `json:"doc,omitempty"`         // Documentation comment
	Exported   bool     `json:"exported"`              // Is name exported (capitalized)
	NumFields  int      `json:"num_fields,omitempty"`  // Number of fields
	NumMethods int      `json:"num_methods,omitempty"` // Number of methods
	Embeds     []string `json:"embeds,omitempty"`      // Embedded types
	Position   Position `json:"position"`              // Source position
}

// InterfaceData holds data for a Go interface type node.
type InterfaceData struct {
	Name       string   `json:"name"`                  // Interface name
	Doc        string   `json:"doc,omitempty"`         // Documentation comment
	Exported   bool     `json:"exported"`              // Is name exported
	NumMethods int      `json:"num_methods,omitempty"` // Number of methods
	Embeds     []string `json:"embeds,omitempty"`      // Embedded interfaces
	Position   Position `json:"position"`              // Source position
}

// FuncData holds data for a Go function node.
type FuncData struct {
	Name      string   `json:"name"`              // Function name
	Doc       string   `json:"doc,omitempty"`     // Documentation comment
	Exported  bool     `json:"exported"`          // Is name exported
	Signature string   `json:"signature"`         // Function signature
	Params    []string `json:"params,omitempty"`  // Parameter types
	Results   []string `json:"results,omitempty"` // Return types
	Position  Position `json:"position"`          // Source position
}

// MethodData holds data for a Go method node.
type MethodData struct {
	Name        string   `json:"name"`                   // Method name
	Doc         string   `json:"doc,omitempty"`          // Documentation comment
	Exported    bool     `json:"exported"`               // Is name exported
	Receiver    string   `json:"receiver"`               // Receiver type name
	ReceiverPtr bool     `json:"receiver_ptr,omitempty"` // Is receiver a pointer
	Signature   string   `json:"signature"`              // Method signature
	Params      []string `json:"params,omitempty"`       // Parameter types
	Results     []string `json:"results,omitempty"`      // Return types
	Position    Position `json:"position"`               // Source position
}

// FieldData holds data for a struct field node.
type FieldData struct {
	Name     string   `json:"name"`          // Field name (empty for embedded)
	Type     string   `json:"type"`          // Field type
	Tag      string   `json:"tag,omitempty"` // Struct tag
	Doc      string   `json:"doc,omitempty"` // Documentation comment
	Exported bool     `json:"exported"`      // Is name exported
	Embedded bool     `json:"embedded"`      // Is embedded field
	Position Position `json:"position"`      // Source position
}

// ConstData holds data for a Go constant node.
type ConstData struct {
	Name     string   `json:"name"`          // Constant name
	Type     string   `json:"type"`          // Type (may be empty for untyped)
	Value    string   `json:"value"`         // Value expression
	Doc      string   `json:"doc,omitempty"` // Documentation comment
	Exported bool     `json:"exported"`      // Is name exported
	Position Position `json:"position"`      // Source position
}

// VarData holds data for a Go variable node.
type VarData struct {
	Name     string   `json:"name"`          // Variable name
	Type     string   `json:"type"`          // Type
	Doc      string   `json:"doc,omitempty"` // Documentation comment
	Exported bool     `json:"exported"`      // Is name exported
	Position Position `json:"position"`      // Source position
}

// Position represents a source code position.
type Position struct {
	File    string `json:"file"`               // Filename
	Line    int    `json:"line"`               // Line number (1-indexed)
	Column  int    `json:"column"`             // Column number (1-indexed)
	EndLine int    `json:"end_line,omitempty"` // End line number (1-indexed), 0 if unknown
}

// RefData holds data for a reference (usage site) to a symbol.
type RefData struct {
	Kind       string   `json:"kind"`                  // Reference kind (call, type, field, value)
	Name       string   `json:"name"`                  // Name being referenced
	TargetType string   `json:"target_type,omitempty"` // Type of target (go:func, go:struct, etc.)
	TargetPkg  string   `json:"target_pkg,omitempty"`  // Package of target symbol
	CallerURI  string   `json:"caller_uri,omitempty"`  // URI of the enclosing func/method (empty if package-scope)
	CallerName string   `json:"caller_name,omitempty"` // Short name of the enclosing func/method
	CallerType string   `json:"caller_type,omitempty"` // Node type of caller: go:func or go:method
	Position   Position `json:"position"`              // Source position of reference
}

// RegisterGoTypes registers Go node and edge types with the registry.
func RegisterGoTypes(r *graph.Registry) {
	graph.RegisterNodeType[ModuleData](r, graph.NodeSpec{
		Type:        TypeGoModule,
		Description: "A Go module (go.mod)",
	})

	graph.RegisterNodeType[PackageData](r, graph.NodeSpec{
		Type:        TypeGoPackage,
		Description: "A Go package",
	})

	graph.RegisterNodeType[StructData](r, graph.NodeSpec{
		Type:        TypeGoStruct,
		Description: "A Go struct type",
	})

	graph.RegisterNodeType[InterfaceData](r, graph.NodeSpec{
		Type:        TypeGoInterface,
		Description: "A Go interface type",
	})

	graph.RegisterNodeType[FuncData](r, graph.NodeSpec{
		Type:        TypeGoFunc,
		Description: "A Go function",
	})

	graph.RegisterNodeType[MethodData](r, graph.NodeSpec{
		Type:        TypeGoMethod,
		Description: "A Go method",
	})

	graph.RegisterNodeType[FieldData](r, graph.NodeSpec{
		Type:        TypeGoField,
		Description: "A Go struct field",
	})

	graph.RegisterNodeType[ConstData](r, graph.NodeSpec{
		Type:        TypeGoConst,
		Description: "A Go constant",
	})

	graph.RegisterNodeType[VarData](r, graph.NodeSpec{
		Type:        TypeGoVar,
		Description: "A Go package-level variable",
	})

	graph.RegisterNodeType[RefData](r, graph.NodeSpec{
		Type:        TypeGoRef,
		Description: "A reference (usage) of a Go symbol",
	})

	// Edge constraints for Go-specific relationships
	// Module contains packages
	r.RegisterEdgeType(graph.EdgeSpec{
		Type:        EdgeContains,
		Description: "Module contains package",
		FromTypes:   []string{TypeGoModule},
		ToTypes:     []string{TypeGoPackage},
	})

	// Package defines types, functions, constants, variables
	r.RegisterEdgeType(graph.EdgeSpec{
		Type:        EdgeDefines,
		Description: "Package defines symbol",
		FromTypes:   []string{TypeGoPackage},
		ToTypes:     []string{TypeGoStruct, TypeGoInterface, TypeGoFunc, TypeGoConst, TypeGoVar},
	})

	// Struct/Interface has fields/methods
	r.RegisterEdgeType(graph.EdgeSpec{
		Type:        EdgeHas,
		Description: "Type has field or method",
		FromTypes:   []string{TypeGoStruct, TypeGoInterface},
		ToTypes:     []string{TypeGoField, TypeGoMethod},
	})

	// Module located at directory
	r.RegisterEdgeType(graph.EdgeSpec{
		Type:        EdgeLocatedAt,
		Description: "Module is located at a directory",
		FromTypes:   []string{TypeGoModule},
		ToTypes:     []string{TypeDir},
	})

	// Struct implements interface
	r.RegisterEdgeType(graph.EdgeSpec{
		Type:        EdgeImplements,
		Description: "Struct implements interface",
		FromTypes:   []string{TypeGoStruct},
		ToTypes:     []string{TypeGoInterface},
	})

	// Function/method calls another function/method
	r.RegisterEdgeType(graph.EdgeSpec{
		Type:        EdgeCalls,
		Description: "Function or method calls another function or method",
		FromTypes:   []string{TypeGoFunc, TypeGoMethod},
		ToTypes:     []string{TypeGoFunc, TypeGoMethod},
	})

	// Struct embeds another struct
	r.RegisterEdgeType(graph.EdgeSpec{
		Type:        EdgeEmbeds,
		Description: "Struct embeds another struct via anonymous field",
		FromTypes:   []string{TypeGoStruct},
		ToTypes:     []string{TypeGoStruct},
	})
}

// GoModulePathToURI converts a module path to a go+file:// URI.
func GoModulePathToURI(path string) string {
	return "go+file://" + path
}

// URIToGoModulePath extracts the path from a go+file:// URI.
func URIToGoModulePath(uri string) string {
	const prefix = "go+file://"
	if len(uri) > len(prefix) && uri[:len(prefix)] == prefix {
		return uri[len(prefix):]
	}
	return uri
}
