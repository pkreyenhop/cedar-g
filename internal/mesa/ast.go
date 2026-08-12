package mesa

// The AST is deliberately small: a handful of expression and statement
// node types cover the Mini-Mesa subset. Every node records the source
// line for runtime error messages.

// ---- Types (type expressions) ----

type TypeExpr interface{ typeNode() }

// NamedType references a predefined or user-declared type by name.
type NamedType struct {
	Name string
	Line int
}

// SubrangeType is a base type constrained to an interval, e.g. INTEGER[0..9].
type SubrangeType struct {
	Base *NamedType
	Ival *Interval
}

// ArrayType is ARRAY index-interval OF element-type.
type ArrayType struct {
	Index *Interval // may be nil (unbounded, sized by constructor)
	Elem  TypeExpr
}

// RecordType is RECORD [field: type, ...].
type RecordType struct {
	Fields []Field
}

type Field struct {
	Name string
	Type TypeExpr
}

// EnumType is an enumeration { a, b, c }.
type EnumType struct {
	Members []string
}

// ProcType is PROCEDURE [params] RETURNS [results].
type ProcType struct {
	Params  []Field
	Results []Field
}

func (*NamedType) typeNode()    {}
func (*SubrangeType) typeNode() {}
func (*ArrayType) typeNode()    {}
func (*RecordType) typeNode()   {}
func (*EnumType) typeNode()     {}
func (*ProcType) typeNode()     {}

// Interval represents a Mesa interval, e.g. [0..10) — closed low, open high.
type Interval struct {
	Lo, Hi Expr
	IncLo  bool
	IncHi  bool
}

// ---- Expressions ----

type Expr interface{ exprNode() }

type IntLit struct {
	Val  int64
	Line int
}
type RealLit struct {
	Val  float64
	Line int
}
type CharLit struct {
	Val  rune
	Line int
}
type StringLit struct {
	Val  string
	Line int
}
type BoolLit struct {
	Val  bool
	Line int
}
type NilLit struct{ Line int }

// Ident is a variable / constant / procedure / type reference.
type Ident struct {
	Name string
	Line int
}

// Unary is NOT / unary minus / @ (address-of, treated as identity here).
type Unary struct {
	Op   string
	X    Expr
	Line int
}

// Binary covers arithmetic, relational and boolean operators.
type Binary struct {
	Op   string
	L, R Expr
	Line int
}

// Apply is postfix f[args] — either a procedure call or an array index;
// resolved at evaluation time from the operand's runtime type. Catch holds any
// call-site "! handler => …" clause (proc[args ! sig => stmt]).
type Apply struct {
	Fun   Expr
	Args  []Expr
	Catch []Handler
	Line  int
}

// FieldAccess is r.field.
type FieldAccess struct {
	X     Expr
	Field string
	Line  int
}

// Aggregate is a bracketed constructor [e1, e2, ...] used to build array
// or record values. Named fields (field: value) are recorded too.
type Aggregate struct {
	Elems []AggElem
	Line  int
}

type AggElem struct {
	Name string // "" if positional
	Val  Expr
}

// IfExpr is IF cond THEN a ELSE b used in expression position.
type IfExpr struct {
	Cond, Then, Else Expr
	Line             int
}

// NewExpr is NEW[type] or NEW[type ← init] — allocate a heap cell (a REF) whose
// referent is a value of the type, optionally initialised.
type NewExpr struct {
	Type TypeExpr
	Init Expr // nil if none
	Line int
}

// Deref is p^ — dereference a REF/POINTER to read (or, as an assignment target,
// write) its referent.
type Deref struct {
	X    Expr
	Line int
}

func (*IntLit) exprNode()      {}
func (*RealLit) exprNode()     {}
func (*CharLit) exprNode()     {}
func (*StringLit) exprNode()   {}
func (*BoolLit) exprNode()     {}
func (*NilLit) exprNode()      {}
func (*Ident) exprNode()       {}
func (*Unary) exprNode()       {}
func (*Binary) exprNode()      {}
func (*Apply) exprNode()       {}
func (*FieldAccess) exprNode() {}
func (*Aggregate) exprNode()   {}
func (*IfExpr) exprNode()      {}
func (*NewExpr) exprNode()     {}
func (*Deref) exprNode()       {}
func (*SelectExpr) exprNode()  {}

// ---- Statements ----

type Stmt interface{ stmtNode() }

