package mesa

import (
	"fmt"
	"io"
	"strings"
)

// ---- Environments ----

type Env struct {
	vars   map[string]any
	parent *Env
}

func newEnv(parent *Env) *Env {
	return &Env{vars: map[string]any{}, parent: parent}
}

func (e *Env) lookup(name string) (any, bool) {
	for s := e; s != nil; s = s.parent {
		if v, ok := s.vars[name]; ok {
			return v, true
		}
	}
	return nil, false
}

// set assigns to an existing binding, searching outward.
func (e *Env) set(name string, v any) bool {
	for s := e; s != nil; s = s.parent {
		if _, ok := s.vars[name]; ok {
			s.vars[name] = v
			return true
		}
	}
	return false
}

func (e *Env) define(name string, v any) { e.vars[name] = v }

// ---- Control-flow signals (implemented with panic/recover) ----

type returnSignal struct{ values []any }
type exitSignal struct{}
type loopSignal struct{}

// runtimeError carries a source line for reporting.
type runtimeError struct {
	line int
	msg  string
}

func (e runtimeError) Error() string {
	if e.line > 0 {
		return fmt.Sprintf("runtime error at line %d: %s", e.line, e.msg)
	}
	return "runtime error: " + e.msg
}

func rerr(line int, format string, a ...any) {
	panic(runtimeError{line: line, msg: fmt.Sprintf(format, a...)})
}

// ---- Interpreter ----

type Interp struct {
	global   *Env
	types    map[string]Type
	out      io.Writer
	steps    int64 // executed statements/evaluations, bounded by maxSteps
	maxSteps int64
}

// defaultMaxSteps bounds a single run. The interpreter has no cancellation, so a
// runaway program (infinite loop or recursion) would otherwise leak a goroutine
// past the caller's wall-clock timeout; this stops it deterministically. The
// budget is generous — real sample programs execute well under a million steps.
const defaultMaxSteps = 50_000_000

func NewInterp(out io.Writer) *Interp {
	i := &Interp{global: newEnv(nil), types: map[string]Type{}, out: out, maxSteps: defaultMaxSteps}
	i.installBuiltins()
	return i
}

// SetMaxSteps overrides the execution budget (0 keeps the default). Useful for
// bulk surveys that want a tighter bound per module.
func (i *Interp) SetMaxSteps(n int64) {
	if n > 0 {
		i.maxSteps = n
	}
}

// tick charges one unit against the execution budget, aborting a runaway run.
func (i *Interp) tick() {
	i.steps++
	if i.steps > i.maxSteps {
		panic(runtimeError{msg: "execution budget exceeded (possible infinite loop)"})
	}
}

// Run executes a parsed module.
func (i *Interp) Run(m *Module) (err error) {
	defer func() {
		if r := recover(); r != nil {
			switch e := r.(type) {
			case runtimeError:
				err = e
			case returnSignal:
				// a top-level RETURN simply ends the program
			default:
				panic(r)
			}
		}
	}()
	// The module body executes directly in the global scope so that its
	// procedures and types are program-visible.
	i.execBlock(m.Body, i.global)
	return nil
}

// execBlock runs a block's items in the given environment, hoisting type
// and procedure declarations first so forward and mutual references work.
func (i *Interp) execBlock(b *Block, env *Env) {
	for _, it := range b.Items {
		switch d := it.(type) {
		case *TypeDecl:
			i.execStmt(d, env)
		case *ProcDecl:
			i.execStmt(d, env)
		}
	}
	for _, it := range b.Items {
		switch it.(type) {
		case *TypeDecl, *ProcDecl:
			// already handled in the hoist pass
		default:
			i.execStmt(it, env)
		}
	}
}

func (i *Interp) execStmt(s Stmt, env *Env) {
	i.tick()
	switch st := s.(type) {
	case *VarDecl:
		i.execVarDecl(st, env)
	case *TypeDecl:
		i.execTypeDecl(st, env)
	case *ProcDecl:
		env.define(st.Name, &Closure{Name: st.Name, Type: st.Type, Body: st.Body, Env: env})
	case *Assign:
		i.assignTo(st.Lhs, i.eval(st.Rhs, env), env)
	case *ExprStmt:
		i.eval(st.X, env)
	case *Block:
		i.execBlock(st, newEnv(env))
	case *IfStmt:
		if toBool(i.eval(st.Cond, env), st.Line) {
			i.execStmt(st.Then, env)
		} else if st.Else != nil {
			i.execStmt(st.Else, env)
		}
	case *Loop:
		i.execLoop(st, env)
	case *SelectStmt:
		i.execSelect(st, env)
	case *ReturnStmt:
		vals := make([]any, len(st.Values))
		for k, e := range st.Values {
			vals[k] = i.eval(e, env)
		}
		panic(returnSignal{values: vals})
	case *ExitStmt:
		panic(exitSignal{})
	case *LoopCtl:
		panic(loopSignal{})
	case *NullStmt:
		// nothing
	default:
		rerr(0, "cannot execute statement %T", s)
	}
}

