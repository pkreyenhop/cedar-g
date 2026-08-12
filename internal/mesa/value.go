package mesa

import (
	"fmt"
	"strings"
)

// Values are represented with plain Go types where possible and small
// wrapper structs otherwise. The interpreter is dynamically typed at
// runtime; declared types drive default initialisation, enum literal
// resolution and loop ranges.
//
//	INTEGER / CARDINAL      -> int64
//	REAL                    -> float64
//	BOOLEAN                 -> bool
//	CHARACTER               -> Char
//	STRING                  -> string
//	enumeration             -> EnumVal
//	ARRAY                   -> *ArrayVal
//	RECORD                  -> *RecordVal
//	PROCEDURE               -> *Closure
//	NIL                     -> nil

// Char is CHARACTER, kept distinct from int64 so printing shows glyphs.
type Char rune

// EnumVal is one member of an enumeration type.
type EnumVal struct {
	TypeName string
	Ord      int
	Name     string
}

// ArrayVal is a 1-D array with an arbitrary lower bound (Mesa arrays are
// indexed by an interval, often [0..n)).
type ArrayVal struct {
	Lo    int64
	Elems []any
}

func (a *ArrayVal) index(i int64) (int, bool) {
	idx := int(i - a.Lo)
	if idx < 0 || idx >= len(a.Elems) {
		return 0, false
	}
	return idx, true
}

// RecordVal is a record; field order is preserved for constructors and
// printing.
type RecordVal struct {
	TypeName string
	Names    []string
	Fields   map[string]any
}

func (r *RecordVal) clone() *RecordVal {
	nf := make(map[string]any, len(r.Fields))
	for k, v := range r.Fields {
		nf[k] = v
	}
	names := append([]string(nil), r.Names...)
	return &RecordVal{TypeName: r.TypeName, Names: names, Fields: nf}
}

// Closure is a procedure value bound to its defining environment.
type Closure struct {
	Name string
	Type *ProcType
	Body *Block
	Env  *Env
}

// Builtin is a host-implemented procedure.
type Builtin struct {
	Name string
	Fn   func(i *Interp, args []any) any
}

// ---- Type descriptors (resolved from TypeExpr) ----

// Type is a resolved, named-or-structural type used for defaults and
// enum resolution.
type Type interface{ typeDesc() }

type BaseType struct{ Name string } // INTEGER, REAL, BOOLEAN, CHARACTER, STRING
type EnumTypeDesc struct {
	Name    string
	Members []string
}
type RecordTypeDesc struct {
	Name   string
	Fields []FieldDesc
}
type FieldDesc struct {
	Name string
	Type Type
}
type ArrayTypeDesc struct {
	Lo, Hi int64 // inclusive bounds
	Elem   Type
}
type SubrangeTypeDesc struct {
	Base   *BaseType
	Lo, Hi int64
}
type ProcTypeDesc struct{ PT *ProcType }

// OpaqueType is a reference or collection type the interpreter treats as an
// opaque handle: REF/POINTER/LIST/SEQUENCE/DESCRIPTOR/ATOM/ANY and imported
// types from interfaces we do not model (e.g. IO.STREAM, ViewerClasses.Viewer).
// Its default value is NIL; values flow through unchanged. Elem, when set,
// records the referent/element type so NEW and dereference can build one.
type OpaqueType struct {
	Name string
	Elem Type
}

func (*BaseType) typeDesc()         {}
func (*EnumTypeDesc) typeDesc()     {}
func (*RecordTypeDesc) typeDesc()   {}
func (*ArrayTypeDesc) typeDesc()    {}
func (*SubrangeTypeDesc) typeDesc() {}
func (*ProcTypeDesc) typeDesc()     {}
func (*OpaqueType) typeDesc()       {}

// FormatValue renders a runtime value the way the sample programs expect
// to see it printed.
func FormatValue(v any) string {
	switch x := v.(type) {
	case nil:
		return "NIL"
	case int64:
		return fmt.Sprintf("%d", x)
	case float64:
		return formatReal(x)
	case bool:
		if x {
			return "TRUE"
		}
		return "FALSE"
	case Char:
		return string(rune(x))
	case string:
		return x
	case EnumVal:
		return x.Name
	case *ArrayVal:
		parts := make([]string, len(x.Elems))
		for i, e := range x.Elems {
			parts[i] = FormatValue(e)
		}
		return "[" + strings.Join(parts, ", ") + "]"
	case *RecordVal:
		parts := make([]string, 0, len(x.Names))
		for _, n := range x.Names {
			parts = append(parts, n+": "+FormatValue(x.Fields[n]))
		}
		return "[" + strings.Join(parts, ", ") + "]"
	case *Closure:
		return "PROCEDURE " + x.Name
	case *Builtin:
		return "PROCEDURE " + x.Name
	default:
		return fmt.Sprintf("%v", x)
	}
}

func formatReal(f float64) string {
	// Trim to a tidy representation; keep a trailing point-zero for whole
	// values so REALs read differently from INTEGERs.
	s := fmt.Sprintf("%g", f)
	if !strings.ContainsAny(s, ".eE") {
		s += ".0"
	}
	return s
}
