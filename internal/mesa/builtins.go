package mesa

import (
	"fmt"
	"math"
	"strings"
)

// installBuiltins seeds the global environment with predefined constants
// and a small runtime library. Real Xerox Mesa/Cedar programs reach the
// terminal through the IO interface (IO.PutRope, IO.PutInt, ...); we
// provide a faithful-looking subset plus a few convenience globals.
func (i *Interp) installBuiltins() {
	g := i.global

	// Predefined constants
	g.define("TRUE", true)
	g.define("FALSE", false)
	g.define("NIL", nil)
	// CODE seeds an ERROR/SIGNAL declaration ("X: ERROR = CODE"). Each such
	// binding wants a distinct code; execVarDecl mints a fresh one, so this global
	// only needs to be a recognisable, non-undefined placeholder.
	g.define("CODE", &Signal{Name: "CODE"})

	// PRED/SUCC step an enumeration or ordinal value.
	g.define("PRED", &Builtin{Name: "PRED", Fn: func(in *Interp, a []any) any { return in.ordStep(arg0(a), -1) }})
	g.define("SUCC", &Builtin{Name: "SUCC", Fn: func(in *Interp, a []any) any { return in.ordStep(arg0(a), +1) }})

	bi := func(name string, fn func(*Interp, []any) any) *Builtin {
		return &Builtin{Name: name, Fn: fn}
	}
	def := func(name string, fn func(*Interp, []any) any) {
		g.define(name, bi(name, fn))
	}

	// ---- Output helpers (global convenience procedures) ----
	putln := func(in *Interp, args []any) any {
		in.printf("%s\n", joinArgs(args))
		return nil
	}
	put := func(in *Interp, args []any) any {
		in.printf("%s", joinArgs(args))
		return nil
	}
	def("WriteLine", putln)
	def("WriteString", put)
	def("Write", put)
	def("WriteInt", func(in *Interp, a []any) any { in.printf("%d", toInt(arg0(a), 0)); return nil })
	def("WriteReal", func(in *Interp, a []any) any { in.printf("%s", formatReal(asFloat(arg0(a), 0))); return nil })
	def("WriteChar", func(in *Interp, a []any) any { in.printf("%c", rune(toInt(arg0(a), 0))); return nil })
	def("WriteBool", func(in *Interp, a []any) any { in.printf("%s", FormatValue(arg0(a))); return nil })
	def("WriteLn", func(in *Interp, a []any) any { in.printf("\n"); return nil })
	def("NewLine", func(in *Interp, a []any) any { in.printf("\n"); return nil })

	// ---- Numeric / conversion ----
	def("ABS", func(in *Interp, a []any) any {
		switch n := arg0(a).(type) {
		case int64:
			if n < 0 {
				return -n
			}
			return n
		case float64:
			return math.Abs(n)
		}
		rerr(0, "ABS needs a number")
		return nil
	})
	def("MAX", func(in *Interp, a []any) any { return reduce(a, true) })
	def("MIN", func(in *Interp, a []any) any { return reduce(a, false) })
	def("Real", func(in *Interp, a []any) any { return asFloat(arg0(a), 0) })
	def("Float", func(in *Interp, a []any) any { return asFloat(arg0(a), 0) })
	def("Round", func(in *Interp, a []any) any { return int64(math.Round(asFloat(arg0(a), 0))) })
	def("Floor", func(in *Interp, a []any) any { return int64(math.Floor(asFloat(arg0(a), 0))) })
	def("Trunc", func(in *Interp, a []any) any { return int64(asFloat(arg0(a), 0)) })
	def("Sqrt", func(in *Interp, a []any) any { return math.Sqrt(asFloat(arg0(a), 0)) })

	// ---- Characters / enumerations ----
	def("ORD", func(in *Interp, a []any) any {
		switch v := arg0(a).(type) {
		case Char:
			return int64(v)
		case EnumVal:
			return int64(v.Ord)
		case int64:
			return v
		}
		rerr(0, "ORD needs a CHARACTER or enumeration")
		return nil
	})
	def("VAL", func(in *Interp, a []any) any { return Char(rune(toInt(arg0(a), 0))) })
	def("CHAR", func(in *Interp, a []any) any { return Char(rune(toInt(arg0(a), 0))) })

	// Numeric type names double as conversion functions (CARDINAL[x], INT[x]) and
	// are otherwise harmless as bare values; seed them so they are not "undefined".
	toIntCast := func(in *Interp, a []any) any { return toInt(arg0(a), 0) }
	for _, n := range []string{"INTEGER", "CARDINAL", "INT", "CARD", "NAT", "LONG",
		"WORD", "BYTE", "INT16", "INT32", "CARD16", "CARD32", "NAT16", "NAT32"} {
		def(n, toIntCast)
	}
	def("REAL", func(in *Interp, a []any) any { return asFloat(arg0(a), 0) })
	def("LONGREAL", func(in *Interp, a []any) any { return asFloat(arg0(a), 0) })

	// ---- Length of STRING, ARRAY or LIST ----
	def("LENGTH", func(in *Interp, a []any) any { return lengthOf(arg0(a)) })
	def("Length", func(in *Interp, a []any) any { return lengthOf(arg0(a)) })
	def("SIZE", func(in *Interp, a []any) any { return lengthOf(arg0(a)) })

	// ---- LIST OF T constructors: LIST[a, b, …] and CONS[head, tail] ----
	def("LIST", func(in *Interp, a []any) any {
		var head *Cons
		for k := len(a) - 1; k >= 0; k-- {
			head = &Cons{First: a[k], Rest: head}
		}
		if head == nil {
			return nil // an empty list is NIL
		}
		return head
	})
	def("CONS", func(in *Interp, a []any) any {
		var rest *Cons
		if len(a) >= 2 {
			if c, ok := deref(a[1]).(*Cons); ok {
				rest = c
			}
		}
		return &Cons{First: arg0(a), Rest: rest}
	})

	// The core Cedar interfaces (IO, Rope, Convert, …) are seeded separately.
	i.installLibraries()
}

