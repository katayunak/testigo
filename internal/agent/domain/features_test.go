package domain

import (
	"strings"
	"testing"

	"github.com/katayunak/testigo/internal/testPlan"
)

func TestEveryFeatureIsWellFormed(t *testing.T) {
	scenarios := map[string]bool{}
	for _, s := range testPlan.Catalog {
		scenarios[s.ID] = true
	}
	areas := map[Area]bool{}
	for _, a := range Areas {
		areas[a] = true
	}

	seen := map[string]bool{}
	for _, f := range Features {
		if seen[f.ID] {
			t.Errorf("%s is declared twice", f.ID)
		}
		seen[f.ID] = true
		if !areas[f.Area] {
			t.Errorf("%s: area %q is not one of the four", f.ID, f.Area)
		}
		if !strings.HasPrefix(f.ID, string(f.Area)+".") {
			t.Errorf("%s: the ID should start with its area %q", f.ID, f.Area)
		}
		if strings.TrimSpace(f.Claim) == "" {
			t.Errorf("%s has no claim", f.ID)
		}
		if len(f.Evidence) == 0 {
			t.Errorf("%s has no evidence, so it can never be concluded", f.ID)
		}
		used := map[string]bool{}
		for _, e := range f.Evidence {
			if used[e.Question] {
				t.Errorf("%s counts %s twice", f.ID, e.Question)
			}
			used[e.Question] = true
			q, ok := QuestionByID(e.Question)
			if !ok {
				t.Errorf("%s: no question %s in any registry", f.ID, e.Question)
				continue
			}
			if !q.TrueOrFalse {
				t.Errorf("%s: %s is not a true/false question, so its answer has no verdict to count", f.ID, e.Question)
			}
		}
		for _, s := range f.Informs {
			if !scenarios[s] {
				t.Errorf("%s informs %s, which is not in the scenario catalog", f.ID, s)
			}
		}
	}
}

func TestEveryTrueOrFalseQuestionIsRolledUpOrSaysWhyNot(t *testing.T) {
	linked := map[string]bool{}
	for _, f := range Features {
		for _, e := range f.Evidence {
			linked[e.Question] = true
		}
	}
	var all []Question
	for _, qs := range PaymentTypeQuestions {
		all = append(all, qs...)
	}
	for _, qs := range ScenarioQuestions {
		all = append(all, qs...)
	}
	for _, qs := range TechniqueQuestions {
		all = append(all, qs...)
	}
	for _, q := range all {
		if !q.TrueOrFalse {
			continue
		}
		_, skipped := NotRolledUp[q.ID]
		switch {
		case linked[q.ID] && skipped:
			t.Errorf("%s is rolled up and also listed as not rolled up", q.ID)
		case !linked[q.ID] && !skipped:
			t.Errorf("%s is a true/false question that no feature counts: link it, or say in NotRolledUp why not", q.ID)
		}
	}
	for id, why := range NotRolledUp {
		q, ok := QuestionByID(id)
		if !ok || !q.TrueOrFalse {
			t.Errorf("NotRolledUp names %s, which is not a true/false question", id)
		}
		if strings.TrimSpace(why) == "" {
			t.Errorf("NotRolledUp gives no reason for %s", id)
		}
	}
}

func conclusionFor(t *testing.T, cs []Conclusion, id string) Conclusion {
	t.Helper()
	for _, c := range cs {
		if c.Feature.ID == id {
			return c
		}
	}
	t.Fatalf("no conclusion for %s", id)
	return Conclusion{}
}

func TestOneContradictionMeansTheFeatureDoesNotHold(t *testing.T) {
	cs := RollUp(map[string]*QuestionAnswer{
		"BALANCE-NEVER-NEGATIVE.guard_is_atomic":    {Verdict: boolp(true)},
		"BALANCE-LIMIT-ENFORCED.checked_atomically": {Verdict: boolp(false), Info: "checked against a balance read earlier"},
	})
	c := conclusionFor(t, cs, "money.balance_guard_is_atomic")
	if got := c.Standing(); got != StandingDoesNotHold {
		t.Fatalf("one guard that is not atomic breaks the claim for every balance, got %q", got)
	}
}

