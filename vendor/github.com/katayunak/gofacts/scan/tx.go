package scan

import (
	"go/token"
	"go/types"
	"sort"

	"golang.org/x/tools/go/ssa"

	"github.com/katayunak/gofacts/fact"
)

// transactions finds every transaction the module opens and computes, on the
// SSA control-flow graph, what happens while it is open. testigo recorded
// three booleans per function — opens, commits, rolls back — so a network call
// after the commit counted as inside the transaction, a call two helpers deep
// did not count at all, and a return between begin and commit was invisible.
func (p *Program) transactions() []fact.Tx {
	p.txLines = map[string]map[int]bool{}
	ids := make([]string, 0, len(p.canon))
	for id := range p.canon {
		ids = append(ids, id)
	}
	sort.Strings(ids)

	var out []fact.Tx
	for _, id := range ids {
		ref := p.funcs[id].Ref
		withClosures(p.canon[id], func(f *ssa.Function) {
			for _, b := range f.Blocks {
				for i, instr := range b.Instrs {
					call, ok := instr.(*ssa.Call)
					if !ok {
						continue
					}
					name, db := txCallee(call.Common())
					switch {
					case db && txOpen[name]:
						out = append(out, p.beginTx(id, ref, f, b, i, call))
					case db && txRunWrapper[name]:
						if tx, ok := p.wrapperTx(id, ref, call); ok {
							out = append(out, tx)
						}
					}
				}
			}
		})
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Begin.Before(out[j].Begin) })
	return out
}

func txCallee(c *ssa.CallCommon) (string, bool) {
	if c.IsInvoke() {
		return c.Method.Name(), isDBPackage(ifacePkg(c.Method))
	}
	if callee := c.StaticCallee(); callee != nil {
		return callee.Name(), isDBPackage(pkgPathOf(callee))
	}
	return "", false
}

func target(c *ssa.CallCommon) string {
	if c.IsInvoke() {
		return c.Method.FullName()
	}
	if callee := c.StaticCallee(); callee != nil {
		return callee.String()
	}
	return ""
}

// aliases is every SSA value that is the transaction: the begin result, loads
// of the variable it was stored in when a closure captures it, and values
// derived from it (rows, statements) whose calls also run on it.
type aliases struct {
	tx      map[ssa.Value]bool
	derived map[ssa.Value]bool
}

func (a aliases) on(v ssa.Value) bool { return a.tx[v] || a.derived[v] }

func newAliases(root ssa.Value) aliases {
	a := aliases{tx: map[ssa.Value]bool{}, derived: map[ssa.Value]bool{}}
	work := []ssa.Value{root}
	addLoads := func(addr ssa.Value) {
		for _, r := range referrers(addr) {
			switch r := r.(type) {
			case *ssa.UnOp:
				if r.Op == token.MUL && r.X == addr {
					work = append(work, r)
				}
			case *ssa.MakeClosure:
				for i, b := range r.Bindings {
					fn, ok := r.Fn.(*ssa.Function)
					if b == addr && ok && i < len(fn.FreeVars) {
						for _, fr := range referrers(fn.FreeVars[i]) {
							if u, ok := fr.(*ssa.UnOp); ok && u.Op == token.MUL {
								work = append(work, u)
							}
						}
					}
				}
			}
		}
	}
	for len(work) > 0 {
		v := work[len(work)-1]
		work = work[:len(work)-1]
		if a.tx[v] {
			continue
		}
		a.tx[v] = true
		for _, r := range referrers(v) {
			switch r := r.(type) {
			case *ssa.Store:
				if r.Val == v {
					if _, ok := r.Addr.(*ssa.Alloc); ok {
						addLoads(r.Addr)
					}
				}
			case *ssa.Phi, *ssa.ChangeType:
				work = append(work, r.(ssa.Value))
			}
		}
	}
	return a
}