func (i *Interp) execVarDecl(st *VarDecl, env *Env) {
	t := i.resolveType(st.Type, env)
	if st.Init != nil {
		v := i.eval(st.Init, env)
		v = i.coerce(v, t)
		for _, n := range st.Names {
			env.define(n, v)
		}
		return
	}
	for _, n := range st.Names {
		env.define(n, i.defaultValue(t))
	}
}

func (i *Interp) execTypeDecl(st *TypeDecl, env *Env) {
	t := i.resolveType(st.Type, env)
	// name the descriptor
	switch d := t.(type) {
	case *EnumTypeDesc:
		d.Name = st.Name
		for ord, m := range d.Members {
			env.define(m, EnumVal{TypeName: st.Name, Ord: ord, Name: m})
		}
	case *RecordTypeDesc:
		d.Name = st.Name
	}
	i.types[st.Name] = t
}

func (i *Interp) execSelect(st *SelectStmt, env *Env) {
	subj := i.eval(st.Subject, env)
	for _, arm := range st.Arms {
		for _, g := range arm.Guards {
			if valueEqual(subj, i.eval(g, env)) {
				i.execStmt(arm.Body, env)
				return
			}
		}
	}
	if st.Default != nil {
		i.execStmt(st.Default, env)
	}
}

func (i *Interp) execLoop(l *Loop, env *Env) {
	lenv := newEnv(env)
	isChar := false
	if nt, ok := l.VarType.(*NamedType); ok && nt.Name == "CHARACTER" {
		isChar = true
	}
	mk := func(n int64) any {
		if isChar {
			return Char(rune(n))
		}
		return n
	}

	guardOK := func() bool {
		if l.While != nil && !toBool(i.eval(l.While, lenv), l.Line) {
			return false
		}
		if l.Until != nil && toBool(i.eval(l.Until, lenv), l.Line) {
			return false
		}
		return true
	}

	switch {
	case l.Interval != nil:
		lo := toInt(i.eval(l.Interval.Lo, lenv), l.Line)
		hi := toInt(i.eval(l.Interval.Hi, lenv), l.Line)
		if !l.Interval.IncLo {
			lo++
		}
		if !l.Interval.IncHi {
			hi--
		}
		for n := lo; n <= hi; n++ {
			if l.Var != "" {
				lenv.define(l.Var, mk(n))
			}
			if !guardOK() {
				break
			}
			if i.runLoopBody(l.Body, lenv) {
				break
			}
		}
	case l.Start != nil:
		lenv.define(l.Var, i.eval(l.Start, lenv))
		for {
			if !guardOK() {
				break
			}
			if i.runLoopBody(l.Body, lenv) {
				break
			}
			lenv.define(l.Var, i.eval(l.Next, lenv))
		}
	default:
		for {
			if !guardOK() {
				break
			}
			if i.runLoopBody(l.Body, lenv) {
				break
			}
		}
	}
}

// runLoopBody runs one iteration; returns true if an EXIT was hit. Each
// iteration charges the execution budget so an empty-bodied loop (DO ENDLOOP)
// cannot spin forever without executing a statement.
func (i *Interp) runLoopBody(body *Block, env *Env) (exit bool) {
	i.tick()
	defer func() {
		if r := recover(); r != nil {
			switch r.(type) {
			case loopSignal:
				// continue with next iteration
			case exitSignal:
				exit = true
			default:
				panic(r)
			}
		}
	}()
	i.execBlock(body, newEnv(env))
	return
}

// ---- Assignment targets ----

