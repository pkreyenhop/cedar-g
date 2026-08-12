package mesa

import (
	"fmt"
	"strings"
)

// Parser is a hand-written recursive-descent parser over a fully
// tokenised source. Having the whole token slice available lets us do
// small fixed lookaheads (e.g. declaration vs. statement) without a
// separate scanner buffer.
type Parser struct {
	toks      []Token
	pos       int
	recovered int // statements skipped by error recovery
}

func NewParser(toks []Token) *Parser { return &Parser{toks: toks} }

func (p *Parser) cur() Token { return p.toks[p.pos] }
func (p *Parser) peek() Token { // one past current
	if p.pos+1 < len(p.toks) {
		return p.toks[p.pos+1]
	}
	return p.toks[len(p.toks)-1] // EOF
}
func (p *Parser) advance() Token {
	t := p.toks[p.pos]
	if p.pos < len(p.toks)-1 {
		p.pos++
	}
	return t
}

func (p *Parser) isKw(kw string) bool {
	t := p.cur()
	return t.Kind == TKeyword && t.Text == kw
}
func (p *Parser) isPunct(s string) bool {
	t := p.cur()
	return t.Kind == TPunct && t.Text == s
}
func (p *Parser) acceptKw(kw string) bool {
	if p.isKw(kw) {
		p.advance()
		return true
	}
	return false
}
func (p *Parser) acceptPunct(s string) bool {
	if p.isPunct(s) {
		p.advance()
		return true
	}
	return false
}

type parseError struct{ msg string }

func (e parseError) Error() string { return e.msg }

func (p *Parser) fail(format string, a ...any) {
	t := p.cur()
	panic(parseError{fmt.Sprintf("parse error at line %d:%d (near %q): %s",
		t.Line, t.Col, t.String(), fmt.Sprintf(format, a...))})
}

func (p *Parser) expectPunct(s string) {
	if !p.acceptPunct(s) {
		p.fail("expected %q", s)
	}
}
func (p *Parser) expectKw(kw string) {
	if !p.acceptKw(kw) {
		p.fail("expected keyword %s", kw)
	}
}
func (p *Parser) expectIdent() string {
	t := p.cur()
	if t.Kind != TIdent {
		p.fail("expected identifier")
	}
	p.advance()
	return t.Text
}

// ParseModule is the top-level entry: NAME: KIND = block.
func (p *Parser) ParseModule() (m *Module, err error) {
	defer func() {
		if r := recover(); r != nil {
			if pe, ok := r.(parseError); ok {
				err = pe
				return
			}
			panic(r)
		}
	}()
	m = &Module{}
	// Cedar files may open with a DIRECTORY clause listing imported interfaces.
	m.Imports = p.skipDirectory()
	m.Name = p.expectIdent()
	p.expectPunct(":")
	// The head runs up to the binding ('=' or Cedar's '~'). It carries optional
	// qualifiers (CEDAR, SAFE, ...), the module kind, and IMPORTS/EXPORTS/params;
	// we record the kind and the IMPORTS local names, and skip the rest (tracking
	// bracket depth so a '=' in a default value doesn't end the head early).
	m.Kind = "MODULE"
	depth := 0
	inImports := false
	expectImportName := false
	for p.cur().Kind != TEOF {
		if depth == 0 && (p.isPunct("=") || p.isPunct("~")) {
			break
		}
		t := p.cur()
		switch {
		case p.isKw("PROGRAM"):
			m.Kind = "PROGRAM"
		case p.isKw("DEFINITIONS"):
			m.Kind = "DEFINITIONS"
		case p.isKw("MONITOR"):
			m.Kind = "MONITOR"
		case t.Kind == TIdent && t.Text == "IMPORTS":
			inImports, expectImportName = true, true
		case t.Kind == TIdent && (t.Text == "EXPORTS" || t.Text == "SHARES" || t.Text == "LOCKS"):
			inImports = false
		case p.isPunct("[") || p.isPunct("("):
			depth++
		case p.isPunct("]") || p.isPunct(")"):
			depth--
		case inImports && depth == 0 && p.isPunct(","):
			expectImportName = true
		case inImports && depth == 0 && expectImportName && t.Kind == TIdent:
			m.Imports = append(m.Imports, t.Text) // the local name of an import
			expectImportName = false
		}
		p.advance()
	}
	if !p.acceptBind() {
		p.fail("expected '=' or '~'")
	}
	m.Body = p.parseBlock()
	p.acceptPunct(".") // trailing '.' after END is optional
	m.Recovered = p.recovered
	return m, nil
}

// acceptBind accepts a binding operator: Mesa '=' or Cedar '~'.
func (p *Parser) acceptBind() bool {
	return p.acceptPunct("=") || p.acceptPunct("~")
}

// skipDirectory consumes a leading "DIRECTORY … ;" clause and returns the
// interface names it lists (the first identifier of each comma-separated entry).
func (p *Parser) skipDirectory() []string {
	if !p.acceptKw("DIRECTORY") {
		return nil
	}
	var names []string
	depth := 0
	expectName := true // at the start of a comma-separated entry
	for p.cur().Kind != TEOF {
		t := p.cur()
		switch {
		case p.isPunct("[") || p.isPunct("("):
			depth++
		case p.isPunct("]") || p.isPunct(")"):
			depth--
		case depth == 0 && p.isPunct(";"):
			p.advance()
			return names
		case depth == 0 && p.isPunct(","):
			expectName = true
		case depth == 0 && expectName && t.Kind == TIdent:
			names = append(names, t.Text)
			expectName = false
		}
		p.advance()
	}
	return names
}

// blockPrefixWords are access/safety qualifiers that may sit directly before a
// block body: the module body "= PUBLIC {…}" or a "TRUSTED BEGIN …" body.
var blockPrefixWords = map[string]bool{
	"PUBLIC": true, "PRIVATE": true,
	"TRUSTED": true, "CHECKED": true, "UNCHECKED": true,
}

// parseBlock handles BEGIN..END and {..} bodies.
func (p *Parser) parseBlock() *Block {
	line := p.cur().Line
	for p.cur().Kind == TIdent && blockPrefixWords[p.cur().Text] {
		p.advance()
	}
	var closer string
	switch {
	case p.acceptKw("BEGIN"):
		closer = "END"
	case p.acceptPunct("{"):
		closer = "}"
	default:
		p.fail("expected BEGIN or '{'")
	}
	handlers := p.blockPrologue()
	items := p.parseStmtSeq(closer)
	// A trailing "EXITS label => stmt; …" clause names block-exit handlers.
	if p.acceptWord("EXITS") {
		for !p.atStop(closer) && p.cur().Kind != TEOF {
			p.skipToArrow()
			if !p.acceptPunct("=>") {
				break
			}
			p.parseStmt()
			p.acceptPunct(";")
		}
	}
	// Close the block, tolerating a mismatched/missing closer (which a skip or a
	// nested-block boundary may have consumed): resync to it, and if it is still
	// absent, treat that as a recovery rather than aborting the whole module.
	if !p.atStop(closer) {
		p.resync(closer)
	}
	var closed bool
	if closer == "END" {
		closed = p.acceptKw("END")
	} else {
		closed = p.acceptPunct("}")
	}
	if !closed {
		p.recovered++
	}
	return &Block{Items: items, Handlers: handlers, Line: line}
}

// atStop reports whether the current token ends the current sequence.
func (p *Parser) atStop(closer string) bool {
	return p.isKw(closer) || p.isPunct(closer)
}