func referrers(v ssa.Value) []ssa.Instruction {
	if v == nil {
		return nil
	}
	if r := v.Referrers(); r != nil {
		return *r
	}
	return nil
}

// receiver is the value a method call runs on.
func receiver(c *ssa.CallCommon) ssa.Value {
	if c.IsInvoke() {
		return c.Value
	}
	if callee := c.StaticCallee(); callee != nil && callee.Signature.Recv() != nil && len(c.Args) > 0 {
		return c.Args[0]
	}
	return nil
}

type endKind int

const (
	notEnd endKind = iota
	ends
	rollsBack
)

func endOf(c *ssa.CallCommon, a aliases) endKind {
	recv := receiver(c)
	if recv == nil || !a.tx[recv] {
		return notEnd
	}
	t := target(c)
	switch methodName(t) {
	case "Commit":
		return ends
	case "Rollback":
		return rollsBack
	case "Close":
		if closesATransaction(t) {
			return rollsBack
		}
	}
	return notEnd
}

type txScan struct {
	p     *Program
	fnID  string
	tx    *fact.Tx
	a     aliases
	err   ssa.Value
	lines map[int]bool
	// closures that end the transaction when called, and those deferred
	enders   map[*ssa.Function]endKind
	deferred map[*ssa.Function]bool
}

func (p *Program) beginTx(fnID string, ref fact.CodeRef, f *ssa.Function, b *ssa.BasicBlock, idx int, call *ssa.Call) fact.Tx {
	tx := &fact.Tx{In: ref, Begin: p.pos(call.Pos()), Via: target(call.Common())}
	txVal, errVal := splitResult(call)
	s := &txScan{
		p: p, fnID: fnID, tx: tx, a: newAliases(txVal), err: errVal, lines: map[int]bool{},
		enders: map[*ssa.Function]endKind{}, deferred: map[*ssa.Function]bool{},
	}
	s.uses(f)
	s.window(b, idx)
	s.record()
	return *tx
}

func splitResult(call *ssa.Call) (txVal, errVal ssa.Value) {
	if _, tuple := call.Type().(*types.Tuple); !tuple {
		return call, nil
	}
	for _, r := range referrers(call) {
		if ex, ok := r.(*ssa.Extract); ok {
			switch ex.Index {
			case 0:
				txVal = ex
			case 1:
				errVal = ex
			}
		}
	}
	return txVal, errVal
}

// uses classifies every use of the transaction in the function and the
// closures that capture it: statements run on it, ends, escapes.
func (s *txScan) uses(f *ssa.Function) {
	closureOf := map[*ssa.Function]*ssa.MakeClosure{}
	withClosures(f, func(fn *ssa.Function) {
		for _, b := range fn.Blocks {
			for _, instr := range b.Instrs {
				if mc, ok := instr.(*ssa.MakeClosure); ok {
					if cf, ok := mc.Fn.(*ssa.Function); ok {
						closureOf[cf] = mc
					}
				}
			}
		}
	})
	for cf, mc := range closureOf {
		for _, r := range referrers(mc) {
			if d, ok := r.(*ssa.Defer); ok && d.Call.Value == mc {
				s.deferred[cf] = true
			}
		}
	}

	withClosures(f, func(fn *ssa.Function) {
		for _, b := range fn.Blocks {
			for _, instr := range b.Instrs {
				s.use(fn, instr)
			}
		}
	})
}

