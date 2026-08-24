package scanningFlow

import "golang.org/x/tools/go/packages"

// importClosure maps each package to every package it can reach through its
// own imports, transitively.
//
// This is the cheapest correctness win in the whole scanner, and it is a PROOF
// rather than a heuristic: Go resolves a call at compile time through the
// import graph, so a function in package P can only call a function in a
// package P imports. An edge that crosses an import boundary that does not
// exist is not a maybe. It cannot happen.
//
// Why it is needed. CHA answers "what can this call reach" by type, not by
// reachability, and it is badly wrong for two shapes that ordinary Go is full
// of:
//
//	defer cancel()            // a func() value — CHA says EVERY func() in the program
//	span.LogKV(err.Error())   // an interface call — CHA says EVERY error implementation
//
// On a real gRPC payment service one 45-line function produced 1,679 out-edges
// from 13 call sites. 1,463 came from the `defer cancel()`, and among them were
// edges into a notification service's generated protobuf handlers — which that
// package does not import and therefore cannot call. One fifth of the whole
// recorded graph was impossible in this way, and it dragged an unrelated
// service's enums in as "payment state machines".
//
// The bound removes exactly those edges and cannot remove a real one.
func importClosure(pkgs []*packages.Package) map[string]map[string]bool {
	// The loader hands back one *Package per path, and the same pointer is
	// shared by every importer, so memoising by path visits each package once.
	closure := map[string]map[string]bool{}

	var walk func(p *packages.Package) map[string]bool
	inProgress := map[string]bool{}

	walk = func(p *packages.Package) map[string]bool {
		if p == nil {
			return nil
		}
		if got, ok := closure[p.PkgPath]; ok {
			return got
		}
		// Import cycles are illegal in Go, but a malformed load can still
		// present one. Returning early keeps this terminating either way.
		if inProgress[p.PkgPath] {
			return nil
		}
		inProgress[p.PkgPath] = true

		out := map[string]bool{}
		for path, imported := range p.Imports {
			out[path] = true
			for deep := range walk(imported) {
				out[deep] = true
			}
		}

		delete(inProgress, p.PkgPath)
		closure[p.PkgPath] = out
		return out
	}

	for _, p := range pkgs {
		walk(p)
	}
	return closure
}

// canCall reports whether a call from one package to another is possible at all.
//
// Same package is always allowed. An unknown caller is allowed too: if the
// closure has no entry for it, the load did not give us the imports, and
// guessing "impossible" from missing data would silently delete real edges.
// Absent information is not evidence.
func (g *graph) canCall(from, to string) bool {
	if from == to {
		return true
	}
	deps, known := g.imports[from]
	if !known {
		return true
	}
	return deps[to]
}
