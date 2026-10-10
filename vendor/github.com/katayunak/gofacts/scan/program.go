// Package scan turns a Go module into facts: every function and how it calls
// the others, where calls leave the process, which transactions are open
// around them, which states a lifecycle moves through, what the schema
// enforces, and what looks wrong.
//
// Two mechanisms produce the output and the distinction is kept everywhere:
// the type checker and SSA prove what a call is and where control can go;
// tables in package naming and the package prefixes in seams.go guess what
// things are for. Findings carry fact.Proved or fact.Heuristic accordingly.
//
// Load builds the whole program once. Consumers then ask for the slice they
// need: testigo asks for the Flow reachable from its entry points, a reviewer
// asks for the functions a diff touches.
package scan

import (
	"context"
	"fmt"
	"go/ast"
	"go/token"
	"go/types"
	"sort"
	"strings"

	"golang.org/x/tools/go/callgraph"
	"golang.org/x/tools/go/packages"
	"golang.org/x/tools/go/ssa"

	"github.com/katayunak/gofacts/fact"
	"github.com/katayunak/gofacts/goenv"
	"github.com/katayunak/gofacts/load"
)

type Options struct {
	Dir       string
	Patterns  []string
	Tests     bool
	BuildTags []string
	Env       goenv.Options

	// SkipDirs are directory names never searched for SQL, in addition to
	// .git, vendor, node_modules, testdata and dot-directories.
	SkipDirs []string

	// NoVTA keeps every dynamic call at CHA precision. VTA is the expensive
	// step on a large module.
	NoVTA bool
}

type Program struct {
	root, module string
	ld           *load.Result

	prod      []*packages.Package
	local     map[string]bool
	variant   map[*types.Package]bool
	generated map[string]bool

	ssa       *ssa.Program
	all       map[*ssa.Function]bool
	cha       *callgraph.Graph
	chaSites  map[ssa.CallInstruction][]*ssa.Function
	vtaSites  map[ssa.CallInstruction][]*ssa.Function
	spawnedBy map[*ssa.Function]spawn

	idOf  map[*ssa.Function]string
	canon map[string]*ssa.Function
	funcs map[string]*fact.Function

	edges    []fact.Edge
	outEdges map[string][]fact.Edge
	inEdges  map[string][]fact.Edge

	sink          map[*ssa.Function]bool
	reach         map[*ssa.Function]kindSet
	importKinds   map[string]kindSet
	directImports map[string]map[string]bool

	seams       []fact.Seam
	txs         []fact.Tx
	txLines     map[string]map[int]bool
	states      []fact.StateMachine
	infra       fact.Infra
	sql         []sqlSite
	writes      []fact.TableWrites
	idem        fact.Candidates
	money       fact.Candidates
	entities    map[string]string
	tableNames  map[*types.Func]string
	hashChains  []fact.HashChain
	findings    []fact.Finding
	genFindings int
}

type spawn struct{ goroutine, deferred bool }

func Load(ctx context.Context, opts Options) (*Program, error) {
	ld, err := load.Packages(ctx, load.Config{
		Dir: opts.Dir, Patterns: opts.Patterns, Tests: opts.Tests,
		BuildTags: opts.BuildTags, Env: opts.Env,
	})
	if err != nil {
		return nil, err
	}
	p := &Program{
		root: ld.Root, module: ld.Module, ld: ld,
		importKinds: map[string]kindSet{},
	}
	p.classifyPackages()
	p.generated = generatedFiles(ld)

	steps := []func() error{
		p.buildSSA,
		func() error { p.buildCallGraph(opts.NoVTA); return nil },
		func() error { p.indexFunctions(); return nil },
		func() error { p.analyzeFunctions(); return nil },
		func() error { p.buildEdges(); return nil },
		func() error { p.infra = p.schema(opts.SkipDirs); return nil },
		func() error { p.sql = p.sqlSites(); return nil },
		func() error { p.writes = p.tableWrites(); return nil },
		func() error { p.txs = p.transactions(); return nil },
		func() error { p.states = p.stateMachines(); return nil },
		func() error { p.hashChains = p.extractHashChains(); return nil },
		func() error { p.idem, p.entities = p.idempotencyCandidates(); return nil },
		func() error { p.money = p.moneyCandidates(); return nil },
		func() error { p.findings, p.genFindings = p.allFindings(); return nil },
	}
	for _, step := range steps {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if err := step(); err != nil {
			return nil, err
		}
	}
	return p, nil
}

