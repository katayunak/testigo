package scanningFlow

import (
	"go/ast"
	"go/types"
	"sort"

	"github.com/katayunak/testigo/internal/scanningFlow/flowEntity"
	"golang.org/x/tools/go/packages"
)

var hashFuncNames = map[string]bool{
	"Sum": true, "Sum224": true, "Sum256": true, "Sum384": true, "Sum512": true, "New": true,
}

var hashPkgIdents = map[string]bool{
	"sha256": true, "sha512": true, "sha1": true, "md5": true, "blake2b": true, "blake2s": true,
}

func extractHashChains(pkgs []*packages.Package, root string) []flowEntity.HashChain {
	var out []flowEntity.HashChain
	seen := map[string]bool{}

	for _, p := range pkgs {
		for _, f := range p.Syntax {
			for _, decl := range f.Decls {
				fn, ok := decl.(*ast.FuncDecl)
				if !ok || fn.Body == nil || fn.Recv == nil || len(fn.Recv.List) == 0 {
					continue
				}
				recvType := resolveNamedType(p, fn.Recv.List[0].Type)
				if recvType == nil || !hasSelfTypedParam(p, fn, recvType) || !callsHashFunction(fn.Body) {
					continue
				}

				key := recvType.Obj().Pkg().Path() + "." + recvType.Obj().Name()
				if seen[key] {
					continue
				}
				seen[key] = true

				pos := p.Fset.Position(fn.Pos())
				out = append(out, flowEntity.HashChain{
					Type: key,
					Method: flowEntity.CodeRef{
						Pkg: p.PkgPath, Symbol: Symbol(fn),
						File: fileRelTo(pos.Filename, root), Line: pos.Line,
					},
				})
			}
		}
	}

	sort.Slice(out, func(i, j int) bool { return out[i].Type < out[j].Type })
	return out
}

func resolveNamedType(p *packages.Package, e ast.Expr) *types.Named {
	if star, ok := e.(*ast.StarExpr); ok {
		e = star.X
	}
	tv, ok := p.TypesInfo.Types[e]
	if !ok {
		return nil
	}
	named, ok := tv.Type.(*types.Named)
	if !ok {
		return nil
	}
	return named
}

func hasSelfTypedParam(p *packages.Package, fn *ast.FuncDecl, recvType *types.Named) bool {
	if fn.Type.Params == nil {
		return false
	}
	for _, field := range fn.Type.Params.List {
		if pt := resolveNamedType(p, field.Type); pt != nil && types.Identical(pt, recvType) {
			return true
		}
	}
	return false
}

func callsHashFunction(body *ast.BlockStmt) bool {
	found := false
	ast.Inspect(body, func(n ast.Node) bool {
		if found {
			return false
		}
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		sel, ok := call.Fun.(*ast.SelectorExpr)
		if !ok {
			return true
		}
		pkgIdent, ok := sel.X.(*ast.Ident)
		if ok && hashPkgIdents[pkgIdent.Name] && hashFuncNames[sel.Sel.Name] {
			found = true
		}
		return true
	})
	return found
}
