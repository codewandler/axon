package aql

import (
	"strconv"
	"strings"

	"github.com/alecthomas/participle/v2"
	"github.com/alecthomas/participle/v2/lexer"
)

// Parser is the AQL query parser.
// A Parser is safe for concurrent use by multiple goroutines.
type Parser struct {
	parser *participle.Parser[queryGrammar]
}

// NewParser creates a new AQL parser.
func NewParser() *Parser {
	p := participle.MustBuild[queryGrammar](
		participle.Lexer(aqlLexer),
		participle.CaseInsensitive("Ident"),
		participle.Map(unquoteString, "String"),
		participle.UseLookahead(3),
		participle.Elide("Whitespace", "Comment"),
	)
	return &Parser{parser: p}
}

// unquoteString removes surrounding quotes and unescapes ” to '.
func unquoteString(tok lexer.Token) (lexer.Token, error) {
	s := tok.Value
	if len(s) >= 2 && s[0] == '\'' && s[len(s)-1] == '\'' {
		s = s[1 : len(s)-1]
		s = strings.ReplaceAll(s, "''", "'")
		tok.Value = s
	}
	return tok, nil
}

// Parse parses an AQL query string and returns the AST.
func (p *Parser) Parse(input string) (*Query, error) {
	g, err := p.parser.ParseString("", input)
	if err != nil {
		return nil, err
	}
	return g.toAST(), nil
}

// Parse is a convenience function that creates a parser and parses the input.
// For repeated parsing, create a Parser once with NewParser() and reuse it.
func Parse(input string) (*Query, error) {
	p := NewParser()
	return p.Parse(input)
}

// ParseWithFilename parses with a filename for better error messages.
func (p *Parser) ParseWithFilename(filename, input string) (*Query, error) {
	g, err := p.parser.ParseString(filename, input)
	if err != nil {
		return nil, err
	}
	return g.toAST(), nil
}

// MustParse parses an AQL query string and panics on error.
// Useful for tests and static queries.
func (p *Parser) MustParse(input string) *Query {
	q, err := p.Parse(input)
	if err != nil {
		panic(err)
	}
	return q
}

// ----------------------------------------------------------------------------
// Grammar Structs (internal, parsed by participle)
// ----------------------------------------------------------------------------

type queryGrammar struct {
	Pos    lexer.Position
	Select *selectGrammar `parser:"@@"`
}

func (g *queryGrammar) toAST() *Query {
	return &Query{
		Position: toPosition(g.Pos),
		Select:   g.Select.toAST(),
	}
}

type selectGrammar struct {
	Pos      lexer.Position
	Distinct bool               `parser:"'SELECT' @'DISTINCT'?"`
	Columns  *columnsGrammar    `parser:"@@"`
	From     *sourceGrammar     `parser:"'FROM' @@"`
	Where    *exprGrammar       `parser:"('WHERE' @@)?"`
	GroupBy  []*selectorGrammar `parser:"('GROUP' 'BY' @@ (',' @@)*)?"`
	Having   *exprGrammar       `parser:"('HAVING' @@)?"`
	OrderBy  []*orderGrammar    `parser:"('ORDER' 'BY' @@ (',' @@)*)?"`
	Limit    *int               `parser:"('LIMIT' @Number)?"`
	Offset   *int               `parser:"('OFFSET' @Number)?"`
}

func (g *selectGrammar) toAST() *SelectStmt {
	stmt := &SelectStmt{
		Position: toPosition(g.Pos),
		Distinct: g.Distinct,
		Columns:  g.Columns.toAST(),
		From:     g.From.toAST(),
		Limit:    g.Limit,
		Offset:   g.Offset,
	}
	if g.Where != nil {
		stmt.Where = g.Where.toAST()
	}
	for _, gb := range g.GroupBy {
		stmt.GroupBy = append(stmt.GroupBy, *gb.toAST())
	}
	if g.Having != nil {
		stmt.Having = g.Having.toAST()
	}
	for _, ob := range g.OrderBy {
		stmt.OrderBy = append(stmt.OrderBy, *ob.toAST())
	}
	return stmt
}

