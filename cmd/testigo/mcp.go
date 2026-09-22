package main

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/katayunak/testigo/internal/agent"
	"github.com/katayunak/testigo/internal/agent/domain"
	"github.com/katayunak/testigo/internal/config"
	"github.com/katayunak/testigo/internal/scanningFlow/flowEntity"
	"github.com/katayunak/testigo/internal/testPlan"
)

func cmdMCP(root string) error {
	srv := &testigoMCP{root: root}

	s := mcp.NewServer(&mcp.Implementation{
		Name:        "testigo",
		Title:       "testigo — payment-flow test generation",
		Version:     "1.0.0",
		Description: "Phase 2 of testigo: answer what the scanner could not prove (round 1), then generate the tests it plans (round 2).",
	}, nil)

	mcp.AddTool(s, &mcp.Tool{
		Name: "testigo_status",
		Description: "Where this repository stands in testigo's two rounds: which round is next, why round 2 " +
			"might be blocked, and the estimated token cost of the next round. Call this first, and again any " +
			"time you are unsure what to do next — it is cheap and never stale.",
	}, srv.status)

	mcp.AddTool(s, &mcp.Tool{
		Name: "testigo_round1_get_pack",
		Description: "Get everything needed to answer round 1 (\"Understand\"): the shared preamble (the flow map, " +
			"the migrations, the rules — read it once, it is not repeated per batch) and every batch of questions " +
			"still unanswered, already grouped so the same kind of question is answered together. Call this once " +
			"per session; submit each batch with testigo_round1_submit_answers as you finish it.",
	}, srv.round1GetPack)

	mcp.AddTool(s, &mcp.Tool{
		Name: "testigo_round1_submit_answers",
		Description: "Submit the answers for one batch from testigo_round1_get_pack. Validated immediately: " +
			"accepted keys are applied, invalid ones come back with what was wrong so you can fix and resend just " +
			"those, and missing ones are named rather than silently dropped. Reports whether round 2 is unblocked " +
			"once this lands.",
	}, srv.round1SubmitAnswers)

	mcp.AddTool(s, &mcp.Tool{
		Name: "testigo_round2_next_case",
		Description: "Get the next generated-test case to write in round 2 (\"Generate\"), or every remaining one " +
			"at once with all=true. Each case is the complete, self-contained brief for exactly one Go test — " +
			"write that one test, then call testigo_round2_submit_test with this case's id before asking for the " +
			"next one.",
	}, srv.round2NextCase)

	mcp.AddTool(s, &mcp.Tool{
		Name: "testigo_round2_submit_test",
		Description: "Submit one written test (or mark it blocked) for a case id from testigo_round2_next_case. " +
			"testigo writes the file, builds it, vets it, and runs it with -race, and returns the outcome with " +
			"any failure output trimmed to what fits. A no_build, vet_fail or failed result is not final — fix " +
			"it and resubmit the same case_id.",
	}, srv.round2SubmitTest)

	return s.Run(context.Background(), &mcp.StdioTransport{})
}

type testigoMCP struct {
	root string

	mu               sync.Mutex
	round2PreambleAt int
}

func (s *testigoMCP) load() (*flowEntity.Flow, *domain.AgentResponse, *config.Rules, error) {
	flow, err := config.LoadFlow(s.root)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("%w — run 'testigo scan' first", err)
	}
	answers, err := agent.LoadAgentResponse(config.Dir(s.root))
	if err != nil {
		return nil, nil, nil, err
	}
	rules, err := config.LoadRules(s.root)
	if err != nil {
		return nil, nil, nil, err
	}
	return flow, answers, rules, nil
}

type StatusIn struct{}

type StatusOut struct {
	Round     int    `json:"round" jsonschema:"the next round to run: 1 (Understand) or 2 (Generate); 0 means nothing is left to ask in either round"`
	RoundName string `json:"round_name" jsonschema:"human name for the next round: understand, generate, or done"`
	Blocked   string `json:"blocked,omitempty" jsonschema:"why round 2 cannot run yet; absent once it can"`
	EstTokens int    `json:"est_tokens" jsonschema:"estimated input tokens the next round's pack would cost, across every remaining prompt"`
	Findings  int    `json:"findings" jsonschema:"static-analysis findings from the last scan, e.g. seams on concrete types"`
	TestRuns  int    `json:"test_runs" jsonschema:"generated tests already on record from round 2"`
}

