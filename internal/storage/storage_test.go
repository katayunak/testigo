package storage

import (
	"os"
	"testing"

	"github.com/katayunak/testigo/internal/codeRef"
	"github.com/katayunak/testigo/internal/scanningFlow/flowEntity"
)

func node(pkg, sym, hash, step string) *flowEntity.Node {
	n := &flowEntity.Node{Ref: codeRef.CodeRef{Pkg: pkg, Symbol: sym, BodyHash: hash}}
	if step != "" {
		n.Notes = &flowEntity.Notes{Step: step, ForHash: hash}
	}
	return n
}

func flowOf(ns ...*flowEntity.Node) *flowEntity.Flow {
	f := flowEntity.NewFlow("example.com/pay")
	for _, n := range ns {
		f.Nodes[n.Ref.ID()] = n
	}
	return f
}

func indexOf(ns ...*flowEntity.Node) *codeRef.Index {
	ix := codeRef.NewIndex()
	for _, n := range ns {
		ix.Add(n.Ref, 500) // large enough to be move-eligible
	}
	return ix
}

// The economics of the whole design come down to this test. If a repeat scanningFlow on
// an unchanged repo re-asks an agent about every function, testigo costs money
// every run and nobody will run it in CI.
func TestUnchangedRepoNeedsNoAgent(t *testing.T) {
	prev := flowOf(
		node("pay/api", "(*S).Create", "h1", "accept request"),
		node("pay/api", "(*S).process", "h2", "reserve funds"),
	)
	next := flowOf(
		node("pay/api", "(*S).Create", "h1", ""),
		node("pay/api", "(*S).process", "h2", ""),
	)
	st := Merge(prev, next, indexOf(next.Nodes["pay/api#(*S).Create"], next.Nodes["pay/api#(*S).process"]))
	if st.Fresh != 2 || st.NeedsAgent() != 0 {
		t.Fatalf("unchanged repo should cost nothing: %s", st)
	}
	if next.Nodes["pay/api#(*S).process"].Notes.Step != "reserve funds" {
		t.Error("notes did not carry over")
	}
}

// A changed function must NOT keep its old notes. This is the safety half of
// the trade: a note that describes the previous body is not evidence about the
// new one, and a plausible-but-wrong description is more dangerous in a report
// than an admitted gap.
func TestChangedFunctionDropsItsNotes(t *testing.T) {
	prev := flowOf(node("pay/api", "(*S).process", "h2", "reserve funds"))
	changed := node("pay/api", "(*S).process", "h2-EDITED", "")
	next := flowOf(changed)

	st := Merge(prev, next, indexOf(changed))
	if st.Stale != 1 {
		t.Fatalf("want 1 stale, got %s", st)
	}
	if changed.Notes != nil {
		t.Error("stale node kept notes describing code that no longer exists")
	}
	if st.NeedsAgent() != 1 {
		t.Errorf("want 1 node needing an agent, got %d", st.NeedsAgent())
	}
}

// A rename keeps the notes and rewrites the codeRef.
func TestRenameCarriesNotesForward(t *testing.T) {
	prev := flowOf(node("pay/api", "(*S).process", "h2", "reserve funds"))
	renamed := node("pay/api", "(*S).handle", "h2", "")
	next := flowOf(renamed)

	st := Merge(prev, next, indexOf(renamed))
	if st.Moved != 1 {
		t.Fatalf("want 1 moved, got %s", st)
	}
	if renamed.Notes == nil || renamed.Notes.Step != "reserve funds" {
		t.Error("notes lost across a pure rename")
	}
}

// Deleted code must not silently take its notes with it. A human should see
// that a step named "reverse the hold" no longer exists anywhere.
func TestDeletedCodeIsQuarantinedNotDropped(t *testing.T) {
	prev := flowOf(node("pay/api", "(*S).compensate", "h9", "reverse the hold"))
	next := flowOf()

	st := Merge(prev, next, indexOf())
	if st.Orphaned != 1 {
		t.Fatalf("want 1 orphaned, got %s", st)
	}
	if len(next.Orphans) != 1 || next.Orphans[0].Notes.Step != "reverse the hold" {
		t.Fatal("orphaned notes were discarded instead of quarantined")
	}
}

func TestSaveLoadRoundTrip(t *testing.T) {
	dir := t.TempDir()
	f := flowOf(node("pay/api", "(*S).Create", "h1", "accept request"))
	f.Seams = []flowEntity.Seam{{Kind: flowEntity.SeamDB, Target: "(*sql.DB).Exec", Injectable: false}}

	if err := Save(dir, f); err != nil {
		t.Fatal(err)
	}
	got, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Nodes) != 1 || got.Nodes["pay/api#(*S).Create"].Notes.Step != "accept request" {
		t.Error("round trip lost data")
	}
	if len(got.Seams) != 1 || got.Seams[0].Injectable {
		t.Error("seam round trip wrong")
	}
}

// A sidecar written by a newer binary must be refused, not misread. Silently
// misinterpreting it would produce a confidently wrong report, which is the one
// failure mode a testing tool cannot afford.
func TestSchemaMismatchIsRefused(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(Dir(dir), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(Path(dir), []byte(`{"schema":9999,"nodes":{}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(dir); err == nil {
		t.Fatal("a future schema version should be an error, not a silent misread")
	}
}

func TestFirstRunHasNoSidecar(t *testing.T) {
	if _, err := Load(t.TempDir()); err != ErrNoSidecar {
		t.Fatalf("want ErrNoSidecar, got %v", err)
	}
}
