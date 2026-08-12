package mesa

import "strings"

// The interpreter is inherently memory-safe: it walks the AST over ordinary Go
// values, so there is no raw memory, no pointer arithmetic, and no way for a
// program to escape the host. Unsafe Cedar constructs are therefore neutralised
// rather than dangerous — LOOPHOLE is an identity cast, address-of yields the
// value, UNCOUNTED/UNSAFE annotations are dropped. What matters for a faithful
// "safe subset" is to *report* when a program relies on such features (its
// behaviour here may differ from real Cedar) or on a low-level runtime we do not
// model, instead of pretending the run was a faithful execution.

// unsafeFeatures maps an unsafe language token to a human description.
var unsafeFeatures = map[string]string{
	"LOOPHOLE":  "LOOPHOLE (unchecked type cast)",
	"UNCOUNTED": "UNCOUNTED references",
	"UNSAFE":    "UNSAFE code",
}

// unsafeInterfaces are low-level interfaces whose semantics (raw memory, the
// processor, the disk) the safe interpreter cannot reproduce.
var unsafeInterfaces = map[string]string{
	"UnsafeStorage": "UnsafeStorage", "PrincOps": "PrincOps",
	"PrincOpsUtils": "PrincOpsUtils", "VM": "VM", "Space": "Space",
	"DiskFace": "DiskFace", "ProcessorFace": "ProcessorFace",
	"PhysicalVolume": "PhysicalVolume", "Frame": "Frame",
}

// Safety describes how far a module sits outside the safe runnable subset.
type Safety struct {
	Features   []string // unsafe language features used (LOOPHOLE, …)
	Interfaces []string // low-level interfaces imported (VM, PrincOps, …)
}

// Safe reports whether the module stays within the safe subset.
func (s Safety) Safe() bool { return len(s.Features) == 0 && len(s.Interfaces) == 0 }

// Note returns a one-line advisory for an unsafe module, or "" when it is safe.
func (s Safety) Note() string {
	if s.Safe() {
		return ""
	}
	var parts []string
	parts = append(parts, s.Features...)
	for _, in := range s.Interfaces {
		parts = append(parts, in+" (low-level interface)")
	}
	return "note: relies on " + strings.Join(parts, ", ") +
		" — run in the safe interpreter, so behaviour may differ from real Cedar"
}

// AnalyzeSafety scans decoded source for unsafe language features and inspects a
// module's imports (m may be nil) for low-level interfaces.
func AnalyzeSafety(src string, m *Module) Safety {
	var s Safety
	seen := map[string]bool{}
	if toks, err := NewLexer(src).Tokenize(); err == nil {
		for _, t := range toks {
			if t.Kind != TIdent && t.Kind != TKeyword {
				continue
			}
			if desc, ok := unsafeFeatures[t.Text]; ok && !seen[t.Text] {
				seen[t.Text] = true
				s.Features = append(s.Features, desc)
			}
		}
	}
	if m != nil {
		for _, imp := range m.Imports {
			if name, ok := unsafeInterfaces[imp]; ok && !seen[imp] {
				seen[imp] = true
				s.Interfaces = append(s.Interfaces, name)
			}
		}
	}
	return s
}
