package codeRef

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

func hashOf(t *testing.T, src string) string {
	t.Helper()
	h, _ := StructuralHash(parseFunc(t, src))
	return h
}

func TestHashIgnoresCommentsAndFormatting(t *testing.T) {
	base := `func Debit(acct string, cents int64) error {
	if cents <= 0 { return errBadAmount }
	return ledger.Post(acct, -cents)
}`
	variants := map[string]string{
		"doc comment": `// Debit removes money.
// It is the second step of the flow.
func Debit(acct string, cents int64) error {
	if cents <= 0 { return errBadAmount }
	return ledger.Post(acct, -cents)
}`,
		"inline comments": `func Debit(acct string, cents int64) error {
	// testigo: step 2 of 5 — funds leave the payer
	if cents <= 0 { return errBadAmount } // guard
	return ledger.Post(acct, -cents)
}`,
		"reformatted": `func Debit(acct string,cents int64)error{
if cents<=0{
return errBadAmount}


	return ledger.Post( acct , -cents )
}`,
	}
	want := hashOf(t, base)
	for name, src := range variants {
		if got := hashOf(t, src); got != want {
			t.Errorf("%s: hash changed (%s != %s); notes would be needlessly invalidated", name, got[:12], want[:12])
		}
	}
}

func TestHashDetectsBehaviourChange(t *testing.T) {
	base := `func Debit(acct string, cents int64) error {
	if cents <= 0 { return errBadAmount }
	return ledger.Post(acct, -cents)
}`
	changes := map[string]string{
		"operator flipped": `func Debit(acct string, cents int64) error {
	if cents < 0 { return errBadAmount }
	return ledger.Post(acct, -cents)
}`,
		"sign dropped": `func Debit(acct string, cents int64) error {
	if cents <= 0 { return errBadAmount }
	return ledger.Post(acct, cents)
}`,
		"guard removed": `func Debit(acct string, cents int64) error {
	return ledger.Post(acct, -cents)
}`,
		"param type widened": `func Debit(acct string, cents float64) error {
	if cents <= 0 { return errBadAmount }
	return ledger.Post(acct, -cents)
}`,
		"call target changed": `func Debit(acct string, cents int64) error {
	if cents <= 0 { return errBadAmount }
	return ledger.Insert(acct, -cents)
}`,
	}
	base_h := hashOf(t, base)
	for name, src := range changes {
		if hashOf(t, src) == base_h {
			t.Errorf("%s: hash unchanged — a mutated function would look fresh", name)
		}
	}
}

func TestResolveMovedOnRename(t *testing.T) {
	body := `{
	if cents <= 0 { return errBadAmount }
	row, err := tx.QueryContext(ctx, "select balance from accounts where id=$1", acct)
	if err != nil { return err }
	defer row.Close()
	return ledger.Post(ctx, acct, -cents)
}`
	old := parseFunc(t, "func Debit(acct string, cents int64) error "+body)
	renamed := parseFunc(t, "func Withdraw(acct string, cents int64) error "+body)

	saved := CodeRef{Pkg: "pay/ledger", Symbol: Symbol(old)}
	saved.BodyHash, _ = StructuralHash(old)

	ix := NewIndex()
	cur := CodeRef{Pkg: "pay/ledger", Symbol: Symbol(renamed)}
	var size int
	cur.BodyHash, size = StructuralHash(renamed)
	ix.Add(cur, size)

	got, res := Resolve(ix, saved)
	if res != Moved {
		t.Fatalf("want Moved, got %s", res)
	}
	if got.Symbol != "Withdraw" {
		t.Fatalf("anchor not rewritten: %s", got.Symbol)
	}
}

func TestResolveOutcomes(t *testing.T) {
	fn := parseFunc(t, `func Reserve(id string) error {
	if id == "" { return errNoID }
	return storage.Lock(id)
}`)
	a := CodeRef{Pkg: "pay", Symbol: Symbol(fn)}
	var size int
	a.BodyHash, size = StructuralHash(fn)

	t.Run("fresh", func(t *testing.T) {
		ix := NewIndex()
		ix.Add(a, size)
		if _, res := Resolve(ix, a); res != Fresh {
			t.Fatalf("want Fresh, got %s", res)
		}
	})

	t.Run("stale", func(t *testing.T) {
		ix := NewIndex()
		changed := a
		changed.BodyHash = "deadbeef"
		ix.Add(changed, size)
		if _, res := Resolve(ix, a); res != Stale {
			t.Fatalf("want Stale, got %s", res)
		}
	})

	t.Run("orphaned", func(t *testing.T) {
		if _, res := Resolve(NewIndex(), a); res != Orphaned {
			t.Fatalf("want Orphaned, got %s", res)
		}
	})
}

func TestTinyBodiesAreNotMoveCandidates(t *testing.T) {
	oldFn := parseFunc(t, `func ID() string { return s.id }`)
	newFn := parseFunc(t, `func Ref() string { return s.id }`)

	saved := CodeRef{Pkg: "pay", Symbol: Symbol(oldFn)}
	saved.BodyHash, _ = StructuralHash(oldFn)

	ix := NewIndex()
	cur := CodeRef{Pkg: "pay", Symbol: Symbol(newFn)}
	var size int
	cur.BodyHash, size = StructuralHash(newFn)
	ix.Add(cur, size)

	if _, res := Resolve(ix, saved); res != Orphaned {
		t.Fatalf("want Orphaned for a trivial body, got %s", res)
	}
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