// blockPrologue consumes leading "OPEN interfaces;" clauses (skipped) and
// "ENABLE handlers;" clauses (captured), which may open a block or loop body,
// and returns the handlers active over the block.
func (p *Parser) blockPrologue() []Handler {
	var handlers []Handler
	for {
		if p.acceptWord("OPEN") {
			for p.cur().Kind != TEOF && !p.isPunct(";") {
				p.advance()
			}
			p.acceptPunct(";")
			continue
		}
		if p.acceptWord("ENABLE") {
			if p.isPunct("{") {
				p.advance()
				handlers = append(handlers, p.parseHandlers("}")...)
				p.acceptPunct("}")
			} else {
				handlers = append(handlers, p.parseHandlers(";")...)
			}
			p.acceptPunct(";")
			continue
		}
		break
	}
	return handlers
}

func (p *Parser) parseStmtSeq(closer string) []Stmt {
	var items []Stmt
	for {
		for p.acceptPunct(";") { // absorb empty statements / separators
		}
		if p.atStop(closer) || p.cur().Kind == TEOF ||
			(p.cur().Kind == TIdent && p.cur().Text == "EXITS") {
			break
		}
		start := p.pos
		stmt, ok := p.tryParseItem()
		if !ok {
			// Error recovery: skip the unparseable statement and continue, so one
			// unsupported construct does not fail the whole module.
			p.recovered++
			p.resync(closer)
			if p.pos == start {
				p.advance() // guarantee forward progress
			}
			continue
		}
		items = append(items, stmt)
		if !p.acceptPunct(";") {
			break
		}
	}
	return items
}

// tryParseItem parses one item, converting a parse error into ok=false so the
// caller can recover instead of aborting the whole parse.
func (p *Parser) tryParseItem() (stmt Stmt, ok bool) {
	defer func() {
		if r := recover(); r != nil {
			if _, isPE := r.(parseError); isPE {
				stmt, ok = nil, false
				return
			}
			panic(r)
		}
	}()
	return p.parseItem(), true
}

// resync advances to the next synchronisation point after a failed statement:
// a ';' or the block closer at the current depth, or an enclosing closer.
func (p *Parser) resync(closer string) {
	depth := 0
	for p.cur().Kind != TEOF {
		if depth == 0 {
			if p.isPunct(";") || p.atStop(closer) ||
				p.isPunct("}") || p.isPunct("]") || p.isPunct(")") || p.isKw("END") {
				return
			}
		}
		switch {
		case p.isPunct("[") || p.isPunct("(") || p.isPunct("{"):
			depth++
		case p.isPunct("]") || p.isPunct(")") || p.isPunct("}"):
			depth--
		}
		p.advance()
	}
}

// ---- Declaration vs. statement ----

// declAhead reports whether the upcoming tokens form a declaration,
// i.e. an identifier list terminated by ':'.
func (p *Parser) declAhead() bool {
	j := p.pos
	if p.toks[j].Kind != TIdent {
		return false
	}
	for {
		if p.toks[j].Kind != TIdent {
			return false
		}
		j++
		t := p.toks[j]
		if t.Kind == TPunct && t.Text == ":" {
			return true
		}
		if t.Kind == TPunct && t.Text == "," {
			j++
			continue
		}
		return false
	}
}

func (p *Parser) parseItem() Stmt {
	if p.declAhead() {
		return p.parseDecl()
	}
	return p.parseStmt()
}

func (p *Parser) parseIdList() []string {
	names := []string{p.expectIdent()}
	for p.acceptPunct(",") {
		names = append(names, p.expectIdent())
	}
	return names
}

func (p *Parser) parseDecl() Stmt {
	line := p.cur().Line
	names := p.parseIdList()
	p.expectPunct(":")

	// Access qualifiers (PUBLIC/PRIVATE/READONLY) may precede TYPE or a type.
	for p.cur().Kind == TIdent && fieldQualifiers[p.cur().Text] {
		p.advance()
	}

	// TYPE declaration: NAME: TYPE [= typeExpr]. A bare "NAME: TYPE" is an
	// opaque (abstract) type, common in DEFINITIONS interfaces.
	if p.acceptKw("TYPE") {
		if p.isPunct("[") { // TYPE[attributes] — e.g. TYPE[UNITS[INT32]]
			p.skipBrackets()
		}
		if !p.acceptBind() && !p.acceptPunct("<-") {
			return &TypeDecl{Name: names[0], Type: &NamedType{Name: names[0]}, Line: line}
		}
		t := p.parseType()
		if p.acceptPunct("<-") && p.startsValue() { // an optional type default value
			p.parseValueExpr()
		}
		return &TypeDecl{Name: names[0], Type: t, Line: line}
	}

	t := p.parseType()

	// Procedure declaration: NAME: <procType> [= | ~ | ←] [qualifiers] body. The
	// type may be an inline PROC[..] or a named/qualified proc type; the binding
	// may be followed by body qualifiers (INLINE, ENTRY, TRUSTED, MACHINE CODE, …)
	// before the block. It is a proc decl when a block ultimately follows.
	if p.isPunct("=") || p.isPunct("~") || p.isPunct("<-") {
		j := p.pos + 1
		for j < len(p.toks) && p.toks[j].Kind == TIdent && bodyQualifiers[p.toks[j].Text] {
			j++
		}
		blockNext := j < len(p.toks) && ((p.toks[j].Kind == TPunct && p.toks[j].Text == "{") ||
			(p.toks[j].Kind == TKeyword && p.toks[j].Text == "BEGIN"))
		if blockNext {
			p.advance() // the binding
			for p.cur().Kind == TIdent && bodyQualifiers[p.cur().Text] {
				p.advance()
			}
			body := p.parseBlock()
			pt, _ := t.(*ProcType)
			if pt == nil {
				pt = &ProcType{}
			}
			return &ProcDecl{Name: names[0], Type: pt, Body: body, Line: line}
		}
	}

	var init Expr
	isConst := false
	if p.acceptBind() {
		isConst = true
		init = p.parseValueExpr()
	} else if p.acceptPunct("<-") {
		init = p.parseValueExpr()
	}
	return &VarDecl{Names: names, Type: t, Init: init, IsConst: isConst, Line: line}
}

// ---- Types ----

// typeQualifiers are leading words that decorate a type; we accept and drop
// them so the underlying type still parses.
var typeQualifiers = map[string]bool{
	"PACKED": true, "ORDERED": true, "BASE": true, "LONG": true,
	"READONLY": true, "VAR": true, "MONITORED": true, "RELATIVE": true,
	"PRIVATE": true, "PUBLIC": true,
	"ENTRY": true, "INTERNAL": true, "SAFE": true, "UNSAFE": true,
	"UNCOUNTED": true, "COMPUTED": true, "OVERLAID": true,
}

// bodyQualifiers may sit between a procedure's binding and its block body.
var bodyQualifiers = map[string]bool{
	"INLINE": true, "TRUSTED": true, "CHECKED": true, "UNCHECKED": true,
	"SAFE": true, "UNSAFE": true, "ENTRY": true, "INTERNAL": true,
	"MACHINE": true, "CODE": true,
}

