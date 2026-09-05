package askingAgent

import (
	"encoding/json"
	"errors"
	"fmt"
	"github.com/katayunak/testigo/internal/askingAgent/askEntity"
	"github.com/katayunak/testigo/internal/testPlan/planEntity"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/katayunak/testigo/internal/scanningFlow/flowEntity"
	"github.com/katayunak/testigo/internal/testPlan"
)

// Collected reports what came back and what did not.
type Collected struct {
	Answered []string
	Missing  []string
	Invalid  map[string]error
	Cases    []*askEntity.CaseAnswer
}

func (c Collected) OK() bool { return len(c.Missing) == 0 && len(c.Invalid) == 0 }

func (c Collected) String() string {
	var b strings.Builder
	fmt.Fprintf(&b, "%d answered", len(c.Answered))
	if len(c.Missing) > 0 {
		fmt.Fprintf(&b, ", %d missing", len(c.Missing))
	}
	if len(c.Invalid) > 0 {
		fmt.Fprintf(&b, ", %d invalid", len(c.Invalid))
	}
	return b.String()
}

// Collect reads the answers, checks them against what the compiler proved, and
// applies the valid ones.
//
// The checking is the point. An answer file is text a model wrote; treating it
// as trusted input would put unverified claims straight into the report, which
// is exactly the failure this tool exists to avoid. So every answer is measured
// against phase 1's facts before it is allowed in: states must be states that
// exist, symbols must carry proof, and the placeholder names from the prompt
// must not appear.
//
// None of that can tell whether an answer is CORRECT. It can tell whether the
// answer is about this repository at all, which catches most of what goes wrong.
func Collect(sidecarDir string, f *flowEntity.Flow, k *askEntity.AgentResponse, asks []askEntity.Ask) (*Collected, error) {
	dir := filepath.Join(sidecarDir, answersDir)
	out := &Collected{Invalid: map[string]error{}}

	machineByType := map[string]flowEntity.StateMachine{}
	for _, m := range f.States {
		machineByType[m.Type] = m
	}

	// One file per batch, and each file holds every answer of that kind.
	//
	// The split back out happens here, so apply() below never learns that the
	// questions were grouped: it still validates one answer against one ask,
	// with exactly the checks it had before. A batched reply is therefore
	// checked as strictly as an individual one, and one bad entry fails only
	// itself.
	for _, batch := range Batches(asks) {
		path := filepath.Join(dir, batch.AnswerFile())
		raw, err := os.ReadFile(path)
		if errors.Is(err, os.ErrNotExist) {
			for _, a := range batch.Asks {
				out.Missing = append(out.Missing, a.ID())
			}
			continue
		}
		if err != nil {
			return nil, err
		}
		raw = stripFence(raw)

		if batch.Single() {
			ask := batch.Asks[0]
			if err := apply(ask, raw, f, k, machineByType, out); err != nil {
				out.Invalid[ask.ID()] = err
				continue
			}
			out.Answered = append(out.Answered, ask.ID())
			continue
		}

		var byKey map[string]json.RawMessage
		if err := json.Unmarshal(raw, &byKey); err != nil {
			out.Invalid[batch.ID()] = fmt.Errorf("%s.json is not one JSON object keyed by answer key: %w", batch.ID(), err)
			continue
		}

		matched := 0
		for _, ask := range batch.Asks {
			part, ok := byKey[ask.ID()]
			if !ok {
				out.Missing = append(out.Missing, ask.ID())
				continue
			}
			matched++
			if err := apply(ask, part, f, k, machineByType, out); err != nil {
				out.Invalid[ask.ID()] = err
				continue
			}
			out.Answered = append(out.Answered, ask.ID())
		}

		if matched == 0 {
			// A file that parses but matches nothing is the wrong SHAPE, not a
			// set of missing answers, and the two need opposite fixes. Reporting
			// "62 missing" sends someone to write 62 answers that are already
			// there under the wrong keys.
			out.Invalid[batch.ID()] = shapeError(batch, byKey)
		}
	}
	return out, nil
}