func (i *Interp) assignTo(lhs Expr, val any, env *Env) {
	switch t := lhs.(type) {
	case *Ident:
		if !env.set(t.Name, val) {
			rerr(t.Line, "assignment to undeclared variable %q", t.Name)
		}
	case *FieldAccess:
		base := deref(i.eval(t.X, env)) // p.field on a REF mutates the referent
		switch b := base.(type) {
		case *RecordVal:
			if _, ok := b.Fields[t.Field]; !ok {
				rerr(t.Line, "record has no field %q", t.Field)
			}
			b.Fields[t.Field] = val
		case *Cons:
			switch t.Field {
			case "first":
				b.First = val
			case "rest":
				if val == nil {
					b.Rest = nil
				} else if c, ok := deref(val).(*Cons); ok {
					b.Rest = c
				} else {
					rerr(t.Line, "list .rest must be a list")
				}
			default:
				rerr(t.Line, "list has no field %q", t.Field)
			}
		default:
			rerr(t.Line, "cannot assign field .%s of non-record", t.Field)
		}
	case *Deref:
		base := i.eval(t.X, env)
		r, ok := base.(*Ref)
		if !ok || r == nil {
			rerr(t.Line, "cannot assign through a non-REF or NIL value")
		}
		r.Elem = val
	case *Apply:
		base := deref(i.eval(t.Fun, env))
		arr, ok := base.(*ArrayVal)
		if !ok {
			rerr(t.Line, "cannot index-assign into non-array")
		}
		if len(t.Args) != 1 {
			rerr(t.Line, "array assignment needs exactly one index")
		}
		idx := toInt(i.eval(t.Args[0], env), t.Line)
		k, ok := arr.index(idx)
		if !ok {
			rerr(t.Line, "array index %d out of bounds", idx)
		}
		arr.Elems[k] = val
	default:
		rerr(0, "invalid assignment target %T", lhs)
	}
}

// ---- Expression evaluation ----

func (i *Interp) eval(e Expr, env *Env) any {
	i.tick()
	switch x := e.(type) {
	case *IntLit:
		return x.Val
	case *RealLit:
		return x.Val
	case *CharLit:
		return Char(x.Val)
	case *StringLit:
		return x.Val
	case *BoolLit:
		return x.Val
	case *NilLit:
		return nil
	case *Ident:
		v, ok := env.lookup(x.Name)
		if !ok {
			rerr(x.Line, "undefined identifier %q", x.Name)
		}
		return v
	case *Unary:
		return i.evalUnary(x, env)
	case *Binary:
		return i.evalBinary(x, env)
	case *Apply:
		return i.evalApply(x, env)
	case *FieldAccess:
		base := deref(i.eval(x.X, env)) // a REF to a record auto-dereferences
		switch b := base.(type) {
		case *RecordVal:
			v, ok := b.Fields[x.Field]
			if !ok {
				rerr(x.Line, "record has no field %q", x.Field)
			}
			return v
		case *Cons: // LIST OF T: .first is the head, .rest the tail
			switch x.Field {
			case "first":
				return b.First
			case "rest":
				if b.Rest == nil {
					return nil
				}
				return b.Rest
			}
			rerr(x.Line, "list has no field %q", x.Field)
		}
		rerr(x.Line, "cannot access field .%s of non-record value", x.Field)
	case *Aggregate:
		return i.evalAggregate(x, env)
	case *IfExpr:
		if toBool(i.eval(x.Cond, env), x.Line) {
			return i.eval(x.Then, env)
		}
		return i.eval(x.Else, env)
	case *NewExpr:
		t := i.resolveType(x.Type, env)
		var elem any
		if x.Init != nil {
			elem = i.coerce(i.eval(x.Init, env), t)
		} else {
			elem = i.defaultValue(t)
		}
		return &Ref{Elem: elem}
	case *Deref:
		v := i.eval(x.X, env)
		r, ok := v.(*Ref)
		if !ok {
			return v // tolerant: p^ on a non-REF is the value itself
		}
		if r == nil {
			rerr(x.Line, "attempt to dereference NIL")
		}
		return r.Elem
	}
	rerr(0, "cannot evaluate expression %T", e)
	return nil
}

func (i *Interp) evalUnary(x *Unary, env *Env) any {
	switch x.Op {
	case "NOT":
		return !toBool(i.eval(x.X, env), x.Line)
	case "-":
		v := i.eval(x.X, env)
		switch n := v.(type) {
		case int64:
			return -n
		case float64:
			return -n
		}
		rerr(x.Line, "unary '-' needs a number")
	case "@":
		// address-of: this model has no separate pointer type
		return i.eval(x.X, env)
	}
	rerr(x.Line, "unknown unary operator %q", x.Op)
	return nil
}

