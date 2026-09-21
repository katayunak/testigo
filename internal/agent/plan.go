package agent

import (
	"fmt"
	"github.com/katayunak/testigo/internal/agent/domain"
	"github.com/katayunak/testigo/internal/agent/planner"
	"github.com/katayunak/testigo/internal/agent/prompts"
	"github.com/katayunak/testigo/internal/config"
	"github.com/katayunak/testigo/internal/testPlan/planEntity"
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

	keepCost := func(a domain.Ask, fields, output int, settled, stale bool) {
		asks = append(asks, a)
		cands = append(cands, planner.Candidate{
			Kind:    string(a.Kind),
			Subject: a.Subject,
			Title:   a.Title,
			Prompt:  a.Prompt,
			Asks:    fields,
			Output:  output,
			Settled: settled,
			Stale:   stale,
		})
	}
	keep := func(a domain.Ask, fields int, settled, stale bool) {
		keepCost(a, fields, 0, settled, stale)
	}

	_, unresolved := domain.FromScan(f)
	keep(domain.Ask{
		Kind:   domain.KindMoneyModel,
		Round:  domain.RoundUnderstand,
		Title:  moneyModelTitle(unresolved),
		Prompt: render(prompts.MoneyModel(f, paths, unresolved)),
	}, 6, len(unresolved) == 0, true)

	if entities := prompts.Entities(f, f.Entities); needsEntityQuestion(entities) {
		keep(domain.Ask{
			Kind:   domain.KindMainEntity,
			Round:  domain.RoundUnderstand,
			Title:  entityTitle(entities),
			Prompt: render(prompts.MainEntity(f, entities)),
		}, 5, false, true)
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
		for _, n := range needs {
			if n.Question.TrueOrFalse {
				est += 5
			} else {
				est += 40
			}
		}
		keepCost(domain.Ask{
			Kind:    domain.KindQuestions,
			Round:   domain.RoundUnderstand,
			Title:   neededTitle(class, needs),
			Subject: string(class.Spine),
			Prompt:  render(prompts.Needed(f, class, needs)),
		}, len(needs), est, false, true)
	}

	for _, m := range f.States {
		keep(domain.Ask{
			Kind:  domain.KindStateRoles,
			Round: domain.RoundUnderstand,

			Title:   fmt.Sprintf("What part does each of %s's %d states play?", shortType(m.Type), len(m.States)),
			Subject: m.Type,
			Prompt:  render(prompts.StateRoles(f, m)),
		}, 3, false, true)
	}

	seamsByTarget := map[string][]flowEntity.Seam{}
	for _, s := range f.Seams {
		seamsByTarget[s.Target] = append(seamsByTarget[s.Target], s)
	}
	for _, target := range prompts.SeamTargets(f) {
		keep(domain.Ask{
			Kind:    domain.KindExternalEffect,
			Round:   domain.RoundUnderstand,
			Title:   "Is a retry of " + target + " free, or does it move money twice?",
			Subject: target,
			Prompt:  render(prompts.ExternalEffectVerify(f, target, seamsByTarget[target], paths)),
		}, 5, false, true)
	}

	stepIn := map[string]struct {
		step prompts.Step
		path prompts.Path
	}{}
	for _, p := range paths {
		for _, s := range p.Steps {
			if _, seen := stepIn[s.Ref]; !seen {
				stepIn[s.Ref] = struct {
					step prompts.Step
					path prompts.Path
				}{s, p}
			}
		}
	}
	var ids []string
	for id, node := range f.Nodes {

		if node.Notes != nil && node.Notes.ForHash == node.Ref.BodyHash {
			continue
		}

		if !worthDescribing(node) {
			continue
		}
		ids = append(ids, id)
	}
	sort.Strings(ids)
	for _, id := range ids {
		node := f.Nodes[id]
		ctx, ok := stepIn[id]
		if !ok {
			continue
		}
		keep(domain.Ask{
			Kind:    domain.KindNotes,
			Round:   domain.RoundUnderstand,
			Title:   "What does " + node.Ref.Symbol + " do, in business terms?",
			Subject: id,
			ForHash: node.Ref.BodyHash,
			Prompt:  render(prompts.Notes(f, node, ctx.step, ctx.path)),
		}, 5, false, true)
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

func paymentKindTitle(c domain.Classification) string {
	names := []string{c.Spine.Human()}
	for _, m := range c.Motions {
		names = append(names, m.Human())
	}
	for _, o := range c.Overlays {
		names = append(names, o.Human())
	}
	return fmt.Sprintf("%d questions for a %s", len(c.Questions()), strings.Join(names, " + "))
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
			Round:   domain.RoundGenerate,
			Title:   c.Scenario.Name,
			Subject: c.Scenario.ID,
			Prompt:  render(prompts.TestCase(c, f)),
		})
	}
	return asks
}

func Cases(f *flowEntity.Flow, k *domain.AgentResponse, rules *config.Rules) []planEntity.TestCase {
	return testPlan.Select(f, FactsFrom(k, rules))
}

func factsOf(k *domain.AgentResponse) planEntity.Facts {
	return FactsFrom(k, nil)
}

func FactsFrom(k *domain.AgentResponse, rules *config.Rules) planEntity.Facts {
	b := planEntity.Facts{Skipped: map[string]string{}}
	if rules != nil {
		b.Domain = rules.Domain
		b.MoneyMovement = rules.MoneyMovement.Description
		b.ExternalSignal = rules.MoneyMovement.ExternalSignal
		b.RetryPolicy = rules.RetryPolicy.Exists
		b.ReversalPossible = rules.Reversal.Possible
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
	fromAgent := planEntity.Facts{
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
	var missing []string
	for _, m := range f.States {
		if k.Transitions[m.Type] == nil {
			missing = append(missing, "transitions for "+shortType(m.Type))
		}
	}
	for _, target := range prompts.SeamTargets(f) {
		if k.ExternalEffects[target] == nil {
			missing = append(missing, "retry safety of "+target)
		}
	}
	if len(missing) == 0 {
		return ""
	}
	if len(missing) > 4 {
		missing = append(missing[:4], fmt.Sprintf("and %d more", len(missing)-4))
	}
	return "still unanswered: " + strings.Join(missing, "; ")
}

func orNone(s string) string {
	if strings.TrimSpace(s) == "" {
		return "(not identified)"
	}
	return s
}

func shortType(qualified string) string {
	if i := strings.LastIndex(qualified, "/"); i >= 0 {
		return qualified[i+1:]
	}
	return qualified
}

func slug(s string) string {
	var b strings.Builder
	lastDash := false
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
			b.WriteRune(r)
			lastDash = false
		default:
			if !lastDash {
				b.WriteByte('-')
				lastDash = true
			}
		}
	}
	out := strings.Trim(b.String(), "-")
	if len(out) > 72 {
		out = strings.Trim(out[len(out)-72:], "-")
	}
	return out
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

func worthDescribing(n *flowEntity.Node) bool {
	if n.Position == flowEntity.NodePositionEntry {
		return true
	}
	f := n.Facts
	return f.OpensTx || f.CommitsTx || f.RollsBackTx || f.TouchesNet || f.TouchesDB ||
		f.ReadsClock || f.Randomness || f.SpawnsGoroutine || f.HandlesMoney ||
		f.HasDeferredTx || len(f.WritesStatus) > 0 || len(f.MoneyTypes) > 0
}
