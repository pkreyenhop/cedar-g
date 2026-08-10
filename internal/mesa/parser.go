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
	toks []Token
	pos  int
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
	p.skipDirectory()
	m.Name = p.expectIdent()
	p.expectPunct(":")
	// The head runs up to the binding ('=' or Cedar's '~'). It carries optional
	// qualifiers (CEDAR, SAFE, ...), the module kind, and IMPORTS/EXPORTS/params;
	// we record the kind and skip the rest (tracking bracket depth so a '=' in a
	// default value doesn't end the head early).
	m.Kind = "MODULE"
	depth := 0
	for p.cur().Kind != TEOF {
		if depth == 0 && (p.isPunct("=") || p.isPunct("~")) {
			break
		}
		switch {
		case p.isKw("PROGRAM"):
			m.Kind = "PROGRAM"
		case p.isKw("DEFINITIONS"):
			m.Kind = "DEFINITIONS"
		case p.isKw("MONITOR"):
			m.Kind = "MONITOR"
		case p.isPunct("[") || p.isPunct("("):
			depth++
		case p.isPunct("]") || p.isPunct(")"):
			depth--
		}
		p.advance()
	}
	if !p.acceptBind() {
		p.fail("expected '=' or '~'")
	}
	m.Body = p.parseBlock()
	p.acceptPunct(".") // trailing '.' after END is optional
	return m, nil
}

// acceptBind accepts a binding operator: Mesa '=' or Cedar '~'.
func (p *Parser) acceptBind() bool {
	return p.acceptPunct("=") || p.acceptPunct("~")
}

// skipDirectory skips a leading "DIRECTORY … ;" import clause, if present.
func (p *Parser) skipDirectory() {
	if !p.acceptKw("DIRECTORY") {
		return
	}
	depth := 0
	for p.cur().Kind != TEOF {
		switch {
		case p.isPunct("[") || p.isPunct("("):
			depth++
		case p.isPunct("]") || p.isPunct(")"):
			depth--
		case depth == 0 && p.isPunct(";"):
			p.advance()
			return
		}
		p.advance()
	}
}

// parseBlock handles BEGIN..END and {..} bodies.
func (p *Parser) parseBlock() *Block {
	line := p.cur().Line
	var closer string
	switch {
	case p.acceptKw("BEGIN"):
		closer = "END"
	case p.acceptPunct("{"):
		closer = "}"
	default:
		p.fail("expected BEGIN or '{'")
	}
	// Cedar blocks may begin with "OPEN interfaces;" and/or "ENABLE handlers;".
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
				p.skipBraces()
			} else {
				for p.cur().Kind != TEOF && !p.isPunct(";") {
					p.advance()
				}
			}
			p.acceptPunct(";")
			continue
		}
		break
	}
	items := p.parseStmtSeq(closer)
	if closer == "END" {
		p.expectKw("END")
	} else {
		p.expectPunct("}")
	}
	return &Block{Items: items, Line: line}
}

// atStop reports whether the current token ends the current sequence.
func (p *Parser) atStop(closer string) bool {
	return p.isKw(closer) || p.isPunct(closer)
}

