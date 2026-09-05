package scanningFlow

import (
	"go/ast"
	"regexp"

	"golang.org/x/tools/go/packages"
)

var generatedMarker = regexp.MustCompile(`^// Code generated .* DO NOT EDIT\.$`)

func generatedFiles(pkgs []*packages.Package, root string) map[string]bool {
	out := map[string]bool{}
	for _, p := range pkgs {
		for _, f := range p.Syntax {
			if !isGenerated(f) {
				continue
			}
			if rel := relPath(p.Fset, f.Pos(), root); rel != "" {
				out[rel] = true
			}
		}
	}
	return out
}

func isGenerated(f *ast.File) bool {
	for _, group := range f.Comments {
		if group.Pos() > f.Package {
			break
		}
		for _, c := range group.List {
			if generatedMarker.MatchString(c.Text) {
				return true
			}
		}
	}
	return false
}

func withoutGenerated[T any](items []T, gen map[string]bool, fileOf func(T) string) ([]T, int) {
	if len(gen) == 0 {
		return items, 0
	}
	kept := make([]T, 0, len(items))
	dropped := 0
	for _, it := range items {
		if gen[fileOf(it)] {
			dropped++
			continue
		}
		kept = append(kept, it)
	}
	return kept, dropped
}