func (i *Interp) printf(format string, a ...any) {
	fmt.Fprintf(i.out, format, a...)
}

// ordStep advances an ordinal value (integer, character or enumeration) by
// delta, used by SUCC (+1) and PRED (-1).
func (i *Interp) ordStep(v any, delta int) any {
	switch x := deref(v).(type) {
	case int64:
		return x + int64(delta)
	case Char:
		return Char(int(x) + delta)
	case EnumVal:
		nord := x.Ord + delta
		name := x.Name
		if d, ok := i.types[x.TypeName].(*EnumTypeDesc); ok && nord >= 0 && nord < len(d.Members) {
			name = d.Members[nord]
		}
		return EnumVal{TypeName: x.TypeName, Ord: nord, Name: name}
	}
	rerr(0, "PRED/SUCC needs an ordinal value")
	return nil
}

func arg0(a []any) any {
	if len(a) == 0 {
		return nil
	}
	return a[0]
}

func joinArgs(a []any) string {
	parts := make([]string, len(a))
	for k, v := range a {
		parts[k] = FormatValue(v)
	}
	return strings.Join(parts, "")
}

func lengthOf(v any) int64 {
	switch x := deref(v).(type) {
	case string:
		return int64(len([]rune(x)))
	case *ArrayVal:
		return int64(len(x.Elems))
	case *Cons:
		var n int64
		for c := x; c != nil; c = c.Rest {
			n++
		}
		return n
	case nil:
		return 0 // an empty list / NIL has length 0
	case *Opaque:
		return 0 // length of an opaque handle
	}
	rerr(0, "LENGTH needs a STRING, ARRAY or LIST")
	return 0
}

func reduce(a []any, max bool) any {
	if len(a) == 0 {
		rerr(0, "MAX/MIN needs at least one argument")
	}
	best := a[0]
	for _, v := range a[1:] {
		c, ok := valueCompare(v, best)
		if !ok {
			rerr(0, "MAX/MIN arguments are not comparable")
		}
		if (max && c > 0) || (!max && c < 0) {
			best = v
		}
	}
	return best
}
