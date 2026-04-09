// Package golang provides an indexer for Go modules and packages.
//
// The indexer triggers on go.mod files and extracts:
//   - Module information (name, Go version)
//   - Packages (name, import path, documentation)
//   - Exported types (structs, interfaces)
//   - Exported functions and methods
//   - Exported constants and variables
//   - Struct fields
//
// Node types emitted:
//   - go:module - The Go module (root of the module graph)
//   - go:package - A Go package
//   - go:struct - A struct type
//   - go:interface - An interface type
//   - go:func - A function
//   - go:method - A method
//   - go:field - A struct field
//   - go:const - A constant
//   - go:var - A package-level variable
//
// Edge relationships:
//   - module -[contains]-> package
//   - package -[defines]-> struct/interface/func/const/var
//   - struct/interface -[has]-> field/method
//   - module -[located_at]-> directory
//
// The indexer also tags go.mod and go.sum files with labels.
package golang

import (
	"context"
	"fmt"
	"go/ast"
	"go/token"
	gotypes "go/types"
	"os"
	"path/filepath"
	"strings"

	"golang.org/x/mod/modfile"
	"golang.org/x/tools/go/packages"

	"github.com/codewandler/axon/graph"
	"github.com/codewandler/axon/indexer"
	"github.com/codewandler/axon/progress"
	"github.com/codewandler/axon/types"
)

// Indexer indexes Go modules and packages.
type Indexer struct {
	// ExportedOnly controls whether to index only exported symbols.
	// Default is true (only exported symbols).
	ExportedOnly bool

	// IndexReferences controls whether to index symbol references (usages).
	// When true, creates go:ref nodes and references edges for Find References.
	// Default is true.
	IndexReferences bool
}

// New creates a new Go indexer.
func New() *Indexer {
	return &Indexer{
		ExportedOnly:    true,
		IndexReferences: true,
	}
}

func (i *Indexer) Name() string {
	return "golang"
}

func (i *Indexer) Schemes() []string {
	return []string{"go+file"}
}

func (i *Indexer) Handles(uri string) bool {
	return strings.HasPrefix(uri, "go+file://")
}

func (i *Indexer) Subscriptions() []indexer.Subscription {
	// Subscribe to go.mod files being visited (triggers module indexing)
	// and go.sum files for tagging
	return []indexer.Subscription{
		{
			EventType: indexer.EventEntryVisited,
			NodeType:  types.TypeFile,
			Name:      "go.mod",
		},
		{
			EventType: indexer.EventEntryVisited,
			NodeType:  types.TypeFile,
			Name:      "go.sum",
		},
		{
			EventType: indexer.EventNodeDeleting,
			NodeType:  types.TypeFile,
			Name:      "go.mod",
		},
	}
}

func (i *Indexer) Index(ctx context.Context, ictx *indexer.Context) error {
	// Go indexer is event-driven only, direct invocation is a no-op
	return nil
}

func (i *Indexer) HandleEvent(ctx context.Context, ictx *indexer.Context, event indexer.Event) error {
	// Handle go.sum - just tag the file
	if event.Name == "go.sum" {
		return i.tagFile(ctx, ictx, event.Node, types.LabelGoSum)
	}

	// Handle go.mod deletion - clean up module nodes
	if event.Type == indexer.EventNodeDeleting {
		return i.cleanup(ctx, ictx, event.Path)
	}

	// Handle go.mod - tag it and index the module
	if err := i.tagFile(ctx, ictx, event.Node, types.LabelGoMod); err != nil {
		return err
	}

	return i.indexModule(ctx, ictx, event.Path)
}

// tagFile adds a label to the file node.
func (i *Indexer) tagFile(ctx context.Context, ictx *indexer.Context, node *graph.Node, label string) error {
	if node == nil {
		return nil
	}

	// Add label if not already present
	for _, l := range node.Labels {
		if l == label {
			return nil
		}
	}

	node.Labels = append(node.Labels, label)
	return ictx.Emitter.EmitNode(ctx, node)
}