func (p *Parser) parseType() TypeExpr {
	// Skip decorating qualifiers (PACKED ARRAY, LONG POINTER, MACHINE DEPENDENT…)
	// and a representation prefix before MACHINE, e.g. WORD32 MACHINE DEPENDENT.
	for (p.cur().Kind == TIdent && typeQualifiers[p.cur().Text]) ||
		(p.cur().Kind == TIdent && p.peek().Kind == TIdent && p.peek().Text == "MACHINE") {
		p.advance()
	}
	if p.cur().Kind == TIdent && p.cur().Text == "MACHINE" {
		p.advance()
		p.acceptWord("DEPENDENT")
	}

	// Cedar reference and collection types. These are interface-level types the
	// interpreter does not execute, so we consume their shape and return a
	// placeholder named type.
	if p.cur().Kind == TIdent {
		switch p.cur().Text {
		case "REF":
			p.advance()
			p.acceptWord("READONLY")
			if p.startsType() {
				p.parseType()
			}
			return &NamedType{Name: "REF"}
		case "LIST":
			p.advance()
			p.acceptKw("OF")
			if p.startsType() {
				p.parseType()
			}
			return &NamedType{Name: "LIST"}
		case "POINTER":
			p.advance()
			p.acceptWord("TO")
			for p.cur().Kind == TIdent && typeQualifiers[p.cur().Text] {
				p.advance()
			}
			if p.startsType() {
				p.parseType()
			}
			return &NamedType{Name: "POINTER"}
		case "SEQUENCE":
			p.advance()
			for !p.isKw("OF") && p.cur().Kind != TEOF && !p.isPunct("]") {
				p.advance() // skip the length/tag clause
			}
			p.acceptKw("OF")
			if p.startsType() {
				p.parseType()
			}
			return &NamedType{Name: "SEQUENCE"}
		case "ANY":
			p.advance()
			return &NamedType{Name: "ANY"}
		case "DESCRIPTOR":
			// DESCRIPTOR [FOR] ARRAY [index] OF elem — a bounds-carrying array ref.
			p.advance()
			p.acceptKw("FOR") // FOR is a reserved word
			if p.startsType() {
				p.parseType()
			}
			return &NamedType{Name: "DESCRIPTOR"}
		case "ERROR", "SIGNAL", "PROCESS", "PORT":
			// Signal/error/process types: like a procedure type.
			p.advance()
			p.acceptWord("ANY")
			if p.isPunct("[") {
				p.advance()
				p.parseFieldList("]")
				p.expectPunct("]")
			}
			if p.acceptKw("RETURNS") {
				if !p.acceptWord("ANY") {
					p.expectPunct("[")
					p.parseFieldList("]")
					p.expectPunct("]")
				}
			}
			return &ProcType{}
		}
	}

	switch {
	case p.acceptKw("ARRAY"):
		var iv *Interval
		if p.isPunct("[") || p.isPunct("(") {
			iv = p.parseInterval()
		} else if !p.isKw("OF") {
			p.parseType() // a named index type (e.g. ARRAY Color OF …)
		}
		p.expectKw("OF")
		elem := p.parseType()
		return &ArrayType{Index: iv, Elem: elem}
	case p.acceptKw("RECORD"):
		p.expectPunct("[")
		fields := p.parseFieldList("]")
		p.expectPunct("]")
		return &RecordType{Fields: fields}
	case p.isKw("PROCEDURE") || p.isKw("PROC"):
		p.advance()
		return p.parseProcType()
	case p.isKw("PROGRAM"), p.isKw("MONITOR"):
		p.advance()
		return &NamedType{Name: "PROGRAM"}
	case p.isKw("SELECT"):
		return p.parseVariant()
	case p.isPunct("{"):
		return p.parseEnumType()
	case p.isPunct("["):
		// "[a: T, ...]" is an anonymous record; "[lo..hi]" is a subrange.
		if p.recordTypeAhead() {
			p.advance()
			fields := p.parseFieldList("]")
			p.expectPunct("]")
			return &RecordType{Fields: fields}
		}
		iv := p.parseInterval()
		return &SubrangeType{Base: &NamedType{Name: "INTEGER"}, Ival: iv}
	case p.isPunct("("):
		iv := p.parseInterval()
		return &SubrangeType{Base: &NamedType{Name: "INTEGER"}, Ival: iv}
	case p.cur().Kind == TIdent:
		name := p.advance()
		qual := name.Text
		for p.isPunct(".") { // qualified name: Pkg.Type
			p.advance()
			qual += "." + p.expectIdent()
		}
		nt := &NamedType{Name: qual, Line: name.Line}
		// A base-relative type: <base> RELATIVE [ORDERED] [POINTER TO] <elem>.
		if p.cur().Kind == TIdent && (typeQualifiers[p.cur().Text] || p.cur().Text == "POINTER") {
			for p.cur().Kind == TIdent && typeQualifiers[p.cur().Text] {
				p.advance()
			}
			p.acceptWord("POINTER")
			p.acceptWord("TO")
			if p.startsType() {
				p.parseType()
			}
			return nt
		}
		if p.isPunct("(") {
			iv := p.parseInterval()
			return &SubrangeType{Base: nt, Ival: iv}
		}
		if p.isPunct("[") {
			if p.bracketHasRange() { // Foo[lo..hi] subrange
				iv := p.parseInterval()
				return &SubrangeType{Base: nt, Ival: iv}
			}
			p.skipBrackets() // Foo[n] — a dimension/size argument
		}
		// A variant-bound type names the variant tag then the base type:
		// REF Success MS.MaintainreturnObject.
		if p.cur().Kind == TIdent && !typeQualifiers[p.cur().Text] {
			return p.parseType()
		}
		return nt
	}
	p.fail("expected a type")
	return nil
}

// bracketHasRange reports whether the '[' at the cursor contains a ".." range
// operator at depth 0 (a subrange) rather than a size/dimension argument.
func (p *Parser) bracketHasRange() bool {
	j := p.pos + 1
	depth := 0
	for ; j < len(p.toks); j++ {
		t := p.toks[j]
		if t.Kind == TEOF {
			return false
		}
		if t.Kind == TPunct {
			switch t.Text {
			case "[", "(":
				depth++
			case "]", ")":
				if depth == 0 {
					return false
				}
				depth--
			case "..":
				if depth == 0 {
					return true
				}
			}
		}
	}
	return false
}

// parseVariant consumes a variant part "SELECT [tag] FROM label => body; …
// ENDCASE", returning a placeholder record type (variants are not executed).
func (p *Parser) parseVariant() TypeExpr {
	p.expectKw("SELECT")
	for !p.isKw("FROM") && p.cur().Kind != TEOF { // [tag:] TagType | * | COMPUTED …
		p.advance()
	}
	p.expectKw("FROM")
	for !p.isKw("ENDCASE") && p.cur().Kind != TEOF {
		p.skipToArrow()
		if !p.acceptPunct("=>") {
			break
		}
		switch {
		case p.isPunct("["):
			p.skipBrackets() // the variant's own fields
		case p.acceptWord("NULL"):
		case p.startsType():
			p.parseType()
		}
		p.acceptPunct(";")
	}
	p.expectKw("ENDCASE")
	return &RecordType{}
}

// acceptWord accepts a specific identifier (a non-reserved Cedar word).
func (p *Parser) acceptWord(w string) bool {
	if p.cur().Kind == TIdent && p.cur().Text == w {
		p.advance()
		return true
	}
	return false
}

// recordTypeAhead reports whether the '[' at the cursor opens an anonymous
// record "[name: T, …]" (or "[]") rather than an interval "[lo..hi]".
func (p *Parser) recordTypeAhead() bool {
	j := p.pos + 1 // just past '['
	if j < len(p.toks) && p.toks[j].Kind == TPunct && p.toks[j].Text == "]" {
		return true // empty record []
	}
	depth := 0
	for ; j < len(p.toks); j++ {
		t := p.toks[j]
		if t.Kind == TEOF {
			return false
		}
		if t.Kind == TPunct {
			switch t.Text {
			case "[", "(":
				depth++
			case "]", ")":
				if depth == 0 {
					return false
				}
				depth--
			case "..":
				if depth == 0 {
					return false // interval
				}
			case ":":
				if depth == 0 {
					return true // field declaration
				}
			}
		}
	}
	return false
}

// startsType reports whether the current token can begin a type expression.
func (p *Parser) startsType() bool {
	t := p.cur()
	switch t.Kind {
	case TIdent:
		return true
	case TKeyword:
		return t.Text == "ARRAY" || t.Text == "RECORD" || t.Text == "PROCEDURE" || t.Text == "PROC"
	case TPunct:
		return t.Text == "{" || t.Text == "[" || t.Text == "("
	}
	return false
}

