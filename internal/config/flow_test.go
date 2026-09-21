package config

import (
	"os"
	"testing"

	"github.com/katayunak/testigo/internal/scanningFlow/flowEntity"
)

func TestSaveLoadRoundTrip(t *testing.T) {
	dir := t.TempDir()
	f := flowEntity.NewFlow("example.com/pay")
	n := &flowEntity.Node{Ref: flowEntity.CodeRef{Pkg: "pay/api", Symbol: "(*S).Create"}}
	f.Nodes[n.Ref.ID()] = n
	f.Seams = []flowEntity.Seam{{Kind: flowEntity.SeamDB, Target: "(*sql.DB).Exec", Injectable: false}}

	if err := SaveFlow(dir, f); err != nil {
		t.Fatal(err)
	}
	got, err := LoadFlow(dir)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := got.Nodes["pay/api#(*S).Create"]; !ok || len(got.Nodes) != 1 {
		t.Error("round trip lost the node")
	}
	if len(got.Seams) != 1 || got.Seams[0].Injectable {
		t.Error("seam round trip wrong")
	}
}

func TestSchemaMismatchIsRefused(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(Dir(dir), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(FlowPath(dir), []byte(`{"schema":9999,"nodes":{}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadFlow(dir); err == nil {
		t.Fatal("a future schema version should be an error, not a silent misread")
	}
}

func TestFirstRunHasNoSidecar(t *testing.T) {
	if _, err := LoadFlow(t.TempDir()); err != ErrNoSidecar {
		t.Fatalf("want ErrNoSidecar, got %v", err)
	}
}
