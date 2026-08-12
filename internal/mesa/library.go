package mesa

import (
	"fmt"
	"math"
	"strconv"
	"strings"
)

// installLibraries seeds the interpreter with stub implementations of the core
// Cedar interfaces that non-GUI programs import. Each interface is a RecordVal
// of *Builtin fields, referenced in source as Pkg.Proc[…] (a FieldAccess then
// an Apply). ROPE is modelled as a Go string throughout.
func (i *Interp) installLibraries() {
	i.global.define("IO", i.ioInterface())
	i.global.define("Rope", i.ropeInterface())
	i.global.define("RopeInline", i.ropeInterface())
	i.global.define("Convert", i.convertInterface())
	i.global.define("RealFns", i.realFnsInterface())
	i.global.define("Basics", i.basicsInterface())
	i.global.define("List", i.listInterface())
	i.global.define("Atom", i.atomInterface())
	i.global.define("RefText", i.refTextInterface())
}

// mkIface builds an interface record from a name→builtin table.
func mkIface(name string, procs map[string]func(*Interp, []any) any) *RecordVal {
	r := &RecordVal{TypeName: name, Fields: map[string]any{}}
	for pn, fn := range procs {
		r.Names = append(r.Names, pn)
		r.Fields[pn] = &Builtin{Name: name + "." + pn, Fn: fn}
	}
	return r
}

// ---- string helpers ----

// ropeStr coerces a value to its ROPE (string) form, dereferencing REFs.
func ropeStr(v any) string {
	switch x := deref(v).(type) {
	case nil:
		return ""
	case string:
		return x
	case Char:
		return string(rune(x))
	default:
		return FormatValue(x)
	}
}

// ---- IO ----

func (i *Interp) ioInterface() *RecordVal {
	r := &RecordVal{TypeName: "IO", Fields: map[string]any{}}
	add := func(name string, fn func(*Interp, []any) any) {
		r.Names = append(r.Names, name)
		r.Fields[name] = &Builtin{Name: "IO." + name, Fn: fn}
	}

	// Stream output. The first argument is the stream (ignored except for a
	// rope-capture stream); a leading string is treated as the format instead,
	// so both IO.PutF[s, fmt, …] and format-first convenience calls work.
	add("PutF", func(in *Interp, a []any) any {
		s, format, rest := splitStreamFormat(a)
		writeStream(in, s, putFFormat(format, rest))
		return nil
	})
	add("PutFL", func(in *Interp, a []any) any {
		s, format, rest := splitStreamFormat(a)
		writeStream(in, s, putFFormat(format, flattenList(rest)))
		return nil
	})
	// PutFR / PutFLR format to a ROPE (no stream argument).
	add("PutFR", func(in *Interp, a []any) any {
		if len(a) == 0 {
			return ""
		}
		return putFFormat(ropeStr(a[0]), a[1:])
	})
	add("PutFLR", func(in *Interp, a []any) any {
		if len(a) == 0 {
			return ""
		}
		return putFFormat(ropeStr(a[0]), flattenList(a[1:]))
	})
	putRope := func(in *Interp, a []any) any {
		s, rest := splitStream(a)
		writeStream(in, s, ropeStr(arg0(rest)))
		return nil
	}
	add("PutRope", putRope)
	add("PutText", putRope)
	add("PutString", putRope)
	add("Put", func(in *Interp, a []any) any {
		_, rest := splitStream(a)
		writeStream(in, streamArg(a), joinArgs(rest))
		return nil
	})
	add("PutChar", func(in *Interp, a []any) any {
		s, rest := splitStream(a)
		writeStream(in, s, string(rune(toInt(arg0(rest), 0))))
		return nil
	})
	add("PutInt", func(in *Interp, a []any) any {
		s, rest := splitStream(a)
		writeStream(in, s, strconv.FormatInt(toInt(arg0(rest), 0), 10))
		return nil
	})
	add("PutReal", func(in *Interp, a []any) any {
		s, rest := splitStream(a)
		writeStream(in, s, formatReal(asFloat(arg0(rest), 0)))
		return nil
	})
	add("PutLine", func(in *Interp, a []any) any {
		s, rest := splitStream(a)
		writeStream(in, s, ropeStr(arg0(rest))+"\n")
		return nil
	})
	add("PutBlanks", func(in *Interp, a []any) any {
		s, rest := splitStream(a)
		writeStream(in, s, strings.Repeat(" ", int(toInt(arg0(rest), 0))))
		return nil
	})

	// Value wrappers for PutF: the format letter drives rendering, so these just
	// carry the underlying value through.
	identity := func(in *Interp, a []any) any { return arg0(a) }
	for _, name := range []string{"int", "card", "rope", "text", "real", "char", "bool", "atom", "refAny", "time"} {
		add(name, identity)
	}

	// Rope-capture streams: IO.ROS[] returns a stream whose text is recovered by
	// IO.RopeFromROS[s]. noWhereStream discards.
	add("ROS", func(in *Interp, a []any) any { return &Stream{Buf: &strings.Builder{}} })
	add("CreateOutputStreamToRope", func(in *Interp, a []any) any { return &Stream{Buf: &strings.Builder{}} })
	add("RopeFromROS", func(in *Interp, a []any) any {
		if s, ok := deref(arg0(a)).(*Stream); ok && s.Buf != nil {
			return s.Buf.String()
		}
		return ""
	})
	add("RIS", func(in *Interp, a []any) any { return &Stream{Buf: &strings.Builder{}} })
	r.Names = append(r.Names, "noWhereStream")
	r.Fields["noWhereStream"] = &Stream{Buf: &strings.Builder{}}
	return r
}

