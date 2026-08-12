package mesa

import (
	"fmt"
	"io"
	"math"
	"strings"
)

// ---- Environments ----

type Env struct {
	vars   map[string]any
	parent *Env
	opens  []any // namespaces from an OPEN clause, searched for unqualified names
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

// lookupOpen resolves a bare name against the namespaces brought into scope by
// enclosing OPEN clauses (Cedar's "OPEN Iface" makes Iface's members visible
// unqualified). A member of an opaque or Open interface resolves to an opaque.
func (e *Env) lookupOpen(name string) (any, bool) {
	for s := e; s != nil; s = s.parent {
		for _, ns := range s.opens {
			switch n := deref(ns).(type) {
			case *RecordVal:
				if v, ok := n.Fields[name]; ok {
					return v, true
				}
				if n.Open {
					return &Opaque{Name: n.TypeName + "." + name}, true
				}
			case *Opaque:
				return &Opaque{Name: n.Name + "." + name}, true
			}
		}
	}
	return nil, false
}

// ---- Control-flow signals (implemented with panic/recover) ----

type returnSignal struct{ values []any }
type exitSignal struct{}
type loopSignal struct{}

// raisedSignal carries a raised ERROR/SIGNAL up the stack until a matching
// ENABLE or "! …" handler catches it (or it reaches the top and aborts).
type raisedSignal struct {
	sig  any
	name string
}

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
			case raisedSignal:
				err = runtimeError{msg: "uncaught " + e.name}
			default:
				panic(r)
			}
		}
	}()
	i.bindImports(m)
	// The module body executes directly in the global scope so that its
	// procedures and types are program-visible.
	i.execBlock(m.Body, i.global)
	return nil
}

// bindImports binds each imported interface name to a value. Names we model
// (IO, Rope, Convert, …) are already defined; the rest become opaque namespaces
// so "Pkg.Proc[…]" references elaborate to opaque handles instead of failing.
func (i *Interp) bindImports(m *Module) {
	for _, name := range m.Imports {
		if _, ok := i.global.lookup(name); ok {
			continue // a real interface stub, or already bound
		}
		i.global.define(name, &Opaque{Name: name})
	}
}

// execBlock runs a block's items in the given environment, hoisting type
// and procedure declarations first so forward and mutual references work. A
// block with ENABLE handlers catches conditions raised anywhere within it.
func (i *Interp) execBlock(b *Block, env *Env) {
	// Bring any OPEN'd namespaces into scope for unqualified member references.
	for _, oc := range b.Opens {
		ns := i.eval(oc.Expr, env)
		env.opens = append(env.opens, ns)
		if oc.Bind != "" {
			env.define(oc.Bind, ns)
		}
	}
	if len(b.Handlers) > 0 {
		defer func() {
			if r := recover(); r != nil {
				rs, ok := r.(raisedSignal)
				if !ok || !i.handle(rs, b.Handlers, env) {
					panic(r)
				}
			}
		}()
	}
	i.execBlockItems(b, env)
}

func (i *Interp) execBlockItems(b *Block, env *Env) {
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
	case *RaiseStmt:
		i.raise(st.Sig, env)
	case *Guarded:
		i.execGuarded(st, env)
	default:
		rerr(0, "cannot execute statement %T", s)
	}
}

// raise evaluates a condition and throws it up the stack to the nearest handler.
func (i *Interp) raise(sigExpr Expr, env *Env) {
	var sig any
	name := "ERROR"
	if sigExpr != nil {
		sig = i.evalRaised(sigExpr, env)
		name = signalName(sig)
	}
	panic(raisedSignal{sig: sig, name: name})
}

// evalRaised resolves the signal named by a raise/handler operand without
// calling it (ERROR Foo[args] names Foo; it does not invoke it).
func (i *Interp) evalRaised(e Expr, env *Env) any {
	switch x := e.(type) {
	case *Ident:
		if v, ok := env.lookup(x.Name); ok {
			return v
		}
		return &Signal{Name: x.Name}
	case *Apply:
		return i.evalRaised(x.Fun, env)
	case *FieldAccess:
		if rec, ok := deref(i.eval(x.X, env)).(*RecordVal); ok {
			if f, ok := rec.Fields[x.Field]; ok {
				return f
			}
		}
		return &Signal{Name: x.Field}
	}
	return i.eval(e, env)
}