// classifyPackages separates the module's production packages from test
// variants. With tests loaded, go/packages returns each package twice — once
// plain, once recompiled with its _test.go files — and every production
// function exists in both. The plain copy is the one facts are read from.
func (p *Program) classifyPackages() {
	p.local = map[string]bool{}
	p.variant = map[*types.Package]bool{}
	for _, pkg := range p.ld.Packages {
		p.local[pkg.PkgPath] = true
		if isVariant(pkg) {
			if pkg.Types != nil {
				p.variant[pkg.Types] = true
			}
			continue
		}
		p.prod = append(p.prod, pkg)
	}
}

func isVariant(pkg *packages.Package) bool { return strings.Contains(pkg.ID, " [") }

func (p *Program) Root() string                { return p.root }
func (p *Program) Module() string              { return p.module }
func (p *Program) Issues() []fact.LoadIssue    { return append([]fact.LoadIssue(nil), p.ld.Issues...) }
func (p *Program) Seams() []fact.Seam          { return append([]fact.Seam(nil), p.seams...) }
func (p *Program) Transactions() []fact.Tx     { return append([]fact.Tx(nil), p.txs...) }
func (p *Program) States() []fact.StateMachine { return append([]fact.StateMachine(nil), p.states...) }
func (p *Program) Infra() fact.Infra           { return p.infra }
func (p *Program) TableWrites() []fact.TableWrites {
	return append([]fact.TableWrites(nil), p.writes...)
}
func (p *Program) IdempotencyKeys() fact.Candidates { return append(fact.Candidates(nil), p.idem...) }
func (p *Program) MoneyTypes() fact.Candidates      { return append(fact.Candidates(nil), p.money...) }
func (p *Program) HashChains() []fact.HashChain {
	return append([]fact.HashChain(nil), p.hashChains...)
}

// Findings are program-wide: every rule applied to every function of the
// module, with findings in generated files dropped.
func (p *Program) Findings() []fact.Finding { return append([]fact.Finding(nil), p.findings...) }

func (p *Program) GeneratedFiles() []string {
	out := make([]string, 0, len(p.generated))
	for f := range p.generated {
		out = append(out, f)
	}
	sort.Strings(out)
	return out
}

func (p *Program) pos(pos token.Pos) fact.Pos { return p.ld.Pos(pos) }

func (p *Program) rel(pos token.Pos) string { return p.ld.Pos(pos).File }

func (p *Program) line(pos token.Pos) int { return p.ld.Fset.Position(pos).Line }

var generatedMarker = "// Code generated "

// generatedFiles follows the Go convention: a "// Code generated ... DO NOT
// EDIT." line before the package clause.
func generatedFiles(ld *load.Result) map[string]bool {
	out := map[string]bool{}
	for _, pkg := range ld.Packages {
		for _, f := range pkg.Syntax {
			if isGenerated(f) {
				out[ld.Pos(f.Pos()).File] = true
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
			if strings.HasPrefix(c.Text, generatedMarker) && strings.HasSuffix(c.Text, " DO NOT EDIT.") {
				return true
			}
		}
	}
	return false
}

func isTestFile(file string) bool { return strings.HasSuffix(file, "_test.go") }

func (p *Program) skipDir(name string, extra []string) bool {
	switch name {
	case ".git", "vendor", "node_modules", "testdata":
		return true
	}
	if strings.HasPrefix(name, ".") && name != "." {
		return true
	}
	for _, e := range extra {
		if e == name {
			return true
		}
	}
	return false
}

func errorf(format string, args ...any) error { return fmt.Errorf(format, args...) }