// splitStream separates a leading stream argument from the rest. Cedar's IO
// procedures take the stream first (a *Stream, or an opaque/NIL handle from an
// interface we do not model). Our own convenience convention omits it and passes
// values directly. We disambiguate structurally: a leading printable primitive
// (rope, number, char, bool, enum) is a value, not a stream, so PutChar[10] and
// PutF["fmt", …] both work alongside PutF[stream, "fmt", …].
func splitStream(a []any) (stream any, rest []any) {
	if len(a) == 0 {
		return nil, nil
	}
	if isPrintablePrimitive(deref(a[0])) {
		return nil, a
	}
	return a[0], a[1:]
}

func isPrintablePrimitive(v any) bool {
	switch v.(type) {
	case string, int64, float64, Char, bool, EnumVal:
		return true
	}
	return false
}

func streamArg(a []any) any {
	s, _ := splitStream(a)
	return s
}

// splitStreamFormat separates a stream, the format rope, and the value args.
func splitStreamFormat(a []any) (stream any, format string, rest []any) {
	s, tail := splitStream(a)
	if len(tail) == 0 {
		return s, "", nil
	}
	return s, ropeStr(tail[0]), tail[1:]
}

// writeStream sends text to a rope-capture stream, or to the interpreter output.
func writeStream(in *Interp, stream any, text string) {
	if s, ok := deref(stream).(*Stream); ok && s.Buf != nil {
		s.Buf.WriteString(text)
		return
	}
	in.printf("%s", text)
}

// flattenList expands a single trailing LIST argument into its elements, so
// PutFL[s, fmt, LIST[a, b]] behaves like PutF[s, fmt, a, b].
func flattenList(a []any) []any {
	if len(a) == 1 {
		if c, ok := deref(a[0]).(*Cons); ok {
			var out []any
			for ; c != nil; c = c.Rest {
				out = append(out, c.First)
			}
			return out
		}
	}
	return a
}

// putFFormat renders a Cedar IO.PutF format string. Directives consume
// successive arguments: %d/%g/%r/%s generic, %b bool, %c char, %f/%e real,
// %x hex, %o octal; an optional width/precision between % and the letter is
// accepted and ignored. %% is a literal percent.
func putFFormat(format string, args []any) string {
	var sb strings.Builder
	ai := 0
	next := func() any {
		if ai < len(args) {
			v := args[ai]
			ai++
			return v
		}
		return ""
	}
	rs := []rune(format)
	for k := 0; k < len(rs); k++ {
		if rs[k] != '%' {
			sb.WriteRune(rs[k])
			continue
		}
		k++
		// skip flags / width / precision
		for k < len(rs) && (rs[k] == '-' || rs[k] == '.' || rs[k] == '*' || (rs[k] >= '0' && rs[k] <= '9')) {
			k++
		}
		if k >= len(rs) {
			sb.WriteByte('%')
			break
		}
		switch rs[k] {
		case '%':
			sb.WriteByte('%')
		case 'c':
			if c, ok := deref(next()).(Char); ok {
				sb.WriteRune(rune(c))
			}
		case 'f', 'e':
			sb.WriteString(formatReal(asFloat(deref(next()), 0)))
		case 'x':
			sb.WriteString(fmt.Sprintf("%X", toInt(deref(next()), 0)))
		case 'o':
			sb.WriteString(fmt.Sprintf("%o", toInt(deref(next()), 0)))
		default: // d, g, r, s, b and anything else: generic value formatting
			sb.WriteString(FormatValue(deref(next())))
		}
	}
	return sb.String()
}

