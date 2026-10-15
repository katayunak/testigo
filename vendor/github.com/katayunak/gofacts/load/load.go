// Package load type-checks a module with go/packages under an environment the
// repository cannot influence, and keeps every load problem as a fact instead
// of a log line.
package load

import (
	"context"
	"fmt"
	"go/token"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"golang.org/x/tools/go/packages"

	"github.com/katayunak/gofacts/fact"
	"github.com/katayunak/gofacts/goenv"
)

type Config struct {
	Dir       string
	Patterns  []string
	Tests     bool
	BuildTags []string
	Env       goenv.Options
}

type Result struct {
	Root     string
	Module   string
	Fset     *token.FileSet
	Packages []*packages.Package
	Issues   []fact.LoadIssue
}

const Mode = packages.NeedName | packages.NeedFiles | packages.NeedCompiledGoFiles |
	packages.NeedImports | packages.NeedDeps | packages.NeedTypes | packages.NeedSyntax |
	packages.NeedTypesInfo | packages.NeedModule

func Packages(ctx context.Context, cfg Config) (*Result, error) {
	root, err := filepath.Abs(cfg.Dir)
	if err != nil {
		return nil, err
	}
	patterns := cfg.Patterns
	if len(patterns) == 0 {
		patterns = []string{"./..."}
	}

	res := &Result{Root: root, Fset: token.NewFileSet()}
	pcfg := &packages.Config{
		Context: ctx,
		Mode:    Mode,
		Dir:     root,
		Tests:   cfg.Tests,
		Env:     goenv.Default(root, cfg.Env),
		Fset:    res.Fset,
	}
	if len(cfg.BuildTags) > 0 {
		pcfg.BuildFlags = []string{"-tags=" + strings.Join(cfg.BuildTags, ",")}
	}

	pkgs, err := packages.Load(pcfg, patterns...)
	if ctxErr := ctx.Err(); ctxErr != nil {
		return nil, fmt.Errorf("load %s: %w", root, ctxErr)
	}
	if err != nil {
		return nil, fmt.Errorf("load %s: %w", root, err)
	}

	for _, p := range pkgs {
		if isTestMain(p) {
			continue
		}
		res.Packages = append(res.Packages, p)
	}
	if len(res.Packages) == 0 {
		return nil, fmt.Errorf("no packages matched %v in %s", patterns, root)
	}
	sort.Slice(res.Packages, func(i, j int) bool { return res.Packages[i].ID < res.Packages[j].ID })

	res.Module = mainModule(res.Packages)
	res.Issues = collectIssues(res.Packages, root)
	return res, nil
}

// isTestMain drops the generated main package go test builds around a test
// binary; it has no source anyone wrote.
func isTestMain(p *packages.Package) bool {
	return strings.HasSuffix(p.ID, ".test") && p.Name == "main"
}

func mainModule(pkgs []*packages.Package) string {
	for _, p := range pkgs {
		if p.Module != nil && p.Module.Main {
			return p.Module.Path
		}
	}
	for _, p := range pkgs {
		if p.Module != nil {
			return p.Module.Path
		}
	}
	return ""
}

func collectIssues(pkgs []*packages.Package, root string) []fact.LoadIssue {
	seen := map[string]bool{}
	var out []fact.LoadIssue
	packages.Visit(pkgs, nil, func(p *packages.Package) {
		for _, e := range p.Errors {
			issue := fact.LoadIssue{
				Pkg:  p.PkgPath,
				Pos:  parsePos(e.Pos, root),
				Kind: kindOf(e.Kind),
				Msg:  e.Msg,
			}
			key := issue.Pkg + "|" + issue.Pos.String() + "|" + issue.Msg
			if seen[key] {
				continue
			}
			seen[key] = true
			out = append(out, issue)
		}
	})
	sort.Slice(out, func(i, j int) bool {
		if out[i].Pkg != out[j].Pkg {
			return out[i].Pkg < out[j].Pkg
		}
		if out[i].Pos != out[j].Pos {
			return out[i].Pos.Before(out[j].Pos)
		}
		return out[i].Msg < out[j].Msg
	})
	return out
}

func kindOf(k packages.ErrorKind) string {
	switch k {
	case packages.ListError:
		return "list"
	case packages.ParseError:
		return "parse"
	case packages.TypeError:
		return "type"
	}
	return "unknown"
}

// parsePos reads the "file:line:col" form go/packages uses, where the file may
// itself contain colons on some systems, so the numbers are taken from the end.
func parsePos(s, root string) fact.Pos {
	if s == "" || s == "-" {
		return fact.Pos{}
	}
	parts := strings.Split(s, ":")
	var nums []int
	for len(parts) > 1 && len(nums) < 2 {
		n, err := strconv.Atoi(parts[len(parts)-1])
		if err != nil {
			break
		}
		nums = append([]int{n}, nums...)
		parts = parts[:len(parts)-1]
	}
	p := fact.Pos{File: Rel(root, strings.Join(parts, ":"))}
	if len(nums) > 0 {
		p.Line = nums[0]
	}
	if len(nums) > 1 {
		p.Col = nums[1]
	}
	return p
}

// Rel makes a path repo-relative and slash-separated, so a reference survives
// being checked in and read on another machine. Paths outside root are kept as
// they are: claiming they are inside would be a lie.
func Rel(root, path string) string {
	if path == "" {
		return ""
	}
	if !filepath.IsAbs(path) {
		return filepath.ToSlash(path)
	}
	rel, err := filepath.Rel(root, path)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return filepath.ToSlash(path)
	}
	return filepath.ToSlash(rel)
}

func (r *Result) Pos(pos token.Pos) fact.Pos {
	if !pos.IsValid() {
		return fact.Pos{}
	}
	p := r.Fset.Position(pos)
	return fact.Pos{File: Rel(r.Root, p.Filename), Line: p.Line, Col: p.Column}
}
