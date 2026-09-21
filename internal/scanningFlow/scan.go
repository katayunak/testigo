package scanningFlow

import (
	"fmt"
	"go/ast"
	"os"
	"reflect"
	"sort"
	"strings"
	"time"

	"github.com/katayunak/testigo/internal/codeRef"
	"github.com/katayunak/testigo/internal/scanningFlow/flowEntity"
	"golang.org/x/tools/go/packages"
)

type Options struct {
	Root    string
	Entries []flowEntity.EntryPoint
}

type Result struct {
	Flow  *flowEntity.Flow
	Index *codeRef.Index
	Local map[string]bool
}

const loadMode = packages.NeedName | packages.NeedFiles | packages.NeedCompiledGoFiles |
	packages.NeedImports | packages.NeedDeps | packages.NeedTypes | packages.NeedSyntax |
	packages.NeedTypesInfo | packages.NeedModule

func Scan(opts Options) (*Result, error) {

	cfg := &packages.Config{
		Mode:  loadMode,
		Dir:   opts.Root,
		Tests: false,
		Env:   append(os.Environ(), "GOWORK=off"),
	}

	pkgs, err := packages.Load(cfg, []string{"./..."}...)
	if err != nil {
		return nil, fmt.Errorf("load packages: %w", err)
	}

	var hardErrs []string
	packages.Visit(pkgs, nil, func(p *packages.Package) {
		for _, e := range p.Errors {

			hardErrs = append(hardErrs, fmt.Sprintf("%s: %s", p.PkgPath, e))
		}
	})
	if len(pkgs) == 0 {
		return nil, fmt.Errorf("no packages matched %v in %s",
			[]string{"./..."}, opts.Root)
	}

	local := map[string]bool{}
	moduleName := ""
	for _, p := range pkgs {
		local[p.PkgPath] = true

		if p.Module != nil && moduleName == "" {
			moduleName = p.Module.Path
		}
	}

	flow := flowEntity.NewFlow(moduleName)
	flow.GeneratedAt = time.Now().Local().Format(time.RFC3339)
	flow.Entries = opts.Entries

	ix := codeRef.NewIndex()
	decls := map[string]*declRef{}

	for _, p := range pkgs {

		for _, file := range p.Syntax {

			for _, declaration := range file.Decls {

				functionDeclaration, ok := declaration.(*ast.FuncDecl)
				if !ok || functionDeclaration.Body == nil {
					continue
				}

				newCodeRef := codeRef.NewCodeRef(p.PkgPath, opts.Root, p.Fset, functionDeclaration)
				_, astNodesCounts := codeRef.StructuralHash(functionDeclaration)
				ix.Add(newCodeRef, astNodesCounts)
				decls[newCodeRef.ID()] = &declRef{codeRef: newCodeRef, decl: functionDeclaration, pkg: p}
			}
		}
	}

	g, err := buildGraph(pkgs, local)
	if err != nil {
		return nil, err
	}

	if err := g.discover(opts.Root, opts.Entries, flow); err != nil {
		return nil, err
	}

	gen := generatedFiles(pkgs, opts.Root)

	flow.Infra = Infra(opts.Root)
	flow.Docs = FindDocs(opts.Root)

	flow.States = extractStateMachines(pkgs, opts.Root, local, gen)
	linkStateWritesToNodes(flow)

	flow.IdempotencyKeys = idempotencyCandidates(pkgs, flow, opts.Root, local)
	flow.MoneyTypes = moneyCandidates(pkgs, flow, opts.Root, local)

	fileOfCandidate := func(c flowEntity.Candidate) string { return c.File }
	flow.IdempotencyKeys, _ = withoutGenerated(flow.IdempotencyKeys, gen, fileOfCandidate)
	flow.MoneyTypes, _ = withoutGenerated(flow.MoneyTypes, gen, fileOfCandidate)

	flow.Findings = append(flow.Findings, stateFindings(flow.States)...)
	flow.Findings = append(flow.Findings, moneyFindings(pkgs, opts.Root)...)
	flow.Findings = append(flow.Findings, structuralFindings(flow)...)
	flow.Findings = append(flow.Findings, infraFindings(pkgs, flow, opts.Root)...)

	flow.Findings, flow.GeneratedFindings = withoutGenerated(
		flow.Findings, gen,
		func(f flowEntity.Finding) string { return f.Ref.File },
	)
	flow.GeneratedFiles = len(gen)

	for _, e := range hardErrs {
		flow.Findings = append(flow.Findings, flowEntity.Finding{
			ID: "LOAD-ERROR", Severity: flowEntity.SevInfo,
			Title:  "package did not type-check",
			Detail: e + " — results for this package are incomplete",
		})
	}

	sortFindings(flow.Findings)
	return &Result{Flow: flow, Index: ix, Local: local}, nil
}