func (p *Parser) parseStmtSeq(closer string) []Stmt {
	var items []Stmt
	for {
		for p.acceptPunct(";") { // absorb empty statements / separators
		}
		if p.atStop(closer) || p.cur().Kind == TEOF {
			break
		}
		items = append(items, p.parseItem())
		if !p.acceptPunct(";") {
			// no separator: next must be the closer
			break
		}
	}
	return items
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

	// TYPE declaration: NAME: TYPE = typeExpr
	if p.acceptKw("TYPE") {
		if !p.acceptBind() {
			p.acceptPunct("<-")
		}
		t := p.parseType()
		return &TypeDecl{Name: names[0], Type: t, Line: line}
	}

	t := p.parseType()

	// Procedure declaration: NAME: PROCEDURE[..] RETURNS[..] = body
	if pt, ok := t.(*ProcType); ok && (p.isPunct("=") || p.isPunct("~")) {
		p.advance()
		body := p.parseBlock()
		return &ProcDecl{Name: names[0], Type: pt, Body: body, Line: line}
	}

	var init Expr
	isConst := false
	if p.acceptBind() {
		isConst = true
		init = p.parseExpr()
	} else if p.acceptPunct("<-") {
		init = p.parseExpr()
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
}

func (p *Parser) parseType() TypeExpr {
	// Skip decorating qualifiers (PACKED ARRAY, LONG POINTER, MACHINE DEPENDENT…).
	for p.cur().Kind == TIdent && typeQualifiers[p.cur().Text] {
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
		}
	}

	switch {
	case p.acceptKw("ARRAY"):
		var iv *Interval
		if p.isPunct("[") || p.isPunct("(") {
			iv = p.parseInterval()
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
		if p.isPunct("[") || p.isPunct("(") {
			iv := p.parseInterval()
			return &SubrangeType{Base: nt, Ival: iv}
		}
		return nt
	}
	p.fail("expected a type")
	return nil
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
	if p.acceptPunct("[") {
		pt.Params = p.parseFieldList("]")
		p.expectPunct("]")
	}
	if p.acceptKw("RETURNS") {
		p.expectPunct("[")
		pt.Results = p.parseFieldList("]")
		p.expectPunct("]")
	}
	return pt
}

func (p *Parser) parseEnumType() *EnumType {
	p.expectPunct("{")
	et := &EnumType{}
	if !p.isPunct("}") {
		et.Members = append(et.Members, p.expectIdent())
		for p.acceptPunct(",") {
			et.Members = append(et.Members, p.expectIdent())
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
			names := p.parseIdList()
			p.expectPunct(":")
			ft := p.parseType()
			// ignore an optional default value
			if p.acceptPunct("<-") || p.acceptPunct("=") {
				p.parseExpr()
			}
			for _, n := range names {
				fields = append(fields, Field{Name: n, Type: ft})
			}
		} else {
			ft := p.parseType()
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
	iv.Lo = p.parseExpr()
	p.expectPunct("..")
	iv.Hi = p.parseExpr()
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
	}
	// expression statement or assignment
	x := p.parseExpr()
	var stmt Stmt = &ExprStmt{X: x, Line: line}
	if p.acceptPunct("<-") {
		rhs := p.parseExpr()
		stmt = &Assign{Lhs: x, Rhs: rhs, Line: line}
	}
	// A trailing "! handler => …" catch clause is skipped (no signal runtime).
	if p.isPunct("!") {
		p.skipCatch()
	}
	return stmt
}

// safetyBlockWords prefix a block with a Cedar safety qualifier.
var safetyBlockWords = map[string]bool{"TRUSTED": true, "CHECKED": true, "UNCHECKED": true}

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
	cond := p.parseExpr()
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
		if p.acceptKw("IN") {
			l.Interval = p.parseInterval()
		} else if p.acceptPunct("<-") {
			l.Start = p.parseExpr()
			p.expectPunct(",")
			l.Next = p.parseExpr()
		} else {
			p.fail("expected IN or '<-' in FOR loop")
		}
	case p.acceptKw("THROUGH"):
		l.Interval = p.parseInterval()
	case p.acceptKw("WHILE"):
		l.While = p.parseExpr()
	case p.acceptKw("UNTIL"):
		l.Until = p.parseExpr()
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
	l.Body = &Block{Items: p.parseStmtSeq("ENDLOOP"), Line: line}
	p.expectKw("ENDLOOP")
	return l
}

func (p *Parser) parseSelect() Stmt {
	line := p.cur().Line
	p.expectKw("SELECT")
	s := &SelectStmt{Line: line}
	s.Subject = p.parseExpr()
	p.expectKw("FROM")
	for !p.isKw("ENDCASE") && p.cur().Kind != TEOF {
		var arm SelectArm
		arm.Guards = append(arm.Guards, p.parseExpr())
		for p.acceptPunct(",") {
			arm.Guards = append(arm.Guards, p.parseExpr())
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

func (p *Parser) parseReturn() Stmt {
	line := p.cur().Line
	p.expectKw("RETURN")
	r := &ReturnStmt{Line: line}
	if p.acceptPunct("[") {
		if !p.isPunct("]") {
			r.Values = append(r.Values, p.parseExpr())
			for p.acceptPunct(",") {
				r.Values = append(r.Values, p.parseExpr())
			}
		}
		p.expectPunct("]")
	}
	return r
}

// ---- Expressions ----

func (p *Parser) parseExpr() Expr {
	if p.isKw("IF") {
		return p.parseIfExpr()
	}
	return p.parseOr()
}

func (p *Parser) parseIfExpr() Expr {
	line := p.cur().Line
	p.expectKw("IF")
	cond := p.parseExpr()
	p.expectKw("THEN")
	then := p.parseExpr()
	p.expectKw("ELSE")
	els := p.parseExpr()
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
	for p.cur().Kind == TPunct && relOps[p.cur().Text] {
		op := p.advance()
		y := p.parseAdd()
		x = &Binary{Op: op.Text, L: x, R: y, Line: op.Line}
	}
	return x
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
	for p.isPunct("*") || p.isPunct("/") || p.isKw("MOD") {
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
	case p.isPunct("@"):
		line := p.advance().Line
		return &Unary{Op: "@", X: p.parseUnary(), Line: line}
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
			if !p.isPunct("]") && !p.isPunct("!") {
				args = append(args, p.parseExpr())
				for p.acceptPunct(",") {
					if p.isPunct("]") || p.isPunct("!") {
						break
					}
					args = append(args, p.parseExpr())
				}
			}
			if p.isPunct("!") { // a call-site catch clause: proc[args ! handler]
				p.skipCatch()
			}
			p.expectPunct("]")
			x = &Apply{Fun: x, Args: args, Line: line}
		case p.isPunct("."):
			line := p.advance().Line
			field := p.expectIdent()
			x = &FieldAccess{X: x, Field: field, Line: line}
		case p.isPunct("^"):
			p.advance() // dereference: identity in this model
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
		case "NIL":
			p.advance()
			return &NilLit{Line: t.Line}
		case "NEW":
			p.advance()
			// Cedar NEW[Type] or NEW[Type ← init]; Mesa also allows a bare type.
			if p.acceptPunct("[") {
				nt := p.parseType()
				if p.acceptPunct("<-") || p.acceptBind() {
					p.parseExpr() // discard the initialiser
				}
				p.expectPunct("]")
				return &NewExpr{Type: nt, Line: t.Line}
			}
			return &NewExpr{Type: p.parseType(), Line: t.Line}
		case "IF":
			return p.parseIfExpr()
		}
	case TPunct:
		switch t.Text {
		case "(":
			p.advance()
			x := p.parseExpr()
			p.expectPunct(")")
			return x
		case "[":
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
		var el AggElem
		// named element: name: value
		if p.cur().Kind == TIdent && p.peek().Kind == TPunct && p.peek().Text == ":" {
			el.Name = p.expectIdent()
			p.expectPunct(":")
		}
		el.Val = p.parseExpr()
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