// ----------------------------------------------------------------------------
// Columns
// ----------------------------------------------------------------------------

type columnsGrammar struct {
	Pos     lexer.Position
	Star    bool             `parser:"( @'*'"`
	Columns []*columnGrammar `parser:"| @@ (',' @@)* )"`
}

func (g *columnsGrammar) toAST() []Column {
	if g.Star {
		return []Column{{
			Position: toPosition(g.Pos),
			Expr:     &Star{Position: toPosition(g.Pos)},
		}}
	}
	var cols []Column
	for _, c := range g.Columns {
		cols = append(cols, *c.toAST())
	}
	return cols
}

type columnGrammar struct {
	Pos      lexer.Position
	Count    bool             `parser:"( @('COUNT' '(' '*' ')')"`
	Selector *selectorGrammar `parser:"| @@ )"`
	Alias    string           `parser:"('AS' @Ident)?"`
}

func (g *columnGrammar) toAST() *Column {
	col := &Column{
		Position: toPosition(g.Pos),
		Alias:    g.Alias,
	}
	if g.Count {
		col.Expr = &CountCall{Position: toPosition(g.Pos)}
	} else {
		col.Expr = g.Selector.toAST()
	}
	return col
}

// ----------------------------------------------------------------------------
// Source
// ----------------------------------------------------------------------------

type sourceGrammar struct {
	Pos       lexer.Position
	Table     string            `parser:"( @('nodes' | 'edges')"`
	TableFunc *tableFuncGrammar `parser:"  (',' @@)?"`
	Patterns  []*patternGrammar `parser:"| @@ (',' @@)* )"`
}

func (g *sourceGrammar) toAST() Source {
	if g.Table != "" {
		if g.TableFunc != nil {
			return &JoinedTableSource{
				Position:  toPosition(g.Pos),
				Table:     strings.ToLower(g.Table),
				TableFunc: g.TableFunc.toAST(),
			}
		}
		return &TableSource{
			Position: toPosition(g.Pos),
			Table:    strings.ToLower(g.Table),
		}
	}
	ps := &PatternSource{Position: toPosition(g.Pos)}
	for _, p := range g.Patterns {
		ps.Patterns = append(ps.Patterns, p.toAST())
	}
	return ps
}

// tableFuncGrammar represents a table-valued function call like json_each(labels).
type tableFuncGrammar struct {
	Pos   lexer.Position
	Name  string           `parser:"@Ident '('"`
	Arg   *selectorGrammar `parser:"@@ ')'"`
	Alias string           `parser:"('AS' @Ident)?"`
}

func (g *tableFuncGrammar) toAST() *TableFunc {
	return &TableFunc{
		Position: toPosition(g.Pos),
		Name:     strings.ToLower(g.Name),
		Arg:      g.Arg.toAST(),
		Alias:    g.Alias,
	}
}

// ----------------------------------------------------------------------------
// Pattern
// ----------------------------------------------------------------------------

type patternGrammar struct {
	Pos   lexer.Position
	First *nodePatternGrammar   `parser:"@@"`
	Rest  []*patternRestGrammar `parser:"@@*"`
}

func (g *patternGrammar) toAST() *Pattern {
	p := &Pattern{Position: toPosition(g.Pos)}
	p.Elements = append(p.Elements, g.First.toAST())
	for _, r := range g.Rest {
		p.Elements = append(p.Elements, r.Edge.toAST())
		p.Elements = append(p.Elements, r.Node.toAST())
	}
	return p
}

type patternRestGrammar struct {
	Edge *edgePatternGrammar `parser:"@@"`
	Node *nodePatternGrammar `parser:"@@"`
}

