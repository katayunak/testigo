package main

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/katayunak/testigo/internal/agent"
	"github.com/katayunak/testigo/internal/agent/domain"
	"github.com/katayunak/testigo/internal/config"
	"github.com/katayunak/testigo/internal/testPlan"
)

func connectMCP(t *testing.T, root string) *mcp.ClientSession {
	t.Helper()
	ctx := context.Background()
	clientSide, serverSide := mcp.NewInMemoryTransports()
	if _, err := newMCPServer(root).Connect(ctx, serverSide, nil); err != nil {
		t.Fatal(err)
	}
	cs, err := mcp.NewClient(&mcp.Implementation{Name: "testigo-test", Version: "0"}, nil).Connect(ctx, clientSide, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = cs.Close() })
	return cs
}

func callTool(t *testing.T, cs *mcp.ClientSession, name string, args any) (*mcp.CallToolResult, error) {
	t.Helper()
	return cs.CallTool(context.Background(), &mcp.CallToolParams{Name: name, Arguments: args})
}

func toolText(res *mcp.CallToolResult) string {
	var parts []string
	for _, c := range res.Content {
		if tc, ok := c.(*mcp.TextContent); ok {
			parts = append(parts, tc.Text)
		}
	}
	return strings.Join(parts, "\n")
}

func callOK[T any](t *testing.T, cs *mcp.ClientSession, name string, args any) T {
	t.Helper()
	res, err := callTool(t, cs, name, args)
	if err != nil {
		t.Fatalf("%s: %v", name, err)
	}
	if res.IsError {
		t.Fatalf("%s returned a tool error: %s", name, toolText(res))
	}
	raw, err := json.Marshal(res.StructuredContent)
	if err != nil {
		t.Fatal(err)
	}
	var out T
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatalf("%s: structured output %s does not fit %T: %v", name, raw, out, err)
	}
	return out
}

func callFails(t *testing.T, cs *mcp.ClientSession, name string, args any) string {
	t.Helper()
	res, err := callTool(t, cs, name, args)
	if err != nil {
		return err.Error()
	}
	if !res.IsError {
		t.Fatalf("%s succeeded, want a tool error", name)
	}
	return toolText(res)
}

func paysvcRoundOneAnswers(t *testing.T) map[string]json.RawMessage {
	t.Helper()
	raw, err := os.ReadFile("testdata/paysvc-round1.json")
	if err != nil {
		t.Fatal(err)
	}
	var byKey map[string]json.RawMessage
	if err := json.Unmarshal(raw, &byKey); err != nil {
		t.Fatal(err)
	}
	return byKey
}