type declRef struct {
	codeRef codeRef.CodeRef
	decl    *ast.FuncDecl
	pkg     *packages.Package
}

func linkStateWritesToNodes(f *flowEntity.Flow) {
	for mi := range f.States {
		for wi := range f.States[mi].Writes {
			write := &f.States[mi].Writes[wi]
			node, ok := f.Nodes[write.In.ID()]
			if !ok {
				continue
			}
			write.InTx = node.Facts.OpensTx
			node.Facts.WritesStatus = appendUnique(node.Facts.WritesStatus, write.To)
		}
	}
}

func appendUnique(list []string, value string) []string {
	for _, existing := range list {
		if existing == value {
			return list
		}
	}
	return append(list, value)
}

func structuralFindings(f *flowEntity.Flow) []flowEntity.Finding {
	var out []flowEntity.Finding

	byFn := map[string][]flowEntity.Seam{}
	for _, s := range f.Seams {
		if !s.Injectable {
			byFn[s.In.ID()] = append(byFn[s.In.ID()], s)
		}
	}
	for id, seams := range byFn {
		kinds := map[flowEntity.SeamKind]bool{}
		for _, s := range seams {
			kinds[s.Kind] = true
		}
		var ks []string
		for k := range kinds {
			ks = append(ks, string(k))
		}
		sort.Strings(ks)
		out = append(out, flowEntity.Finding{
			ID: "SEAM-CONCRETE", Severity: flowEntity.SevHigh,
			Title:  "I/O called on a concrete type, so faults cannot be injected",
			Detail: fmt.Sprintf("%d call(s) to %s go straight to a concrete type. A test cannot make them time out, fail, or return a duplicate, which is where payment bugs live.", len(seams), strings.Join(ks, ", ")),
			Ref:    seams[0].In,
			Line:   seams[0].Line,
		})
		_ = id
	}

	for _, n := range f.Nodes {
		if n.Facts.OpensTx && !n.Facts.RollsBackTx {
			out = append(out, flowEntity.Finding{
				ID: "TX-NO-ROLLBACK", Severity: flowEntity.SevHigh,
				Title:  "transaction opened with no rollback in the same function",
				Detail: "An early return between BEGIN and COMMIT leaves the transaction open. Idiomatic Go pairs the Begin with an immediate `defer tx.Rollback()`, which is a no-op after a successful commit.",
				Ref:    n.Ref, Line: n.Ref.Line,
			})
		}
		if n.Facts.OpensTx && n.Facts.TouchesNet {
			out = append(out, flowEntity.Finding{
				ID: "TX-NET-CALL", Severity: flowEntity.SevCritical,
				Title:  "network call inside a database transaction",
				Detail: "Holding a transaction open across a network round trip pins locks for the duration of someone else's timeout. Under load this turns a slow provider into a database outage, and a retry of the outer request can double-apply the effect.",
				Ref:    n.Ref, Line: n.Ref.Line,
			})
		}
	}
	return out
}

func stateFindings(ms []flowEntity.StateMachine) []flowEntity.Finding {
	var out []flowEntity.Finding
	for _, m := range ms {
		for _, st := range m.NeverAssigned {
			out = append(out, flowEntity.Finding{
				ID: "STATE-NEVER-SET", Severity: flowEntity.SevMedium,
				Title:  m.Type + "." + st + " is declared but nothing in this module produces it",
				Detail: "No assignment, struct literal, return statement or call argument anywhere in the scanned packages produces this state. Either it is dead, or something outside this code — a migration, a manual fix, another service — puts payments into it. If the second, every read path has to handle a state no write path here produces, and no test currently covers that.",
				Ref:    codeRef.CodeRef{Pkg: pkgOf(m.Type), Symbol: "const " + st},
			})
		}
	}
	return out
}