type nodePatternGrammar struct {
	Pos          lexer.Position
	Variable     string       `parser:"'(' ( @Ident"`
	TypeDomain   string       `parser:"    (':' @Ident"`
	TypeName     string       `parser:"     ':' @(Ident | Glob))?"`
	TypeOnlyDom  string       `parser:"  | ':' @Ident"`
	TypeOnlyName string       `parser:"    ':' @(Ident | Glob)"`
	Where        *exprGrammar `parser:"  )? ('WHERE' @@)? ')'"`
}

func (g *nodePatternGrammar) toAST() *NodePattern {
	np := &NodePattern{
		Position: toPosition(g.Pos),
		Variable: g.Variable,
	}
	// Handle type - could come from Variable:Domain:Name or :Domain:Name
	if g.TypeDomain != "" && g.TypeName != "" {
		np.Type = g.TypeDomain + ":" + g.TypeName
	} else if g.TypeOnlyDom != "" && g.TypeOnlyName != "" {
		np.Type = g.TypeOnlyDom + ":" + g.TypeOnlyName
	}
	if g.Where != nil {
		np.Where = g.Where.toAST()
	}
	return np
}

type edgePatternGrammar struct {
	Pos lexer.Position
	// Direction prefix
	Incoming bool `parser:"( @'<-'"`
	Outgoing bool `parser:"| @'-' ) '['"`
	// Edge contents - use nested struct
	Inner *edgeInnerGrammar `parser:"@@?"`
	// Quantifier
	Star     bool `parser:"@'*'?"`
	MinHops  *int `parser:"@Number?"`
	HasRange bool `parser:"@Range?"`
	MaxHops  *int `parser:"@Number?"`
	// Direction suffix
	OutArrow bool `parser:"']' ( @'->'"`
	EndDash  bool `parser:"    | @'-' )"`
}

type edgeInnerGrammar struct {
	Pos lexer.Position
	// Simple capture: optionally colon, optionally ident, optionally colon, optionally ident
	// This matches: [], [:type], [var], [var:type], [var:], [:type1|type2|...]
	Colon1 bool   `parser:"@':'?"`
	Ident1 string `parser:"@Ident?"`
	Colon2 bool   `parser:"@':'?"`
	Ident2 string `parser:"@Ident?"`
	// Additional types for multi-type syntax: [:type1|type2]
	RestTypes []string `parser:"( '|' @Ident )*"`
}

func (g *edgePatternGrammar) toAST() *EdgePattern {
	ep := &EdgePattern{
		Position: toPosition(g.Pos),
	}

	// Handle variable and type from inner struct
	// Parsing results:
	// []           -> C1=false I1="" C2=false I2=""
	// [:type]      -> C1=true  I1="type" C2=false I2=""
	// [var]        -> C1=false I1="var" C2=false I2=""
	// [var:type]   -> C1=false I1="var" C2=true I2="type"
	// [var:]       -> C1=false I1="var" C2=true I2=""
	// [:type1|t2]  -> C1=true  I1="type1" C2=false I2="" RestTypes=["t2"]
	if g.Inner != nil {
		if g.Inner.Colon1 && g.Inner.Ident1 != "" {
			// [:type] or [:type1|type2|...]
			if len(g.Inner.RestTypes) > 0 {
				// Multi-type: [:type1|type2]
				ep.Types = append([]string{g.Inner.Ident1}, g.Inner.RestTypes...)
			} else {
				// Single type: [:type]
				ep.Type = g.Inner.Ident1
			}
		} else if g.Inner.Ident1 != "" && g.Inner.Colon2 {
			// [var:type] or [var:]
			ep.Variable = g.Inner.Ident1
			if len(g.Inner.RestTypes) > 0 {
				// Multi-type with variable: [var:type1|type2]
				ep.Types = append([]string{g.Inner.Ident2}, g.Inner.RestTypes...)
			} else {
				// Single type: [var:type]
				ep.Type = g.Inner.Ident2
			}
		} else if g.Inner.Ident1 != "" {
			// [var]
			ep.Variable = g.Inner.Ident1
		}
	}
	// else: [] - empty edge, no var or type

	// Determine direction
	if g.Incoming && g.EndDash {
		ep.Direction = Incoming // <-[]-
	} else if g.Outgoing && g.OutArrow {
		ep.Direction = Outgoing // -[]->
	} else {
		ep.Direction = Undirected // -[]-
	}

	// Handle quantifier
	if g.Star {
		// Variable length path
		if g.MinHops != nil {
			ep.MinHops = g.MinHops
		} else {
			// Default: *  means 1..unbounded
			one := 1
			ep.MinHops = &one
		}
		if g.HasRange && g.MaxHops != nil {
			ep.MaxHops = g.MaxHops
		} else if g.HasRange {
			// *1.. means 1..unbounded (MaxHops stays nil)
		} else if g.MinHops != nil && !g.HasRange {
			// *3 means exactly 3 hops
			ep.MaxHops = g.MinHops
		}
		// else: * alone means 1..unbounded (MaxHops stays nil)
	}

	return ep
}

