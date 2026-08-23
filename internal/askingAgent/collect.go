package askingAgent

import (
	"encoding/json"
	"errors"
	"fmt"
	"github.com/katayunak/testigo/internal/askingAgent/askEntity"
	"github.com/katayunak/testigo/internal/testPlan/planEntity"
	"os"
	"path/filepath"
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
// exist, symbols must carry evidence, and the placeholder names from the prompt
// must not appear.
//
// None of that can tell whether an answer is CORRECT. It can tell whether the
// answer is about this repository at all, which catches most of what goes wrong.
func Collect(sidecarDir string, f *flowEntity.Flow, k *askEntity.Knowledge, asks []askEntity.Ask) (*Collected, error) {
	dir := filepath.Join(sidecarDir, answersDir)
	out := &Collected{Invalid: map[string]error{}}

	machineByType := map[string]flowEntity.StateMachine{}
	for _, m := range f.Machines {
		machineByType[m.Type] = m
	}

	for _, ask := range asks {
		path := filepath.Join(dir, ask.AnswerFile())
		raw, err := os.ReadFile(path)
		if errors.Is(err, os.ErrNotExist) {
			out.Missing = append(out.Missing, ask.ID())
			continue
		}
		if err != nil {
			return nil, err
		}
		raw = stripFence(raw)

		if err := apply(ask, raw, f, k, machineByType, out); err != nil {
			out.Invalid[ask.ID()] = err
			continue
		}
		out.Answered = append(out.Answered, ask.ID())
	}
	return out, nil
}

func apply(ask askEntity.Ask, raw []byte, f *flowEntity.Flow, k *askEntity.Knowledge,
	machines map[string]flowEntity.StateMachine, out *Collected) error {

	switch ask.Kind {
	case askEntity.KindBinding:
		var a askEntity.BindingAnswer
		if err := json.Unmarshal(raw, &a); err != nil {
			return fmt.Errorf("not valid JSON: %w", err)
		}
		if err := a.Validate(); err != nil {
			return err
		}
		k.Binding = &a

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
		for _, c := range testPlan.Select(f, bindingsOf(k)) {
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

// LoadKnowledge reads .testigo/knowledge.json, or returns an empty one.
func LoadKnowledge(sidecarDir string) (*askEntity.Knowledge, error) {
	b, err := os.ReadFile(filepath.Join(sidecarDir, "knowledge.json"))
	if errors.Is(err, os.ErrNotExist) {
		return askEntity.NewKnowledge(), nil
	}
	if err != nil {
		return nil, err
	}
	var k askEntity.Knowledge
	if err := json.Unmarshal(b, &k); err != nil {
		return nil, fmt.Errorf("knowledge.json is corrupt: %w", err)
	}
	if k.SchemaVersion != askEntity.KnowledgeSchema {
		return nil, fmt.Errorf("knowledge.json is schema v%d, this binary speaks v%d: delete it and re-run the round",
			k.SchemaVersion, askEntity.KnowledgeSchema)
	}
	if k.Transitions == nil {
		k.Transitions = map[string]*askEntity.TransitionsAnswer{}
	}
	if k.ExternalEffects == nil {
		k.ExternalEffects = map[string]*askEntity.ExternalEffectAnswer{}
	}
	return &k, nil
}

// SaveKnowledge writes it back, atomically.
func SaveKnowledge(sidecarDir string, k *askEntity.Knowledge) error {
	if err := os.MkdirAll(sidecarDir, 0o755); err != nil {
		return err
	}
	b, err := json.MarshalIndent(k, "", "  ")
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(sidecarDir, ".knowledge-*.json")
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
	return os.Rename(tmp.Name(), filepath.Join(sidecarDir, "knowledge.json"))
}
