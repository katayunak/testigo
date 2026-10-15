package scan

import (
	"go/ast"
	"go/constant"
	"go/token"
	"go/types"
	"sort"

	"golang.org/x/tools/go/ast/astutil"
	"golang.org/x/tools/go/packages"

	"github.com/katayunak/gofacts/fact"
	"github.com/katayunak/gofacts/naming"
)

type lifecycle struct {
	named  *types.Named
	key    string
	states map[string]bool
	byVal  map[string]string
	decl   map[string]fact.Pos
	fields []string
	writes []fact.StateWrite
}

// stateMachines finds the lifecycles of the module. A type is one when it is
// named like a status and assigned, or when it is stored in a struct field and
// moved by at least two functions — SettlementMode counts, DeclineCode, which
// is only returned and compared, does not.
func (p *Program) stateMachines() []fact.StateMachine {
	cands := p.lifecycleCandidates()
	if len(cands) == 0 {
		return nil
	}
	for _, pkg := range p.prod {
		p.collectStateWrites(pkg, cands)
	}

	var out []fact.StateMachine
	for _, key := range sortedKeys(cands) {
		lc := cands[key]
		if len(lc.states) == 0 || !isLifecycle(lc) {
			continue
		}
		states := make([]string, 0, len(lc.states))
		for s := range lc.states {
			states = append(states, s)
		}
		sort.Strings(states)
		sort.SliceStable(lc.writes, func(i, j int) bool {
			a, b := lc.writes[i], lc.writes[j]
			if a.In.ID() != b.In.ID() {
				return a.In.ID() < b.In.ID()
			}
			if a.Line != b.Line {
				return a.Line < b.Line
			}
			return a.To < b.To
		})
		for i := range lc.writes {
			w := &lc.writes[i]
			w.InTx = p.txLines[w.In.ID()][w.Line]
		}
		written := map[string]bool{}
		for _, w := range lc.writes {
			written[w.To] = true
		}
		var never []string
		for _, s := range states {
			if !written[s] {
				never = append(never, s)
			}
		}
		sort.Strings(lc.fields)
		m := fact.StateMachine{
			Type: key, Fields: lc.fields, States: states, Writes: lc.writes, NeverAssigned: never,
			Decl: p.pos(lc.named.Obj().Pos()),
		}
		if len(lc.fields) > 0 {
			m.Field = lc.fields[0]
		}
		out = append(out, m)
	}
	p.linkStateWrites(out)
	return out
}

func sortedKeys[V any](m map[string]V) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func (p *Program) lifecycleCandidates() map[string]*lifecycle {
	cands := map[string]*lifecycle{}
	for _, pkg := range p.prod {
		if pkg.Types == nil {
			continue
		}
		scope := pkg.Types.Scope()
		for _, name := range scope.Names() {
			tn, ok := scope.Lookup(name).(*types.TypeName)
			if !ok || tn.IsAlias() || !(naming.IsStateType(name) || naming.IsWeakStateType(name)) {
				continue
			}
			if p.generated[p.rel(tn.Pos())] {
				continue
			}
			named, ok := tn.Type().(*types.Named)
			if !ok {
				continue
			}
			if b, ok := named.Underlying().(*types.Basic); !ok || b.Info()&(types.IsString|types.IsInteger) == 0 {
				continue
			}
			key := types.TypeString(named, relativeTo)
			cands[key] = &lifecycle{named: named, key: key, states: map[string]bool{}, byVal: map[string]string{}, decl: map[string]fact.Pos{}}
		}
	}
	for _, pkg := range p.prod {
		if pkg.Types == nil {
			continue
		}
		scope := pkg.Types.Scope()
		for _, name := range scope.Names() {
			c, ok := scope.Lookup(name).(*types.Const)
			if !ok {
				continue
			}
			lc, ok := cands[types.TypeString(c.Type(), relativeTo)]
			if !ok {
				continue
			}
			lc.states[c.Name()] = true
			lc.decl[c.Name()] = p.pos(c.Pos())
			if _, dup := lc.byVal[c.Val().ExactString()]; !dup {
				lc.byVal[c.Val().ExactString()] = c.Name()
			}
		}
	}
	return cands
}

func isLifecycle(lc *lifecycle) bool {
	distinct := map[string]bool{}
	for _, w := range lc.writes {
		distinct[w.In.ID()] = true
	}
	if naming.IsStateType(lc.named.Obj().Name()) {
		return len(lc.writes) > 0
	}
	return len(lc.fields) > 0 && len(distinct) >= 2
}