// indexModule parses go.mod and indexes all packages in the module.
func (i *Indexer) indexModule(ctx context.Context, ictx *indexer.Context, goModPath string) error {
	modDir := filepath.Dir(goModPath)

	// Report start
	if ictx.Progress != nil {
		ictx.Progress <- progress.Started(i.Name())
	}

	// Parse go.mod
	data, err := os.ReadFile(goModPath)
	if err != nil {
		if ictx.Progress != nil {
			ictx.Progress <- progress.Error(i.Name(), err)
		}
		return err
	}

	modFile, err := modfile.Parse(goModPath, data, nil)
	if err != nil {
		if ictx.Progress != nil {
			ictx.Progress <- progress.Error(i.Name(), err)
		}
		return err
	}

	// Create module node
	moduleURI := types.GoModulePathToURI(modDir)
	moduleNode := graph.NewNode(types.TypeGoModule).
		WithURI(moduleURI).
		WithKey(modDir).
		WithName(modFile.Module.Mod.Path).
		WithData(types.ModuleData{
			Name:    modFile.Module.Mod.Path,
			GoVer:   modFile.Go.Version,
			ModFile: goModPath,
		})

	if err := ictx.Emitter.EmitNode(ctx, moduleNode); err != nil {
		return err
	}

	// Link module to directory (compute ID directly to avoid read during write)
	dirURI := types.PathToURI(modDir)
	dirID := graph.IDFromURI(dirURI)
	edge := graph.NewEdge(types.EdgeLocatedAt, moduleNode.ID, dirID)
	if err := ictx.Emitter.EmitEdge(ctx, edge); err != nil {
		return err
	}

	// Load all packages in the module using go/packages
	cfg := &packages.Config{
		Mode: packages.NeedName | packages.NeedFiles | packages.NeedSyntax |
			packages.NeedTypes | packages.NeedTypesInfo | packages.NeedImports |
			packages.NeedDeps | packages.NeedModule,
		Dir:   modDir,
		Tests: false, // Skip test packages for now
	}

	pkgs, err := packages.Load(cfg, "./...")
	if err != nil {
		if ictx.Progress != nil {
			ictx.Progress <- progress.Error(i.Name(), err)
		}
		return err
	}

	// Report progress
	if ictx.Progress != nil {
		ictx.Progress <- progress.ProgressWithTotal(i.Name(), 0, len(pkgs), "loading packages")
	}

	// Index each package
	for idx, pkg := range pkgs {
		if ictx.Progress != nil {
			ictx.Progress <- progress.ProgressWithTotal(i.Name(), idx+1, len(pkgs), pkg.PkgPath)
		}

		// Skip packages with errors
		if len(pkg.Errors) > 0 {
			continue
		}

		// Skip packages outside the module directory
		if len(pkg.GoFiles) > 0 {
			pkgDir := filepath.Dir(pkg.GoFiles[0])
			if !strings.HasPrefix(pkgDir, modDir) {
				continue
			}
		}

		if err := i.indexPackage(ctx, ictx, moduleNode.ID, moduleURI, pkg); err != nil {
			// Log but continue
			continue
		}
	}

	// Report completion
	if ictx.Progress != nil {
		ictx.Progress <- progress.Completed(i.Name(), len(pkgs))
	}

	return nil
}

// indexPackage indexes a single Go package.
func (i *Indexer) indexPackage(ctx context.Context, ictx *indexer.Context, moduleID, moduleURI string, pkg *packages.Package) error {
	// Determine package directory
	pkgDir := ""
	if len(pkg.GoFiles) > 0 {
		pkgDir = filepath.Dir(pkg.GoFiles[0])
	}

	// Create package node
	pkgURI := moduleURI + "/pkg/" + pkg.PkgPath
	pkgNode := graph.NewNode(types.TypeGoPackage).
		WithURI(pkgURI).
		WithKey(pkg.PkgPath).
		WithName(pkg.Name).
		WithData(types.PackageData{
			Name:       pkg.Name,
			ImportPath: pkg.PkgPath,
			Dir:        pkgDir,
			IsMain:     pkg.Name == "main",
			NumFiles:   len(pkg.GoFiles),
		})

	if err := ictx.Emitter.EmitNode(ctx, pkgNode); err != nil {
		return err
	}

	// Module contains package
	if err := indexer.EmitContainment(ctx, ictx.Emitter, moduleID, pkgNode.ID); err != nil {
		return err
	}

	// Index all declarations in the package's syntax
	fset := pkg.Fset
	for _, file := range pkg.Syntax {
		if err := i.indexFile(ctx, ictx, pkgNode.ID, pkgURI, fset, file); err != nil {
			return err
		}
	}

	// Index references (symbol usages) if enabled
	if i.IndexReferences && pkg.TypesInfo != nil {
		if err := i.indexReferences(ctx, ictx, moduleURI, pkg); err != nil {
			return err
		}
	}

	return nil
}