// shapeError says what the file actually contained and what it should have.
func shapeError(b Batch, got map[string]json.RawMessage) error {
	keys := make([]string, 0, len(got))
	for k := range got {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	if len(keys) > 6 {
		keys = append(keys[:6], "...")
	}
	return fmt.Errorf(
		"%s.json parsed but none of its keys is an answer key.\n  it has:      %s\n  it needs:    %s (and %d more)\n  shape:       {\"%s\": { ...one answer... }, ...}",
		b.ID(), strings.Join(keys, ", "), b.Asks[0].ID(), len(b.Asks)-1, b.Asks[0].ID())
}

func apply(ask askEntity.Ask, raw []byte, f *flowEntity.Flow, k *askEntity.AgentResponse,
	machines map[string]flowEntity.StateMachine, out *Collected) error {

	switch ask.Kind {
	case askEntity.KindMoneyModel:
		var a askEntity.MoneyModelAnswer
		if err := json.Unmarshal(raw, &a); err != nil {
			return fmt.Errorf("not valid JSON: %w", err)
		}
		if err := a.Validate(); err != nil {
			return err
		}
		k.MoneyModel = &a

	case askEntity.KindMainEntity:
		var a askEntity.MainEntityAnswer
		if err := json.Unmarshal(raw, &a); err != nil {
			return fmt.Errorf("not valid JSON: %w", err)
		}
		if err := a.Validate(); err != nil {
			return err
		}
		k.MainEntity = &a

		// Feed it forward. Round two plans replay and concurrency tests around
		// the key, and an answer nobody reads is an answer nobody paid for.
		if k.MoneyModel != nil && a.IdempotencyKey.Field != "" && k.MoneyModel.Idempotency.KeyField == "" {
			k.MoneyModel.Idempotency.KeyField = a.IdempotencyKey.Field
			k.MoneyModel.Idempotency.Proof = a.IdempotencyKey.Proof
		}

	case askEntity.KindPaymentKind:
		var a askEntity.PaymentKindAnswer
		if err := json.Unmarshal(raw, &a); err != nil {
			return fmt.Errorf("not valid JSON: %w", err)
		}
		class := askEntity.Classify(f)
		if k.PaymentKind == nil {
			k.PaymentKind = map[string]*askEntity.QuestionAnswer{}
		}

		// Per question, not per file.
		//
		// This ask carries fifteen questions, so failing the whole file over one
		// bad entry throws away fourteen answers somebody paid for and sends
		// them back to redo all fifteen. Each answer is checked on its own, the
		// good ones are kept, and the bad ones are named individually.
		kept := 0
		byID := map[string]askEntity.Question{}
		for _, q := range class.Questions() {
			byID[q.ID] = q
		}
		for id, ans := range a.Answers {
			q, known := byID[id]
			if !known {
				out.Invalid[id] = fmt.Errorf("not one of the questions asked; check the key against %s.md", ask.ID())
				continue
			}
			if ans == nil {
				out.Invalid[id] = fmt.Errorf("null answer")
				continue
			}
			if err := ans.Validate(q); err != nil {
				out.Invalid[id] = err
				continue
			}
			k.PaymentKind[id] = ans
			kept++
		}
		for _, q := range class.Questions() {
			if _, ok := a.Answers[q.ID]; !ok {
				out.Missing = append(out.Missing, q.ID)
			}
		}
		if kept == 0 {
			return fmt.Errorf("none of the %d answers in this file was usable", len(a.Answers))
		}
		k.Classification = &class

	case askEntity.KindStateRoles:
		var a askEntity.StateRolesAnswer
		if err := json.Unmarshal(raw, &a); err != nil {
			return fmt.Errorf("not valid JSON: %w", err)
		}
		m, ok := machines[ask.Subject]
		if !ok {
			return fmt.Errorf("no state machine %q in the flow", ask.Subject)
		}
		if err := a.ValidateAgainst(m.States); err != nil {
			return err
		}
		// The matrix is DERIVED, never asked for. Everything downstream still
		// reads TransitionsAnswer, so the question changed shape without the
		// rest of testigo having to know.
		k.Transitions[ask.Subject] = a.Transitions(m.NeverAssigned)
		k.StateRoles[ask.Subject] = &a

	case askEntity.KindTransitions:
		var a askEntity.TransitionsAnswer
		if err := json.Unmarshal(raw, &a); err != nil {
			return fmt.Errorf("not valid JSON: %w", err)
		}
		m, ok := machines[ask.Subject]
		if !ok {
			return fmt.Errorf("answer is about %q, which is not a state machine in this flow", ask.Subject)
		}
		if err := a.ValidateAgainst(m.States); err != nil {
			return err
		}
		k.Transitions[ask.Subject] = &a

	case askEntity.KindExternalEffect:
		var a askEntity.ExternalEffectAnswer
		if err := json.Unmarshal(raw, &a); err != nil {
			return fmt.Errorf("not valid JSON: %w", err)
		}
		a.Target = ask.Subject
		k.ExternalEffects[ask.Subject] = &a

	case askEntity.KindNotes:
		var a askEntity.NotesAnswer
		if err := json.Unmarshal(raw, &a); err != nil {
			return fmt.Errorf("not valid JSON: %w", err)
		}
		if err := a.Validate(); err != nil {
			return err
		}
		node, ok := f.Nodes[ask.Subject]
		if !ok {
			return fmt.Errorf("answer is about %q, which is no longer in the flow", ask.Subject)
		}
		// Pinning the answer to the hash it was asked about is what stops a
		// note describing code that changed while the agent was working.
		if ask.ForHash != "" && ask.ForHash != node.Ref.BodyHash {
			return fmt.Errorf("%s changed while this question was being answered; re-ask it", node.Ref.Symbol)
		}
		node.Notes = &flowEntity.Notes{
			Step:        a.Step,
			Purpose:     a.Purpose,
			Effects:     a.Effects,
			Assumptions: a.Assumptions,
			ForHash:     node.Ref.BodyHash,
		}

	case askEntity.KindTestCase:
		var a askEntity.CaseAnswer
		if err := json.Unmarshal(raw, &a); err != nil {
			return fmt.Errorf("not valid JSON: %w", err)
		}
		a.CaseID = ask.Subject
		size := planEntity.SizeSmall
		for _, c := range testPlan.Select(f, factsOf(k)) {
			if c.Scenario.ID == ask.Subject {
				size = c.Size
				break
			}
		}
		if err := a.Validate(size); err != nil {
			return err
		}
		out.Cases = append(out.Cases, &a)
	}
	return nil
}

// stripFence tolerates the most common thing an agent does wrong: wrapping the
// JSON in a markdown fence despite being told not to.
//
// Being strict here would be principled and would waste the user's money — the
// answer is right, the packaging is wrong, and re-running the prompt costs
// tokens to fix a problem three lines of code can fix. Be strict about content,
// forgiving about formatting.
func stripFence(b []byte) []byte {
	s := strings.TrimSpace(string(b))
	if !strings.HasPrefix(s, "```") {
		return []byte(s)
	}
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		s = s[i+1:]
	}
	if i := strings.LastIndex(s, "```"); i >= 0 {
		s = s[:i]
	}
	return []byte(strings.TrimSpace(s))
}