func (p *Program) collectStateWrites(pkg *packages.Package, cands map[string]*lifecycle) {
	info := pkg.TypesInfo
	ft := p.funcTable(pkg)
	candOf := func(t types.Type) *lifecycle {
		if t == nil {
			return nil
		}
		return cands[types.TypeString(t, relativeTo)]
	}
	for _, file := range pkg.Syntax {
		write := func(lc *lifecycle, e ast.Expr, at token.Pos, via fact.WriteVia, from []string) {
			to := fact.DynamicState
			if tv, ok := info.Types[e]; ok && tv.Value != nil {
				to = constName(lc, e, tv.Value)
			}
			lc.writes = append(lc.writes, fact.StateWrite{
				In: ft.at(p, pkg, at), To: to, From: from, Line: p.line(at), Via: via,
			})
		}
		ast.Inspect(file, func(n ast.Node) bool {
			switch t := n.(type) {
			case *ast.StructType:
				for _, fld := range t.Fields.List {
					if lc := candOf(info.TypeOf(fld.Type)); lc != nil {
						for _, nm := range fld.Names {
							lc.fields = appendUnique(lc.fields, nm.Name)
						}
					}
				}
			case *ast.CompositeLit:
				for _, el := range t.Elts {
					kv, ok := el.(*ast.KeyValueExpr)
					if !ok {
						continue
					}
					if lc := candOf(info.TypeOf(kv.Value)); lc != nil && isConst(info, kv.Value) {
						write(lc, kv.Value, kv.Pos(), fact.ViaLiteral, nil)
					}
				}
			case *ast.ReturnStmt:
				for _, r := range t.Results {
					if lc := candOf(info.TypeOf(r)); lc != nil && isConst(info, r) {
						write(lc, r, r.Pos(), fact.ViaReturn, nil)
					}
				}
			case *ast.AssignStmt:
				if len(t.Lhs) != len(t.Rhs) {
					return true
				}
				for i, lhs := range t.Lhs {
					lc := candOf(info.TypeOf(lhs))
					if lc == nil {
						continue
					}
					rhs := t.Rhs[i]
					if !isConst(info, rhs) && !isField(info, lhs) {
						continue
					}
					write(lc, rhs, t.Pos(), fact.ViaAssign, p.guards(file, info, lhs, t))
				}
			case *ast.CallExpr:
				p.argumentWrites(info, t, candOf, write)
			}
			return true
		})
	}
}

// argumentWrites counts a state constant passed to a parameter declared with
// the lifecycle type — repo.UpdateStatus(ctx, id, StatusCaptured) produces
// StatusCaptured. A parameter of interface type (fmt.Println) does not: that
// is a print, not a write. testigo counted neither, so a state set only through
// a repository call was reported as never set.
func (p *Program) argumentWrites(info *types.Info, call *ast.CallExpr, candOf func(types.Type) *lifecycle,
	write func(*lifecycle, ast.Expr, token.Pos, fact.WriteVia, []string)) {
	if tv, ok := info.Types[call.Fun]; ok && tv.IsType() {
		return
	}
	sig, ok := types.Unalias(info.TypeOf(call.Fun)).(*types.Signature)
	if !ok {
		return
	}
	params := sig.Params()
	for i, arg := range call.Args {
		lc := candOf(info.TypeOf(arg))
		if lc == nil || !isConst(info, arg) {
			continue
		}
		var pt types.Type
		switch {
		case i < params.Len()-1 || (i < params.Len() && !sig.Variadic()):
			pt = params.At(i).Type()
		case sig.Variadic() && params.Len() > 0:
			if sl, ok := params.At(params.Len() - 1).Type().(*types.Slice); ok {
				pt = sl.Elem()
			}
		}
		if pt != nil && types.Identical(pt, lc.named) {
			write(lc, arg, arg.Pos(), fact.ViaArgument, nil)
		}
	}
}

func isConst(info *types.Info, e ast.Expr) bool {
	tv, ok := info.Types[e]
	return ok && tv.Value != nil
}

// isField: a computed value assigned to a local (`prev := p.Status`) moves no
// state; one assigned to a struct field does, even when the value is unknown.
func isField(info *types.Info, e ast.Expr) bool { return fieldVar(info, e) != nil }

// constName names the state a constant expression produces: the identifier
// when it is one, otherwise the constant declared with the same value, so
// PaymentStatus("captured") is StatusCaptured.
func constName(lc *lifecycle, e ast.Expr, val constant.Value) string {
	switch x := ast.Unparen(e).(type) {
	case *ast.Ident:
		if lc.states[x.Name] {
			return x.Name
		}
	case *ast.SelectorExpr:
		if lc.states[x.Sel.Name] {
			return x.Sel.Name
		}
	}
	if name, ok := lc.byVal[val.ExactString()]; ok {
		return name
	}
	if val.Kind() == constant.String {
		return constant.StringVal(val)
	}
	return val.ExactString()
}