func (p *Parser) parseProcType() *ProcType {
	pt := &ProcType{}
	p.acceptWord("ANY") // PROC ANY …
	if p.acceptPunct("[") {
		pt.Params = p.parseFieldList("]")
		p.expectPunct("]")
	}
	if p.acceptKw("RETURNS") {
		if !p.acceptWord("ANY") { // RETURNS ANY
			p.expectPunct("[")
			pt.Results = p.parseFieldList("]")
			p.expectPunct("]")
		}
	}
	return pt
}

func (p *Parser) parseEnumType() *EnumType {
	p.expectPunct("{")
	et := &EnumType{}
	for !p.isPunct("}") && p.cur().Kind != TEOF {
		if p.cur().Kind == TIdent {
			et.Members = append(et.Members, p.expectIdent())
		}
		// MACHINE DEPENDENT enums give explicit codes: name(20H) or anonymous (0).
		if p.acceptPunct("(") {
			p.parseExpr()
			p.expectPunct(")")
		}
		if !p.acceptPunct(",") {
			break
		}
	}
	p.expectPunct("}")
	return et
}

// namedGroupAhead reports whether the tokens starting at pos form
// "id (',' id)* ':'", i.e. a named field group.
func (p *Parser) namedGroupAhead() bool {
	j := p.pos
	if p.toks[j].Kind != TIdent {
		return false
	}
	for {
		if p.toks[j].Kind != TIdent {
			return false
		}
		j++
		t := p.toks[j]
		// A MACHINE DEPENDENT field may carry a position spec: name(0:0..15).
		if t.Kind == TPunct && t.Text == "(" {
			depth := 1
			j++
			for j < len(p.toks) && depth > 0 {
				if p.toks[j].Kind == TPunct && p.toks[j].Text == "(" {
					depth++
				} else if p.toks[j].Kind == TPunct && p.toks[j].Text == ")" {
					depth--
				}
				j++
			}
			t = p.toks[j]
		}
		if t.Kind == TPunct && t.Text == ":" {
			return true
		}
		if t.Kind == TPunct && t.Text == "," {
			j++
			continue
		}
		return false
	}
}

// fieldQualifiers precede a record field or parameter in Cedar.
var fieldQualifiers = map[string]bool{"PRIVATE": true, "PUBLIC": true, "READONLY": true}

func (p *Parser) parseFieldList(closer string) []Field {
	var fields []Field
	for !p.isPunct(closer) && p.cur().Kind != TEOF {
		for p.cur().Kind == TIdent && fieldQualifiers[p.cur().Text] {
			p.advance()
		}
		if p.isPunct(closer) {
			break
		}
		if p.namedGroupAhead() {
			// A comma-separated name list where each name may carry a MACHINE
			// DEPENDENT position spec: a(0:0..5), b(0:6..6): T.
			var names []string
			for {
				names = append(names, p.expectIdent())
				if p.isPunct("(") {
					p.skipParens()
				}
				if !p.acceptPunct(",") {
					break
				}
			}
			p.expectPunct(":")
			ft := p.parseType()
			// ignore an optional default value
			if (p.acceptPunct("<-") || p.acceptPunct("=")) && p.startsValue() {
				p.parseValueExpr()
			}
			for _, n := range names {
				fields = append(fields, Field{Name: n, Type: ft})
			}
		} else {
			ft := p.parseType()
			if (p.acceptPunct("<-") || p.acceptPunct("=")) && p.startsValue() { // anonymous field default
				p.parseValueExpr()
			}
			fields = append(fields, Field{Name: "", Type: ft})
		}
		if !p.acceptPunct(",") {
			break
		}
	}
	return fields
}

func (p *Parser) parseInterval() *Interval {
	iv := &Interval{}
	switch {
	case p.acceptPunct("["):
		iv.IncLo = true
	case p.acceptPunct("("):
		iv.IncLo = false
	default:
		p.fail("expected '[' or '(' to start interval")
	}
	iv.Lo = p.parseValueExpr()
	p.expectPunct("..")
	iv.Hi = p.parseValueExpr()
	switch {
	case p.acceptPunct("]"):
		iv.IncHi = true
	case p.acceptPunct(")"):
		iv.IncHi = false
	default:
		p.fail("expected ']' or ')' to close interval")
	}
	return iv
}

// ---- Statements ----

func (p *Parser) parseStmt() Stmt {
	line := p.cur().Line
	switch {
	case p.isKw("IF"):
		return p.parseIfStmt()
	case p.isKw("SELECT"):
		return p.parseSelect()
	case p.isKw("FOR"), p.isKw("WHILE"), p.isKw("UNTIL"),
		p.isKw("THROUGH"), p.isKw("DO"):
		return p.parseLoop()
	case p.isKw("BEGIN"), p.isPunct("{"):
		return p.parseBlock()
	case p.cur().Kind == TIdent && safetyBlockWords[p.cur().Text]:
		// TRUSTED / CHECKED / UNCHECKED prefix a block.
		p.advance()
		return p.parseBlock()
	case p.isKw("RETURN"):
		return p.parseReturn()
	case p.acceptKw("EXIT"):
		return &ExitStmt{Line: line}
	case p.acceptKw("LOOP"):
		return &LoopCtl{Line: line}
	case p.acceptKw("NULL"):
		return &NullStmt{Line: line}
	case p.cur().Kind == TIdent && p.cur().Text == "WITH":
		return p.parseWithSelect()
	case p.cur().Kind == TIdent && (p.cur().Text == "ERROR" || p.cur().Text == "SIGNAL" || p.cur().Text == "RAISE"):
		// A raise: ERROR/SIGNAL/RAISE [signal[args]]. A bare ERROR re-raises.
		p.advance()
		r := &RaiseStmt{Line: line}
		if p.startsExprStmt() {
			r.Sig = p.parseValueExpr()
		}
		return r
	case p.cur().Kind == TIdent && raiseWords[p.cur().Text]:
		// WAIT / NOTIFY / BROADCAST / RESUME [expr]: monitor/condition ops we do
		// not model — evaluate any operand for effect, then continue.
		p.advance()
		if p.startsExprStmt() {
			p.parseValueExpr()
		}
		return &NullStmt{Line: line}
	case p.cur().Kind == TIdent && p.cur().Text == "ENABLE":
		// A statement-level ENABLE handler clause standing on its own.
		p.advance()
		var hs []Handler
		if p.isPunct("{") {
			p.advance()
			hs = p.parseHandlers("}")
			p.acceptPunct("}")
		} else {
			hs = p.parseHandlers(";")
		}
		return &Guarded{Stmt: &NullStmt{Line: line}, Handlers: hs, Line: line}
	case p.cur().Kind == TIdent && p.cur().Text == "GOTO",
		p.cur().Kind == TIdent && p.cur().Text == "GO" && p.peek().Kind == TIdent && p.peek().Text == "TO":
		p.advance() // GOTO or GO
		p.acceptWord("TO")
		if p.cur().Kind == TIdent { // the label
			p.advance()
		}
		return &NullStmt{Line: line}
	case p.cur().Kind == TIdent && controlWords[p.cur().Text]:
		// CONTINUE / RETRY / RESTART / REJECT: control-transfer statements.
		p.advance()
		return &NullStmt{Line: line}
	}
	// expression statement or assignment
	x := p.parseExpr()
	var stmt Stmt = &ExprStmt{X: x, Line: line}
	if p.acceptPunct("<-") {
		rhs := p.parseExpr()
		for p.acceptPunct("<-") { // chained assignment a ← b ← c
			rhs = p.parseExpr()
		}
		stmt = &Assign{Lhs: x, Rhs: rhs, Line: line}
	}
	// A trailing "! handler => …" catch clause wraps the statement.
	if p.isPunct("!") {
		p.advance()
		hs := p.parseHandlers(";")
		stmt = &Guarded{Stmt: stmt, Handlers: hs, Line: line}
	}
	return stmt
}

