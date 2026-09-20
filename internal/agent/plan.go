package agent

import (
	"fmt"
	"github.com/katayunak/testigo/internal/agent/domain"
	"github.com/katayunak/testigo/internal/agent/planner"
	"github.com/katayunak/testigo/internal/agent/prompts"
	"github.com/katayunak/testigo/internal/config"
	"sort"
	"strings"

	"github.com/katayunak/testigo/internal/scanningFlow/flowEntity"
	"github.com/katayunak/testigo/internal/testPlan"
)

func Plan(f *flowEntity.Flow, k *domain.AgentResponse, round domain.Round) []domain.Ask {
	asks, _ := PlanWith(f, k, round, nil, 0)
	return asks
}

func PlanWith(f *flowEntity.Flow, k *domain.AgentResponse, round domain.Round,
	rules *config.Rules, budget int) ([]domain.Ask, planner.Plan) {

	paths := prompts.Paths(f)

	switch round {
	case domain.RoundUnderstand:
		return planUnderstand(f, k, paths, rules, budget)
	case domain.RoundGenerate:
		return planGenerate(f, k, paths), planner.Plan{}
	}
	return nil, planner.Plan{}
}

var MaxPromptChars = 24000

func render(p *prompts.Prompt) string {
	if p == nil {
		return ""
	}
	p.Trim(MaxPromptChars)
	return p.Render()
}

func planUnderstand(f *flowEntity.Flow, k *domain.AgentResponse, paths []prompts.Path,
	rules *config.Rules, budget int) ([]domain.Ask, planner.Plan) {

	var asks []domain.Ask
	var cands []planner.Candidate

	d := planner.Demanded(f, FactsFrom(k, rules))

	keepCost := func(a domain.Ask, fields, output int, settled bool) {
		asks = append(asks, a)
		cands = append(cands, planner.Candidate{
			Kind:    string(a.Kind),
			Subject: a.Subject,
			Title:   a.Title,
			Prompt:  a.Prompt,
			Asks:    fields,
			Output:  output,
			Settled: settled,
		})
	}
	keep := func(a domain.Ask, fields int, settled bool) {
		keepCost(a, fields, 0, settled)
	}

	_, unresolved := domain.FromScan(f)
	keep(domain.Ask{
		Kind:   domain.KindMoneyModel,
		Title:  moneyModelTitle(unresolved),
		Prompt: render(prompts.MoneyModel(f, paths, unresolved)),
	}, 4, len(unresolved) == 0)

	if entities := prompts.Entities(f, f.Entities); needsEntityQuestion(entities) {
		keep(domain.Ask{
			Kind:   domain.KindMainEntity,
			Title:  entityTitle(entities),
			Prompt: render(prompts.MainEntity(f, entities)),
		}, 4, false)
	}

	class := domain.Classify(f)
	answered := map[string]bool{}
	if k != nil {
		for id := range k.PaymentKind {
			answered[id] = true
		}
	}
	if needs := domain.Needed(class, d.Scenarios, d.Techniques, answered); len(needs) > 0 {
		est := 0
		ids := make([]string, 0, len(needs))
		for _, n := range needs {
			ids = append(ids, n.Question.ID)
			if n.Question.TrueOrFalse {
				est += 5
			} else {
				est += 40
			}
		}
		keepCost(domain.Ask{
			Kind:      domain.KindQuestions,
			Title:     neededTitle(class, needs),
			Subject:   string(class.Spine),
			Questions: ids,
			Prompt:    render(prompts.Needed(f, class, needs)),
		}, len(needs), est, false)
	}

	for _, m := range f.States {
		keep(domain.Ask{
			Kind: domain.KindStateRoles,

			Title:   fmt.Sprintf("What part does each of %s's %d states play?", shortType(m.Type), len(m.States)),
			Subject: m.Type,
			Prompt:  render(prompts.StateRoles(f, m)),
		}, 2, false)
	}

	seamsByTarget := map[string][]flowEntity.Seam{}
	for _, s := range f.Seams {
		seamsByTarget[s.Target] = append(seamsByTarget[s.Target], s)
	}
	for _, target := range prompts.SeamTargets(f) {
		keep(domain.Ask{
			Kind:    domain.KindExternalEffect,
			Title:   "Is a retry of " + target + " free, or does it move money twice?",
			Subject: target,
			Prompt:  render(prompts.ExternalEffectVerify(f, target, seamsByTarget[target], paths)),
		}, 5, false)
	}

	p := planner.Make(cands, d, budget)

	chosen := map[string]bool{}
	for _, c := range p.Chosen() {
		chosen[c.Kind+"\x00"+c.Subject] = true
	}
	var out []domain.Ask
	for _, a := range asks {
		if chosen[string(a.Kind)+"\x00"+a.Subject] {
			out = append(out, a)
		}
	}
	return out, p
}