func (s *testigoMCP) status(_ context.Context, _ *mcp.CallToolRequest, _ StatusIn) (*mcp.CallToolResult, StatusOut, error) {
	flow, answers, rules, err := s.load()
	if err != nil {
		return nil, StatusOut{}, err
	}

	out := StatusOut{Findings: len(flow.Findings), TestRuns: len(answers.TestRuns)}

	asks1, _ := agent.PlanWith(flow, answers, domain.RoundUnderstand, rules, 0)
	if len(asks1) > 0 {
		out.Round, out.RoundName = 1, domain.RoundUnderstand.String()
		out.EstTokens = agent.Estimate(domain.RoundUnderstand, asks1, agent.Preamble(flow, domain.RoundUnderstand)).EstTokens
		return nil, out, nil
	}

	if reason := agent.BlockedReason(flow, answers); reason != "" {
		out.Round, out.RoundName, out.Blocked = 2, domain.RoundGenerate.String(), reason
		return nil, out, nil
	}

	asks2 := agent.Plan(flow, answers, domain.RoundGenerate)
	if len(asks2) > 0 {
		out.Round, out.RoundName = 2, domain.RoundGenerate.String()
		out.EstTokens = agent.Estimate(domain.RoundGenerate, asks2, agent.Preamble(flow, domain.RoundGenerate)).EstTokens
		return nil, out, nil
	}

	out.RoundName = "done"
	return nil, out, nil
}

type Round1PackIn struct {
	Budget int `json:"budget,omitempty" jsonschema:"stop planning once this many weighted tokens are committed; 0 (the default) means no ceiling"`
}

type Round1Batch struct {
	ID         string   `json:"id" jsonschema:"pass this back as batch_id to testigo_round1_submit_answers"`
	Title      string   `json:"title"`
	Prompt     string   `json:"prompt" jsonschema:"every question in this batch, with the facts already proved and the shape each answer must take"`
	AnswerKeys []string `json:"answer_keys" jsonschema:"the JSON keys your answers object must use, one per question in this batch"`
}

type Round1PackOut struct {
	Done      bool          `json:"done" jsonschema:"true when round 1 already has nothing left to ask; batches is empty"`
	Preamble  string        `json:"preamble,omitempty" jsonschema:"shared context for every batch below: the flow map, the migrations, the rules. Read once, do not re-fetch per batch"`
	Batches   []Round1Batch `json:"batches"`
	EstTokens int           `json:"est_tokens"`
}

func (s *testigoMCP) round1GetPack(_ context.Context, _ *mcp.CallToolRequest, in Round1PackIn) (*mcp.CallToolResult, Round1PackOut, error) {
	flow, answers, rules, err := s.load()
	if err != nil {
		return nil, Round1PackOut{}, err
	}

	asks, _ := agent.PlanWith(flow, answers, domain.RoundUnderstand, rules, in.Budget)
	if len(asks) == 0 {
		return nil, Round1PackOut{Done: true}, nil
	}

	preamble := agent.Preamble(flow, domain.RoundUnderstand)
	if _, err := agent.Write(config.Dir(s.root), domain.RoundUnderstand, asks, preamble); err != nil {
		return nil, Round1PackOut{}, err
	}

	out := Round1PackOut{
		Preamble:  preamble,
		EstTokens: agent.Estimate(domain.RoundUnderstand, asks, preamble).EstTokens,
	}
	for _, b := range agent.Batches(asks) {
		out.Batches = append(out.Batches, Round1Batch{
			ID:         b.ID(),
			Title:      batchTitle(b),
			Prompt:     b.Prompt(),
			AnswerKeys: answerKeys(b),
		})
	}
	return nil, out, nil
}

func batchTitle(b agent.Batch) string {
	if b.Single() {
		return b.Asks[0].Title
	}
	return fmt.Sprintf("%d %s question(s)", len(b.Asks), b.Kind)
}

func answerKeys(b agent.Batch) []string {
	keys := make([]string, len(b.Asks))
	for i, a := range b.Asks {
		keys[i] = a.ID()
	}
	return keys
}

type Round1SubmitIn struct {
	BatchID string `json:"batch_id" jsonschema:"the id of the batch you are answering, from testigo_round1_get_pack"`
	Answers any    `json:"answers" jsonschema:"the answer, in the shape the batch's prompt Output section describes. When answer_keys has exactly one entry, this is that one answer object directly (not wrapped in a key); with more than one, it is a JSON object keyed by each entry in answer_keys"`
}

type Round1SubmitOut struct {
	Accepted      []string          `json:"accepted,omitempty"`
	Missing       []string          `json:"missing,omitempty" jsonschema:"answer keys this batch needed but your answers object did not include"`
	Invalid       map[string]string `json:"invalid,omitempty" jsonschema:"answer key -> what was wrong with it; fix and resend only these in a new call for the same batch_id"`
	Round2Ready   bool              `json:"round2_ready"`
	Round2Blocked string            `json:"round2_blocked,omitempty"`
}