// parseHandlers parses a sequence of catch arms "guard[, guard] => stmt; …" up
// to closer (or the end of the statement). A guard of ANY, UNWIND or a bare
// signal name selects which raised conditions the arm handles.
func (p *Parser) parseHandlers(closer string) []Handler {
	var hs []Handler
	for p.handlerAhead(closer) {
		var h Handler
		h.Guards = append(h.Guards, p.parseHandlerGuard())
		for p.acceptPunct(",") {
			if p.isPunct("=>") {
				break
			}
			h.Guards = append(h.Guards, p.parseHandlerGuard())
		}
		if !p.acceptPunct("=>") {
			break
		}
		h.Body = p.parseStmt()
		hs = append(hs, h)
		if !p.acceptPunct(";") {
			break
		}
	}
	return hs
}

// handlerAhead reports whether the upcoming tokens form another catch arm — a
// "guard … => " before the next depth-0 ';', the closer, or a block terminator.
// This stops a handler list from swallowing the statement that follows it.
func (p *Parser) handlerAhead(closer string) bool {
	depth := 0
	for j := p.pos; j < len(p.toks); j++ {
		t := p.toks[j]
		if t.Kind == TEOF {
			return false
		}
		if depth == 0 {
			if t.Kind == TPunct && t.Text == "=>" {
				return true
			}
			if t.Kind == TPunct && (t.Text == ";" || t.Text == closer) {
				return false
			}
			if t.Kind == TKeyword && (t.Text == closer || t.Text == "END" || t.Text == "ENDLOOP" || t.Text == "ENDCASE") {
				return false
			}
		}
		switch {
		case t.Kind == TPunct && (t.Text == "[" || t.Text == "(" || t.Text == "{"):
			depth++
		case t.Kind == TPunct && (t.Text == "]" || t.Text == ")" || t.Text == "}"):
			depth--
		}
	}
	return false
}

// parseHandlerGuard parses one catch guard: ANY, UNWIND, or a signal expression
// (a name, possibly with argument bindings "Foo[a, b]" which we consume).
func (p *Parser) parseHandlerGuard() Expr {
	line := p.cur().Line
	if p.acceptWord("ANY") || p.acceptWord("UNWIND") {
		return nil // matches any condition
	}
	x := p.parsePostfix()
	_ = line
	return x
}

// safetyBlockWords prefix a block with a Cedar safety qualifier.
var safetyBlockWords = map[string]bool{"TRUSTED": true, "CHECKED": true, "UNCHECKED": true}

// controlWords are Cedar control-transfer statements (mostly used in handlers).
var controlWords = map[string]bool{"CONTINUE": true, "RETRY": true, "RESTART": true, "REJECT": true}

// raiseWords introduce a raise/synchronisation statement taking an optional
// expression operand.
var raiseWords = map[string]bool{
	"ERROR": true, "SIGNAL": true, "RAISE": true, "RESUME": true,
	"WAIT": true, "NOTIFY": true, "BROADCAST": true,
}

// startsValue reports whether a value follows (i.e. the cursor is not at a
// separator/closer), used to skip empty "← ," defaults while still accepting
// keyword values like NIL, NEW and IF.
func (p *Parser) startsValue() bool {
	t := p.cur()
	if t.Kind == TEOF {
		return false
	}
	if t.Kind == TPunct {
		switch t.Text {
		case ",", "]", ")", "}", ";", "=>":
			return false
		}
	}
	if t.Kind == TKeyword && clauseKeywords[t.Text] {
		return false // ELSE/THEN/DO/… end a value, they don't start one
	}
	return true
}

// clauseKeywords end an expression/statement; they never begin a value, so a
// bracketless RETURN (etc.) must not consume them.
var clauseKeywords = map[string]bool{
	"ELSE": true, "THEN": true, "DO": true, "FROM": true, "END": true,
	"ENDCASE": true, "ENDLOOP": true, "EXITS": true, "REPEAT": true,
	"UNTIL": true, "WHILE": true,
}

// startsExprStmt reports whether the current token can begin an expression
// operand (used to decide if a raise word has an argument).
func (p *Parser) startsExprStmt() bool {
	t := p.cur()
	switch t.Kind {
	case TIdent, TInt, TReal, TChar, TString:
		return true
	case TPunct:
		return t.Text == "(" || t.Text == "[" || t.Text == "@" || t.Text == "-" || t.Text == "~"
	}
	return false
}

// skipCatchArgs skips a "! handler" clause inside a call's brackets, up to the
// enclosing ']' or ')' (its arms are ';'-separated, so ';' is not a terminator).
func (p *Parser) skipCatchArgs() {
	depth := 0
	for p.cur().Kind != TEOF {
		switch {
		case p.isPunct("[") || p.isPunct("(") || p.isPunct("{"):
			depth++
		case p.isPunct("]") || p.isPunct(")"):
			if depth == 0 {
				return
			}
			depth--
		case p.isPunct("}"):
			if depth > 0 {
				depth--
			}
		}
		p.advance()
	}
}

// skipCatch2 skips a handler clause up to the end of the statement.
func (p *Parser) skipCatch2() {
	depth := 0
	for p.cur().Kind != TEOF {
		switch {
		case p.isPunct("[") || p.isPunct("(") || p.isPunct("{"):
			depth++
		case p.isPunct("]") || p.isPunct(")") || p.isPunct("}"):
			if depth == 0 {
				return
			}
			depth--
		case depth == 0 && (p.isPunct(";") || p.isKw("END")):
			return
		}
		p.advance()
	}
}

// skipBraces consumes a balanced { … } group at the cursor.
func (p *Parser) skipBraces() {
	if !p.acceptPunct("{") {
		return
	}
	depth := 1
	for depth > 0 && p.cur().Kind != TEOF {
		switch {
		case p.isPunct("{"):
			depth++
		case p.isPunct("}"):
			depth--
		}
		p.advance()
	}
}

// skipParens consumes a balanced ( … ) group at the cursor.
func (p *Parser) skipParens() {
	if !p.acceptPunct("(") {
		return
	}
	depth := 1
	for depth > 0 && p.cur().Kind != TEOF {
		switch {
		case p.isPunct("("):
			depth++
		case p.isPunct(")"):
			depth--
		}
		p.advance()
	}
}

// skipBrackets consumes a balanced [ … ] group at the cursor.
func (p *Parser) skipBrackets() {
	if !p.acceptPunct("[") {
		return
	}
	depth := 1
	for depth > 0 && p.cur().Kind != TEOF {
		switch {
		case p.isPunct("["):
			depth++
		case p.isPunct("]"):
			depth--
		}
		p.advance()
	}
}

// skipCatch consumes a "! …" exception-handler clause up to the end of the
// enclosing statement (a ';' or block closer at depth 0).
func (p *Parser) skipCatch() {
	p.advance() // '!'
	depth := 0
	for p.cur().Kind != TEOF {
		switch {
		case p.isPunct("[") || p.isPunct("(") || p.isPunct("{"):
			depth++
		case p.isPunct("]") || p.isPunct(")") || p.isPunct("}"):
			if depth == 0 {
				return
			}
			depth--
		case depth == 0 && (p.isPunct(";") || p.isKw("END")):
			return
		}
		p.advance()
	}
}