// indexFile indexes all declarations in a Go source file.
func (i *Indexer) indexFile(ctx context.Context, ictx *indexer.Context, pkgID, pkgURI string, fset *token.FileSet, file *ast.File) error {
	for _, decl := range file.Decls {
		switch d := decl.(type) {
		case *ast.GenDecl:
			if err := i.indexGenDecl(ctx, ictx, pkgID, pkgURI, fset, d); err != nil {
				return err
			}
		case *ast.FuncDecl:
			if err := i.indexFuncDecl(ctx, ictx, pkgID, pkgURI, fset, d); err != nil {
				return err
			}
		}
	}
	return nil
}

// indexGenDecl indexes a generic declaration (type, const, var).
func (i *Indexer) indexGenDecl(ctx context.Context, ictx *indexer.Context, pkgID, pkgURI string, fset *token.FileSet, decl *ast.GenDecl) error {
	doc := decl.Doc.Text()

	for _, spec := range decl.Specs {
		switch s := spec.(type) {
		case *ast.TypeSpec:
			if err := i.indexTypeSpec(ctx, ictx, pkgID, pkgURI, fset, s, doc); err != nil {
				return err
			}
		case *ast.ValueSpec:
			if err := i.indexValueSpec(ctx, ictx, pkgID, pkgURI, fset, s, decl.Tok, doc); err != nil {
				return err
			}
		}
	}
	return nil
}

// indexTypeSpec indexes a type declaration (struct, interface, type alias).
func (i *Indexer) indexTypeSpec(ctx context.Context, ictx *indexer.Context, pkgID, pkgURI string, fset *token.FileSet, spec *ast.TypeSpec, doc string) error {
	name := spec.Name.Name
	exported := ast.IsExported(name)

	// Skip unexported if ExportedOnly
	if i.ExportedOnly && !exported {
		return nil
	}

	// Use spec doc if available, otherwise use decl doc
	if spec.Doc != nil {
		doc = spec.Doc.Text()
	}

	pos := fset.Position(spec.Pos())
	endPos := fset.Position(spec.End())
	position := types.Position{
		File:    pos.Filename,
		Line:    pos.Line,
		Column:  pos.Column,
		EndLine: endPos.Line,
	}

	switch t := spec.Type.(type) {
	case *ast.StructType:
		return i.indexStruct(ctx, ictx, pkgID, pkgURI, fset, name, exported, doc, position, t)
	case *ast.InterfaceType:
		return i.indexInterface(ctx, ictx, pkgID, pkgURI, fset, name, exported, doc, position, t)
	}

	// Other type declarations (aliases, etc.) could be indexed here
	return nil
}

// indexStruct indexes a struct type.
func (i *Indexer) indexStruct(ctx context.Context, ictx *indexer.Context, pkgID, pkgURI string, fset *token.FileSet, name string, exported bool, doc string, position types.Position, st *ast.StructType) error {
	// Collect embedded types
	var embeds []string
	numFields := 0
	if st.Fields != nil {
		for _, field := range st.Fields.List {
			if len(field.Names) == 0 {
				// Embedded field
				embeds = append(embeds, typeToString(field.Type))
			}
			numFields++
		}
	}

	// Create struct node
	structURI := pkgURI + "/struct/" + name
	structNode := graph.NewNode(types.TypeGoStruct).
		WithURI(structURI).
		WithKey(name).
		WithName(name).
		WithData(types.StructData{
			Name:      name,
			Doc:       strings.TrimSpace(doc),
			Exported:  exported,
			NumFields: numFields,
			Embeds:    embeds,
			Position:  position,
		})

	if err := ictx.Emitter.EmitNode(ctx, structNode); err != nil {
		return err
	}

	// Package defines struct
	edge := graph.NewEdge(types.EdgeDefines, pkgID, structNode.ID)
	if err := ictx.Emitter.EmitEdge(ctx, edge); err != nil {
		return err
	}

	// Index struct fields
	if st.Fields != nil {
		for _, field := range st.Fields.List {
			if err := i.indexField(ctx, ictx, structNode.ID, structURI, fset, field); err != nil {
				return err
			}
		}
	}

	return nil
}