// ----------------------------------------------------------------------------
// Order
// ----------------------------------------------------------------------------

type orderGrammar struct {
	Pos      lexer.Position
	Count    bool             `parser:"( @('COUNT' '(' '*' ')')"`
	Selector *selectorGrammar `parser:"| @@ )"`
	Desc     bool             `parser:"( @'DESC' | 'ASC' )?"`
}

func (g *orderGrammar) toAST() *OrderSpec {
	os := &OrderSpec{
		Position:   toPosition(g.Pos),
		Descending: g.Desc,
	}
	if g.Count {
		os.Expr = &CountCall{Position: toPosition(g.Pos)}
	} else {
		os.Expr = g.Selector.toAST()
	}
	return os
}

// ----------------------------------------------------------------------------
// Selector
// ----------------------------------------------------------------------------

type selectorGrammar struct {
	Pos   lexer.Position
	Parts []string `parser:"@Ident ('.' @Ident)*"`
}

func (g *selectorGrammar) toAST() *Selector {
	return &Selector{
		Position: toPosition(g.Pos),
		Parts:    g.Parts,
	}
}

// ----------------------------------------------------------------------------
// Expressions
// ----------------------------------------------------------------------------

type exprGrammar struct {
	Pos lexer.Position
	Or  *orExprGrammar `parser:"@@"`
}

func (g *exprGrammar) toAST() Expression {
	return g.Or.toAST()
}

type orExprGrammar struct {
	Pos   lexer.Position
	Left  *andExprGrammar   `parser:"@@"`
	Right []*andExprGrammar `parser:"('OR' @@)*"`
}

func (g *orExprGrammar) toAST() Expression {
	result := g.Left.toAST()
	for _, r := range g.Right {
		result = &BinaryExpr{
			Position: toPosition(g.Pos),
			Left:     result,
			Op:       OpOr,
			Right:    r.toAST(),
		}
	}
	return result
}

type andExprGrammar struct {
	Pos   lexer.Position
	Left  *notExprGrammar   `parser:"@@"`
	Right []*notExprGrammar `parser:"('AND' @@)*"`
}

func (g *andExprGrammar) toAST() Expression {
	result := g.Left.toAST()
	for _, r := range g.Right {
		result = &BinaryExpr{
			Position: toPosition(g.Pos),
			Left:     result,
			Op:       OpAnd,
			Right:    r.toAST(),
		}
	}
	return result
}

type notExprGrammar struct {
	Pos       lexer.Position
	NotExists *existsExprGrammar  `parser:"  'NOT' 'EXISTS' @@"`
	Not       bool                `parser:"| @'NOT'"`
	Operand   *primaryExprGrammar `parser:"  @@"`
	Primary   *primaryExprGrammar `parser:"| @@"`
}