func (p *Parser) parseIfStmt() Stmt {
	line := p.cur().Line
	p.expectKw("IF")
	cond := p.parseValueExpr()
	p.expectKw("THEN")
	then := p.parseThenElseBody()
	var els Stmt
	if p.acceptKw("ELSE") {
		els = p.parseThenElseBody()
	}
	return &IfStmt{Cond: cond, Then: then, Else: els, Line: line}
}

// parseThenElseBody allows either a block or a single statement after
// THEN/ELSE. A bare declaration is not valid there.
func (p *Parser) parseThenElseBody() Stmt {
	if p.isKw("BEGIN") || p.isPunct("{") {
		return p.parseBlock()
	}
	return p.parseStmt()
}

func (p *Parser) parseLoop() Stmt {
	line := p.cur().Line
	l := &Loop{Line: line}
	switch {
	case p.acceptKw("FOR"):
		l.Var = p.expectIdent()
		if p.acceptPunct(":") {
			l.VarType = p.parseType()
		}
		if !p.acceptWord("DECREASING") {
			p.acceptWord("INCREASING")
		}
		if p.acceptKw("IN") {
			if p.isPunct("[") || p.isPunct("(") {
				l.Interval = p.parseInterval()
			} else {
				p.parseType() // FOR x IN EnumType (iterate a whole type)
			}
		} else if p.acceptPunct("<-") {
			l.Start = p.parseValueExpr()
			p.expectPunct(",")
			l.Next = p.parseValueExpr() // the step may reassign: edge ← edge.next
		} else {
			p.fail("expected IN or '<-' in FOR loop")
		}
	case p.acceptKw("THROUGH"):
		// THROUGH [Type][lo..hi] — an optional type prefix before the interval.
		if !p.isPunct("[") && !p.isPunct("(") && p.cur().Kind == TIdent {
			p.advance()
			for p.isPunct(".") {
				p.advance()
				p.expectIdent()
			}
		}
		l.Interval = p.parseInterval()
	case p.acceptKw("WHILE"):
		l.While = p.parseValueExpr()
	case p.acceptKw("UNTIL"):
		l.Until = p.parseValueExpr()
	case p.acceptKw("DO"):
		// bare DO loop; step back so the shared DO handling below works
		p.pos--
	}
	// optional trailing guard for iterator forms
	if p.acceptKw("WHILE") {
		l.While = p.parseExpr()
	} else if p.acceptKw("UNTIL") {
		l.Until = p.parseExpr()
	}
	p.expectKw("DO")
	loopHandlers := p.blockPrologue() // a loop body may also open with OPEN/ENABLE
	// The body runs up to ENDLOOP or a REPEAT (loop-exit handler) clause.
	var items []Stmt
	for {
		for p.acceptPunct(";") {
		}
		if p.isKw("ENDLOOP") || p.cur().Kind == TEOF ||
			(p.cur().Kind == TIdent && p.cur().Text == "REPEAT") {
			break
		}
		start := p.pos
		stmt, ok := p.tryParseItem()
		if !ok { // recover within the loop body too
			p.recovered++
			p.resync("ENDLOOP")
			if p.pos == start {
				p.advance()
			}
			continue
		}
		items = append(items, stmt)
		if !p.acceptPunct(";") {
			break
		}
	}
	l.Body = &Block{Items: items, Handlers: loopHandlers, Line: line}
	if p.acceptWord("REPEAT") { // exit handlers: label => stmt; …
		for !p.isKw("ENDLOOP") && p.cur().Kind != TEOF {
			p.skipToArrow()
			if !p.acceptPunct("=>") {
				break
			}
			p.parseStmt()
			p.acceptPunct(";")
		}
	}
	p.acceptKw("ENDLOOP") // tolerant: recovery may have consumed it
	return l
}

// skipToArrow skips a SELECT/WITH/REPEAT arm guard up to its '=>' (or a
// terminator), tracking bracket depth.
func (p *Parser) skipToArrow() {
	depth := 0
	for p.cur().Kind != TEOF {
		switch {
		case p.isPunct("[") || p.isPunct("(") || p.isPunct("{"):
			depth++
		case p.isPunct("]") || p.isPunct(")") || p.isPunct("}"):
			if depth == 0 {
				return
			}
			depth--
		case depth == 0 && (p.isPunct("=>") || p.isKw("ENDCASE")):
			return
		}
		p.advance()
	}
}

// parseWithSelect parses "WITH v SELECT [tag] FROM arm => stmt; … ENDCASE => s".
// Variant discrimination is not executed, so the arm guards are skipped and only
// the arm bodies are parsed (to keep the structure and nested declarations).
func (p *Parser) parseWithSelect() Stmt {
	line := p.cur().Line
	p.advance() // WITH
	s := &SelectStmt{Line: line}
	if p.cur().Kind == TIdent && p.peek().Kind == TPunct && p.peek().Text == ":" {
		p.advance() // binding name
		p.advance() // ':'
	}
	if p.cur().Kind == TIdent && p.peek().Kind == TPunct && p.peek().Text == "~" {
		p.advance() // binding name (WITH name ~ value SELECT)
		p.advance() // '~'
	}
	s.Subject = p.parseValueExpr()
	p.expectKw("SELECT")
	for !p.isKw("FROM") && p.cur().Kind != TEOF { // an optional discriminator tag
		p.advance()
	}
	p.expectKw("FROM")
	for !p.isKw("ENDCASE") && p.cur().Kind != TEOF {
		p.skipToArrow()
		if !p.acceptPunct("=>") {
			break
		}
		var arm SelectArm
		arm.Body = p.parseStmt()
		s.Arms = append(s.Arms, arm)
		p.acceptPunct(";")
	}
	p.expectKw("ENDCASE")
	if p.acceptPunct("=>") {
		s.Default = p.parseStmt()
	}
	return s
}

func (p *Parser) parseSelect() Stmt {
	line := p.cur().Line
	p.expectKw("SELECT")
	s := &SelectStmt{Line: line}
	s.Subject = p.parseValueExpr()
	p.expectKw("FROM")
	for !p.isKw("ENDCASE") && p.cur().Kind != TEOF {
		var arm SelectArm
		arm.Guards = append(arm.Guards, p.parseSelectGuard(s.Subject))
		for p.acceptPunct(",") {
			if p.isPunct("=>") {
				break
			}
			arm.Guards = append(arm.Guards, p.parseSelectGuard(s.Subject))
		}
		p.expectPunct("=>")
		arm.Body = p.parseStmt()
		s.Arms = append(s.Arms, arm)
		p.acceptPunct(";")
	}
	p.expectKw("ENDCASE")
	if p.acceptPunct("=>") {
		s.Default = p.parseStmt()
	}
	return s
}

// parseSelectExpr parses SELECT used in expression position, where each arm and
// the ENDCASE default yield a value rather than a statement.
func (p *Parser) parseSelectExpr() Expr {
	line := p.cur().Line
	p.expectKw("SELECT")
	s := &SelectExpr{Line: line}
	s.Subject = p.parseValueExpr()
	p.expectKw("FROM")
	for !p.isKw("ENDCASE") && p.cur().Kind != TEOF {
		var arm SelectExprArm
		arm.Guards = append(arm.Guards, p.parseSelectGuard(s.Subject))
		for p.acceptPunct(",") {
			if p.isPunct("=>") {
				break
			}
			arm.Guards = append(arm.Guards, p.parseSelectGuard(s.Subject))
		}
		p.expectPunct("=>")
		arm.Val = p.parseValueExpr()
		s.Arms = append(s.Arms, arm)
		p.acceptPunct(",")
		p.acceptPunct(";")
	}
	p.expectKw("ENDCASE")
	if p.acceptPunct("=>") {
		s.Default = p.parseValueExpr()
	}
	return s
}

