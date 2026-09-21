package testPlan

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"strings"
)

func CheckSize(filename, src string, want Size) []string {
	if want != SizeSmall {
		return nil
	}
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, filename, src, parser.ParseComments)
	if err != nil {
		return []string{fmt.Sprintf("cannot parse: %v", err)}
	}

	banned := map[string]string{
		"time.Sleep":   "a small test must not sleep; sleeping is the single largest documented source of flaky tests, and a barrier or a channel expresses the same intent deterministically",
		"time.Now":     "a small test must not read the wall clock; inject a clock so the expiry case is reachable at all",
		"time.After":   "a small test must not depend on real elapsed time",
		"time.Tick":    "a small test must not depend on real elapsed time",
		"net.Dial":     "a small test must not open a socket",
		"net.Listen":   "a small test must not open a socket",
		"http.Get":     "a small test must not make a real request; use httptest or a fake transport",
		"http.Post":    "a small test must not make a real request",
		"os.Open":      "a small test must not touch the disk",
		"os.Create":    "a small test must not touch the disk",
		"os.ReadFile":  "a small test must not touch the disk",
		"os.WriteFile": "a small test must not touch the disk",
		"exec.Command": "a small test must not start a process",
		"sql.Open":     "a small test must not open a database; this belongs in a medium test behind a build tag",
	}

	var problems []string
	seenRandSeeded := strings.Contains(src, "rand.New(")

	ast.Inspect(file, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		sel, ok := call.Fun.(*ast.SelectorExpr)
		if !ok {
			return true
		}
		pkg, ok := sel.X.(*ast.Ident)
		if !ok {
			return true
		}
		name := pkg.Name + "." + sel.Sel.Name
		if why, bad := banned[name]; bad {
			pos := fset.Position(call.Pos())
			problems = append(problems, fmt.Sprintf("line %d: %s — %s", pos.Line, name, why))
		}

		if (name == "rand.Intn" || name == "rand.Int63" || name == "rand.Float64") && !seenRandSeeded {
			pos := fset.Position(call.Pos())
			problems = append(problems, fmt.Sprintf("line %d: %s uses the global source — seed it with rand.New(rand.NewSource(seed)) and log the seed, or a failure cannot be reproduced", pos.Line, name))
		}
		return true
	})
	return problems
}
