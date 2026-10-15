package scan

import (
	"go/ast"
	"go/types"
	"sort"

	"golang.org/x/tools/go/packages"

	"github.com/katayunak/gofacts/fact"
)

var hashPackages = map[string]bool{
	"crypto/sha256": true, "crypto/sha512": true, "crypto/sha1": true, "crypto/md5": true,
	"crypto/sha3": true, "golang.org/x/crypto/blake2b": true, "golang.org/x/crypto/blake2s": true,
	"golang.org/x/crypto/sha3": true, "hash/fnv": true,
}

// extractHashChains finds types with a method that takes a previous instance
// of the same type and hashes: the shape of a tamper-evident log. The hash call
// is recognised by the package it resolves to, so an aliased import counts.
func (p *Program) extractHashChains() []fact.HashChain {
	var out []fact.HashChain
	seen := map[string]bool{}
	for _, pkg := range p.prod {
		for _, f := range pkg.Syntax {
			for _, decl := range f.Decls {
				fn, ok := decl.(*ast.FuncDecl)
				if !ok || fn.Body == nil || fn.Recv == nil || len(fn.Recv.List) == 0 {
					continue
				}
				recv := namedOf(pkg.TypesInfo.TypeOf(fn.Recv.List[0].Type))
				if recv == nil || recv.Obj().Pkg() == nil || !takesSelf(pkg, fn, recv) || !hashes(pkg, fn.Body) {
					continue
				}
				key := recv.Obj().Pkg().Path() + "." + recv.Obj().Name()
				if seen[key] {
					continue
				}
				seen[key] = true
				out = append(out, fact.HashChain{Type: key, Method: fact.CodeRef{
					Pkg: pkg.PkgPath, Symbol: declSymbol(pkg, fn), File: p.rel(fn.Pos()), Line: p.line(fn.Pos()),
				}})
			}
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Type < out[j].Type })
	return out
}

func takesSelf(pkg *packages.Package, fn *ast.FuncDecl, recv *types.Named) bool {
	if fn.Type.Params == nil {
		return false
	}
	for _, field := range fn.Type.Params.List {
		if pt := namedOf(pkg.TypesInfo.TypeOf(field.Type)); pt != nil && types.Identical(pt, recv) {
			return true
		}
	}
	return false
}

func hashes(pkg *packages.Package, body *ast.BlockStmt) bool {
	found := false
	ast.Inspect(body, func(n ast.Node) bool {
		if found {
			return false
		}
		if call, ok := n.(*ast.CallExpr); ok {
			if fn := calleeFunc(pkg.TypesInfo, call); fn != nil && fn.Pkg() != nil && hashPackages[fn.Pkg().Path()] {
				found = true
			}
		}
		return true
	})
	return found
}