func (s *txScan) use(fn *ssa.Function, instr ssa.Instruction) {
	switch in := instr.(type) {
	case ssa.CallInstruction:
		c := in.Common()
		_, isDefer := instr.(*ssa.Defer)
		switch end := endOf(c, s.a); {
		case end != notEnd:
			at := s.p.pos(instr.Pos())
			if end == ends {
				s.tx.Commits = append(s.tx.Commits, at)
			} else {
				s.tx.Rollbacks = append(s.tx.Rollbacks, at)
				if isDefer || s.deferred[fn] {
					s.tx.DeferredRollback = true
				}
			}
			if fn.Parent() != nil && !s.deferred[fn] {
				s.enders[fn] = max(s.enders[fn], end)
			}
			return
		case receiver(c) != nil && s.a.on(receiver(c)):
			s.tx.Used = true
			if call, ok := instr.(*ssa.Call); ok {
				s.derive(call)
			}
			return
		}
		for _, arg := range c.Args {
			if s.a.tx[arg] {
				s.tx.Used = true
				s.tx.Escapes = s.tx.Escapes || s.mayEnd(c)
			}
		}
		if c.IsInvoke() && s.a.tx[c.Value] {
			s.tx.Used = true
		}
	case *ssa.Return:
		for _, r := range in.Results {
			if s.a.tx[r] {
				s.tx.Escapes, s.tx.Used = true, true
			}
		}
	case *ssa.Store:
		if s.a.tx[in.Val] {
			if _, local := in.Addr.(*ssa.Alloc); !local {
				s.tx.Escapes, s.tx.Used = true, true
			}
		}
	case *ssa.MakeInterface:
		if s.a.tx[in.X] {
			s.tx.Escapes, s.tx.Used = true, true
		}
	case *ssa.MakeClosure:
		cf, _ := in.Fn.(*ssa.Function)
		if cf == nil || s.deferred[cf] || !captures(in, s.a) {
			return
		}
		if !calledDirectly(in) {
			s.tx.Escapes, s.tx.Used = true, true
		}
	}
}

func (s *txScan) derive(call *ssa.Call) {
	s.a.derived[call] = true
	for _, r := range referrers(call) {
		if ex, ok := r.(*ssa.Extract); ok {
			s.a.derived[ex] = true
		}
	}
}

func captures(mc *ssa.MakeClosure, a aliases) bool {
	for _, b := range mc.Bindings {
		for _, r := range referrers(b) {
			if u, ok := r.(*ssa.UnOp); ok && a.tx[u] {
				return true
			}
		}
		if a.tx[b] {
			return true
		}
	}
	return false
}

func calledDirectly(mc *ssa.MakeClosure) bool {
	for _, r := range referrers(mc) {
		c, ok := r.(*ssa.Call)
		if !ok || c.Call.Value != mc {
			return false
		}
	}
	return true
}

type cursor struct {
	b *ssa.BasicBlock
	i int
}

// window walks forward from the begin. A path stops at the first commit or
// rollback on the transaction, or a call to a closure that performs one. The
// branch taken when begin itself failed is not walked: there is nothing to
// roll back on it.
func (s *txScan) window(b *ssa.BasicBlock, idx int) {
	seen := map[*ssa.BasicBlock]bool{b: true}
	queue := []cursor{{b, idx + 1}}
	for len(queue) > 0 {
		cur := queue[0]
		queue = queue[1:]
		if s.walkBlock(cur) {
			continue
		}
		for i, succ := range cur.b.Succs {
			if s.beginFailed(cur.b, i) || seen[succ] {
				continue
			}
			seen[succ] = true
			queue = append(queue, cursor{succ, 0})
		}
	}
}

// walkBlock visits a block from cur.i and reports whether the transaction
// ended inside it.
func (s *txScan) walkBlock(cur cursor) bool {
	for _, instr := range cur.b.Instrs[cur.i:] {
		if pos := instr.Pos(); pos.IsValid() {
			s.lines[s.p.line(pos)] = true
		}
		switch in := instr.(type) {
		case *ssa.Return:
			s.tx.Leaks = append(s.tx.Leaks, s.returnPos(in))
		case *ssa.Call:
			if endOf(in.Common(), s.a) != notEnd || s.endsByClosure(in.Common()) {
				return true
			}
			s.effect(in)
		}
	}
	return false
}