func (s *testigoMCP) round1SubmitAnswers(_ context.Context, _ *mcp.CallToolRequest, in Round1SubmitIn) (*mcp.CallToolResult, Round1SubmitOut, error) {
	flow, answers, _, err := s.load()
	if err != nil {
		return nil, Round1SubmitOut{}, err
	}

	pack, err := agent.ReadPack(config.Dir(s.root))
	if err != nil {
		return nil, Round1SubmitOut{}, fmt.Errorf("%w — call testigo_round1_get_pack first", err)
	}

	var target *agent.Batch
	for _, b := range agent.Batches(pack.Asks) {
		if b.ID() == in.BatchID {
			bb := b
			target = &bb
			break
		}
	}
	if target == nil {
		return nil, Round1SubmitOut{}, fmt.Errorf("no batch %q in the current pack — call testigo_round1_get_pack to see the current batch ids", in.BatchID)
	}

	raw, err := json.Marshal(in.Answers)
	if err != nil {
		return nil, Round1SubmitOut{}, fmt.Errorf("answers did not marshal to JSON: %w", err)
	}
	if err := agent.WriteAnswer(config.Dir(s.root), *target, raw); err != nil {
		return nil, Round1SubmitOut{}, err
	}

	got, err := agent.Collect(config.Dir(s.root), flow, answers, pack.Asks)
	if err != nil {
		return nil, Round1SubmitOut{}, err
	}
	if err := agent.SaveAgentResponse(config.Dir(s.root), answers); err != nil {
		return nil, Round1SubmitOut{}, err
	}
	if err := config.SaveFlow(s.root, flow); err != nil {
		return nil, Round1SubmitOut{}, err
	}

	out := Round1SubmitOut{Accepted: got.Answered, Missing: got.Missing}
	if len(got.Invalid) > 0 {
		out.Invalid = map[string]string{}
		for k, e := range got.Invalid {
			out.Invalid[k] = e.Error()
		}
	}
	if reason := agent.BlockedReason(flow, answers); reason != "" {
		out.Round2Blocked = reason
	} else {
		out.Round2Ready = true
	}
	return nil, out, nil
}

type Round2NextIn struct {
	All bool `json:"all,omitempty" jsonschema:"return every remaining runnable case instead of just the next one"`
}

type Round2Case struct {
	ID     string `json:"id" jsonschema:"pass this back as case_id to testigo_round2_submit_test"`
	Title  string `json:"title"`
	Prompt string `json:"prompt" jsonschema:"the complete, self-contained brief for exactly one test: what to prove, the facts already known, and what NOT to write"`
}

type Round2NextOut struct {
	Done      bool         `json:"done" jsonschema:"true when there is nothing left to generate"`
	Blocked   string       `json:"blocked,omitempty" jsonschema:"round 1 is not finished yet; answer that first"`
	Preamble  string       `json:"preamble,omitempty" jsonschema:"the rules every case shares — present only on your first call this session, since it does not change per case"`
	Cases     []Round2Case `json:"cases"`
	Remaining int          `json:"remaining" jsonschema:"how many runnable cases, including these, still need a test written"`
}

func (s *testigoMCP) round2NextCase(_ context.Context, _ *mcp.CallToolRequest, in Round2NextIn) (*mcp.CallToolResult, Round2NextOut, error) {
	flow, answers, _, err := s.load()
	if err != nil {
		return nil, Round2NextOut{}, err
	}
	if reason := agent.BlockedReason(flow, answers); reason != "" {
		return nil, Round2NextOut{Blocked: reason}, nil
	}

	done := map[string]bool{}
	for _, r := range answers.TestRuns {
		done[r.CaseID] = true
	}

	var pending []domain.Ask
	for _, a := range agent.Plan(flow, answers, domain.RoundGenerate) {
		if !done[a.Subject] {
			pending = append(pending, a)
		}
	}

	out := Round2NextOut{Remaining: len(pending)}
	if len(pending) == 0 {
		out.Done = true
		return nil, out, nil
	}

	want := pending
	if !in.All {
		want = pending[:1]
	}
	for _, a := range want {
		out.Cases = append(out.Cases, Round2Case{ID: a.Subject, Title: a.Title, Prompt: a.Prompt})
	}

	s.mu.Lock()
	if s.round2PreambleAt == 0 {
		out.Preamble = agent.Preamble(flow, domain.RoundGenerate)
	}
	s.round2PreambleAt++
	s.mu.Unlock()

	return nil, out, nil
}