func questionsAnswer(t *testing.T, root string) json.RawMessage {
	t.Helper()
	pack, err := agent.ReadPack(config.Dir(root))
	if err != nil {
		t.Fatal(err)
	}
	unsettled := map[string]map[string]string{}
	for _, a := range pack.Asks {
		if a.Kind != domain.KindQuestions {
			continue
		}
		for _, id := range a.Questions {
			unsettled[id] = map[string]string{"info": "not settled by this fixture"}
		}
	}
	raw, err := json.Marshal(unsettled)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func answerRoundOne(t *testing.T, cs *mcp.ClientSession, root string) Round1SubmitOut {
	t.Helper()
	pack := callOK[Round1PackOut](t, cs, "testigo_round1_get_pack", map[string]any{})
	if pack.Done || len(pack.Batches) == 0 {
		t.Fatalf("round 1 pack is empty on a fresh scan: %+v", pack)
	}
	if pack.Preamble == "" {
		t.Fatal("round 1 pack carries no preamble")
	}

	known := paysvcRoundOneAnswers(t)
	known["questions-double-entry"] = questionsAnswer(t, root)

	var last Round1SubmitOut
	for _, b := range pack.Batches {
		for _, k := range b.AnswerKeys {
			if _, ok := known[k]; !ok && !strings.HasPrefix(k, "questions-") {
				t.Fatalf("batch %s asks for %s, which testdata/paysvc-round1.json does not answer", b.ID, k)
			}
		}
		var answers any
		if len(b.AnswerKeys) == 1 {
			answers = known[b.AnswerKeys[0]]
			if strings.HasPrefix(b.AnswerKeys[0], "questions-") {
				answers = questionsAnswer(t, root)
			}
		} else {
			keyed := map[string]json.RawMessage{}
			for _, k := range b.AnswerKeys {
				keyed[k] = known[k]
			}
			answers = keyed
		}
		last = callOK[Round1SubmitOut](t, cs, "testigo_round1_submit_answers", map[string]any{"batch_id": b.ID, "answers": answers})
		if len(last.Invalid) > 0 {
			t.Fatalf("batch %s: answers rejected: %v", b.ID, last.Invalid)
		}
	}
	return last
}

func TestMCPServesTheFiveToolsTheReadmeNames(t *testing.T) {
	cs := connectMCP(t, t.TempDir())
	res, err := cs.ListTools(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, tool := range res.Tools {
		names = append(names, tool.Name)
		if tool.InputSchema == nil {
			t.Errorf("%s has no input schema", tool.Name)
		}
	}
	sort.Strings(names)
	want := []string{"testigo_round1_get_pack", "testigo_round1_submit_answers", "testigo_round2_next_case", "testigo_round2_submit_test", "testigo_status"}
	if strings.Join(names, ",") != strings.Join(want, ",") {
		t.Fatalf("tools = %v, want %v", names, want)
	}
}

func TestMCPWithoutAScanSaysToRunScan(t *testing.T) {
	cs := connectMCP(t, t.TempDir())
	if msg := callFails(t, cs, "testigo_status", map[string]any{}); !strings.Contains(msg, "testigo scan") {
		t.Fatalf("error = %q, want it to say run 'testigo scan'", msg)
	}
}

func TestMCPSubmitToAnUnknownBatchNamesTheFix(t *testing.T) {
	root := scannedFixture(t, "paysvc")
	cs := connectMCP(t, root)
	callOK[Round1PackOut](t, cs, "testigo_round1_get_pack", map[string]any{})

	msg := callFails(t, cs, "testigo_round1_submit_answers", map[string]any{"batch_id": "no-such-batch", "answers": map[string]any{}})
	if !strings.Contains(msg, "testigo_round1_get_pack") {
		t.Fatalf("error = %q, want it to point at testigo_round1_get_pack", msg)
	}
}

func TestMCPReportsAnInvalidAnswerInsteadOfApplyingIt(t *testing.T) {
	root := scannedFixture(t, "paysvc")
	cs := connectMCP(t, root)
	callOK[Round1PackOut](t, cs, "testigo_round1_get_pack", map[string]any{})

	placeholder := map[string]any{
		"money":         map[string]any{"type": "pkg.Money", "amount_field": "Cents", "proof": map[string]any{"symbol": "pkg.Money", "at": "money.go:14"}},
		"transfer_func": map[string]any{"symbol": "pkg#(*Ledger).Post", "at": "ledger.go:41"},
	}
	out := callOK[Round1SubmitOut](t, cs, "testigo_round1_submit_answers", map[string]any{"batch_id": "moneyModel", "answers": placeholder})
	if _, bad := out.Invalid["moneyModel"]; !bad {
		t.Fatalf("the prompt's own example was accepted as an answer: %+v", out)
	}
	if out.Round2Ready {
		t.Fatal("round 2 reported ready after an invalid money model")
	}
}

func TestMCPCarriesAFixtureFromRoundOneToAWrittenTest(t *testing.T) {
	root := scannedFixture(t, "paysvc")
	cs := connectMCP(t, root)

	status := callOK[StatusOut](t, cs, "testigo_status", map[string]any{})
	if status.Round != 1 || status.EstTokens == 0 || status.Findings == 0 {
		t.Fatalf("status on a fresh scan = %+v, want round 1 with a cost and findings", status)
	}

	early := callOK[Round2NextOut](t, cs, "testigo_round2_next_case", map[string]any{})
	if early.Blocked == "" {
		t.Fatalf("round 2 handed out work before round 1: %+v", early)
	}

	if last := answerRoundOne(t, cs, root); !last.Round2Ready {
		t.Fatalf("every round 1 batch was accepted but round 2 is still blocked: %s", last.Round2Blocked)
	}

	first := callOK[Round2NextOut](t, cs, "testigo_round2_next_case", map[string]any{})
	if first.Blocked != "" || len(first.Cases) != 1 || first.Preamble == "" || first.Remaining == 0 {
		t.Fatalf("first round 2 call = blocked %q, %d case(s), preamble %d chars, %d remaining; want one case and the preamble",
			first.Blocked, len(first.Cases), len(first.Preamble), first.Remaining)
	}
	all := callOK[Round2NextOut](t, cs, "testigo_round2_next_case", map[string]any{"all": true})
	if all.Preamble != "" {
		t.Error("the round 2 preamble was sent twice in one session")
	}
	if len(all.Cases) != all.Remaining {
		t.Errorf("all=true returned %d case(s) of %d remaining", len(all.Cases), all.Remaining)
	}

	small := smallRunnableCase(t, root)

	broken := callOK[Round2SubmitOut](t, cs, "testigo_round2_submit_test", map[string]any{
		"case_id":            small,
		"status":             "written",
		"file":               map[string]any{"path": "domain/terminal_testigo_test.go", "package": "domain", "content": "package domain\n\nimport \"testing\"\n\nfunc TestTerminal(t *testing.T) { undefinedHelper(t) }\n"},
		"func_name":          "TestTerminal",
		"reached_assertions": []string{"Terminal() is called on every final status"},
	})
	if broken.Status != "no_build" || !strings.Contains(broken.Output, "undefinedHelper") {
		t.Fatalf("a test that does not compile came back %q with output %q, want no_build naming the error", broken.Status, broken.Output)
	}

	passing := callOK[Round2SubmitOut](t, cs, "testigo_round2_submit_test", map[string]any{
		"case_id": small,
		"status":  "written",
		"file": map[string]any{"path": "domain/terminal_testigo_test.go", "package": "domain", "content": `package domain

import "testing"

func TestTerminal(t *testing.T) {
	for _, s := range []PaymentStatus{StatusCaptured, StatusFailed, StatusRefunded} {
		if !(&Payment{Status: s}).Terminal() {
			t.Fatalf("%s is not terminal", s)
		}
	}
}
`},
		"func_name":          "TestTerminal",
		"reached_assertions": []string{"Terminal() is called on every final status"},
	})
	if passing.Status != "passed" || passing.Passed == nil || !*passing.Passed {
		t.Fatalf("a passing test came back %+v", passing)
	}

	blocked := callOK[Round2SubmitOut](t, cs, "testigo_round2_submit_test", map[string]any{
		"case_id":        first.Cases[0].ID,
		"status":         "blocked",
		"blocked_reason": "the seam it needs is on a concrete type",
	})
	if blocked.Status != "blocked" {
		t.Fatalf("a blocked case came back %+v", blocked)
	}

	after := callOK[Round2NextOut](t, cs, "testigo_round2_next_case", map[string]any{"all": true})
	want := first.Remaining - 2
	if small == first.Cases[0].ID {
		want = first.Remaining - 1
	}
	if after.Remaining != want {
		t.Errorf("remaining after two submissions = %d, want %d", after.Remaining, want)
	}

	answers, err := agent.LoadAgentResponse(config.Dir(root))
	if err != nil {
		t.Fatal(err)
	}
	if len(answers.TestRuns) != first.Remaining-want {
		t.Errorf("%d test run(s) on record, want one per submitted case", len(answers.TestRuns))
	}
}

func TestMCPRefusesACaseThatWouldOverwriteAnotherCasesTest(t *testing.T) {
	root := scannedFixture(t, "paysvc")
	cs := connectMCP(t, root)
	answerRoundOne(t, cs, root)

	first, second := twoSmallRunnableCases(t, root)
	shared := "domain/terminal_testigo_test.go"
	callOK[Round2SubmitOut](t, cs, "testigo_round2_submit_test", map[string]any{
		"case_id": first,
		"status":  "written",
		"file": map[string]any{"path": shared, "package": "domain", "content": `package domain

import "testing"

func TestTerminal(t *testing.T) {
	if !(&Payment{Status: StatusCaptured}).Terminal() {
		t.Fatal("captured is not terminal")
	}
}
`},
		"func_name":          "TestTerminal",
		"reached_assertions": []string{"Terminal() is called on a captured payment"},
	})

	refusal := callFails(t, cs, "testigo_round2_submit_test", map[string]any{
		"case_id":            second,
		"status":             "written",
		"file":               map[string]any{"path": shared, "package": "domain", "content": "package domain\n\nimport \"testing\"\n\nfunc TestOther(t *testing.T) {}\n"},
		"func_name":          "TestOther",
		"reached_assertions": []string{"nothing"},
	})
	if !strings.Contains(refusal, first) || !strings.Contains(refusal, "own file") {
		t.Fatalf("refusal = %q, want it to name %s and ask for a file of its own", refusal, first)
	}
	kept, err := os.ReadFile(filepath.Join(root, shared))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(kept), "TestTerminal") {
		t.Fatalf("%s no longer holds the first case's test:\n%s", shared, kept)
	}
}

func twoSmallRunnableCases(t *testing.T, root string) (string, string) {
	t.Helper()
	flow, err := config.LoadFlow(root)
	if err != nil {
		t.Fatal(err)
	}
	answers, err := agent.LoadAgentResponse(config.Dir(root))
	if err != nil {
		t.Fatal(err)
	}
	rules, err := config.LoadRules(root)
	if err != nil {
		t.Fatal(err)
	}
	var ids []string
	for _, c := range agent.Cases(flow, answers, rules) {
		if c.Runnable() && c.Size == testPlan.SizeSmall {
			ids = append(ids, c.Scenario.ID)
		}
	}
	if len(ids) < 2 {
		t.Fatalf("fixture setup: %d small runnable case(s) for paysvc after round 1, want two", len(ids))
	}
	return ids[0], ids[1]
}

func TestMCPStatusMovesOnToRoundTwoOnceRoundOneIsAnswered(t *testing.T) {
	root := scannedFixture(t, "paysvc")
	cs := connectMCP(t, root)
	answerRoundOne(t, cs, root)

	status := callOK[StatusOut](t, cs, "testigo_status", map[string]any{})
	if status.Round != 2 {
		t.Errorf("status after round 1 = round %d (%s), want round 2", status.Round, status.RoundName)
	}
	pack := callOK[Round1PackOut](t, cs, "testigo_round1_get_pack", map[string]any{})
	if !pack.Done {
		var ids []string
		for _, b := range pack.Batches {
			ids = append(ids, b.ID)
		}
		t.Errorf("round 1 pack after every batch was accepted still asks %v", ids)
	}
}

func smallRunnableCase(t *testing.T, root string) string {
	t.Helper()
	flow, err := config.LoadFlow(root)
	if err != nil {
		t.Fatal(err)
	}
	answers, err := agent.LoadAgentResponse(config.Dir(root))
	if err != nil {
		t.Fatal(err)
	}
	rules, err := config.LoadRules(root)
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range agent.Cases(flow, answers, rules) {
		if c.Runnable() && c.Size == testPlan.SizeSmall {
			return c.Scenario.ID
		}
	}
	t.Fatal("no small runnable case for paysvc after round 1")
	return ""
}
