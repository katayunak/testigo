package codeRef

import "go/ast"

// Symbol renders a function's name in a form that is stable and unambiguous
// within a package: "Reserve", "Service.Reserve", "(*Service).Reserve"
func Symbol(decl *ast.FuncDecl) string {
	if decl.Recv == nil || len(decl.Recv.List) == 0 {
		return decl.Name.Name // no receiver
	}

	receiver := decl.Recv.List[0].Type
	if star, ok := receiver.(*ast.StarExpr); ok {
		return "(*" + typeName(star.X) + ")." + decl.Name.Name
	}

	return typeName(receiver) + "." + decl.Name.Name
}

// handling receiver's type
func typeName(e ast.Expr) string {
	switch t := e.(type) {
	case *ast.Ident:
		return t.Name
	case *ast.IndexExpr: // generic receiver: Foo[T]
		return typeName(t.X)
	case *ast.IndexListExpr: // generic receiver: Foo[T, U]
		return typeName(t.X)
	case *ast.SelectorExpr:
		return typeName(t.X) + "." + t.Sel.Name
	}
	return "?"
}