func (s *txScan) endsByClosure(c *ssa.CallCommon) bool {
	var fn *ssa.Function
	switch v := c.Value.(type) {
	case *ssa.MakeClosure:
		fn, _ = v.Fn.(*ssa.Function)
	case *ssa.Function:
		fn = v
	}
	return fn != nil && s.enders[fn] != notEnd
}

func (s *txScan) returnPos(r *ssa.Return) fact.Pos {
	if r.Pos().IsValid() {
		return s.p.pos(r.Pos())
	}
	if decl := r.Parent().Syntax(); decl != nil {
		return s.p.pos(decl.End())
	}
	return s.tx.Begin
}

// beginFailed reports whether successor i of b is the branch where the begin
// returned an error (or gorm's tx.Error is set).
func (s *txScan) beginFailed(b *ssa.BasicBlock, i int) bool {
	if len(b.Instrs) == 0 {
		return false
	}
	cond, ok := b.Instrs[len(b.Instrs)-1].(*ssa.If)
	if !ok {
		return false
	}
	bin, ok := cond.Cond.(*ssa.BinOp)
	if !ok || (bin.Op != token.NEQ && bin.Op != token.EQL) {
		return false
	}
	isErr := func(v ssa.Value) bool { return (s.err != nil && v == s.err) || s.errorField(v) }
	isNil := func(v ssa.Value) bool { c, ok := v.(*ssa.Const); return ok && c.IsNil() }
	if !(isErr(bin.X) && isNil(bin.Y) || isErr(bin.Y) && isNil(bin.X)) {
		return false
	}
	failed := 0
	if bin.Op == token.EQL {
		failed = 1
	}
	return i == failed
}

func (s *txScan) errorField(v ssa.Value) bool {
	var base ssa.Value
	var field int
	switch x := v.(type) {
	case *ssa.UnOp:
		fa, ok := x.X.(*ssa.FieldAddr)
		if !ok || x.Op != token.MUL {
			return false
		}
		base, field = fa.X, fa.Field
	case *ssa.Field:
		base, field = x.X, x.Field
	default:
		return false
	}
	if !s.a.tx[base] {
		return false
	}
	st, ok := structOf(base.Type())
	return ok && field < st.NumFields() && st.Field(field).Name() == "Error"
}

func structOf(t types.Type) (*types.Struct, bool) {
	t = types.Unalias(t)
	if ptr, ok := t.(*types.Pointer); ok {
		t = ptr.Elem()
	}
	st, ok := t.Underlying().(*types.Struct)
	return st, ok
}

// effect records a boundary crossing inside the window. Module functions are
// judged by what they can reach; whether they run on the transaction is judged
// by whether it is passed to them.
func (s *txScan) effect(call *ssa.Call) {
	c := call.Common()
	at := s.p.pos(call.Pos())
	if callee := c.StaticCallee(); callee != nil && s.p.isLocal(callee) && !c.IsInvoke() {
		var kinds kindSet
		withClosures(callee, func(f *ssa.Function) { kinds |= s.p.reach[f] })
		via := s.p.idOf[callee]
		if callee.Parent() != nil {
			via = ""
		}
		passes := s.passesTx(c)
		for _, k := range (kinds & ksNet).list() {
			s.add(fact.TxEffect{At: at, Kind: k, Target: via, Via: via, Basis: fact.Heuristic})
		}
		if kinds&ksDB != 0 {
			s.add(fact.TxEffect{At: at, Kind: fact.SeamDB, Target: via, Via: via, OnTx: passes, Bypasses: !passes, Basis: fact.Heuristic})
			if passes {
				s.tx.Used = true
			}
		}
		return
	}
	cl := s.p.classify(c, s.p.chaSites[call])
	if cl.target == "" {
		return
	}
	basis := fact.Proved
	if cl.injectable || cl.guessed {
		basis = fact.Heuristic
	}
	for _, k := range (cl.kinds & ksNet).list() {
		s.add(fact.TxEffect{At: at, Kind: k, Target: cl.target, Basis: basis})
	}
	if cl.kinds&ksDB != 0 && !s.passesTx(c) {
		s.add(fact.TxEffect{At: at, Kind: fact.SeamDB, Target: cl.target, Bypasses: true, Basis: basis})
	}
}

