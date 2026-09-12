package scanningFlow

import (
	"go/ast"
	"go/parser"
	"go/token"
	"testing"
)

func parseFunc(t *testing.T, src string) *ast.FuncDecl {
	t.Helper()
	f, err := parser.ParseFile(token.NewFileSet(), "x.go", "package p\n"+src, parser.ParseComments)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	for _, d := range f.Decls {
		if fd, ok := d.(*ast.FuncDecl); ok {
			return fd
		}
	}
	t.Fatal("no func found")
	return nil
}

func TestSymbolNaming(t *testing.T) {
	cases := map[string]string{
		"func Pay() {}":               "Pay",
		"func (s Service) Pay() {}":   "Service.Pay",
		"func (s *Service) Pay() {}":  "(*Service).Pay",
		"func (r *Repo[T]) Save() {}": "(*Repo).Save",
	}
	for src, want := range cases {
		if got := Symbol(parseFunc(t, src)); got != want {
			t.Errorf("%q: got %q want %q", src, got, want)
		}
	}
}