// indexField indexes a struct field.
func (i *Indexer) indexField(ctx context.Context, ictx *indexer.Context, structID, structURI string, fset *token.FileSet, field *ast.Field) error {
	fieldType := typeToString(field.Type)
	embedded := len(field.Names) == 0

	// Handle embedded field
	if embedded {
		name := fieldType
		exported := ast.IsExported(name)
		if i.ExportedOnly && !exported {
			return nil
		}

		pos := fset.Position(field.Pos())
		endPos := fset.Position(field.End())
		fieldURI := structURI + "/field/" + name
		fieldNode := graph.NewNode(types.TypeGoField).
			WithURI(fieldURI).
			WithKey(name).
			WithName(name).
			WithData(types.FieldData{
				Name:     name,
				Type:     fieldType,
				Doc:      strings.TrimSpace(field.Doc.Text()),
				Exported: exported,
				Embedded: true,
				Position: types.Position{
					File:    pos.Filename,
					Line:    pos.Line,
					Column:  pos.Column,
					EndLine: endPos.Line,
				},
			})

		if err := ictx.Emitter.EmitNode(ctx, fieldNode); err != nil {
			return err
		}

		return indexer.EmitOwnership(ctx, ictx.Emitter, structID, fieldNode.ID)
	}

	// Handle named fields
	for _, ident := range field.Names {
		name := ident.Name
		exported := ast.IsExported(name)
		if i.ExportedOnly && !exported {
			continue
		}

		pos := fset.Position(ident.Pos())
		endPos := fset.Position(field.End())
		tag := ""
		if field.Tag != nil {
			tag = field.Tag.Value
		}

		fieldURI := structURI + "/field/" + name
		fieldNode := graph.NewNode(types.TypeGoField).
			WithURI(fieldURI).
			WithKey(name).
			WithName(name).
			WithData(types.FieldData{
				Name:     name,
				Type:     fieldType,
				Tag:      tag,
				Doc:      strings.TrimSpace(field.Doc.Text()),
				Exported: exported,
				Embedded: false,
				Position: types.Position{
					File:    pos.Filename,
					Line:    pos.Line,
					Column:  pos.Column,
					EndLine: endPos.Line,
				},
			})

		if err := ictx.Emitter.EmitNode(ctx, fieldNode); err != nil {
			return err
		}

		if err := indexer.EmitOwnership(ctx, ictx.Emitter, structID, fieldNode.ID); err != nil {
			return err
		}
	}

	return nil
}

// indexInterface indexes an interface type.
func (i *Indexer) indexInterface(ctx context.Context, ictx *indexer.Context, pkgID, pkgURI string, fset *token.FileSet, name string, exported bool, doc string, position types.Position, it *ast.InterfaceType) error {
	// Collect embedded interfaces
	var embeds []string
	numMethods := 0
	if it.Methods != nil {
		for _, method := range it.Methods.List {
			if len(method.Names) == 0 {
				// Embedded interface
				embeds = append(embeds, typeToString(method.Type))
			} else {
				numMethods++
			}
		}
	}

	// Create interface node
	ifaceURI := pkgURI + "/interface/" + name
	ifaceNode := graph.NewNode(types.TypeGoInterface).
		WithURI(ifaceURI).
		WithKey(name).
		WithName(name).
		WithData(types.InterfaceData{
			Name:       name,
			Doc:        strings.TrimSpace(doc),
			Exported:   exported,
			NumMethods: numMethods,
			Embeds:     embeds,
			Position:   position,
		})

	if err := ictx.Emitter.EmitNode(ctx, ifaceNode); err != nil {
		return err
	}

	// Package defines interface
	edge := graph.NewEdge(types.EdgeDefines, pkgID, ifaceNode.ID)
	if err := ictx.Emitter.EmitEdge(ctx, edge); err != nil {
		return err
	}

	// Index interface methods
	if it.Methods != nil {
		for _, method := range it.Methods.List {
			// Skip embedded interfaces
			if len(method.Names) == 0 {
				continue
			}
			if err := i.indexInterfaceMethod(ctx, ictx, ifaceNode.ID, ifaceURI, fset, method); err != nil {
				return err
			}
		}
	}

	return nil
}