// guards returns the source states an assignment to a status field is
// conditioned on in the same function: an enclosing `if x.F == A`, a `case A`
// of `switch x.F`, or an earlier `if x.F != A { return }` in the same block.
func (p *Program) guards(file *ast.File, info *types.Info, lhs ast.Expr, assign *ast.AssignStmt) []string {
	target := types.ExprString(lhs)
	path, _ := astutil.PathEnclosingInterval(file, assign.Pos(), assign.End())
	var from []string
	for i, n := range path {
		switch s := n.(type) {
		case *ast.IfStmt:
			if i > 0 && path[i-1] == s.Body {
				from = append(from, equalities(info, s.Cond, target, token.EQL)...)
			}
		case *ast.CaseClause:
			if i+2 < len(path) {
				if sw, ok := path[i+2].(*ast.SwitchStmt); ok && sw.Tag != nil && types.ExprString(sw.Tag) == target {
					for _, e := range s.List {
						if name, ok := stateConst(info, e); ok {
							from = append(from, name)
						}
					}
				}
			}
		case *ast.BlockStmt:
			for _, st := range s.List {
				if st.Pos() >= assign.Pos() {
					break
				}
				ifs, ok := st.(*ast.IfStmt)
				if !ok || !returns(ifs.Body) {
					continue
				}
				from = append(from, equalities(info, ifs.Cond, target, token.NEQ)...)
			}
		case *ast.FuncDecl, *ast.FuncLit:
			return dedupe(from)
		}
	}
	return dedupe(from)
}

// equalities collects constants compared with target using op, through ||
// for == and && for != (the shapes "is one of" and "is none of").
func equalities(info *types.Info, cond ast.Expr, target string, op token.Token) []string {
	b, ok := ast.Unparen(cond).(*ast.BinaryExpr)
	if !ok {
		return nil
	}
	joiner := token.LOR
	if op == token.NEQ {
		joiner = token.LAND
	}
	if b.Op == joiner {
		return append(equalities(info, b.X, target, op), equalities(info, b.Y, target, op)...)
	}
	if b.Op != op {
		return nil
	}
	for _, pair := range [][2]ast.Expr{{b.X, b.Y}, {b.Y, b.X}} {
		if types.ExprString(pair[0]) == target {
			if name, ok := stateConst(info, pair[1]); ok {
				return []string{name}
			}
		}
	}
	return nil
}

func stateConst(info *types.Info, e ast.Expr) (string, bool) {
	if !isConst(info, e) {
		return "", false
	}
	switch x := ast.Unparen(e).(type) {
	case *ast.Ident:
		return x.Name, true
	case *ast.SelectorExpr:
		return x.Sel.Name, true
	}
	return "", false
}

func returns(body *ast.BlockStmt) bool {
	if body == nil || len(body.List) == 0 {
		return false
	}
	_, ok := body.List[len(body.List)-1].(*ast.ReturnStmt)
	return ok
}

func dedupe(list []string) []string {
	var out []string
	for _, s := range list {
		out = appendUnique(out, s)
	}
	sort.Strings(out)
	return out
}

func (p *Program) linkStateWrites(ms []fact.StateMachine) {
	for _, m := range ms {
		for _, w := range m.Writes {
			if f := p.funcs[w.In.ID()]; f != nil && w.To != fact.DynamicState {
				f.Facts.WritesStatus = appendUnique(f.Facts.WritesStatus, w.To)
			}
		}
	}
}

func (p *Program) stateFindings() []fact.Finding {
	var out []fact.Finding
	for _, m := range p.states {
		dynamic := false
		for _, w := range m.Writes {
			dynamic = dynamic || w.To == fact.DynamicState
		}
		pkg, _ := splitQualified(m.Type)
		for _, st := range m.NeverAssigned {
			decl := p.constDecl(m.Type, st)
			f := fact.Finding{
				ID: "STATE-NEVER-SET", Severity: fact.SevMedium, Basis: fact.Proved,
				Title:  m.Type + "." + st + " is declared but nothing in this module produces it",
				Detail: "No assignment, struct literal, return statement or typed call argument in the scanned packages produces this state. Either it is dead, or something outside this code — a migration, a manual fix, another service — puts rows into it. If the second, every read path has to handle a state no write path here produces, and no test covers that.",
				Ref:    fact.CodeRef{Pkg: pkg, Symbol: "const " + st, File: decl.File, Line: decl.Line},
				Line:   decl.Line,
			}
			if dynamic {
				f.Basis = fact.Heuristic
				f.Detail += " Some writes of this type are computed at run time, so one of them may produce it."
			}
			out = append(out, f)
		}
	}
	return out
}

func (p *Program) constDecl(typ, name string) fact.Pos {
	pkgPath, _ := splitQualified(typ)
	for _, pkg := range p.prod {
		if pkg.PkgPath != pkgPath || pkg.Types == nil {
			continue
		}
		if c := pkg.Types.Scope().Lookup(name); c != nil {
			return p.pos(c.Pos())
		}
	}
	return fact.Pos{}
}

func splitQualified(q string) (pkg, name string) {
	for i := len(q) - 1; i >= 0; i-- {
		if q[i] == '.' {
			return q[:i], q[i+1:]
		}
	}
	return q, ""
}
