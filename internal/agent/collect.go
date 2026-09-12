package agent

import (
	"encoding/json"
	"errors"
	"fmt"
	"github.com/katayunak/testigo/internal/agent/domain"
	"github.com/katayunak/testigo/internal/agent/planner"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/katayunak/testigo/internal/scanningFlow/flowEntity"
	"github.com/katayunak/testigo/internal/testPlan"
)

type Collected struct {
	Answered []string
	Missing  []string
	Invalid  map[string]error
	Cases    []*domain.CaseAnswer
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

func Collect(sidecarDir string, f *flowEntity.Flow, k *domain.AgentResponse, asks []domain.Ask) (*Collected, error) {
	dir := filepath.Join(sidecarDir, answersDir)
	out := &Collected{Invalid: map[string]error{}}

	machineByType := map[string]flowEntity.StateMachine{}
	for _, m := range f.States {
		machineByType[m.Type] = m
	}

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

			out.Invalid[batch.ID()] = shapeError(batch, byKey)
		}
	}
	return out, nil
}

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

func apply(ask domain.Ask, raw []byte, f *flowEntity.Flow, k *domain.AgentResponse,
	machines map[string]flowEntity.StateMachine, out *Collected) error {

	switch ask.Kind {
	case domain.KindMoneyModel:
		var a domain.MoneyModelAnswer
		if err := json.Unmarshal(raw, &a); err != nil {
			return fmt.Errorf("not valid JSON: %w", err)
		}
		if err := a.Validate(); err != nil {
			return err
		}
		k.MoneyModel = &a

	case domain.KindMainEntity:
		var a domain.MainEntityAnswer
		if err := json.Unmarshal(raw, &a); err != nil {
			return fmt.Errorf("not valid JSON: %w", err)
		}
		if err := a.Validate(); err != nil {
			return err
		}
		k.MainEntity = &a

		if k.MoneyModel != nil && a.IdempotencyKey.Field != "" && k.MoneyModel.Idempotency.KeyField == "" {
			k.MoneyModel.Idempotency.KeyField = a.IdempotencyKey.Field
			k.MoneyModel.Idempotency.Proof = a.IdempotencyKey.Proof
		}

	case domain.KindQuestions:
		var answers map[string]*domain.QuestionAnswer
		if err := json.Unmarshal(raw, &answers); err != nil {
			return fmt.Errorf("not valid JSON: %w", err)
		}
		class := domain.Classify(f)
		if k.PaymentKind == nil {
			k.PaymentKind = map[string]*domain.QuestionAnswer{}
		}

		asked := ask.Questions
		if len(asked) == 0 {
			d := planner.Demanded(f, FactsFrom(k, nil))
			for _, n := range domain.Needed(class, d.Scenarios, d.Techniques, nil) {
				asked = append(asked, n.Question.ID)
			}
		}
		wanted := map[string]bool{}
		for _, id := range asked {
			wanted[id] = true
		}

		kept := 0
		for id, ans := range answers {
			if !wanted[id] {
				out.Invalid[id] = fmt.Errorf("not one of the questions asked; check the key against %s.md", ask.ID())
				continue
			}
			if ans == nil {
				out.Invalid[id] = fmt.Errorf("null answer")
				continue
			}
			q, known := domain.QuestionByID(id)
			if !known {
				out.Invalid[id] = fmt.Errorf("no question with this id exists in any registry")
				continue
			}
			if err := ans.Validate(q); err != nil {
				out.Invalid[id] = err
				continue
			}
			k.PaymentKind[id] = ans
			kept++
		}
		for _, id := range asked {
			if _, ok := answers[id]; !ok {
				out.Missing = append(out.Missing, id)
			}
		}
		if kept == 0 {
			return fmt.Errorf("none of the %d answers in this file was usable", len(answers))
		}
		k.Classification = &class

	case domain.KindStateRoles:
		var a domain.StateRolesAnswer
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

		k.Transitions[ask.Subject] = a.Transitions(m.NeverAssigned)
		k.StateRoles[ask.Subject] = &a

	case domain.KindExternalEffect:
		var a domain.ExternalEffectAnswer
		if err := json.Unmarshal(raw, &a); err != nil {
			return fmt.Errorf("not valid JSON: %w", err)
		}
		a.Target = ask.Subject
		k.ExternalEffects[ask.Subject] = &a

	case domain.KindTestCase:
		var a domain.CaseAnswer
		if err := json.Unmarshal(raw, &a); err != nil {
			return fmt.Errorf("not valid JSON: %w", err)
		}
		a.CaseID = ask.Subject
		size := testPlan.SizeSmall
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

func LoadAgentResponse(sidecarDir string) (*domain.AgentResponse, error) {
	b, err := os.ReadFile(filepath.Join(sidecarDir, "agentResponse.json"))
	if errors.Is(err, os.ErrNotExist) {
		b, err = readRenamedFile(sidecarDir)
	}
	if errors.Is(err, os.ErrNotExist) {
		return domain.NewAgentResponse(), nil
	}
	if err != nil {
		return nil, err
	}
	var k domain.AgentResponse
	if err := json.Unmarshal(b, &k); err != nil {
		return nil, fmt.Errorf("agentResponse.json is corrupt: %w", err)
	}
	if k.SchemaVersion != domain.AgentResponseSchema {
		return nil, fmt.Errorf("agentResponse.json is schema v%d, this binary speaks v%d: delete it and re-run the round",
			k.SchemaVersion, domain.AgentResponseSchema)
	}
	if k.Transitions == nil {
		k.Transitions = map[string]*domain.TransitionsAnswer{}
	}
	if k.ExternalEffects == nil {
		k.ExternalEffects = map[string]*domain.ExternalEffectAnswer{}
	}
	if k.StateRoles == nil {
		k.StateRoles = map[string]*domain.StateRolesAnswer{}
	}
	return &k, nil
}

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

func SaveAgentResponse(sidecarDir string, k *domain.AgentResponse) error {
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