// execGuarded runs a statement guarded by "! …" catch clauses.
func (i *Interp) execGuarded(g *Guarded, env *Env) {
	defer func() {
		if r := recover(); r != nil {
			rs, ok := r.(raisedSignal)
			if !ok || !i.handle(rs, g.Handlers, env) {
				panic(r)
			}
		}
	}()
	i.execStmt(g.Stmt, env)
}

// handle runs the first handler whose guard matches the raised signal. A guard
// with no expressions (ANY/UNWIND) matches anything. Returns whether it handled.
func (i *Interp) handle(rs raisedSignal, handlers []Handler, env *Env) bool {
	for _, h := range handlers {
		if len(h.Guards) == 0 {
			i.execStmt(h.Body, env)
			return true
		}
		for _, g := range h.Guards {
			if g == nil { // ANY/UNWIND recorded as a nil guard
				i.execStmt(h.Body, env)
				return true
			}
			gv := i.evalRaised(g, env)
			if gv == rs.sig || signalName(gv) == rs.name {
				i.execStmt(h.Body, env)
				return true
			}
		}
	}
	return false
}

// isOpaque reports whether v is a handle from an unmodeled interface.
func isOpaque(v any) bool {
	_, ok := v.(*Opaque)
	return ok
}

// isTypeName reports whether name denotes a type (a declared type or a base
// type) — used to read the type attributes T.FIRST/T.LAST.
func (i *Interp) isTypeName(name string, env *Env) bool {
	if _, ok := i.types[name]; ok {
		return true
	}
	_, ok := baseTypeAliases[name]
	return ok
}

// signalName returns a printable/matchable name for a raised value.
func signalName(v any) string {
	switch s := v.(type) {
	case *Signal:
		return s.Name
	case *Builtin:
		return s.Name
	case nil:
		return "ERROR"
	}
	return FormatValue(v)
}

func (i *Interp) execVarDecl(st *VarDecl, env *Env) {
	t := i.resolveType(st.Type, env)
	if st.Init != nil {
		v := i.eval(st.Init, env)
		// "X: ERROR = CODE" / "X: SIGNAL = CODE": mint a distinct signal per name.
		if s, ok := v.(*Signal); ok && s.Name == "CODE" {
			for _, n := range st.Names {
				env.define(n, &Signal{Name: n})
			}
			return
		}
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
			if i.matchGuard(subj, g, env) {
				i.execStmt(arm.Body, env)
				return
			}
		}
	}
	if st.Default != nil {
		i.execStmt(st.Default, env)
	}
}

// matchGuard tests one SELECT/WITH arm guard against the subject. A relational
// guard (< x, IN [a..b]) already evaluates to a boolean and is used directly; a
// plain guard is an equality test against the subject. When the subject is
// itself a BOOLEAN, guards are matched by equality (TRUE => / FALSE =>).
func (i *Interp) matchGuard(subj any, g Expr, env *Env) bool {
	v := i.eval(g, env)
	if b, ok := v.(bool); ok {
		if _, subjBool := subj.(bool); !subjBool {
			return b
		}
	}
	return valueEqual(subj, v)
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
				if b.Open { // assigning an unmodeled interface member: record it loosely
					b.Names = append(b.Names, t.Field)
					b.Fields[t.Field] = val
					return
				}
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
		case *Opaque:
			// Writing a field of an opaque interface handle is a no-op.
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
		for _, a := range t.Args {
			i.eval(a, env) // evaluate the subscript(s) for effect regardless
		}
		if base == nil || isOpaque(base) {
			return // index-assign into an opaque/NIL container is a no-op
		}
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
			if len(arr.Elems) == 0 {
				return // an opaque-sized (empty) array: ignore the write
			}
			rerr(t.Line, "array index %d out of bounds", idx)
		}
		arr.Elems[k] = val
	case *Aggregate:
		i.assignExtractor(t, val, env)
	default:
		rerr(0, "invalid assignment target %T", lhs)
	}
}