func (i *Interp) evalBinary(x *Binary, env *Env) any {
	// short-circuit boolean operators
	switch x.Op {
	case "AND":
		if !toBool(i.eval(x.L, env), x.Line) {
			return false
		}
		return toBool(i.eval(x.R, env), x.Line)
	case "OR":
		if toBool(i.eval(x.L, env), x.Line) {
			return true
		}
		return toBool(i.eval(x.R, env), x.Line)
	}

	l := i.eval(x.L, env)
	r := i.eval(x.R, env)

	switch x.Op {
	case "=":
		return valueEqual(l, r)
	case "#":
		return !valueEqual(l, r)
	case "<", "<=", ">", ">=":
		c, ok := valueCompare(l, r)
		if !ok {
			rerr(x.Line, "cannot order %s and %s", typeName(l), typeName(r))
		}
		switch x.Op {
		case "<":
			return c < 0
		case "<=":
			return c <= 0
		case ">":
			return c > 0
		default:
			return c >= 0
		}
	}

	// String concatenation with '+'
	if x.Op == "+" {
		if ls, ok := l.(string); ok {
			return ls + FormatValue(r)
		}
		if rs, ok := r.(string); ok {
			return FormatValue(l) + rs
		}
	}

	return i.arith(x.Op, l, r, x.Line)
}

func (i *Interp) arith(op string, l, r any, line int) any {
	// If either operand is REAL, compute in float.
	lf, lIsF := l.(float64)
	rf, rIsF := r.(float64)
	li, lIsI := l.(int64)
	ri, rIsI := r.(int64)

	if lIsF || rIsF {
		a := asFloat(l, line)
		b := asFloat(r, line)
		switch op {
		case "+":
			return a + b
		case "-":
			return a - b
		case "*":
			return a * b
		case "/":
			if b == 0 {
				rerr(line, "division by zero")
			}
			return a / b
		case "MOD":
			rerr(line, "MOD is not defined for REAL")
		}
	}

	if lIsI && rIsI {
		switch op {
		case "+":
			return li + ri
		case "-":
			return li - ri
		case "*":
			return li * ri
		case "/":
			if ri == 0 {
				rerr(line, "division by zero")
			}
			return li / ri
		case "MOD":
			if ri == 0 {
				rerr(line, "MOD by zero")
			}
			return li % ri
		}
	}
	_ = lf
	_ = rf
	rerr(line, "operator %q needs numbers, got %s and %s", op, typeName(l), typeName(r))
	return nil
}

func (i *Interp) evalApply(x *Apply, env *Env) any {
	fn := deref(i.eval(x.Fun, env)) // a REF to an array/proc auto-dereferences
	switch f := fn.(type) {
	case *Closure:
		return i.callClosure(f, x.Args, env, x.Line)
	case *Builtin:
		args := make([]any, len(x.Args))
		for k, a := range x.Args {
			args[k] = i.eval(a, env)
		}
		return f.Fn(i, args)
	case *ArrayVal:
		if len(x.Args) != 1 {
			rerr(x.Line, "array index needs exactly one subscript")
		}
		idx := toInt(i.eval(x.Args[0], env), x.Line)
		k, ok := f.index(idx)
		if !ok {
			rerr(x.Line, "array index %d out of bounds", idx)
		}
		return f.Elems[k]
	case string:
		if len(x.Args) != 1 {
			rerr(x.Line, "string index needs exactly one subscript")
		}
		idx := toInt(i.eval(x.Args[0], env), x.Line)
		rs := []rune(f)
		if idx < 0 || int(idx) >= len(rs) {
			rerr(x.Line, "string index %d out of bounds", idx)
		}
		return Char(rs[idx])
	}
	rerr(x.Line, "cannot call or index a %s", typeName(fn))
	return nil
}

func (i *Interp) callClosure(c *Closure, argExprs []Expr, env *Env, line int) any {
	if len(argExprs) != len(c.Type.Params) {
		rerr(line, "%s expects %d argument(s), got %d",
			c.Name, len(c.Type.Params), len(argExprs))
	}
	local := newEnv(c.Env)
	for k, p := range c.Type.Params {
		v := i.eval(argExprs[k], env)
		v = i.coerce(v, i.resolveType(p.Type, c.Env))
		local.define(p.Name, v)
	}
	// initialise named result variables to defaults
	for _, res := range c.Type.Results {
		if res.Name != "" {
			local.define(res.Name, i.defaultValue(i.resolveType(res.Type, c.Env)))
		}
	}

	var sig *returnSignal
	func() {
		defer func() {
			if r := recover(); r != nil {
				if rs, ok := r.(returnSignal); ok {
					sig = &rs
					return
				}
				panic(r)
			}
		}()
		i.execBlock(c.Body, local)
	}()

	return i.computeReturn(sig, c.Type.Results, local, c.Env)
}