// ---- Rope ----

func (i *Interp) ropeInterface() *RecordVal {
	r := &RecordVal{TypeName: "Rope", Fields: map[string]any{}}
	add := func(name string, fn func(*Interp, []any) any) {
		r.Names = append(r.Names, name)
		r.Fields[name] = &Builtin{Name: "Rope." + name, Fn: fn}
	}
	add("Cat", func(in *Interp, a []any) any {
		var sb strings.Builder
		for _, v := range a {
			sb.WriteString(ropeStr(v))
		}
		return sb.String()
	})
	add("Concat", func(in *Interp, a []any) any { return ropeStr(arg0(a)) + ropeStr(arg1(a)) })
	add("Length", func(in *Interp, a []any) any { return int64(len([]rune(ropeStr(arg0(a))))) })
	add("Size", func(in *Interp, a []any) any { return int64(len([]rune(ropeStr(arg0(a))))) })
	add("IsEmpty", func(in *Interp, a []any) any { return ropeStr(arg0(a)) == "" })
	add("Fetch", func(in *Interp, a []any) any {
		rs := []rune(ropeStr(arg0(a)))
		idx := int(toInt(arg1(a), 0))
		if idx < 0 || idx >= len(rs) {
			rerr(0, "Rope.Fetch index out of range")
		}
		return Char(rs[idx])
	})
	add("Substr", func(in *Interp, a []any) any {
		rs := []rune(ropeStr(arg0(a)))
		start := int(toInt(arg1(a), 0))
		length := len(rs) - start
		if len(a) >= 3 {
			length = int(toInt(a[2], 0))
		}
		if start < 0 {
			start = 0
		}
		if start > len(rs) {
			start = len(rs)
		}
		end := start + length
		if end > len(rs) {
			end = len(rs)
		}
		if end < start {
			end = start
		}
		return string(rs[start:end])
	})
	add("Flatten", func(in *Interp, a []any) any { return ropeStr(arg0(a)) })
	add("Equal", func(in *Interp, a []any) any {
		s1, s2 := ropeStr(arg0(a)), ropeStr(arg1(a))
		if len(a) >= 3 && !toBool(a[2], 0) { // caseSensitive FALSE
			return strings.EqualFold(s1, s2)
		}
		return s1 == s2
	})
	add("Compare", func(in *Interp, a []any) any {
		s1, s2 := ropeStr(arg0(a)), ropeStr(arg1(a))
		if len(a) >= 3 && !toBool(a[2], 0) {
			s1, s2 = strings.ToLower(s1), strings.ToLower(s2)
		}
		return int64(strings.Compare(s1, s2))
	})
	find := func(in *Interp, a []any) any {
		base := ropeStr(arg0(a))
		sub := ropeStr(arg1(a))
		start := 0
		if len(a) >= 3 {
			start = int(toInt(a[2], 0))
		}
		if start < 0 || start > len(base) {
			return int64(-1)
		}
		if idx := strings.Index(base[start:], sub); idx >= 0 {
			return int64(start + idx)
		}
		return int64(-1)
	}
	add("Find", find)
	add("Index", func(in *Interp, a []any) any {
		// Rope.Index[s1, index1, s2]: search s1 (from index1) for s2, returning
		// the position, or Length[s1] when absent.
		base := ropeStr(arg0(a))
		start := int(toInt(arg1(a), 0))
		sub := ropeStr(arg2(a))
		if start < 0 || start > len(base) {
			return int64(len([]rune(base)))
		}
		if idx := strings.Index(base[start:], sub); idx >= 0 {
			return int64(start + idx)
		}
		return int64(len([]rune(base)))
	})
	add("FromChar", func(in *Interp, a []any) any { return string(rune(toInt(arg0(a), 0))) })
	add("FromRefText", func(in *Interp, a []any) any { return ropeStr(arg0(a)) })
	add("ToRefText", func(in *Interp, a []any) any { return ropeStr(arg0(a)) })
	add("Lower", func(in *Interp, a []any) any { return strings.ToLower(ropeStr(arg0(a))) })
	add("Upper", func(in *Interp, a []any) any { return strings.ToUpper(ropeStr(arg0(a))) })
	return r
}

