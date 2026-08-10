// Package mesa is a small tree-walking interpreter for a practical subset of
// Xerox Mesa. It is vendored from the standalone mesa-g project (package main
// there) as an importable library: the only change is the package name.
//
// The public entry points are ParseSource, which parses source into a *Module,
// and NewInterp(io.Writer).Run, which evaluates a module and writes its output
// to the given writer. The interpreter performs no I/O of its own beyond that
// writer, so it is safe to run in-process.
package mesa
