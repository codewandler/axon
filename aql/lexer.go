package aql

import (
	"github.com/alecthomas/participle/v2/lexer"
)

// aqlLexer defines the lexical rules for AQL.
//
// Note: The grammar handles type patterns like "fs:file" as Ident ":" Ident.
// We don't use a special TypePattern token to avoid ambiguity in patterns
// like "(n:fs:file)" where "n" is the variable and "fs:file" is the type.
var aqlLexer = lexer.MustStateful(lexer.Rules{
	"Root": {
		// Whitespace - skip
		{Name: "Whitespace", Pattern: `[\s]+`},

		// Comments - skip
		{Name: "Comment", Pattern: `--[^\n]*`},

		// Multi-character operators (must come before single char)
		{Name: "Arrow", Pattern: `->|<-`},
		{Name: "Comparison", Pattern: `<=|>=|!=`},
		{Name: "Range", Pattern: `\.\.`},

		// String literal (single quotes, '' for escape)
		{Name: "String", Pattern: `'(?:''|[^'])*'`},

		// Numbers (integer and float)
		{Name: "Number", Pattern: `\d+(?:\.\d+)?`},

		// Parameters
		{Name: "NamedParam", Pattern: `\$[a-zA-Z_][a-zA-Z0-9_]*`},
		{Name: "PosParam", Pattern: `\$\d+`},

		// Identifiers (including keywords - keywords handled by participle)
		// Also matches type patterns like fs:file, fs:*, glob patterns
		{Name: "Ident", Pattern: `[a-zA-Z_][a-zA-Z0-9_]*`},

		// Glob wildcards (standalone)
		{Name: "Glob", Pattern: `[*?]+`},

		// Pipe for edge type alternatives (must come before Punct)
		{Name: "Pipe", Pattern: `\|`},

		// Single character operators and punctuation
		{Name: "Punct", Pattern: `[-(),.:[\]=<>]`},
	},
})

// TokenType constants for reference.
const (
	TokenWhitespace = "Whitespace"
	TokenComment    = "Comment"
	TokenArrow      = "Arrow"
	TokenComparison = "Comparison"
	TokenRange      = "Range"
	TokenString     = "String"
	TokenNumber     = "Number"
	TokenNamedParam = "NamedParam"
	TokenPosParam   = "PosParam"
	TokenIdent      = "Ident"
	TokenGlob       = "Glob"
	TokenPipe       = "Pipe"
	TokenPunct      = "Punct"
)