// indexInterfaceMethod indexes an interface method.
func (i *Indexer) indexInterfaceMethod(ctx context.Context, ictx *indexer.Context, ifaceID, ifaceURI string, fset *token.FileSet, method *ast.Field) error {
	for _, ident := range method.Names {
		name := ident.Name
		exported := ast.IsExported(name)
		if i.ExportedOnly && !exported {
			continue
		}

		ft, ok := method.Type.(*ast.FuncType)
		if !ok {
			continue
		}

		pos := fset.Position(ident.Pos())
		endPos := fset.Position(method.End())
		params, results := extractFuncSignature(ft)

		methodURI := ifaceURI + "/method/" + name
		methodNode := graph.NewNode(types.TypeGoMethod).
			WithURI(methodURI).
			WithKey(name).
			WithName(name).
			WithData(types.MethodData{
				Name:      name,
				Doc:       strings.TrimSpace(method.Doc.Text()),
				Exported:  exported,
				Receiver:  "", // Interface methods have no receiver
				Signature: formatSignature(name, params, results),
				Params:    params,
				Results:   results,
				Position: types.Position{
					File:    pos.Filename,
					Line:    pos.Line,
					Column:  pos.Column,
					EndLine: endPos.Line,
				},
			})

		if err := ictx.Emitter.EmitNode(ctx, methodNode); err != nil {
			return err
		}

		if err := indexer.EmitOwnership(ctx, ictx.Emitter, ifaceID, methodNode.ID); err != nil {
			return err
		}
	}

	return nil
}

// indexValueSpec indexes a const or var declaration.
func (i *Indexer) indexValueSpec(ctx context.Context, ictx *indexer.Context, pkgID, pkgURI string, fset *token.FileSet, spec *ast.ValueSpec, tok token.Token, doc string) error {
	// Use spec doc if available
	if spec.Doc != nil {
		doc = spec.Doc.Text()
	}

	typeStr := ""
	if spec.Type != nil {
		typeStr = typeToString(spec.Type)
	}

	for idx, ident := range spec.Names {
		name := ident.Name
		exported := ast.IsExported(name)
		if i.ExportedOnly && !exported {
			continue
		}

		pos := fset.Position(ident.Pos())
		endPos := fset.Position(spec.End())
		position := types.Position{
			File:    pos.Filename,
			Line:    pos.Line,
			Column:  pos.Column,
			EndLine: endPos.Line,
		}

		if tok == token.CONST {
			// Get value expression
			value := ""
			if idx < len(spec.Values) {
				value = exprToString(spec.Values[idx])
			}

			constURI := pkgURI + "/const/" + name
			constNode := graph.NewNode(types.TypeGoConst).
				WithURI(constURI).
				WithKey(name).
				WithName(name).
				WithData(types.ConstData{
					Name:     name,
					Type:     typeStr,
					Value:    value,
					Doc:      strings.TrimSpace(doc),
					Exported: exported,
					Position: position,
				})

			if err := ictx.Emitter.EmitNode(ctx, constNode); err != nil {
				return err
			}

			edge := graph.NewEdge(types.EdgeDefines, pkgID, constNode.ID)
			if err := ictx.Emitter.EmitEdge(ctx, edge); err != nil {
				return err
			}
		} else {
			varURI := pkgURI + "/var/" + name
			varNode := graph.NewNode(types.TypeGoVar).
				WithURI(varURI).
				WithKey(name).
				WithName(name).
				WithData(types.VarData{
					Name:     name,
					Type:     typeStr,
					Doc:      strings.TrimSpace(doc),
					Exported: exported,
					Position: position,
				})

			if err := ictx.Emitter.EmitNode(ctx, varNode); err != nil {
				return err
			}

			edge := graph.NewEdge(types.EdgeDefines, pkgID, varNode.ID)
			if err := ictx.Emitter.EmitEdge(ctx, edge); err != nil {
				return err
			}
		}
	}

	return nil
}