func TestAFalseCanBeTheSafeAnswer(t *testing.T) {
	cs := RollUp(map[string]*QuestionAnswer{
		"LOST-UPDATE.locking": {Verdict: boolp(false)},
	})
	if got := conclusionFor(t, cs, "money.balance_guard_is_atomic").Standing(); got != StandingHolds {
		t.Fatalf("no read-then-write anywhere supports the claim, got %q", got)
	}
}

func TestAnUnknownLeavesTheFeatureUnsettledNotHolding(t *testing.T) {
	cs := RollUp(map[string]*QuestionAnswer{
		"TIMEOUT-UNKNOWN-OUTCOME.requery":           {Verdict: boolp(true)},
		"TIMEOUT-UNKNOWN-OUTCOME.requery_resubmits": {Info: "unknown — the requery client is generated code"},
	})
	c := conclusionFor(t, cs, "seams.timeout_resolved_by_reading")
	if got := c.Standing(); got != StandingUnsettled {
		t.Fatalf("an unknown is the unsafe case: the claim cannot be called true, got %q", got)
	}
	for _, m := range c.Marks {
		if m.Question == "TIMEOUT-UNKNOWN-OUTCOME.requery_resubmits" && m.Sign != SignUnknown {
			t.Fatalf("an answer with no verdict must be marked unknown, not counted either way")
		}
	}
}

func TestAContradictionOutranksAnUnknown(t *testing.T) {
	cs := RollUp(map[string]*QuestionAnswer{
		"TIMEOUT-UNKNOWN-OUTCOME.requery":           {Info: "unknown"},
		"TIMEOUT-UNKNOWN-OUTCOME.requery_resubmits": {Verdict: boolp(true), Info: "the status job calls Pay again"},
	})
	if got := conclusionFor(t, cs, "seams.timeout_resolved_by_reading").Standing(); got != StandingDoesNotHold {
		t.Fatalf("a known break must not be hidden behind an unknown, got %q", got)
	}
}

func TestAFeatureNobodyAnsweredIsLeftOut(t *testing.T) {
	cs := RollUp(map[string]*QuestionAnswer{
		"IDEM-CONCURRENT.exactly_one": {Verdict: boolp(true)},
		"oneshot.money_leaves":        {Info: "at the provider call"},
	})
	if len(cs) != 1 || cs[0].Feature.ID != "entity.same_key_runs_once" {
		var ids []string
		for _, c := range cs {
			ids = append(ids, c.Feature.ID)
		}
		t.Fatalf("only features with an answered question belong in the roll-up, got %v", ids)
	}
}

func TestTheRollUpIsGroupedByArea(t *testing.T) {
	answers := map[string]*QuestionAnswer{}
	for _, f := range Features {
		for _, e := range f.Evidence {
			answers[e.Question] = &QuestionAnswer{Verdict: boolp(e.HoldsWhen)}
		}
	}
	cs := RollUp(answers)
	if len(cs) != len(Features) {
		t.Fatalf("every feature was answered, got %d of %d", len(cs), len(Features))
	}
	rank := map[Area]int{}
	for i, a := range Areas {
		rank[a] = i
	}
	for i := 1; i < len(cs); i++ {
		if rank[cs[i].Feature.Area] < rank[cs[i-1].Feature.Area] {
			t.Fatalf("%s comes after %s, but its area comes first", cs[i].Feature.ID, cs[i-1].Feature.ID)
		}
	}
	for _, c := range cs {
		if c.Standing() != StandingHolds {
			t.Errorf("%s: every answer was the safe one, got %q", c.Feature.ID, c.Standing())
		}
	}
}
