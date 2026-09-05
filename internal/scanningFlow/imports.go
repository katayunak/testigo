package scanningFlow

import "golang.org/x/tools/go/packages"

func importClosure(pkgs []*packages.Package) map[string]map[string]bool {

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
