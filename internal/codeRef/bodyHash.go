package codeRef

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"go/ast"
	"io"
)

// StructuralHash hashes the shape and semantic content of a function's
// signature and body. It returns the hash and the number of AST nodes walked,
// which callers use to decide whether the function is distinctive enough to be
// tracked across a rename

func StructuralHash(decl *ast.FuncDecl) (string, int) {
	h := sha256.New()
	n := 0

	if decl.Type != nil {
		n += writeStructure(h, decl.Type)
	}
	if decl.Body != nil {
		n += writeStructure(h, decl.Body)
	}

	return hex.EncodeToString(h.Sum(nil)), n
}

func writeStructure(w io.Writer, root ast.Node) int {
	count := 0

	ast.Inspect(root, func(n ast.Node) bool {
		if n == nil {
			fmt.Fprint(w, ")")
			return false
		}

		count++
		switch t := n.(type) {
		// Comments carry no behavior, skipping them here is what makes the
		// hash survive documentation edits and testigo's own annotations
		case *ast.Comment, *ast.CommentGroup:
			return false

		case *ast.Ident:
			fmt.Fprintf(w, "(id:%s", t.Name)
		case *ast.BasicLit:
			fmt.Fprintf(w, "(lit:%s:%s", t.Kind, t.Value)
		case *ast.BinaryExpr:
			fmt.Fprintf(w, "(bin:%s", t.Op)
		case *ast.UnaryExpr:
			fmt.Fprintf(w, "(un:%s", t.Op)
		case *ast.AssignStmt:
			fmt.Fprintf(w, "(assign:%s", t.Tok)
		case *ast.IncDecStmt:
			fmt.Fprintf(w, "(incdec:%s", t.Tok)
		case *ast.BranchStmt:
			fmt.Fprintf(w, "(branch:%s", t.Tok)
		case *ast.GenDecl:
			fmt.Fprintf(w, "(decl:%s", t.Tok)
		case *ast.RangeStmt:
			fmt.Fprintf(w, "(range:%s", t.Tok)
		case *ast.ChanType:
			fmt.Fprintf(w, "(chan:%d", t.Dir)

		default:
			fmt.Fprintf(w, "(%T", n)
		}
		return true
	})

	return count
}