func (s *txScan) passesTx(c *ssa.CallCommon) bool {
	for _, arg := range c.Args {
		if s.a.on(arg) {
			return true
		}
	}
	return c.IsInvoke() && s.a.on(c.Value)
}

func (s *txScan) add(e fact.TxEffect) {
	for _, have := range s.tx.Inside {
		if have.At == e.At && have.Kind == e.Kind {
			return
		}
	}
	s.tx.Inside = append(s.tx.Inside, e)
}

func (s *txScan) record() {
	if s.tx.DeferredRollback || s.tx.Escapes {
		s.tx.Leaks = nil
	}
	s.tx.Leaks = uniquePos(s.tx.Leaks)
	lines := s.p.txLines[s.fnID]
	if lines == nil {
		lines = map[int]bool{}
		s.p.txLines[s.fnID] = lines
	}
	for l := range s.lines {
		lines[l] = true
	}
}

func uniquePos(ps []fact.Pos) []fact.Pos {
	seen := map[fact.Pos]bool{}
	var out []fact.Pos
	for _, p := range ps {
		if !seen[p] {
			seen[p] = true
			out = append(out, p)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Before(out[j]) })
	return out
}

// wrapperTx handles a library helper that owns the transaction: the callback
// runs inside it, on the transaction it is handed. Statements the callback
// runs on anything else are outside it — the classic
// db.Transaction(func(tx *gorm.DB) error { return db.Create(&x).Error }).
func (p *Program) wrapperTx(fnID string, ref fact.CodeRef, call *ssa.Call) (fact.Tx, bool) {
	var cb *ssa.Function
	for _, arg := range call.Call.Args {
		switch v := arg.(type) {
		case *ssa.MakeClosure:
			cb, _ = v.Fn.(*ssa.Function)
		case *ssa.Function:
			if v.Parent() != nil {
				cb = v
			}
		}
	}
	if cb == nil {
		return fact.Tx{}, false
	}
	tx := &fact.Tx{In: ref, Begin: p.pos(call.Pos()), Via: target(call.Common()), Wrapper: true}
	a := aliases{tx: map[ssa.Value]bool{}, derived: map[ssa.Value]bool{}}
	for _, param := range cb.Params {
		if n := namedOf(param.Type()); n != nil && n.Obj().Pkg() != nil && isDBPackage(n.Obj().Pkg().Path()) {
			a = newAliases(param)
			break
		}
	}
	s := &txScan{p: p, fnID: fnID, tx: tx, a: a, lines: map[int]bool{},
		enders: map[*ssa.Function]endKind{}, deferred: map[*ssa.Function]bool{}}
	for _, b := range cb.Blocks {
		for _, instr := range b.Instrs {
			if pos := instr.Pos(); pos.IsValid() {
				s.lines[p.line(pos)] = true
			}
			c, ok := instr.(*ssa.Call)
			if !ok {
				continue
			}
			if recv := receiver(c.Common()); recv != nil && a.on(recv) {
				tx.Used = true
				s.derive(c)
				continue
			}
			s.effect(c)
		}
	}
	tx.Used = tx.Used || len(a.tx) == 0
	s.record()
	return *tx, true
}

// mayEnd reports whether handing the transaction to this call can end it. A
// library function or a module helper with no commit or rollback of its own
// only borrows it; anything resolved at run time might keep it.
func (s *txScan) mayEnd(c *ssa.CallCommon) bool {
	callee := c.StaticCallee()
	if callee == nil {
		return true
	}
	if !s.p.isLocal(callee) {
		return false
	}
	f := s.p.funcs[s.p.idOf[callee]]
	return f == nil || f.Facts.CommitsTx || f.Facts.RollsBackTx
}
