package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/katayunak/testigo/internal/config"
)

func copiedFixture(t *testing.T, name string) string {
	t.Helper()
	root := t.TempDir()
	if err := os.CopyFS(root, os.DirFS(filepath.Join("..", "..", "testdata", name))); err != nil {
		t.Fatal(err)
	}
	return root
}

func scannedFixture(t *testing.T, name string) string {
	t.Helper()
	root := copiedFixture(t, name)
	if err := run([]string{"scan", root}); err != nil {
		t.Fatalf("scan %s: %v", name, err)
	}
	return root
}

func TestEntryAddWithoutADirectoryUsesTheCurrentOne(t *testing.T) {
	root := t.TempDir()
	if err := run([]string{"init", root}); err != nil {
		t.Fatal(err)
	}
	t.Chdir(root)

	if err := run([]string{"entry", "add", "example.com/x/api#(*S).Do", "API"}); err != nil {
		t.Fatalf("entry add with no directory: %v", err)
	}

	cfg, err := config.Load(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.Entries) != 1 || cfg.Entries[0].Symbol != "(*S).Do" || cfg.Entries[0].Label != "API" {
		t.Fatalf("entries = %+v, want the one just added", cfg.Entries)
	}
}

func TestEntryAddWithADirectory(t *testing.T) {
	root := t.TempDir()
	if err := run([]string{"init", root}); err != nil {
		t.Fatal(err)
	}

	if err := run([]string{"entry", root, "add", "example.com/x/api#(*S).Do", "API", "create"}); err != nil {
		t.Fatal(err)
	}
	if err := run([]string{"entry", root, "add", "example.com/x/api#(*S).Do"}); err == nil {
		t.Fatal("adding the same entry twice was accepted")
	}

	cfg, err := config.Load(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.Entries) != 1 || cfg.Entries[0].Label != "API create" {
		t.Fatalf("entries = %+v, want one entry labelled %q", cfg.Entries, "API create")
	}
}

func TestEntryRejectsASpecWithoutAHash(t *testing.T) {
	root := t.TempDir()
	if err := run([]string{"init", root}); err != nil {
		t.Fatal(err)
	}
	err := run([]string{"entry", root, "add", "example.com/x/api.Do"})
	if err == nil || !strings.Contains(err.Error(), "<import-path>#<Symbol>") {
		t.Fatalf("err = %v, want the <import-path>#<Symbol> hint", err)
	}
}

func TestScanRefusesAnEmptyEntryList(t *testing.T) {
	root := t.TempDir()
	if err := run([]string{"init", root}); err != nil {
		t.Fatal(err)
	}
	if err := run([]string{"scan", root}); err == nil {
		t.Fatal("scan with no entry points succeeded; a guessed or empty entry list must be an error")
	}
}

func TestScanOfTheBundledFixturesFindsTheirPlantedDefects(t *testing.T) {
	for _, tc := range []struct {
		fixture string
		want    []string
	}{
		{"paysvc", []string{"IDEM-KEY-NOT-UNIQUE", "MONEY-FLOAT", "TX-NET-CALL"}},
		{"walletsvc", nil},
	} {
		t.Run(tc.fixture, func(t *testing.T) {
			root := scannedFixture(t, tc.fixture)
			flow, err := config.LoadFlow(root)
			if err != nil {
				t.Fatal(err)
			}
			if len(flow.Entries) == 0 || len(flow.Nodes) == 0 {
				t.Fatalf("flow has %d entries and %d nodes, want both non-zero", len(flow.Entries), len(flow.Nodes))
			}
			found := map[string]bool{}
			for _, f := range flow.Findings {
				found[f.ID] = true
				if f.Ref.File == "" && f.Ref.Pkg == "" {
					t.Errorf("finding %s carries no file or package", f.ID)
				}
			}
			for _, id := range tc.want {
				if !found[id] {
					t.Errorf("finding %s is planted in %s but was not reported", id, tc.fixture)
				}
			}
		})
	}
}

func TestRoundTwoAskIsRefusedBeforeRoundOne(t *testing.T) {
	root := scannedFixture(t, "paysvc")
	err := run([]string{"ask", "--round", "2", root})
	if err == nil || !strings.Contains(err.Error(), "round 2 needs round 1 first") {
		t.Fatalf("err = %v, want round 2 refused until round 1 is answered", err)
	}
}

func TestEveryReadOnlyCommandRunsOnAScannedFixture(t *testing.T) {
	root := scannedFixture(t, "paysvc")
	for _, args := range [][]string{
		{"flow", root},
		{"ask", "--explain", "--round", "1", root},
		{"collect", root},
		{"cases", root},
		{"report", root},
		{"report", "--ask", root},
	} {
		if err := run(args); err != nil {
			t.Errorf("testigo %s: %v", strings.Join(args[:len(args)-1], " "), err)
		}
	}
	if _, err := os.Stat(filepath.Join(config.Dir(root), "asks", "report.md")); err != nil {
		t.Errorf("report --ask did not write report.md: %v", err)
	}
}

func TestUnknownCommandIsAnError(t *testing.T) {
	if err := run([]string{"nope"}); err == nil {
		t.Fatal("an unknown command succeeded")
	}
}

func TestFailedNamesReadsGoTestOutput(t *testing.T) {
	out := "=== RUN   TestA\n--- FAIL: TestA (0.00s)\n=== RUN   TestB\n--- PASS: TestB (0.00s)\n    --- FAIL: TestC/sub (0.01s)\n"
	got := failedNames(out)
	if !got["TestA"] || !got["TestC/sub"] || got["TestB"] {
		t.Fatalf("failedNames = %v, want TestA and TestC/sub only", got)
	}
}
