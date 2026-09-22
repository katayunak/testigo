package agent

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/katayunak/testigo/internal/agent/domain"
)

func TestApplyCaseBuildsVetsRunsAndReportsPassFail(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "go.mod"), []byte("module scratch\n\ngo 1.24\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, "pkg"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "pkg", "pkg.go"), []byte("package pkg\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	pass := &domain.CaseAnswer{CaseID: "X-PASS", Status: "written", FuncName: "TestXPass"}
	pass.File.Path = "pkg/pass_test.go"
	pass.File.Package = "pkg"
	pass.File.Content = "package pkg\n\nimport \"testing\"\n\nfunc TestXPass(t *testing.T) {}\n"

	run, err := ApplyCase(root, pass)
	if err != nil {
		t.Fatal(err)
	}
	if run.Status != "written" || !run.Green() {
		t.Fatalf("expected a written, passing run, got status=%q passed=%v output=%q", run.Status, run.Passed, run.Output)
	}

	fail := &domain.CaseAnswer{CaseID: "X-FAIL", Status: "written", FuncName: "TestXFail"}
	fail.File.Path = "pkg/fail_test.go"
	fail.File.Package = "pkg"
	fail.File.Content = "package pkg\n\nimport \"testing\"\n\nfunc TestXFail(t *testing.T) { t.Fatal(\"nope\") }\n"

	run2, err := ApplyCase(root, fail)
	if err != nil {
		t.Fatal(err)
	}
	if !run2.Red() {
		t.Fatalf("expected a failing run, got status=%q passed=%v", run2.Status, run2.Passed)
	}
	if run2.Output == "" {
		t.Error("a failed run must carry its output — that is the one thing an agent needs to fix and resubmit")
	}
}

func TestApplyCaseReportsNoBuildWhenThePackageItselfWontCompile(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "go.mod"), []byte("module scratch\n\ngo 1.24\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, "pkg"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "pkg", "pkg.go"), []byte("package pkg\n\nfunc this is not go {\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	broken := &domain.CaseAnswer{CaseID: "X-BROKEN", Status: "written", FuncName: "TestBroken"}
	broken.File.Path = "pkg/broken_test.go"
	broken.File.Package = "pkg"
	broken.File.Content = "package pkg\n\nimport \"testing\"\n\nfunc TestBroken(t *testing.T) {}\n"

	run, err := ApplyCase(root, broken)
	if err != nil {
		t.Fatal(err)
	}
	if run.Status != "no_build" {
		t.Fatalf("expected no_build, got %q", run.Status)
	}
	if run.Passed != nil {
		t.Error("a case that never compiled was never run — passed must stay unset, not false")
	}
	if run.Output == "" {
		t.Error("a no_build result must carry the compiler error, or there is nothing to fix from")
	}
}

func TestApplyCasePassesThroughABlockedAnswerWithoutTouchingDisk(t *testing.T) {
	root := t.TempDir()
	blocked := &domain.CaseAnswer{CaseID: "X-BLOCKED", Status: "blocked", BlockedReason: "the seam is called on a concrete type"}

	run, err := ApplyCase(root, blocked)
	if err != nil {
		t.Fatal(err)
	}
	if run.Status != "blocked" || run.Reason != "the seam is called on a concrete type" {
		t.Fatalf("expected the blocked reason to pass through unchanged, got %+v", run)
	}
	entries, _ := os.ReadDir(root)
	if len(entries) != 0 {
		t.Errorf("a blocked case should write nothing, found: %v", entries)
	}
}