// parseSelectGuard parses one SELECT arm guard. An "open" guard begins with a
// relational operator or IN and is relative to the subject (< x, IN [a..b]); a
// plain expression is an equality test.
func (p *Parser) parseSelectGuard(subject Expr) Expr {
	if p.isPunct("<-") {
		// "< -n" mis-lexes as the arrow "<-": recover a less-than-negative guard.
		line := p.advance().Line
		neg := &Unary{Op: "-", X: p.parseAdd(), Line: line}
		return &Binary{Op: "<", L: subject, R: neg, Line: line}
	}
	if p.cur().Kind == TPunct && relOps[p.cur().Text] {
		op := p.advance()
		return &Binary{Op: op.Text, L: subject, R: p.parseAdd(), Line: op.Line}
	}
	if p.isKw("IN") {
		return p.parseInExpr(subject)
	}
	return p.parseExpr()
}

func (p *Parser) parseReturn() Stmt {
	line := p.cur().Line
	p.expectKw("RETURN")
	r := &ReturnStmt{Line: line}
	if p.acceptWord("WITH") { // RETURN WITH ERROR expr
		p.acceptWord("ERROR")
		if p.startsExprStmt() {
			p.parseValueExpr()
		}
		return r
	}
	if p.acceptPunct("[") {
		for !p.isPunct("]") && !p.isPunct("!") && p.cur().Kind != TEOF {
			if p.isPunct(",") { // an omitted result
				p.advance()
				continue
			}
			if p.cur().Kind == TIdent && p.peek().Kind == TPunct &&
				(p.peek().Text == ":" || p.peek().Text == "~") {
				p.advance() // named result
				p.advance() // ':' or '~'
			}
			r.Values = append(r.Values, p.parseValueExpr())
			if !p.acceptPunct(",") {
				break
			}
		}
		if p.isPunct("!") { // a catch clause on the returned call
			p.skipCatchArgs()
		}
		p.expectPunct("]")
	} else if p.startsValue() { // bracketless return: RETURN expr
		r.Values = append(r.Values, p.parseValueExpr())
	}
	return r
}

// ---- Expressions ----

func (p *Parser) parseExpr() Expr {
	if p.isKw("IF") {
		return p.parseIfExpr()
	}
	if p.isKw("SELECT") {
		return p.parseSelectExpr()
	}
	if p.cur().Kind == TIdent && p.cur().Text == "WITH" {
		return p.parseWithSelectExpr()
	}
	if p.cur().Kind == TIdent && exprRaiseWords[p.cur().Text] {
		// ERROR/SIGNAL/RAISE used as a value (e.g. IF c THEN ERROR Foo ELSE x).
		line := p.advance().Line
		if p.startsExprStmt() {
			p.parseValueExpr()
		}
		return &Ident{Name: "NIL", Line: line}
	}
	return p.parseOr()
}

// exprRaiseWords may raise from an expression position.
var exprRaiseWords = map[string]bool{"ERROR": true, "SIGNAL": true, "RAISE": true}

// typeValPrefix words prefix a type used as a value (LONG CARDINAL, REF INT),
// e.g. as a NARROW/LAST/SIZE argument; they are consumed as no-ops.
var typeValPrefix = map[string]bool{
	"LONG": true, "SHORT": true, "REF": true,
	"UNCOUNTED": true, "SAFE": true, "UNSAFE": true, "PACKED": true,
}

// parseWithSelectExpr consumes a WITH … SELECT used as a value; not executed.
func (p *Parser) parseWithSelectExpr() Expr {
	line := p.cur().Line
	p.advance()                                   // WITH
	for !p.isKw("FROM") && p.cur().Kind != TEOF { // consume the subject and SELECT
		p.advance()
	}
	p.acceptKw("FROM")
	depth := 0
	for p.cur().Kind != TEOF {
		if depth == 0 && p.isKw("ENDCASE") {
			p.advance()
			break
		}
		switch {
		case p.isKw("SELECT"):
			depth++
		case p.isKw("ENDCASE"):
			depth--
		}
		p.advance()
	}
	if p.acceptPunct("=>") {
		p.parseValueExpr()
	}
	return &Ident{Name: "NIL", Line: line}
}

// parseValueExpr parses an expression that may itself be an assignment, as
// permitted inside call arguments and aggregates: f[x ← e], [a ← b].
func (p *Parser) parseValueExpr() Expr {
	x := p.parseExpr()
	for p.acceptPunct("<-") {
		x = p.parseExpr()
	}
	return x
}

func (p *Parser) parseIfExpr() Expr {
	line := p.cur().Line
	p.expectKw("IF")
	cond := p.parseValueExpr()
	p.expectKw("THEN")
	then := p.parseValueExpr()
	p.expectKw("ELSE")
	els := p.parseValueExpr()
	return &IfExpr{Cond: cond, Then: then, Else: els, Line: line}
}

func (p *Parser) parseOr() Expr {
	x := p.parseAnd()
	for p.isKw("OR") {
		line := p.advance().Line
		y := p.parseAnd()
		x = &Binary{Op: "OR", L: x, R: y, Line: line}
	}
	return x
}

func (p *Parser) parseAnd() Expr {
	x := p.parseRel()
	for p.isKw("AND") {
		line := p.advance().Line
		y := p.parseRel()
		x = &Binary{Op: "AND", L: x, R: y, Line: line}
	}
	return x
}

var relOps = map[string]bool{"=": true, "#": true, "~=": true, "#=": true, "<": true, "<=": true, ">": true, ">=": true}

func (p *Parser) parseRel() Expr {
	x := p.parseAdd()
	for {
		switch {
		case p.cur().Kind == TPunct && relOps[p.cur().Text]:
			op := p.advance()
			y := p.parseAdd()
			x = &Binary{Op: op.Text, L: x, R: y, Line: op.Line}
		case p.isKw("IN"):
			x = p.parseInExpr(x)
		case (p.isKw("NOT") || p.isPunct("~")) && p.peek().Kind == TKeyword && p.peek().Text == "IN":
			line := p.advance().Line // NOT
			x = &Unary{Op: "NOT", X: p.parseInExpr(x), Line: line}
		default:
			return x
		}
	}
}

// parseInExpr handles "x IN [lo..hi]" (desugared to a range comparison so it
// executes) and "x IN Type" (kept as a placeholder true — parse only).
func (p *Parser) parseInExpr(x Expr) Expr {
	line := p.advance().Line // IN
	if p.isPunct("[") || p.isPunct("(") {
		iv := p.parseInterval()
		loOp, hiOp := ">", "<"
		if iv.IncLo {
			loOp = ">="
		}
		if iv.IncHi {
			hiOp = "<="
		}
		lo := &Binary{Op: loOp, L: x, R: iv.Lo, Line: line}
		hi := &Binary{Op: hiOp, L: x, R: iv.Hi, Line: line}
		return &Binary{Op: "AND", L: lo, R: hi, Line: line}
	}
	p.parseType() // membership in a named set/type: parse and ignore
	return &Ident{Name: "TRUE", Line: line}
}

func (p *Parser) parseAdd() Expr {
	x := p.parseMul()
	for p.isPunct("+") || p.isPunct("-") {
		op := p.advance()
		y := p.parseMul()
		x = &Binary{Op: op.Text, L: x, R: y, Line: op.Line}
	}
	return x
}

func (p *Parser) parseMul() Expr {
	x := p.parseUnary()
	for p.isPunct("*") || p.isPunct("/") || p.isPunct("**") || p.isKw("MOD") {
		op := p.advance()
		y := p.parseUnary()
		x = &Binary{Op: op.Text, L: x, R: y, Line: op.Line}
	}
	return x
}