// indexFuncDecl indexes a function or method declaration.
func (i *Indexer) indexFuncDecl(ctx context.Context, ictx *indexer.Context, pkgID, pkgURI string, fset *token.FileSet, decl *ast.FuncDecl) error {
	name := decl.Name.Name
	exported := ast.IsExported(name)
	if i.ExportedOnly && !exported {
		return nil
	}

	pos := fset.Position(decl.Pos())
	endPos := fset.Position(decl.End())
	position := types.Position{
		File:    pos.Filename,
		Line:    pos.Line,
		Column:  pos.Column,
		EndLine: endPos.Line,
	}

	doc := ""
	if decl.Doc != nil {
		doc = decl.Doc.Text()
	}

	params, results := extractFuncSignature(decl.Type)

	// Check if this is a method (has receiver)
	if decl.Recv != nil && len(decl.Recv.List) > 0 {
		recv := decl.Recv.List[0]
		recvType, isPtr := extractReceiverType(recv.Type)

		methodURI := pkgURI + "/method/" + recvType + "." + name
		methodNode := graph.NewNode(types.TypeGoMethod).
			WithURI(methodURI).
			WithKey(recvType + "." + name).
			WithName(name).
			WithData(types.MethodData{
				Name:        name,
				Doc:         strings.TrimSpace(doc),
				Exported:    exported,
				Receiver:    recvType,
				ReceiverPtr: isPtr,
				Signature:   formatSignature(name, params, results),
				Params:      params,
				Results:     results,
				Position:    position,
			})

		if err := ictx.Emitter.EmitNode(ctx, methodNode); err != nil {
			return err
		}

		// Link method to its struct (compute ID directly to avoid read during write)
		// The struct should have been indexed earlier in the same package
		structURI := pkgURI + "/struct/" + recvType
		structID := graph.IDFromURI(structURI)
		if err := indexer.EmitOwnership(ctx, ictx.Emitter, structID, methodNode.ID); err != nil {
			return err
		}

		return nil
	}

	// Regular function
	funcURI := pkgURI + "/func/" + name
	funcNode := graph.NewNode(types.TypeGoFunc).
		WithURI(funcURI).
		WithKey(name).
		WithName(name).
		WithData(types.FuncData{
			Name:      name,
			Doc:       strings.TrimSpace(doc),
			Exported:  exported,
			Signature: formatSignature(name, params, results),
			Params:    params,
			Results:   results,
			Position:  position,
		})

	if err := ictx.Emitter.EmitNode(ctx, funcNode); err != nil {
		return err
	}

	edge := graph.NewEdge(types.EdgeDefines, pkgID, funcNode.ID)
	return ictx.Emitter.EmitEdge(ctx, edge)
}

// cleanup removes all Go nodes for a module when go.mod is deleted.
func (i *Indexer) cleanup(ctx context.Context, ictx *indexer.Context, goModPath string) error {
	modDir := filepath.Dir(goModPath)
	moduleURI := types.GoModulePathToURI(modDir)

	// Delete all nodes under this module's URI prefix
	deleted, err := ictx.Graph.Storage().DeleteByURIPrefix(ctx, moduleURI)
	if deleted > 0 {
		ictx.AddNodesDeleted(deleted)
	}
	return err
}

// Helper functions