func (i *Interp) computeReturn(sig *returnSignal, results []Field, local, defEnv *Env) any {
	n := len(results)
	if n == 0 {
		return nil
	}
	vals := make([]any, n)
	for k, res := range results {
		switch {
		case sig != nil && k < len(sig.values):
			vals[k] = i.coerce(sig.values[k], i.resolveType(res.Type, defEnv))
		case res.Name != "":
			v, _ := local.lookup(res.Name)
			vals[k] = v
		default:
			vals[k] = i.defaultValue(i.resolveType(res.Type, defEnv))
		}
	}
	if n == 1 {
		return vals[0]
	}
	// multiple results -> a record keyed by result name (or f0, f1, ...)
	rec := &RecordVal{Fields: map[string]any{}}
	for k, res := range results {
		name := res.Name
		if name == "" {
			name = fmt.Sprintf("f%d", k)
		}
		rec.Names = append(rec.Names, name)
		rec.Fields[name] = vals[k]
	}
	return rec
}

func (i *Interp) evalAggregate(x *Aggregate, env *Env) any {
	named := false
	for _, el := range x.Elems {
		if el.Name != "" {
			named = true
		}
	}
	if named {
		rec := &RecordVal{Fields: map[string]any{}}
		for k, el := range x.Elems {
			name := el.Name
			if name == "" {
				name = fmt.Sprintf("f%d", k)
			}
			rec.Names = append(rec.Names, name)
			rec.Fields[name] = i.eval(el.Val, env)
		}
		return rec
	}
	arr := &ArrayVal{Lo: 0}
	for _, el := range x.Elems {
		arr.Elems = append(arr.Elems, i.eval(el.Val, env))
	}
	return arr
}

// ---- Type resolution and defaults ----

var baseTypeAliases = map[string]string{
	"INTEGER": "INTEGER", "CARDINAL": "INTEGER", "NAT": "INTEGER",
	"LONG": "INTEGER", "WORD": "INTEGER", "UNSPECIFIED": "INTEGER",
	"INT": "INTEGER", "CARD": "INTEGER", "BYTE": "INTEGER",
	"INT16": "INTEGER", "INT32": "INTEGER", "CARD16": "INTEGER", "CARD32": "INTEGER",
	"NAT16": "INTEGER", "NAT32": "INTEGER", "WORD16": "INTEGER", "WORD32": "INTEGER",
	"BOOLEAN": "BOOLEAN", "BOOL": "BOOLEAN",
	"CHARACTER": "CHARACTER", "CHAR": "CHARACTER",
	"REAL": "REAL", "LONGREAL": "REAL",
	"STRING": "STRING", "ROPE": "STRING", "TEXT": "STRING",
}

// qualifiedTypeAliases maps a few well-known imported types onto our primitives.
// Everything else qualified (Pkg.Type) becomes an opaque handle.
var qualifiedTypeAliases = map[string]string{
	"Rope.ROPE": "STRING", "Rope.Text": "STRING", "Rope.Ref": "STRING",
	"RopeInline.ROPE": "STRING", "Convert.Base": "INTEGER",
}

// opaqueTypeNames are the reference and collection type keywords the interpreter
// carries as opaque handles (default NIL). REF/POINTER/LIST/SEQUENCE also record
// a referent type via resolveType so Step 4's NEW/deref can allocate.
var opaqueTypeNames = map[string]bool{
	"REF": true, "POINTER": true, "LIST": true, "SEQUENCE": true,
	"DESCRIPTOR": true, "ANY": true, "ATOM": true, "PROGRAM": true,
	"PROCESS": true, "CONDITION": true, "MONITORLOCK": true, "ZONE": true,
}

