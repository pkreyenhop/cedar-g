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

	// ---- Length of STRING or ARRAY ----
	def("LENGTH", func(in *Interp, a []any) any { return lengthOf(arg0(a)) })
	def("Length", func(in *Interp, a []any) any { return lengthOf(arg0(a)) })
	def("SIZE", func(in *Interp, a []any) any { return lengthOf(arg0(a)) })

	// ---- The IO interface record (IO.PutRope, IO.PutInt, ...) ----
	io := &RecordVal{TypeName: "IO", Fields: map[string]any{}}
	addIO := func(name string, fn func(*Interp, []any) any) {
		io.Names = append(io.Names, name)
		io.Fields[name] = bi("IO."+name, fn)
	}
	addIO("PutRope", put)
	addIO("PutText", put)
	addIO("PutString", put)
	addIO("PutLine", putln)
	addIO("PutInt", func(in *Interp, a []any) any { in.printf("%d", toInt(arg0(a), 0)); return nil })
	addIO("PutReal", func(in *Interp, a []any) any { in.printf("%s", formatReal(asFloat(arg0(a), 0))); return nil })
	addIO("PutChar", func(in *Interp, a []any) any { in.printf("%c", rune(toInt(arg0(a), 0))); return nil })
	addIO("PutF", func(in *Interp, a []any) any { in.printf("%s", mesaFormat(a)); return nil })
	g.define("IO", io)
}

func (i *Interp) printf(format string, a ...any) {
	fmt.Fprintf(i.out, format, a...)
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
	switch x := v.(type) {
	case string:
		return int64(len([]rune(x)))
	case *ArrayVal:
		return int64(len(x.Elems))
	}
	rerr(0, "LENGTH needs a STRING or ARRAY")
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

// mesaFormat implements a tiny subset of IO.PutF's format string: %g / %d
// / %r / %c consume successive arguments; everything else is literal.
func mesaFormat(a []any) string {
	if len(a) == 0 {
		return ""
	}
	format, ok := a[0].(string)
	if !ok {
		return joinArgs(a)
	}
	rest := a[1:]
	var sb strings.Builder
	ai := 0
	nextArg := func() any {
		if ai < len(rest) {
			v := rest[ai]
			ai++
			return v
		}
		return ""
	}
	runes := []rune(format)
	for k := 0; k < len(runes); k++ {
		c := runes[k]
		if c == '%' && k+1 < len(runes) {
			k++
			switch runes[k] {
			case 'd', 'g', 'r', 'c', 's':
				sb.WriteString(FormatValue(nextArg()))
			case '%':
				sb.WriteByte('%')
			default:
				sb.WriteByte('%')
				sb.WriteRune(runes[k])
			}
			continue
		}
		sb.WriteRune(c)
	}
	return sb.String()
}