type Round2SubmitIn struct {
	CaseID        string `json:"case_id" jsonschema:"the case id from testigo_round2_next_case"`
	Status        string `json:"status" jsonschema:"written, or blocked if this case cannot be honestly tested"`
	BlockedReason string `json:"blocked_reason,omitempty" jsonschema:"required when status is blocked: why, specifically"`
	Needed        string `json:"needed,omitempty" jsonschema:"what would unblock it, e.g. an interface to extract"`
	File          struct {
		Path    string `json:"path" jsonschema:"repo-relative path; must end in _test.go"`
		Package string `json:"package"`
		Content string `json:"content" jsonschema:"the complete Go source of the test file"`
	} `json:"file,omitempty"`
	FuncName          string   `json:"func_name,omitempty"`
	ReachedAssertions []string `json:"reached_assertions,omitempty" jsonschema:"what proves the interesting situation was actually reached, not just that nothing panicked"`
	ExpectedToFail    string   `json:"expected_to_fail,omitempty" jsonschema:"why this should go red on the current code, or empty if it should pass"`
}

type Round2SubmitOut struct {
	Status         string `json:"status" jsonschema:"blocked | refused | no_build | vet_fail | written (compiled and vetted; passed/failed follow once run)"`
	Passed         *bool  `json:"passed,omitempty" jsonschema:"absent until the test actually ran"`
	File           string `json:"file,omitempty"`
	Output         string `json:"output,omitempty" jsonschema:"build, vet or test output, trimmed to fit; a non-empty value on anything but a clean pass means fix and resubmit this same case_id"`
	ExpectedToFail string `json:"expected_to_fail,omitempty"`
	Note           string `json:"note,omitempty"`
}

func (s *testigoMCP) round2SubmitTest(_ context.Context, _ *mcp.CallToolRequest, in Round2SubmitIn) (*mcp.CallToolResult, Round2SubmitOut, error) {
	flow, answers, rules, err := s.load()
	if err != nil {
		return nil, Round2SubmitOut{}, err
	}

	size := testPlan.SizeSmall
	found := false
	for _, c := range agent.Cases(flow, answers, rules) {
		if c.Scenario.ID == in.CaseID {
			size, found = c.Size, true
			break
		}
	}
	if !found {
		return nil, Round2SubmitOut{}, fmt.Errorf("%q is not a runnable case right now — call testigo_round2_next_case to see current case ids", in.CaseID)
	}

	ans := &domain.CaseAnswer{
		CaseID:            in.CaseID,
		Status:            in.Status,
		BlockedReason:     in.BlockedReason,
		Needed:            in.Needed,
		FuncName:          in.FuncName,
		ReachedAssertions: in.ReachedAssertions,
		ExpectedToFail:    in.ExpectedToFail,
	}
	ans.File.Path, ans.File.Package, ans.File.Content = in.File.Path, in.File.Package, in.File.Content

	if err := ans.Validate(size); err != nil {
		return nil, Round2SubmitOut{}, err
	}

	run, err := agent.ApplyCase(s.root, ans)
	if err != nil {
		return nil, Round2SubmitOut{}, err
	}

	answers.TestRuns = replaceRun(answers.TestRuns, *run)
	if err := agent.SaveAgentResponse(config.Dir(s.root), answers); err != nil {
		return nil, Round2SubmitOut{}, err
	}

	out := Round2SubmitOut{Status: run.Status, Passed: run.Passed, File: run.File, ExpectedToFail: run.ExpectedToFail}
	switch run.Status {
	case "no_build":
		out.Output = run.Output
		out.Note = "does not compile — fix and resubmit this same case_id"
	case "vet_fail":
		out.Output = run.Output
		out.Note = "go vet failed — fix and resubmit this same case_id"
	case "refused":
		out.Output = run.Reason
	case "blocked":
		out.Note = run.Reason
	case "written":
		if run.Passed != nil && !*run.Passed {
			out.Status = "failed"
			out.Output = run.Output
			if in.ExpectedToFail == "" {
				out.Note = "went red and no expected_to_fail was given — if this is a real gap, that is fine, but say so next time; if not, the test asserts the wrong thing"
			}
		} else if run.Passed != nil {
			out.Status = "passed"
		}
	}
	return nil, out, nil
}

func replaceRun(runs []domain.TestRun, r domain.TestRun) []domain.TestRun {
	for i, existing := range runs {
		if existing.CaseID == r.CaseID {
			runs[i] = r
			return runs
		}
	}
	return append(runs, r)
}