func (p *Parser) parseUnary() Expr {
	switch {
	case p.isKw("NOT") || p.isPunct("~"):
		line := p.advance().Line
		return &Unary{Op: "NOT", X: p.parseUnary(), Line: line}
	case p.isPunct("-"):
		line := p.advance().Line
		return &Unary{Op: "-", X: p.parseUnary(), Line: line}
	case p.isPunct("+"):
		p.advance() // unary plus: a no-op
		return p.parseUnary()
	case p.cur().Kind == TIdent && (p.cur().Text == "FORK" || p.cur().Text == "JOIN"):
		p.advance() // FORK/JOIN a call: yield the call expression
		return p.parseUnary()
	case p.isPunct("@"):
		line := p.advance().Line
		return &Unary{Op: "@", X: p.parseUnary(), Line: line}
	case p.cur().Kind == TIdent && typeValPrefix[p.cur().Text] && p.peek().Kind == TIdent:
		// A type used as a value: LAST[LONG CARDINAL], NARROW[x, REF INT].
		p.advance()
		return p.parseUnary()
	case p.cur().Kind == TIdent && p.cur().Text == "POINTER" &&
		p.peek().Kind == TIdent && p.peek().Text == "TO":
		// A pointer type used as a value: LOOPHOLE[x, POINTER TO CARD]. Gated on
		// "TO" so a bare "POINTER" identifier elsewhere is unaffected.
		p.advance() // POINTER
		p.advance() // TO
		return p.parseUnary()
	case p.cur().Kind == TIdent && p.cur().Text == "LIST" &&
		p.peek().Kind == TKeyword && p.peek().Text == "OF":
		// A list type used as a value: ISTYPE[x, LIST OF REF].
		p.advance() // LIST
		p.advance() // OF
		if p.cur().Kind == TIdent {
			return p.parseUnary()
		}
		return &Ident{Name: "NIL", Line: p.cur().Line}
	case (p.isKw("PROC") || p.isKw("PROCEDURE")) && p.peek().Kind == TPunct && p.peek().Text == "[":
		// A procedure type as a value: LOOPHOLE[proc, PROC [a, b: POINTER]].
		line := p.advance().Line
		p.parseProcType()
		return &Ident{Name: "NIL", Line: line}
	case p.isKw("ARRAY") || p.isKw("RECORD"):
		// An array/record type as a value: NEW[ARRAY I OF T], SIZE[ARRAY[0..4) OF W].
		line := p.cur().Line
		p.parseType()
		return &Ident{Name: "NIL", Line: line}
	}
	return p.parsePostfix()
}

func (p *Parser) parsePostfix() Expr {
	x := p.parsePrimary()
	for {
		switch {
		case p.isPunct("["):
			line := p.advance().Line
			var args []Expr
			for !p.isPunct("]") && !p.isPunct("!") && p.cur().Kind != TEOF {
				if p.isPunct(",") { // an omitted (defaulted) argument
					p.advance()
					continue
				}
				if p.cur().Kind == TIdent && p.peek().Kind == TPunct &&
					(p.peek().Text == ":" || p.peek().Text == "~") {
					p.advance() // keyword-argument name
					p.advance() // ':' or '~'
				}
				if p.startsValue() { // an omitted value ([name: ]) uses the default
					args = append(args, p.parseValueExpr())
				}
				if !p.acceptPunct(",") {
					break
				}
			}
			var catch []Handler
			if p.isPunct("!") { // a call-site catch clause: proc[args ! handler]
				p.advance()
				catch = p.parseHandlers("]")
			}
			p.expectPunct("]")
			x = &Apply{Fun: x, Args: args, Catch: catch, Line: line}
		case p.isPunct("."):
			line := p.advance().Line
			// The field is usually an identifier, but Cedar allows zone.NEW[…]
			// allocation, so a keyword such as NEW is accepted here too.
			field := p.cur().Text
			if p.cur().Kind == TIdent || p.cur().Kind == TKeyword {
				p.advance()
			} else {
				p.fail("expected a field name")
			}
			x = &FieldAccess{X: x, Field: field, Line: line}
			if field == "NEW" && p.isPunct("[") {
				// zone.NEW[Type ← init]: the brackets hold a type, not arguments.
				p.advance()
				nt := p.parseType()
				var init Expr
				if p.acceptPunct("<-") || p.acceptBind() {
					init = p.parseValueExpr()
				}
				p.expectPunct("]")
				x = &NewExpr{Type: nt, Init: init, Line: line}
			}
		case p.isPunct("^"):
			line := p.advance().Line // dereference p^
			x = &Deref{X: x, Line: line}
		default:
			return x
		}
	}
}

func (p *Parser) parsePrimary() Expr {
	t := p.cur()
	switch t.Kind {
	case TInt:
		p.advance()
		return &IntLit{Val: t.Int, Line: t.Line}
	case TReal:
		p.advance()
		return &RealLit{Val: t.Real, Line: t.Line}
	case TChar:
		p.advance()
		return &CharLit{Val: t.Char, Line: t.Line}
	case TString:
		p.advance()
		return &StringLit{Val: t.Str, Line: t.Line}
	case TIdent:
		p.advance()
		return &Ident{Name: t.Text, Line: t.Line}
	case TKeyword:
		switch t.Text {
		case "NIL", "NULL":
			p.advance()
			return &NilLit{Line: t.Line}
		case "NEW":
			p.advance()
			// Cedar NEW[Type] or NEW[Type ← init]; Mesa also allows a bare type.
			if p.acceptPunct("[") {
				nt := p.parseType()
				var init Expr
				if p.acceptPunct("<-") || p.acceptBind() {
					init = p.parseExpr()
				}
				p.expectPunct("]")
				return &NewExpr{Type: nt, Init: init, Line: t.Line}
			}
			return &NewExpr{Type: p.parseType(), Line: t.Line}
		case "IF":
			return p.parseIfExpr()
		case "SELECT":
			return p.parseSelectExpr()
		}
	case TPunct:
		switch t.Text {
		case "(":
			p.advance()
			x := p.parseExpr()
			for p.acceptPunct("<-") { // (v ← expr): assignment as an expression
				x = p.parseExpr()
			}
			p.expectPunct(")")
			return x
		case "[":
			if p.bracketHasRange() { // a subrange type as a value: BITS[[0..n)]
				p.parseInterval() // handles the mixed [lo..hi) bracket form
				return &Ident{Name: "NIL", Line: t.Line}
			}
			return p.parseAggregate()
		}
	}
	p.fail("unexpected token in expression")
	return nil
}

func (p *Parser) parseAggregate() Expr {
	line := p.cur().Line
	p.expectPunct("[")
	agg := &Aggregate{Line: line}
	for !p.isPunct("]") && p.cur().Kind != TEOF {
		if p.isPunct(",") { // an omitted element: [, , a, b]
			p.advance()
			continue
		}
		var el AggElem
		// named element: name: value  or  Cedar's  name~value
		if p.cur().Kind == TIdent && p.peek().Kind == TPunct &&
			(p.peek().Text == ":" || p.peek().Text == "~") {
			el.Name = p.expectIdent()
			p.advance() // ':' or '~'
		}
		if p.startsValue() { // an omitted value ([name: , …]) uses the default
			el.Val = p.parseValueExpr()
		}
		agg.Elems = append(agg.Elems, el)
		if !p.acceptPunct(",") {
			break
		}
	}
	p.expectPunct("]")
	return agg
}

// ParseSource is a convenience wrapper: lex + parse a whole module.
func ParseSource(src string) (*Module, error) {
	lx := NewLexer(src)
	toks, err := lx.Tokenize()
	if err != nil {
		return nil, err
	}
	return NewParser(toks).ParseModule()
}

// summarizeTokens is a small debugging aid (unused in normal runs).
func summarizeTokens(toks []Token) string {
	var sb strings.Builder
	for _, t := range toks {
		sb.WriteString(t.String())
		sb.WriteByte(' ')
	}
	return sb.String()
}