func pkgOf(qualified string) string {
	if i := strings.LastIndex(qualified, "."); i >= 0 {
		return qualified[:i]
	}
	return qualified
}

func infraFindings(pkgs []*packages.Package, f *flowEntity.Flow, root string) []flowEntity.Finding {
	var out []flowEntity.Finding
	if len(f.Infra.MigrationDirs) == 0 {
		return nil
	}

	for _, p := range pkgs {
		for _, file := range p.Syntax {
			ast.Inspect(file, func(n ast.Node) bool {
				spec, ok := n.(*ast.TypeSpec)
				if !ok {
					return true
				}
				st, ok := spec.Type.(*ast.StructType)
				if !ok {
					return true
				}
				for _, fld := range st.Fields.List {
					for _, nm := range fld.Names {

						cand, scored := f.IdempotencyKeys.Find(spec.Name.Name, nm.Name)
						if !scored || !cand.Credible() {
							continue
						}
						col := columnOf(fld, nm.Name)
						if _, covered := f.Infra.CoversColumn("", col); covered {
							continue
						}
						pos := p.Fset.Position(fld.Pos())
						out = append(out, flowEntity.Finding{
							ID: "IDEM-KEY-NOT-UNIQUE", Severity: flowEntity.SevCritical,
							Title:  spec.Name.Name + "." + nm.Name + " is an idempotency key with no unique constraint behind it",
							Detail: "The migrations in " + strings.Join(f.Infra.MigrationDirs, ", ") + " create no UNIQUE index covering column \"" + col + "\". Whatever prevents duplicates in Go is therefore a read followed by a write, and two requests arriving together can both pass the read before either writes. Add a unique index and let the database refuse the second one.\n\nWhy this field: " + strings.Join(cand.Proof, "; ") + ".",
							Ref:    flowEntity.CodeRefOf(p.PkgPath, spec.Name.Name, relPath(p.Fset, fld.Pos(), root), pos.Line),
							Line:   pos.Line,
						})
					}
				}
				return true
			})
		}
	}
	return out
}

func columnOf(fld *ast.Field, fieldName string) string {
	if fld.Tag != nil {
		tag := strings.Trim(fld.Tag.Value, "`")
		for _, key := range []string{"db", "sql", "pg", "gorm", "json"} {
			if v := reflect.StructTag(tag).Get(key); v != "" {
				name := strings.Split(v, ",")[0]
				name = strings.TrimPrefix(name, "column:")
				if name != "" && name != "-" {
					return name
				}
			}
		}
	}
	return snakeCase(fieldName)
}

func snakeCase(s string) string {
	runes := []rune(s)
	var b strings.Builder
	for i, r := range runes {
		upper := r >= 'A' && r <= 'Z'
		if upper && i > 0 {
			prevLower := runes[i-1] >= 'a' && runes[i-1] <= 'z'
			nextLower := i+1 < len(runes) && runes[i+1] >= 'a' && runes[i+1] <= 'z'

			if prevLower || nextLower {
				b.WriteByte('_')
			}
		}
		if upper {
			r += 32
		}
		b.WriteRune(r)
	}
	return b.String()
}

var sevRank = map[flowEntity.Severity]int{
	flowEntity.SevCritical: 0, flowEntity.SevHigh: 1, flowEntity.SevMedium: 2, flowEntity.SevInfo: 3,
}

func sortFindings(fs []flowEntity.Finding) {
	sort.SliceStable(fs, func(i, j int) bool {
		if sevRank[fs[i].Severity] != sevRank[fs[j].Severity] {
			return sevRank[fs[i].Severity] < sevRank[fs[j].Severity]
		}
		if fs[i].ID != fs[j].ID {
			return fs[i].ID < fs[j].ID
		}
		if fs[i].Ref.ID() != fs[j].Ref.ID() {
			return fs[i].Ref.ID() < fs[j].Ref.ID()
		}
		return fs[i].Line < fs[j].Line
	})
}