func neededTitle(c domain.Classification, needs []domain.Need) string {
	tf := 0
	for _, n := range needs {
		if n.Question.TrueOrFalse {
			tf++
		}
	}
	return fmt.Sprintf("%d question(s) for a %s, %d of them true/false",
		len(needs), c.Spine.Human(), tf)
}

func moneyModelTitle(unresolved []string) string {
	if len(unresolved) == 0 {
		return "Confirm the money model (both candidates decided by proof) and name the transfer"
	}
	return "Decide " + strings.Join(unresolved, " and ") + ", then name the transfer"
}

func planGenerate(f *flowEntity.Flow, k *domain.AgentResponse, paths []prompts.Path) []domain.Ask {
	var asks []domain.Ask
	for _, c := range testPlan.Select(f, factsOf(k)) {
		if !c.Runnable() {
			continue
		}
		asks = append(asks, domain.Ask{
			Kind:    domain.KindTestCase,
			Title:   c.Scenario.Name,
			Subject: c.Scenario.ID,
			Prompt:  render(prompts.TestCase(c, f, k)),
		})
	}
	return asks
}

func Cases(f *flowEntity.Flow, k *domain.AgentResponse, rules *config.Rules) []testPlan.TestCase {
	return testPlan.Select(f, FactsFrom(k, rules))
}

func factsOf(k *domain.AgentResponse) testPlan.Facts {
	return FactsFrom(k, nil)
}

func FactsFrom(k *domain.AgentResponse, rules *config.Rules) testPlan.Facts {
	b := testPlan.Facts{Skipped: map[string]string{}}
	if rules != nil {
		if len(rules.MoneyMovement.Symbols) > 0 {
			b.TransferFunc = rules.MoneyMovement.Symbols[0]
			b.Known = true
		}
		for _, sk := range rules.Skip {
			why := sk.Why
			if why == "" {
				why = "no reason given"
			}
			b.Skipped[sk.Scenario] = why
		}
	}
	if k == nil || k.MoneyModel == nil {
		return b
	}
	fromAgent := testPlan.Facts{
		Known:          true,
		MoneyType:      k.MoneyModel.Money.Type,
		BalanceFunc:    k.MoneyModel.BalanceFunc.Symbol,
		TransferFunc:   k.MoneyModel.TransferFunc.Symbol,
		IdempotencyKey: k.MoneyModel.Idempotency.KeyField,
		Uniqueness:     k.MoneyModel.Idempotency.Uniqueness,
	}
	b.Known = true
	b.MoneyType = fromAgent.MoneyType
	b.BalanceFunc = fromAgent.BalanceFunc
	if b.TransferFunc == "" {
		b.TransferFunc = fromAgent.TransferFunc
	}
	b.IdempotencyKey = fromAgent.IdempotencyKey
	b.Uniqueness = fromAgent.Uniqueness
	return b
}

func BlockedReason(f *flowEntity.Flow, k *domain.AgentResponse) string {
	if k == nil || k.MoneyModel == nil {
		return "the money model has not been named yet — answer moneyModel.json first"
	}
	d := planner.Demanded(f, factsOf(k))
	var missing []string
	for _, m := range f.States {
		if len(d.States[m.Type]) == 0 {
			continue
		}
		if k.Transitions[m.Type] == nil {
			missing = append(missing, "transitions for "+shortType(m.Type))
		}
	}
	for target := range d.Seams {
		if k.ExternalEffects[target] == nil {
			missing = append(missing, "retry safety of "+target)
		}
	}
	sort.Strings(missing)
	if len(missing) == 0 {
		return ""
	}
	if len(missing) > 4 {
		missing = append(missing[:4], fmt.Sprintf("and %d more", len(missing)-4))
	}
	return "still unanswered: " + strings.Join(missing, "; ")
}

func shortType(qualified string) string {
	if i := strings.LastIndex(qualified, "/"); i >= 0 {
		return qualified[i+1:]
	}
	return qualified
}

func needsEntityQuestion(entities []prompts.Entity) bool {
	if len(entities) == 0 {
		return false
	}
	if len(entities) == 1 && len(entities[0].IDs) < 2 {
		return false
	}
	return true
}

func entityTitle(entities []prompts.Entity) string {
	ids := 0
	for _, e := range entities {
		ids += len(e.IDs)
	}
	if len(entities) == 1 {
		return fmt.Sprintf("Which of %s's %d identifiers is the idempotency key?", entities[0].Name, ids)
	}
	return fmt.Sprintf("Which struct does this flow move, and which of its %d identifiers is the idempotency key?", ids)
}

func Preamble(f *flowEntity.Flow, round domain.Round) string {
	switch round {
	case domain.RoundUnderstand:
		return prompts.UnderstandPreamble(f, prompts.Paths(f))
	case domain.RoundGenerate:
		return prompts.Preamble
	}
	return ""
}