// ---- Convert ----

func (i *Interp) convertInterface() *RecordVal {
	r := &RecordVal{TypeName: "Convert", Fields: map[string]any{}}
	add := func(name string, fn func(*Interp, []any) any) {
		r.Names = append(r.Names, name)
		r.Fields[name] = &Builtin{Name: "Convert." + name, Fn: fn}
	}
	fromInt := func(in *Interp, a []any) any { return strconv.FormatInt(toInt(arg0(a), 0), 10) }
	add("RopeFromInt", fromInt)
	add("RopeFromCard", fromInt)
	add("RopeFromLongCard", fromInt)
	add("RopeFromReal", func(in *Interp, a []any) any { return formatReal(asFloat(arg0(a), 0)) })
	add("RopeFromChar", func(in *Interp, a []any) any { return string(rune(toInt(arg0(a), 0))) })
	add("RopeFromBool", func(in *Interp, a []any) any {
		if toBool(arg0(a), 0) {
			return "TRUE"
		}
		return "FALSE"
	})
	add("IntFromRope", func(in *Interp, a []any) any {
		n, err := strconv.ParseInt(strings.TrimSpace(ropeStr(arg0(a))), 10, 64)
		if err != nil {
			rerr(0, "Convert.IntFromRope: not a number: %q", ropeStr(arg0(a)))
		}
		return n
	})
	add("CardFromRope", func(in *Interp, a []any) any {
		n, _ := strconv.ParseInt(strings.TrimSpace(ropeStr(arg0(a))), 10, 64)
		return n
	})
	add("RealFromRope", func(in *Interp, a []any) any {
		f, err := strconv.ParseFloat(strings.TrimSpace(ropeStr(arg0(a))), 64)
		if err != nil {
			rerr(0, "Convert.RealFromRope: not a number: %q", ropeStr(arg0(a)))
		}
		return f
	})
	return r
}

// ---- RealFns (floating-point math) ----

func (i *Interp) realFnsInterface() *RecordVal {
	f1 := func(g func(float64) float64) func(*Interp, []any) any {
		return func(in *Interp, a []any) any { return g(asFloat(deref(arg0(a)), 0)) }
	}
	return mkIface("RealFns", map[string]func(*Interp, []any) any{
		"Sqrt":   f1(math.Sqrt),
		"SqRt":   f1(math.Sqrt),
		"Sin":    f1(math.Sin),
		"Cos":    f1(math.Cos),
		"Tan":    f1(math.Tan),
		"ArcTan": f1(math.Atan),
		"Exp":    f1(math.Exp),
		"Ln":     f1(math.Log),
		"Log":    f1(math.Log10),
		"ArcTans": func(in *Interp, a []any) any {
			return math.Atan2(asFloat(deref(arg0(a)), 0), asFloat(deref(arg1(a)), 0))
		},
		"Power": func(in *Interp, a []any) any {
			return math.Pow(asFloat(deref(arg0(a)), 0), asFloat(deref(arg1(a)), 0))
		},
	})
}

// ---- Basics (bit and word operations) ----

