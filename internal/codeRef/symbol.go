package codeRef

import "go/ast"

func Symbol(decl *ast.FuncDecl) string {
	if decl.Recv == nil || len(decl.Recv.List) == 0 {
		return decl.Name.Name
	}

	receiver := decl.Recv.List[0].Type
	if star, ok := receiver.(*ast.StarExpr); ok {
		return "(*" + typeName(star.X) + ")." + decl.Name.Name
	}

	return typeName(receiver) + "." + decl.Name.Name
}

func typeName(e ast.Expr) string {
	switch t := e.(type) {
	case *ast.Ident:
		return t.Name
	case *ast.IndexExpr:
		return typeName(t.X)
	case *ast.IndexListExpr:
		return typeName(t.X)
	case *ast.SelectorExpr:
		return typeName(t.X) + "." + t.Sel.Name
	}
	return "?"
}