// typeToString converts an AST type expression to a string.
func typeToString(expr ast.Expr) string {
	switch t := expr.(type) {
	case *ast.Ident:
		return t.Name
	case *ast.StarExpr:
		return "*" + typeToString(t.X)
	case *ast.SelectorExpr:
		return typeToString(t.X) + "." + t.Sel.Name
	case *ast.ArrayType:
		if t.Len == nil {
			return "[]" + typeToString(t.Elt)
		}
		return "[...]" + typeToString(t.Elt)
	case *ast.MapType:
		return "map[" + typeToString(t.Key) + "]" + typeToString(t.Value)
	case *ast.ChanType:
		return "chan " + typeToString(t.Value)
	case *ast.FuncType:
		return "func(...)"
	case *ast.InterfaceType:
		return "interface{}"
	case *ast.StructType:
		return "struct{}"
	case *ast.Ellipsis:
		return "..." + typeToString(t.Elt)
	default:
		return "unknown"
	}
}

// exprToString converts an expression to a string (for const values).
func exprToString(expr ast.Expr) string {
	switch e := expr.(type) {
	case *ast.BasicLit:
		return e.Value
	case *ast.Ident:
		return e.Name
	case *ast.BinaryExpr:
		return exprToString(e.X) + " " + e.Op.String() + " " + exprToString(e.Y)
	case *ast.UnaryExpr:
		return e.Op.String() + exprToString(e.X)
	case *ast.CallExpr:
		return typeToString(e.Fun) + "(...)"
	default:
		return "..."
	}
}

// extractFuncSignature extracts parameter and result types from a function type.
func extractFuncSignature(ft *ast.FuncType) (params, results []string) {
	if ft.Params != nil {
		for _, field := range ft.Params.List {
			typeStr := typeToString(field.Type)
			if len(field.Names) == 0 {
				params = append(params, typeStr)
			} else {
				for range field.Names {
					params = append(params, typeStr)
				}
			}
		}
	}

	if ft.Results != nil {
		for _, field := range ft.Results.List {
			typeStr := typeToString(field.Type)
			if len(field.Names) == 0 {
				results = append(results, typeStr)
			} else {
				for range field.Names {
					results = append(results, typeStr)
				}
			}
		}
	}

	return
}

// extractReceiverType extracts the receiver type name and whether it's a pointer.
func extractReceiverType(expr ast.Expr) (string, bool) {
	switch t := expr.(type) {
	case *ast.StarExpr:
		name, _ := extractReceiverType(t.X)
		return name, true
	case *ast.Ident:
		return t.Name, false
	case *ast.IndexExpr:
		// Generic type T[U]
		name, _ := extractReceiverType(t.X)
		return name, false
	case *ast.IndexListExpr:
		// Generic type T[U, V]
		name, _ := extractReceiverType(t.X)
		return name, false
	default:
		return "unknown", false
	}
}

// formatSignature formats a function signature for display.
func formatSignature(name string, params, results []string) string {
	sig := name + "(" + strings.Join(params, ", ") + ")"
	if len(results) > 0 {
		if len(results) == 1 {
			sig += " " + results[0]
		} else {
			sig += " (" + strings.Join(results, ", ") + ")"
		}
	}
	return sig
}