// LoadAgentResponse reads .testigo/agentResponse.json, or returns an empty one.
func LoadAgentResponse(sidecarDir string) (*askEntity.AgentResponse, error) {
	b, err := os.ReadFile(filepath.Join(sidecarDir, "agentResponse.json"))
	if errors.Is(err, os.ErrNotExist) {
		b, err = readRenamedFile(sidecarDir)
	}
	if errors.Is(err, os.ErrNotExist) {
		return askEntity.NewAgentResponse(), nil
	}
	if err != nil {
		return nil, err
	}
	var k askEntity.AgentResponse
	if err := json.Unmarshal(b, &k); err != nil {
		return nil, fmt.Errorf("agentResponse.json is corrupt: %w", err)
	}
	if k.SchemaVersion != askEntity.AgentResponseSchema {
		return nil, fmt.Errorf("agentResponse.json is schema v%d, this binary speaks v%d: delete it and re-run the round",
			k.SchemaVersion, askEntity.AgentResponseSchema)
	}
	if k.Transitions == nil {
		k.Transitions = map[string]*askEntity.TransitionsAnswer{}
	}
	if k.ExternalEffects == nil {
		k.ExternalEffects = map[string]*askEntity.ExternalEffectAnswer{}
	}
	if k.StateRoles == nil {
		k.StateRoles = map[string]*askEntity.StateRolesAnswer{}
	}
	return &k, nil
}

// readRenamedFile finds answers written before this file was called
// agentResponse.json.
//
// This exists because those answers cost money. A real round one on a recharge
// service was 35 KB of paid replies, and a rename that silently starts from an
// empty file spends that again for nothing. The old key name is accepted too:
// the money-model answer used to be stored under "binding".
//
// Delete this once nobody has a knowledge.json left.
func readRenamedFile(sidecarDir string) ([]byte, error) {
	b, err := os.ReadFile(filepath.Join(sidecarDir, "knowledge.json"))
	if err != nil {
		return nil, err
	}
	var m map[string]json.RawMessage
	if err := json.Unmarshal(b, &m); err != nil {
		return nil, fmt.Errorf("knowledge.json is corrupt: %w", err)
	}
	if v, ok := m["binding"]; ok {
		if _, taken := m["money_model"]; !taken {
			m["money_model"] = v
		}
		delete(m, "binding")
	}
	fmt.Fprintln(os.Stderr, "testigo: read knowledge.json, which is now called agentResponse.json. The next save uses the new name; you can delete the old file.")
	return json.Marshal(m)
}

// SaveAgentResponse writes it back, atomically.
func SaveAgentResponse(sidecarDir string, k *askEntity.AgentResponse) error {
	if err := os.MkdirAll(sidecarDir, 0o755); err != nil {
		return err
	}
	b, err := json.MarshalIndent(k, "", "  ")
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(sidecarDir, ".agentResponse-*.json")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.Write(append(b, '\n')); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), filepath.Join(sidecarDir, "agentResponse.json"))
}