func (g *notExprGrammar) toAST() Expression {
	if g.NotExists != nil {
		return &ExistsExpr{
			Position: toPosition(g.Pos),
			Not:      true,
			Pattern:  g.NotExists.Pattern.toAST(),
		}
	}
	if g.Not {
		return &UnaryExpr{
			Position: toPosition(g.Pos),
			Op:       OpNot,
			Operand:  g.Operand.toAST(),
		}
	}
	return g.Primary.toAST()
}

type primaryExprGrammar struct {
	Pos        lexer.Position
	Exists     *existsExprGrammar     `parser:"  'EXISTS' @@"`
	Paren      *exprGrammar           `parser:"| '(' @@ ')'"`
	Comparison *comparisonExprGrammar `parser:"| @@"`
}

func (g *primaryExprGrammar) toAST() Expression {
	if g.Exists != nil {
		return &ExistsExpr{
			Position: toPosition(g.Pos),
			Not:      false,
			Pattern:  g.Exists.Pattern.toAST(),
		}
	}
	if g.Paren != nil {
		return &ParenExpr{
			Position: toPosition(g.Pos),
			Inner:    g.Paren.toAST(),
		}
	}
	return g.Comparison.toAST()
}

type existsExprGrammar struct {
	Pos     lexer.Position
	Pattern *patternGrammar `parser:"@@"`
}

type comparisonExprGrammar struct {
	Pos lexer.Position

	// COUNT(*) comparison (for HAVING)
	Count    bool          `parser:"( @('COUNT' '(' '*' ')')"`
	CountOp  string        `parser:"  @( '<=' | '>=' | '!=' | '=' | '<' | '>' )"`
	CountVal *valueGrammar `parser:"  @@ )"`

	// Regular selector-based expressions
	Selector *selectorGrammar `parser:"| ( @@"`

	// IS [NOT] NULL
	IsNull    bool `parser:"  ( 'IS'"`
	IsNotNull bool `parser:"    @'NOT'?"`
	Null      bool `parser:"    @'NULL'"`

	// CONTAINS [ANY|ALL] / NOT CONTAINS
	NotContains bool     `parser:"  | @('NOT' 'CONTAINS')"`
	LabelList1  []string `parser:"    '(' @String (',' @String)* ')'"`

	ContainsAny bool     `parser:"  | 'CONTAINS' @'ANY'"`
	LabelList2  []string `parser:"    '(' @String (',' @String)* ')'"`

	ContainsAll bool     `parser:"  | 'CONTAINS' @'ALL'"`
	LabelList3  []string `parser:"    '(' @String (',' @String)* ')'"`

	// IN (...)
	In       bool            `parser:"  | @'IN'"`
	InValues []*valueGrammar `parser:"    '(' @@ (',' @@)* ')'"`

	// BETWEEN ... AND ...
	Between     bool          `parser:"  | @'BETWEEN'"`
	BetweenLow  *valueGrammar `parser:"    @@"`
	BetweenAnd  bool          `parser:"    'AND'"`
	BetweenHigh *valueGrammar `parser:"    @@"`

	// Comparison operators
	Op    string        `parser:"  | @( '<=' | '>=' | '!=' | '=' | '<' | '>' | 'LIKE' | 'GLOB' )"`
	Value *valueGrammar `parser:"    @@ )? )"`
}