func (i *Interp) basicsInterface() *RecordVal {
	return mkIface("Basics", map[string]func(*Interp, []any) any{
		"BITAND":    func(in *Interp, a []any) any { return toInt(arg0(a), 0) & toInt(arg1(a), 0) },
		"BITOR":     func(in *Interp, a []any) any { return toInt(arg0(a), 0) | toInt(arg1(a), 0) },
		"BITXOR":    func(in *Interp, a []any) any { return toInt(arg0(a), 0) ^ toInt(arg1(a), 0) },
		"BITNOT":    func(in *Interp, a []any) any { return ^toInt(arg0(a), 0) },
		"BITLSHIFT": func(in *Interp, a []any) any { return toInt(arg0(a), 0) << uint(toInt(arg1(a), 0)) },
		"BITRSHIFT": func(in *Interp, a []any) any { return toInt(arg0(a), 0) >> uint(toInt(arg1(a), 0)) },
		"BitAnd":    func(in *Interp, a []any) any { return toInt(arg0(a), 0) & toInt(arg1(a), 0) },
		"BitOr":     func(in *Interp, a []any) any { return toInt(arg0(a), 0) | toInt(arg1(a), 0) },
		"BitXor":    func(in *Interp, a []any) any { return toInt(arg0(a), 0) ^ toInt(arg1(a), 0) },
		"LongMult":  func(in *Interp, a []any) any { return toInt(arg0(a), 0) * toInt(arg1(a), 0) },
		"LowHalf":   func(in *Interp, a []any) any { return toInt(arg0(a), 0) & 0xFFFF },
		"HighHalf":  func(in *Interp, a []any) any { return (toInt(arg0(a), 0) >> 16) & 0xFFFF },
	})
}

// ---- List (operations on LIST OF T / *Cons) ----

func (i *Interp) listInterface() *RecordVal {
	toCons := func(v any) *Cons {
		c, _ := deref(v).(*Cons)
		return c
	}
	return mkIface("List", map[string]func(*Interp, []any) any{
		"Length": func(in *Interp, a []any) any { return lengthOf(arg0(a)) },
		"Cons": func(in *Interp, a []any) any {
			return &Cons{First: arg0(a), Rest: toCons(arg1(a))}
		},
		"Nth": func(in *Interp, a []any) any {
			c := toCons(arg0(a))
			n := int(toInt(arg1(a), 0))
			for ; c != nil && n > 1; n-- { // Cedar List.Nth is 1-based
				c = c.Rest
			}
			if c == nil {
				return nil
			}
			return c.First
		},
		"Reverse": func(in *Interp, a []any) any {
			var out *Cons
			for c := toCons(arg0(a)); c != nil; c = c.Rest {
				out = &Cons{First: c.First, Rest: out}
			}
			if out == nil {
				return nil
			}
			return out
		},
		"Append": func(in *Interp, a []any) any {
			var elems []any
			for c := toCons(arg0(a)); c != nil; c = c.Rest {
				elems = append(elems, c.First)
			}
			tail := toCons(arg1(a))
			out := tail
			for k := len(elems) - 1; k >= 0; k-- {
				out = &Cons{First: elems[k], Rest: out}
			}
			if out == nil {
				return nil
			}
			return out
		},
		"Member": func(in *Interp, a []any) any {
			target := arg0(a)
			for c := toCons(arg1(a)); c != nil; c = c.Rest {
				if valueEqual(c.First, target) {
					return true
				}
			}
			return false
		},
	})
}

// ---- Atom ----

func (i *Interp) atomInterface() *RecordVal {
	return mkIface("Atom", map[string]func(*Interp, []any) any{
		// Atoms are modelled by their print name (a rope); MakeAtom/GetPName round-trip.
		"MakeAtom": func(in *Interp, a []any) any { return ropeStr(arg0(a)) },
		"GetPName": func(in *Interp, a []any) any { return ropeStr(arg0(a)) },
		"GetProp":  func(in *Interp, a []any) any { return nil },
		"PutProp":  func(in *Interp, a []any) any { return nil },
	})
}

// ---- RefText (mutable text buffers, modelled as ropes) ----

func (i *Interp) refTextInterface() *RecordVal {
	return mkIface("RefText", map[string]func(*Interp, []any) any{
		"New":             func(in *Interp, a []any) any { return "" },
		"ObtainScratch":   func(in *Interp, a []any) any { return "" },
		"ReleaseScratch":  func(in *Interp, a []any) any { return nil },
		"TrustTextAsRope": func(in *Interp, a []any) any { return ropeStr(arg0(a)) },
		"AppendRope":      func(in *Interp, a []any) any { return ropeStr(arg0(a)) + ropeStr(arg1(a)) },
		"AppendChar":      func(in *Interp, a []any) any { return ropeStr(arg0(a)) + string(rune(toInt(arg1(a), 0))) },
	})
}

func arg1(a []any) any {
	if len(a) < 2 {
		return nil
	}
	return a[1]
}

func arg2(a []any) any {
	if len(a) < 3 {
		return nil
	}
	return a[2]
}