// assignExtractor implements an extracting assignment "[a, b] ← multiValue" or
// "[x: a, y: b] ← record": each aggregate element receives the matching
// component of val — by field name when the element is named, positionally
// otherwise. A missing element (a skipped "[ , b]" slot) is ignored.
func (i *Interp) assignExtractor(agg *Aggregate, val any, env *Env) {
	get := func(k int, name string) any {
		switch src := deref(val).(type) {
		case *RecordVal:
			if name != "" {
				return src.Fields[name]
			}
			if k < len(src.Names) {
				return src.Fields[src.Names[k]]
			}
		case *ArrayVal:
			if k < len(src.Elems) {
				return src.Elems[k]
			}
		default:
			if k == 0 && name == "" {
				return val // a single value into a one-element extractor
			}
		}
		return nil
	}
	for k, el := range agg.Elems {
		if el.Val == nil {
			continue
		}
		i.assignTo(el.Val, get(k, el.Name), env)
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
		if v, ok := env.lookup(x.Name); ok {
			return v
		}
		if v, ok := env.lookupOpen(x.Name); ok { // an OPEN'd namespace member
			return v
		}
		rerr(x.Line, "undefined identifier %q", x.Name)
	case *Unary:
		return i.evalUnary(x, env)
	case *Binary:
		return i.evalBinary(x, env)
	case *Apply:
		return i.evalApply(x, env)
	case *FieldAccess:
		// Type attributes on a type name: T.FIRST, T.LAST.
		if id, ok := x.X.(*Ident); ok && (x.Field == "FIRST" || x.Field == "LAST") && i.isTypeName(id.Name, env) {
			return i.typeBound(id, env, x.Field == "FIRST")
		}
		base := deref(i.eval(x.X, env)) // a REF to a record auto-dereferences
		if rec, ok := base.(*RecordVal); ok {
			if v, ok := rec.Fields[x.Field]; ok {
				return v // a real field takes precedence over an attribute name
			}
		}
		// Value attributes: arr.FIRST/arr.LAST index bounds; v.ORD/PRED/SUCC on an
		// ordinal value (Cedar's postfix attribute syntax).
		switch x.Field {
		case "FIRST", "LAST":
			if arr, ok := base.(*ArrayVal); ok {
				if x.Field == "FIRST" {
					return arr.Lo
				}
				return arr.Lo + int64(len(arr.Elems)) - 1
			}
		case "ORD":
			if o, ok := ordinalOf(base); ok {
				return o
			}
		case "PRED":
			if _, ok := ordinalOf(base); ok {
				return i.ordStep(base, -1)
			}
		case "SUCC":
			if _, ok := ordinalOf(base); ok {
				return i.ordStep(base, +1)
			}
		}
		switch b := base.(type) {
		case *RecordVal:
			if b.Open { // an unmodeled member of a known interface stays opaque
				return &Opaque{Name: b.TypeName + "." + x.Field}
			}
			rerr(x.Line, "record has no field %q", x.Field)
		case *Opaque: // Pkg.member of an unmodeled interface stays opaque
			return &Opaque{Name: b.Name + "." + x.Field}
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
	case *SelectExpr:
		subj := i.eval(x.Subject, env)
		for _, arm := range x.Arms {
			for _, g := range arm.Guards {
				if i.matchGuard(subj, g, env) {
					return i.eval(arm.Val, env)
				}
			}
		}
		if x.Default != nil {
			return i.eval(x.Default, env)
		}
		return nil
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
		if isOpaque(l) || isOpaque(r) {
			return false // an opaque handle is unordered; comparisons read as false
		}
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
	// An opaque operand (an unmodeled interface value) absorbs arithmetic: the
	// result is opaque too, so a computation threading such a value through still
	// completes rather than aborting.
	if isOpaque(l) || isOpaque(r) {
		return &Opaque{Name: "value"}
	}
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
		case "**":
			return math.Pow(a, b)
		}
	}

	if lIsI && rIsI {
		return intArith(op, li, ri, line)
	}
	// Ordinal operands: CHARACTER, enumeration and BOOLEAN behave as integers
	// (e.g. c + 1 to step a character). A CHARACTER +/- integer stays a CHARACTER.
	if lo, lok := ordinalOf(l); lok {
		if ro, rok := ordinalOf(r); rok {
			res := intArith(op, lo, ro, line)
			if n, ok := res.(int64); ok {
				if _, lc := l.(Char); lc && (op == "+" || op == "-") {
					return Char(n)
				}
				if _, rc := r.(Char); rc && (op == "+" || op == "-") {
					return Char(n)
				}
			}
			return res
		}
	}
	_ = lf
	_ = rf
	rerr(line, "operator %q needs numbers, got %s and %s", op, typeName(l), typeName(r))
	return nil
}