func (g *comparisonExprGrammar) toAST() Expression {
	// COUNT(*) comparison (for HAVING)
	if g.Count {
		return &ComparisonExpr{
			Position: toPosition(g.Pos),
			Left:     &Selector{Position: toPosition(g.Pos), Parts: []string{"COUNT(*)"}},
			Op:       parseComparisonOp(g.CountOp),
			Right:    g.CountVal.toAST(),
		}
	}

	sel := g.Selector.toAST()

	// IS [NOT] NULL
	if g.Null {
		return &IsNullExpr{
			Position: toPosition(g.Pos),
			Selector: sel,
			Not:      g.IsNotNull,
		}
	}

	// NOT CONTAINS
	if g.NotContains {
		return &LabelExpr{
			Position: toPosition(g.Pos),
			Selector: sel,
			Op:       OpNotContains,
			Labels:   stringsToValues(g.LabelList1),
		}
	}

	// CONTAINS ANY
	if g.ContainsAny {
		return &LabelExpr{
			Position: toPosition(g.Pos),
			Selector: sel,
			Op:       OpContainsAny,
			Labels:   stringsToValues(g.LabelList2),
		}
	}

	// CONTAINS ALL
	if g.ContainsAll {
		return &LabelExpr{
			Position: toPosition(g.Pos),
			Selector: sel,
			Op:       OpContainsAll,
			Labels:   stringsToValues(g.LabelList3),
		}
	}

	// IN
	if g.In {
		var values []Value
		for _, v := range g.InValues {
			values = append(values, v.toAST())
		}
		return &InExpr{
			Position: toPosition(g.Pos),
			Left:     sel,
			Values:   values,
		}
	}

	// BETWEEN
	if g.Between {
		return &BetweenExpr{
			Position: toPosition(g.Pos),
			Left:     sel,
			Low:      g.BetweenLow.toAST(),
			High:     g.BetweenHigh.toAST(),
		}
	}

	// Comparison operator
	if g.Op != "" && g.Value != nil {
		return &ComparisonExpr{
			Position: toPosition(g.Pos),
			Left:     sel,
			Op:       parseComparisonOp(g.Op),
			Right:    g.Value.toAST(),
		}
	}

	// Just a selector (shouldn't happen in valid queries, but handle it)
	return sel
}

// ----------------------------------------------------------------------------
// Values
// ----------------------------------------------------------------------------

type valueGrammar struct {
	Pos        lexer.Position
	String     *string `parser:"  @String"`
	Number     *string `parser:"| @Number"`
	True       bool    `parser:"| @'TRUE'"`
	False      bool    `parser:"| @'FALSE'"`
	NamedParam *string `parser:"| @NamedParam"`
	PosParam   *string `parser:"| @PosParam"`
}

func (g *valueGrammar) toAST() Value {
	pos := toPosition(g.Pos)

	switch {
	case g.String != nil:
		return &StringLit{Position: pos, Value: *g.String}

	case g.Number != nil:
		val, _ := strconv.ParseFloat(*g.Number, 64)
		isInt := !strings.Contains(*g.Number, ".")
		return &NumberLit{Position: pos, Value: val, IsInt: isInt}

	case g.True:
		return &BoolLit{Position: pos, Value: true}

	case g.False:
		return &BoolLit{Position: pos, Value: false}

	case g.NamedParam != nil:
		// $name -> name
		name := strings.TrimPrefix(*g.NamedParam, "$")
		return &Parameter{Position: pos, Name: name}

	case g.PosParam != nil:
		// $1 -> 1
		idxStr := strings.TrimPrefix(*g.PosParam, "$")
		idx, _ := strconv.Atoi(idxStr)
		return &Parameter{Position: pos, Index: idx}

	default:
		// Unreachable: participle grammar guarantees one alternative matches
		return &StringLit{Position: pos, Value: ""}
	}
}

// ----------------------------------------------------------------------------
// Helpers
// ----------------------------------------------------------------------------

func toPosition(p lexer.Position) Position {
	return Position{
		Line:   p.Line,
		Column: p.Column,
		Offset: p.Offset,
	}
}

func parseComparisonOp(s string) ComparisonOp {
	switch strings.ToUpper(s) {
	case "=":
		return OpEq
	case "!=":
		return OpNe
	case "<":
		return OpLt
	case "<=":
		return OpLe
	case ">":
		return OpGt
	case ">=":
		return OpGe
	case "LIKE":
		return OpLike
	case "GLOB":
		return OpGlob
	default:
		// Unreachable: grammar only allows valid operators
		return OpEq
	}
}

func stringsToValues(ss []string) []Value {
	var values []Value
	for _, s := range ss {
		values = append(values, &StringLit{Value: s})
	}
	return values
}