func (i *Interp) resolveType(te TypeExpr, env *Env) Type {
	switch t := te.(type) {
	case nil:
		return &BaseType{Name: "INTEGER"}
	case *NamedType:
		if base, ok := baseTypeAliases[t.Name]; ok {
			return &BaseType{Name: base}
		}
		if d, ok := i.types[t.Name]; ok {
			return d
		}
		if opaqueTypeNames[t.Name] {
			return &OpaqueType{Name: t.Name}
		}
		if base, ok := qualifiedTypeAliases[t.Name]; ok {
			return &BaseType{Name: base}
		}
		// An imported (Pkg.Type) or otherwise unresolved type: its definition
		// lives in an interface we do not model. Carry it as an opaque handle
		// rather than aborting the whole program.
		return &OpaqueType{Name: t.Name}
	case *SubrangeType:
		lo := toInt(i.evalConst(t.Ival.Lo), 0)
		hi := toInt(i.evalConst(t.Ival.Hi), 0)
		if !t.Ival.IncLo {
			lo++
		}
		if !t.Ival.IncHi {
			hi--
		}
		base := &BaseType{Name: "INTEGER"}
		if t.Base != nil {
			if b, ok := baseTypeAliases[t.Base.Name]; ok {
				base = &BaseType{Name: b}
			}
		}
		return &SubrangeTypeDesc{Base: base, Lo: lo, Hi: hi}
	case *ArrayType:
		var lo, hi int64 = 0, -1
		if t.Index != nil {
			lo = toInt(i.evalConst(t.Index.Lo), 0)
			hi = toInt(i.evalConst(t.Index.Hi), 0)
			if !t.Index.IncLo {
				lo++
			}
			if !t.Index.IncHi {
				hi--
			}
		}
		return &ArrayTypeDesc{Lo: lo, Hi: hi, Elem: i.resolveType(t.Elem, env)}
	case *RecordType:
		d := &RecordTypeDesc{}
		for _, f := range t.Fields {
			d.Fields = append(d.Fields, FieldDesc{Name: f.Name, Type: i.resolveType(f.Type, env)})
		}
		return d
	case *EnumType:
		return &EnumTypeDesc{Members: append([]string(nil), t.Members...)}
	case *ProcType:
		return &ProcTypeDesc{PT: t}
	}
	rerr(0, "cannot resolve type %T", te)
	return nil
}

// evalConst evaluates a compile-time expression (interval bounds) in the
// global environment.
func (i *Interp) evalConst(e Expr) any {
	return i.eval(e, i.global)
}

func (i *Interp) defaultValue(t Type) any {
	switch d := t.(type) {
	case *BaseType:
		switch d.Name {
		case "INTEGER":
			return int64(0)
		case "REAL":
			return float64(0)
		case "BOOLEAN":
			return false
		case "CHARACTER":
			return Char(0)
		case "STRING":
			return ""
		}
	case *SubrangeTypeDesc:
		return d.Lo
	case *EnumTypeDesc:
		name := ""
		if len(d.Members) > 0 {
			name = d.Members[0]
		}
		return EnumVal{TypeName: d.Name, Ord: 0, Name: name}
	case *RecordTypeDesc:
		rec := &RecordVal{TypeName: d.Name, Fields: map[string]any{}}
		for _, f := range d.Fields {
			rec.Names = append(rec.Names, f.Name)
			rec.Fields[f.Name] = i.defaultValue(f.Type)
		}
		return rec
	case *ArrayTypeDesc:
		arr := &ArrayVal{Lo: d.Lo}
		n := d.Hi - d.Lo + 1
		for k := int64(0); k < n; k++ {
			arr.Elems = append(arr.Elems, i.defaultValue(d.Elem))
		}
		return arr
	case *ProcTypeDesc:
		return nil
	}
	return nil
}