// indexReferences indexes all symbol usages in a package using go/types info.
// This enables "Find References" functionality.
func (i *Indexer) indexReferences(ctx context.Context, ictx *indexer.Context, moduleURI string, pkg *packages.Package) error {
	if pkg.TypesInfo == nil || pkg.TypesInfo.Uses == nil {
		return nil
	}

	// Need module info to filter references
	if pkg.Module == nil {
		return nil
	}
	modulePath := pkg.Module.Path

	fset := pkg.Fset

	// Process all identifier usages
	for ident, obj := range pkg.TypesInfo.Uses {
		if obj == nil {
			continue
		}

		// Skip builtins and universe scope
		if obj.Pkg() == nil {
			continue
		}

		// Only track references to symbols within this module
		objPkgPath := obj.Pkg().Path()
		if !strings.HasPrefix(objPkgPath, modulePath) {
			continue
		}

		// Skip references to unexported symbols when ExportedOnly is set.
		// Unexported symbol nodes are never created, so these edges would be orphaned.
		if i.ExportedOnly && !obj.Exported() {
			continue
		}

		// Determine reference kind and target type
		kind, targetType := classifyReference(obj)
		if kind == "" {
			continue
		}

		// Build target URI based on object type
		targetURI := buildTargetURI(moduleURI, obj)
		if targetURI == "" {
			continue
		}

		// Get position of the reference
		pos := fset.Position(ident.Pos())
		if !pos.IsValid() {
			continue
		}

		// Create reference node
		refURI := fmt.Sprintf("%s/ref/%s:%d:%d", moduleURI, pos.Filename, pos.Line, pos.Column)

		refNode := graph.NewNode(types.TypeGoRef).
			WithURI(refURI).
			WithKey(refURI).
			WithName(ident.Name).
			WithData(types.RefData{
				Kind:       kind,
				Name:       ident.Name,
				TargetType: targetType,
				TargetPkg:  objPkgPath,
				Position: types.Position{
					File:   pos.Filename,
					Line:   pos.Line,
					Column: pos.Column,
				},
			})

		if err := ictx.Emitter.EmitNode(ctx, refNode); err != nil {
			return err
		}

		// Create edge from reference to target symbol
		targetID := graph.IDFromURI(targetURI)
		edge := graph.NewEdge(types.EdgeReferences, refNode.ID, targetID)
		if err := ictx.Emitter.EmitEdge(ctx, edge); err != nil {
			return err
		}
	}

	return nil
}

// classifyReference determines the kind and target type of a reference.
func classifyReference(obj gotypes.Object) (kind, targetType string) {
	switch o := obj.(type) {
	case *gotypes.Func:
		if o.Type().(*gotypes.Signature).Recv() != nil {
			return types.RefKindCall, types.TypeGoMethod
		}
		return types.RefKindCall, types.TypeGoFunc
	case *gotypes.TypeName:
		underlying := o.Type().Underlying()
		switch underlying.(type) {
		case *gotypes.Struct:
			return types.RefKindType, types.TypeGoStruct
		case *gotypes.Interface:
			return types.RefKindType, types.TypeGoInterface
		default:
			return types.RefKindType, types.TypeGoStruct // type alias or other
		}
	case *gotypes.Var:
		if o.IsField() {
			return types.RefKindField, types.TypeGoField
		}
		return types.RefKindValue, types.TypeGoVar
	case *gotypes.Const:
		return types.RefKindValue, types.TypeGoConst
	case *gotypes.PkgName:
		return types.RefKindImport, types.TypeGoPackage
	default:
		return "", ""
	}
}

// buildTargetURI constructs the URI for the target symbol.
func buildTargetURI(moduleURI string, obj gotypes.Object) string {
	if obj == nil || obj.Pkg() == nil {
		return ""
	}

	pkgPath := obj.Pkg().Path()
	name := obj.Name()

	switch o := obj.(type) {
	case *gotypes.Func:
		sig := o.Type().(*gotypes.Signature)
		if recv := sig.Recv(); recv != nil {
			// Method - get receiver type name
			recvType := recv.Type()
			if ptr, ok := recvType.(*gotypes.Pointer); ok {
				recvType = ptr.Elem()
			}
			if named, ok := recvType.(*gotypes.Named); ok {
				return moduleURI + "/pkg/" + pkgPath + "/method/" + named.Obj().Name() + "." + name
			}
		}
		return moduleURI + "/pkg/" + pkgPath + "/func/" + name
	case *gotypes.TypeName:
		underlying := o.Type().Underlying()
		switch underlying.(type) {
		case *gotypes.Interface:
			return moduleURI + "/pkg/" + pkgPath + "/interface/" + name
		default:
			return moduleURI + "/pkg/" + pkgPath + "/struct/" + name
		}
	case *gotypes.Var:
		if o.IsField() {
			// Field - need parent struct, which is complex to determine
			// For now, skip field references (would need more context)
			return ""
		}
		return moduleURI + "/pkg/" + pkgPath + "/var/" + name
	case *gotypes.Const:
		return moduleURI + "/pkg/" + pkgPath + "/const/" + name
	case *gotypes.PkgName:
		return moduleURI + "/pkg/" + pkgPath
	default:
		return ""
	}
}