// VarDecl declares one or more variables/constants with an optional init.
type VarDecl struct {
	Names   []string
	Type    TypeExpr
	Init    Expr // nil if none
	IsConst bool // declared with '=' rather than '<-'
	Line    int
}

// TypeDecl binds a name to a type (id: TYPE = typeExpr).
type TypeDecl struct {
	Name string
	Type TypeExpr
	Line int
}

// ProcDecl binds a name to a procedure value.
type ProcDecl struct {
	Name string
	Type *ProcType
	Body *Block
	Line int
}

// Assign is lhs <- rhs. Lhs must be an assignable expression.
type Assign struct {
	Lhs  Expr
	Rhs  Expr
	Line int
}

// ExprStmt is a bare expression evaluated for effect (a call).
type ExprStmt struct {
	X    Expr
	Line int
}

// Block is BEGIN ... END or a bracketed body; a scope with items. A leading
// ENABLE clause installs Handlers over the whole block.
type Block struct {
	Items    []Stmt
	Handlers []Handler // ENABLE handlers active over this block
	Line     int
}

// Handler is one arm of an ENABLE or "! …" catch clause: a set of signal guards
// and the body to run when one is raised. An empty Guards matches any signal
// (ANY => …).
type Handler struct {
	Guards []Expr
	Body   Stmt
}

// RaiseStmt raises a condition: ERROR/SIGNAL/RAISE [signal[args]]. A nil Sig is
// a bare ERROR (re-raise) or RETURN WITH ERROR.
type RaiseStmt struct {
	Sig  Expr
	Line int
}

// Guarded wraps a statement with its trailing "! handler => …" catch clauses.
type Guarded struct {
	Stmt     Stmt
	Handlers []Handler
	Line     int
}

type IfStmt struct {
	Cond Expr
	Then Stmt
	Else Stmt // nil if none
	Line int
}

// Loop is the unified loop node covering FOR/WHILE/UNTIL/THROUGH/DO.
type Loop struct {
	// Iterator (FOR var IN interval, or FOR var <- start, next):
	Var      string
	VarType  TypeExpr
	Interval *Interval // FOR var IN [..] / THROUGH [..]
	Start    Expr      // FOR var <- start, next
	Next     Expr
	// Guards:
	While Expr // WHILE cond
	Until Expr // UNTIL cond
	Body  *Block
	Line  int
}

// SelectStmt is SELECT subject FROM arm... ENDCASE => default.
type SelectStmt struct {
	Subject Expr
	Arms    []SelectArm
	Default Stmt // nil if no ENDCASE arm
	Line    int
}

type SelectArm struct {
	Guards []Expr // one arm may match several values: 2, 3 =>
	Body   Stmt
}

// SelectExpr is SELECT subject FROM guard => value, … ENDCASE => value used in
// expression position (x ← SELECT c FROM …).
type SelectExpr struct {
	Subject Expr
	Arms    []SelectExprArm
	Default Expr // nil if no ENDCASE arm
	Line    int
}

type SelectExprArm struct {
	Guards []Expr
	Val    Expr
}

type ReturnStmt struct {
	Values []Expr // aggregate of results, may be empty
	Line   int
}

type ExitStmt struct{ Line int }
type LoopCtl struct{ Line int } // the LOOP statement (continue)
type NullStmt struct{ Line int }

func (*VarDecl) stmtNode()    {}
func (*TypeDecl) stmtNode()   {}
func (*ProcDecl) stmtNode()   {}
func (*Assign) stmtNode()     {}
func (*ExprStmt) stmtNode()   {}
func (*Block) stmtNode()      {}
func (*IfStmt) stmtNode()     {}
func (*Loop) stmtNode()       {}
func (*SelectStmt) stmtNode() {}
func (*ReturnStmt) stmtNode() {}
func (*ExitStmt) stmtNode()   {}
func (*LoopCtl) stmtNode()    {}
func (*NullStmt) stmtNode()   {}
func (*RaiseStmt) stmtNode()  {}
func (*Guarded) stmtNode()    {}

// Module is a top-level PROGRAM/DEFINITIONS/MODULE unit.
type Module struct {
	Name string
	Kind string // "PROGRAM", "DEFINITIONS", "MODULE"
	Body *Block
	// Recovered counts statements skipped by error recovery; 0 means a clean
	// parse, > 0 means some fragments were not understood but were tolerated.
	Recovered int
}