// coerce adapts a value (often an aggregate literal) to a declared type.
func (i *Interp) coerce(v any, t Type) any {
	switch d := t.(type) {
	case *BaseType:
		if d.Name == "REAL" {
			if n, ok := v.(int64); ok {
				return float64(n)
			}
		}
		return v
	case *ArrayTypeDesc:
		if arr, ok := v.(*ArrayVal); ok {
			out := &ArrayVal{Lo: d.Lo}
			for _, e := range arr.Elems {
				out.Elems = append(out.Elems, i.coerce(e, d.Elem))
			}
			return out
		}
		return v
	case *RecordTypeDesc:
		switch src := v.(type) {
		case *ArrayVal: // positional aggregate -> record
			rec := &RecordVal{TypeName: d.Name, Fields: map[string]any{}}
			for k, f := range d.Fields {
				rec.Names = append(rec.Names, f.Name)
				if k < len(src.Elems) {
					rec.Fields[f.Name] = i.coerce(src.Elems[k], f.Type)
				} else {
					rec.Fields[f.Name] = i.defaultValue(f.Type)
				}
			}
			return rec
		case *RecordVal: // named aggregate -> record
			rec := &RecordVal{TypeName: d.Name, Fields: map[string]any{}}
			for _, f := range d.Fields {
				rec.Names = append(rec.Names, f.Name)
				if fv, ok := src.Fields[f.Name]; ok {
					rec.Fields[f.Name] = i.coerce(fv, f.Type)
				} else {
					rec.Fields[f.Name] = i.defaultValue(f.Type)
				}
			}
			return rec
		}
		return v
	}
	return v
}

// ---- Value helpers ----

func toBool(v any, line int) bool {
	b, ok := v.(bool)
	if !ok {
		rerr(line, "expected BOOLEAN, got %s", typeName(v))
	}
	return b
}

func toInt(v any, line int) int64 {
	switch n := v.(type) {
	case int64:
		return n
	case Char:
		return int64(n)
	case float64:
		return int64(n)
	}
	rerr(line, "expected INTEGER, got %s", typeName(v))
	return 0
}

func asFloat(v any, line int) float64 {
	switch n := v.(type) {
	case float64:
		return n
	case int64:
		return float64(n)
	}
	rerr(line, "expected a number, got %s", typeName(v))
	return 0
}

func valueEqual(a, b any) bool {
	switch x := a.(type) {
	case int64:
		if y, ok := b.(int64); ok {
			return x == y
		}
		if y, ok := b.(float64); ok {
			return float64(x) == y
		}
	case float64:
		return x == asFloatOrNaN(b)
	case bool:
		y, ok := b.(bool)
		return ok && x == y
	case Char:
		y, ok := b.(Char)
		return ok && x == y
	case string:
		y, ok := b.(string)
		return ok && x == y
	case EnumVal:
		y, ok := b.(EnumVal)
		return ok && x.TypeName == y.TypeName && x.Ord == y.Ord
	case *Ref: // REF equality is cell identity
		y, ok := b.(*Ref)
		return ok && x == y
	case *Cons: // list identity (empty lists are represented by untyped nil)
		y, ok := b.(*Cons)
		return ok && x == y
	case nil:
		return b == nil
	}
	return false
}

func asFloatOrNaN(v any) float64 {
	switch n := v.(type) {
	case float64:
		return n
	case int64:
		return float64(n)
	}
	return 1e308 // sentinel that won't equal typical values
}

// valueCompare returns -1/0/1 and whether the comparison is defined.
func valueCompare(a, b any) (int, bool) {
	switch x := a.(type) {
	case int64:
		switch y := b.(type) {
		case int64:
			return cmpInt(x, y), true
		case float64:
			return cmpFloat(float64(x), y), true
		}
	case float64:
		switch y := b.(type) {
		case float64:
			return cmpFloat(x, y), true
		case int64:
			return cmpFloat(x, float64(y)), true
		}
	case Char:
		if y, ok := b.(Char); ok {
			return cmpInt(int64(x), int64(y)), true
		}
	case string:
		if y, ok := b.(string); ok {
			return strings.Compare(x, y), true
		}
	case EnumVal:
		if y, ok := b.(EnumVal); ok && x.TypeName == y.TypeName {
			return cmpInt(int64(x.Ord), int64(y.Ord)), true
		}
	}
	return 0, false
}

func cmpInt(a, b int64) int {
	switch {
	case a < b:
		return -1
	case a > b:
		return 1
	default:
		return 0
	}
}
func cmpFloat(a, b float64) int {
	switch {
	case a < b:
		return -1
	case a > b:
		return 1
	default:
		return 0
	}
}

func typeName(v any) string {
	switch v.(type) {
	case int64:
		return "INTEGER"
	case float64:
		return "REAL"
	case bool:
		return "BOOLEAN"
	case Char:
		return "CHARACTER"
	case string:
		return "STRING"
	case EnumVal:
		return "enumeration"
	case *ArrayVal:
		return "ARRAY"
	case *RecordVal:
		return "RECORD"
	case *Closure, *Builtin:
		return "PROCEDURE"
	case nil:
		return "NIL"
	}
	return fmt.Sprintf("%T", v)
}