// intArith performs one integer arithmetic operation.
func intArith(op string, a, b int64, line int) any {
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
		if b == 0 {
			rerr(line, "MOD by zero")
		}
		return a % b
	case "**": // exponentiation: integer base to a non-negative integer power
		if b < 0 {
			return math.Pow(float64(a), float64(b))
		}
		result := int64(1)
		for k := int64(0); k < b; k++ {
			result *= a
		}
		return result
	}
	rerr(line, "unknown arithmetic operator %q", op)
	return nil
}

func (i *Interp) evalApply(x *Apply, env *Env) (result any) {
	// A call-site "! handler" clause catches conditions raised by the call.
	if len(x.Catch) > 0 {
		defer func() {
			if r := recover(); r != nil {
				rs, ok := r.(raisedSignal)
				if !ok || !i.handle(rs, x.Catch, env) {
					panic(r)
				}
				result = nil // the handler ran for effect
			}
		}()
	}
	return i.evalApplyInner(x, env)
}

func (i *Interp) evalApplyInner(x *Apply, env *Env) any {
	// Language builtins whose argument is a type name (LAST[INTEGER]) or a value
	// paired with a type (NARROW[x, T]) must be dispatched before evaluating the
	// function or arguments, since the type operand is not an ordinary value.
	if id, ok := x.Fun.(*Ident); ok {
		if v, handled := i.specialApply(id.Name, x.Args, env); handled {
			return v
		}
	}
	fn := deref(i.eval(x.Fun, env)) // a REF to an array/proc auto-dereferences
	if op, ok := fn.(*Opaque); ok {
		// Calling an opaque interface procedure: evaluate the arguments for their
		// effects, then yield an opaque result.
		for _, a := range x.Args {
			i.eval(a, env)
		}
		return &Opaque{Name: op.Name}
	}
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
			if len(f.Elems) == 0 {
				return nil // an opaque-sized (empty) array reads as NIL
			}
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

// CallProc invokes a top-level (usually exported) procedure by name with the
// given argument values, returning its result. This is the entry model for
// library modules, which have no side-effecting main body: run the module to
// elaborate its declarations, then call one of its procedures. Run must be
// called first.
func (i *Interp) CallProc(name string, args ...any) (result any, err error) {
	defer func() {
		if r := recover(); r != nil {
			switch e := r.(type) {
			case runtimeError:
				err = e
			case raisedSignal:
				err = runtimeError{msg: "uncaught " + e.name}
			default:
				panic(r)
			}
		}
	}()
	v, ok := i.global.lookup(name)
	if !ok {
		return nil, fmt.Errorf("no such procedure %q", name)
	}
	cl, ok := deref(v).(*Closure)
	if !ok {
		return nil, fmt.Errorf("%q is not a procedure", name)
	}
	return i.callClosureValues(cl, args), nil
}

// callClosureValues calls a closure with already-evaluated argument values.
func (i *Interp) callClosureValues(c *Closure, args []any) any {
	local := newEnv(c.Env)
	for k, p := range c.Type.Params {
		var v any
		if k < len(args) {
			v = args[k]
		}
		local.define(p.Name, i.coerce(v, i.resolveType(p.Type, c.Env)))
	}
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

// specialApply handles language builtins whose operands are not ordinary
// values: a type name (LAST[INTEGER], BYTES[T]) or a value paired with a type
// (NARROW[x, T], LOOPHOLE[x, T]). It returns handled=false for everything else.
func (i *Interp) specialApply(name string, args []Expr, env *Env) (any, bool) {
	switch name {
	case "LAST", "FIRST":
		if len(args) != 1 {
			return nil, false
		}
		return i.typeBound(args[0], env, name == "FIRST"), true
	case "BYTES", "WORDS", "BITS", "UNITS", "BITSIZE", "BYTESIZE", "WORDSIZE":
		return int64(2), true // a nonzero placeholder storage size
	case "NARROW", "LOOPHOLE":
		// A checked/unchecked cast: yield the value, ignore the target type.
		if len(args) == 0 {
			return nil, true
		}
		return i.eval(args[0], env), true
	case "ISTYPE":
		return true, true // best-effort: assume the runtime type matches
	case "ALL":
		var e any
		if len(args) > 0 {
			e = i.eval(args[0], env)
		}
		return AllVal{Elem: e}, true
	}
	return nil, false
}

// typeBound returns FIRST[T] or LAST[T] for the type named by arg.
func (i *Interp) typeBound(arg Expr, env *Env, first bool) any {
	if id, ok := arg.(*Ident); ok {
		switch id.Name {
		case "INTEGER", "INT", "LONG", "WORD", "UNSPECIFIED", "INT16", "INT32", "WORD16", "WORD32":
			if first {
				return int64(-2147483648)
			}
			return int64(2147483647)
		case "CARDINAL", "CARD", "NAT", "BYTE", "CARD16", "CARD32", "NAT16", "NAT32":
			if first {
				return int64(0)
			}
			return int64(2147483647)
		case "CHARACTER", "CHAR":
			if first {
				return Char(0)
			}
			return Char(0xFFFF)
		case "BOOLEAN", "BOOL":
			return !first // FIRST -> FALSE, LAST -> TRUE
		}
		if d, ok := i.types[id.Name]; ok {
			return typeExtreme(d, first)
		}
	}
	return int64(0)
}

// typeExtreme returns the first or last value of a resolved type.
func typeExtreme(d Type, first bool) any {
	switch t := d.(type) {
	case *EnumTypeDesc:
		if len(t.Members) == 0 {
			return int64(0)
		}
		idx := 0
		if !first {
			idx = len(t.Members) - 1
		}
		return EnumVal{TypeName: t.Name, Ord: idx, Name: t.Members[idx]}
	case *SubrangeTypeDesc:
		if first {
			return t.Lo
		}
		return t.Hi
	}
	return int64(0)
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
		if all, ok := v.(AllVal); ok { // ALL[x] fills every element with x
			out := &ArrayVal{Lo: d.Lo}
			for k := int64(0); k <= d.Hi-d.Lo; k++ {
				out.Elems = append(out.Elems, i.coerce(all.Elem, d.Elem))
			}
			return out
		}
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
	if b, ok := v.(bool); ok {
		return b
	}
	if _, ok := v.(*Opaque); ok {
		return false // an opaque handle reads as NIL/false
	}
	rerr(line, "expected BOOLEAN, got %s", typeName(v))
	return false
}

func toInt(v any, line int) int64 {
	switch n := v.(type) {
	case int64:
		return n
	case Char:
		return int64(n)
	case float64:
		return int64(n)
	case EnumVal:
		return int64(n.Ord)
	case bool:
		if n {
			return 1
		}
		return 0
	case *Opaque:
		return 0
	}
	rerr(line, "expected INTEGER, got %s", typeName(v))
	return 0
}

// ordinalOf returns v as an integer ordinal when it is one of the discrete
// ordinal kinds (INTEGER, CHARACTER, enumeration, BOOLEAN), for mixed-type
// arithmetic like 'c + 1' or 'ORD-free enum stepping.
func ordinalOf(v any) (int64, bool) {
	switch n := v.(type) {
	case int64:
		return n, true
	case Char:
		return int64(n), true
	case EnumVal:
		return int64(n.Ord), true
	case bool:
		if n {
			return 1, true
		}
		return 0, true
	}
	return 0, false
}

func asFloat(v any, line int) float64 {
	switch n := v.(type) {
	case float64:
		return n
	case int64:
		return float64(n)
	case *Opaque:
		return 0
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
	case *Opaque: // an opaque handle is NIL-like: equal to NIL or another opaque
		if b == nil {
			return true
		}
		_, ok := b.(*Opaque)
		return ok
	case nil:
		if b == nil {
			return true
		}
		_, ok := b.(*Opaque)
		return ok
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
