package scanningFlow

import (
	"go/ast"
	"go/token"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"golang.org/x/tools/go/packages"
)

type sqlChunk struct {
	SQL  string
	File string
	Line int
	Down bool
}

var reDDL = regexp.MustCompile(`(?is)\b(create\s+table|alter\s+table|create\s+(unique\s+)?index|create\s+type|drop\s+table|drop\s+index)\b`)

var reDownOnly = regexp.MustCompile(`(?is)^\s*(drop|alter\s+table\s+\S+\s+drop)\b`)

var migrationDirWords = []string{"migration", "migrate", "schema", "ddl", "sql"}

func looksLikeMigrationDir(rel string) bool {
	lower := strings.ToLower(filepath.ToSlash(rel))
	for _, part := range strings.Split(lower, "/") {
		for _, w := range migrationDirWords {
			if strings.Contains(part, w) {
				return true
			}
		}
	}
	return false
}

var registerFuncs = map[string]bool{
	"MustRegisterTx": true, "MustRegister": true,
	"RegisterTx": true, "Register": true,
}

func sqlFromGo(pkgs []*packages.Package, root string) ([]sqlChunk, string, int, int) {
	var out []sqlChunk
	tool := ""
	withDown, total := 0, 0

	seen := map[string]bool{}
	for _, p := range pkgs {
		for _, f := range p.Syntax {
			pos := p.Fset.Position(f.Pos())
			rel := fileRelTo(pos.Filename, root)
			if seen[rel] {
				continue
			}
			seen[rel] = true

			var downSpans [][2]token.Pos
			ast.Inspect(f, func(n ast.Node) bool {
				call, ok := n.(*ast.CallExpr)
				if !ok {
					return true
				}
				name := calleeName(call.Fun)
				if !registerFuncs[name] {
					return true
				}
				if strings.Contains(importPathsOf(f), "go-pg/migrations") {
					tool = "go-pg/migrations"
				}
				total++
				if len(call.Args) > 1 {
					if fl, isFunc := call.Args[1].(*ast.FuncLit); isFunc {
						withDown++
						downSpans = append(downSpans, [2]token.Pos{fl.Pos(), fl.End()})
					}
				}
				return true
			})

			ast.Inspect(f, func(n ast.Node) bool {
				lit, ok := n.(*ast.BasicLit)
				if !ok || lit.Kind != token.STRING {
					return true
				}
				text, err := strconv.Unquote(lit.Value)
				if err != nil {
					return true
				}
				if !reDDL.MatchString(text) {
					return true
				}
				down := false
				for _, s := range downSpans {
					if lit.Pos() >= s[0] && lit.End() <= s[1] {
						down = true
						break
					}
				}
				out = append(out, sqlChunk{
					SQL:  text,
					File: rel,
					Line: p.Fset.Position(lit.Pos()).Line,
					Down: down,
				})
				return true
			})
		}
	}

	sort.SliceStable(out, func(i, j int) bool {
		if out[i].File != out[j].File {
			return out[i].File < out[j].File
		}
		return out[i].Line < out[j].Line
	})
	return out, tool, withDown, total
}

func calleeName(e ast.Expr) string {
	switch f := e.(type) {
	case *ast.Ident:
		return f.Name
	case *ast.SelectorExpr:
		return f.Sel.Name
	}
	return ""
}

func importPathsOf(f *ast.File) string {
	var b strings.Builder
	for _, im := range f.Imports {
		b.WriteString(im.Path.Value)
		b.WriteByte(' ')
	}
	return b.String()
}

func fileRelTo(abs, root string) string {
	if rel, err := filepath.Rel(root, abs); err == nil && !strings.HasPrefix(rel, "..") {
		return filepath.ToSlash(rel)
	}
	return filepath.ToSlash(abs)
}

func isReversalOnly(sql string) bool {
	for _, stmt := range strings.Split(sql, ";") {
		if strings.TrimSpace(stmt) == "" {
			continue
		}
		if !reDownOnly.MatchString(stmt) {
			return false
		}
	}
	return true
}
