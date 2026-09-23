package agent

import (
	"strings"

	"github.com/katayunak/testigo/internal/agent/domain"
)

func ApplyCase(root string, ans *domain.CaseAnswer) (*domain.TestRun, error) {
	r := &domain.TestRun{CaseID: ans.CaseID, Func: ans.FuncName, ExpectedToFail: ans.ExpectedToFail}

	if !ans.Written() {
		r.Status = "blocked"
		r.Reason = ans.BlockedReason
		return r, nil
	}

	out, err := WriteTest(root, ans)
	if err != nil {
		r.Status = "refused"
		r.Reason = err.Error()
		return r, nil
	}
	r.File = out.Path

	switch {
	case !out.Compiles:
		r.Status = "no_build"
		r.Output = capOutput(out.Output)
		return r, nil
	case !out.Vets:
		r.Status = "vet_fail"
		r.Output = capOutput(out.Output)
		return r, nil
	}

	r.Status = "written"
	testOut, passed := RunTests(root, out.Path, []string{ans.FuncName})
	ok := passed
	r.Passed = &ok
	if !passed {
		r.Output = capOutput(testOut)
	}
	return r, nil
}

func capOutput(s string) string {
	s = strings.TrimSpace(s)
	if len(s) > 1200 {
		return s[:1200] + "\n… truncated"
	}
	return s
}
