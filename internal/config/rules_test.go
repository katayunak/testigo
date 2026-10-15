package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestTheStarterRulesFileChangesNothingUntilEdited(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(Dir(root), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(Dir(root), RulesFileName), []byte(RulesExample), 0o644); err != nil {
		t.Fatal(err)
	}

	r, err := LoadRules(root)
	if err != nil {
		t.Fatalf("the starter rules file does not parse: %v", err)
	}
	if len(r.MoneyMovement.Symbols) != 0 {
		t.Errorf("the starter names a transfer function nobody chose: %v", r.MoneyMovement.Symbols)
	}
	if len(r.Skip) != 0 {
		t.Errorf("the starter turns off scenarios nobody chose to skip: %v", r.Skip)
	}
}

func TestRulesFileWithCommentsIsRead(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(Dir(root), 0o755); err != nil {
		t.Fatal(err)
	}
	body := `{
  // a comment line
  "money_movement": { "symbols": ["example.com/x/ledger#(*Ledger).Post"] },
  "skip": [ { "scenario": "minor-unit-conversion" } ]
}`
	if err := os.WriteFile(filepath.Join(Dir(root), RulesFileName), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}

	r, err := LoadRules(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(r.MoneyMovement.Symbols) != 1 {
		t.Errorf("symbols = %v, want the one declared", r.MoneyMovement.Symbols)
	}
	why, skipped := r.Skipped("MINOR-UNIT-CONVERSION")
	if !skipped || why == "" {
		t.Errorf("Skipped = %q, %v; want the scenario skipped, case-insensitively, with a default reason", why, skipped)
	}
}

func TestNoRulesFileMeansNoRules(t *testing.T) {
	r, err := LoadRules(t.TempDir())
	if err != nil || r != nil {
		t.Fatalf("LoadRules on a repository without rules.json = %v, %v; want nil, nil", r, err)
	}
	if _, skipped := r.Skipped("IDEM-REPLAY"); skipped {
		t.Error("a nil rules value skipped a scenario")
	}
}
