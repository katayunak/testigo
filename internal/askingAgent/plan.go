package askingAgent

import (
	"fmt"
	"github.com/katayunak/testigo/internal/askingAgent/askEntity"
	"github.com/katayunak/testigo/internal/askingAgent/prompts"
	"github.com/katayunak/testigo/internal/config"
	"github.com/katayunak/testigo/internal/testPlan/planEntity"
	"sort"
	"strings"

	"github.com/katayunak/testigo/internal/scanningFlow/flowEntity"
	"github.com/katayunak/testigo/internal/testPlan"
)

// Plan decides which questions are worth asking about this repository right now.
//
// Two things shape the list, and both are about not wasting the user's money.
//
// First, incrementality. A node whose notes are still fresh is not asked about
// again — phase 1's code references already prove its body has not changed, so
// the previous answer is still true and re-asking would buy nothing.
//
// Second, ordering. Binding comes first and alone, because every later question
// depends on knowing which type is money and which call commits the money. If that
// answer is wrong, everything built on it is wrong, and it is far cheaper to
// catch at one prompt than at twenty.
func Plan(f *flowEntity.Flow, k *askEntity.AgentResponse, round askEntity.Round) []askEntity.Ask {
	paths := prompts.Paths(f)

	switch round {
	case askEntity.RoundUnderstand:
		return planUnderstand(f, paths)
	case askEntity.RoundGenerate:
		return planGenerate(f, k, paths)
	}
	return nil
}

// MaxPromptChars caps one question file.
//
// This is the third lever on the bill, and the smallest of the three. Hoisting
// the shared half into PREAMBLE.md removed what was repeated; batching by kind
// removed turns, which is the term that actually dominates. This one only stops
// a single pathological prompt from swallowing a round — the case that produced
// it was one state machine with 122 write sites, where the question is what ROLE
// a state plays and the 123rd example answers nothing the first three did not.
//
// 24000 characters is roughly 6k tokens. It is set where it does not fire on a
// normal prompt, because a budget that trims every prompt is a budget that is
// silently deciding what the agent may know. Proof-priority context is never
// cut, and anything that is cut is named in the prompt with the file that holds
// it — an agent told what is missing does one lookup, an agent left to notice
// reads the repository.
//
// Zero turns it off.
var MaxPromptChars = 24000

// render applies the budget and writes the prompt out. Every ask goes through
// here, so there is exactly one place where a prompt's size is decided.
func render(p *prompts.Prompt) string {
	if p == nil {
		return ""
	}
	p.Trim(MaxPromptChars)
	return p.Render()
}

func planUnderstand(f *flowEntity.Flow, paths []prompts.Path) []askEntity.Ask {
	var asks []askEntity.Ask

	// Ask about the money model only where the proof did not settle it. On a
	// repository with one obvious money type and a client-supplied key, most of
	// this question disappears.
	_, unresolved := askEntity.FromScan(f)
	asks = append(asks, askEntity.Ask{
		Kind:   askEntity.KindMoneyModel,
		Round:  askEntity.RoundUnderstand,
		Title:  moneyModelTitle(unresolved),
		Prompt: render(prompts.MoneyModel(f, paths, unresolved)),
	})

	// Which entity the flow moves, and which of its several IDs a retry
	// repeats. Asked only when the repository has a lifecycle entity carrying
	// more than one identifier — one candidate is not a question, and no
	// lifecycle means there is no entity to ask about.
	if entities := prompts.Entities(f, f.Entities); needsEntityQuestion(entities) {
		asks = append(asks, askEntity.Ask{
			Kind:   askEntity.KindMainEntity,
			Round:  askEntity.RoundUnderstand,
			Title:  entityTitle(entities),
			Prompt: render(prompts.MainEntity(f, entities)),
		})
	}

	// The questions this KIND of payment system raises. Which kind it is was
	// decided in Go from the schema and the type names, so only the questions
	// that can apply to it are ever written to a file.
	if class := askEntity.Classify(f); len(class.Questions()) > 0 {
		asks = append(asks, askEntity.Ask{
			Kind:    askEntity.KindPaymentKind,
			Round:   askEntity.RoundUnderstand,
			Title:   paymentKindTitle(class),
			Subject: string(class.Spine),
			Prompt:  render(prompts.PaymentKind(f, class)),
		})
	}

	for _, m := range f.States {
		asks = append(asks, askEntity.Ask{
			Kind:  askEntity.KindStateRoles,
			Round: askEntity.RoundUnderstand,
			// N answers, not N x N cells. testigo derives the matrix.
			Title:   fmt.Sprintf("What part does each of %s's %d states play?", shortType(m.Type), len(m.States)),
			Subject: m.Type,
			Prompt:  render(prompts.StateRoles(f, m)),
		})
	}

	seamsByTarget := map[string][]flowEntity.Seam{}
	for _, s := range f.Seams {
		seamsByTarget[s.Target] = append(seamsByTarget[s.Target], s)
	}
	for _, target := range prompts.SeamTargets(f) {
		asks = append(asks, askEntity.Ask{
			Kind:    askEntity.KindExternalEffect,
			Round:   askEntity.RoundUnderstand,
			Title:   "Is a retry of " + target + " free, or does it move money twice?",
			Subject: target,
			Prompt:  render(prompts.ExternalEffect(f, target, seamsByTarget[target], paths)),
		})
	}

	// Notes last: they are the cheapest to regenerate and the least load-bearing.
	// If a budget runs out mid-pack, running out here costs the least.
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
		// Fresh notes describe code that has not changed. Asking again would
		// pay for an answer we already have.
		if node.Notes != nil && node.Notes.ForHash == node.Ref.BodyHash {
			continue
		}

		// A function the compiler proved nothing about has no business meaning
		// to describe. On a real payment service 43 of 73 reachable functions
		// were in this category — protobuf getters, logger constructors,
		// wrappers — and each one bought a prompt asking an agent for the
		// "purpose", "effects" and "caller assumptions" of code with no
		// effects and no assumptions.
		//
		// The bar is deliberately low: any proved fact at all, or being an
		// entry point. A function that opens a transaction, leaves the process,
		// touches money, reads the clock or sets a status is worth a sentence.
		// One that does none of those is a name.
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
		asks = append(asks, askEntity.Ask{
			Kind:    askEntity.KindNotes,
			Round:   askEntity.RoundUnderstand,
			Title:   "What does " + node.Ref.Symbol + " do, in business terms?",
			Subject: id,
			ForHash: node.Ref.BodyHash,
			Prompt:  render(prompts.Notes(f, node, ctx.step, ctx.path)),
		})
	}
	return asks
}

// paymentKindTitle names the kinds, so a person reading `testigo ask` output can
// see whether the classification is wrong BEFORE paying for the answers.
func paymentKindTitle(c askEntity.Classification) string {
	names := []string{c.Spine.Human()}
	for _, m := range c.Motions {
		names = append(names, m.Human())
	}
	for _, o := range c.Overlays {
		names = append(names, o.Human())
	}
	return fmt.Sprintf("%d questions for a %s", len(c.Questions()), strings.Join(names, " + "))
}

// moneyModelTitle says what is actually left to decide, so a person scanning the
// manifest can see at a glance whether phase 1 did its job.
func moneyModelTitle(unresolved []string) string {
	if len(unresolved) == 0 {
		return "Confirm the money model (both candidates decided by proof) and name the transfer"
	}
	return "Decide " + strings.Join(unresolved, " and ") + ", then name the transfer"
}

func planGenerate(f *flowEntity.Flow, k *askEntity.AgentResponse, paths []prompts.Path) []askEntity.Ask {
	var asks []askEntity.Ask
	for _, c := range testPlan.Select(f, factsOf(k)) {
		if !c.Runnable() {
			continue // reported in the summary, not paid for as a prompt
		}
		asks = append(asks, askEntity.Ask{
			Kind:    askEntity.KindTestCase,
			Round:   askEntity.RoundGenerate,
			Title:   c.Scenario.Name,
			Subject: c.Scenario.ID,
			Prompt:  render(prompts.TestCase(c, f)),
		})
	}
	return asks
}

// Cases exposes the full selection, including the blocked ones, so the CLI can
// report what could not be tested and why. A short list with no explanation
// reads as "there was not much to test"; the same list with eleven blocked
// entries reads as "your seams are concrete", which is the actionable version.
func Cases(f *flowEntity.Flow, k *askEntity.AgentResponse, rules *config.Rules) []planEntity.TestCase {
	return testPlan.Select(f, FactsFrom(k, rules))
}

// factsOf narrows round one's answers to the fields the catalog filters on.
func factsOf(k *askEntity.AgentResponse) planEntity.Facts {
	return FactsFrom(k, nil)
}

// FactsFrom merges what round one answered with what the team wrote down.
//
// The rules file wins where the two disagree, because a person wrote it on
// purpose and an agent inferred the other one.
func FactsFrom(k *askEntity.AgentResponse, rules *config.Rules) planEntity.Facts {
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

// BlockedReason explains why round two cannot start yet, or returns empty.
//
// askEntity.Round two is gated rather than best-effort on purpose. Generating tests from
// half-collected context produces tests that look complete and check the wrong
// thing, which is the failure this whole two-round split exists to prevent.
func BlockedReason(f *flowEntity.Flow, k *askEntity.AgentResponse) string {
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

func renderAgentResponse(k *askEntity.AgentResponse) string {
	if k == nil {
		return "(none)\n"
	}
	var b strings.Builder
	if k.MoneyModel != nil {
		bd := k.MoneyModel
		b.WriteString("MONEY MODEL\n")
		fmt.Fprintf(&b, "  money type:      %s (%s)\n", orNone(bd.Money.Type), orNone(bd.Money.Representation))
		fmt.Fprintf(&b, "  amount field:    %s\n", orNone(bd.Money.AmountField))
		fmt.Fprintf(&b, "  currency:        %s\n", orNone(bd.Money.Currency))
		fmt.Fprintf(&b, "  transfer:        %s   %s\n", orNone(bd.TransferFunc.Symbol), bd.TransferFunc.At)
		fmt.Fprintf(&b, "  balance read:    %s   %s\n", orNone(bd.BalanceFunc.Symbol), bd.BalanceFunc.At)
		fmt.Fprintf(&b, "  entity id field: %s\n", orNone(bd.EntityIDField))
		b.WriteString("\nIDEMPOTENCY\n")
		fmt.Fprintf(&b, "  key source:      %s\n", orNone(bd.Idempotency.KeySource))
		fmt.Fprintf(&b, "  key field:       %s\n", orNone(bd.Idempotency.KeyField))
		fmt.Fprintf(&b, "  stored in:       %s\n", orNone(bd.Idempotency.StoredIn))
		fmt.Fprintf(&b, "  uniqueness:      %s   %s\n", orNone(bd.Idempotency.Uniqueness), bd.Idempotency.Proof)
		if bd.Idempotency.Uniqueness == "app_check_then_write" {
			b.WriteString("\n  NOTE: uniqueness is enforced by reading and then writing in application\n")
			b.WriteString("  code, not by a database constraint. Two concurrent requests can both pass\n")
			b.WriteString("  the read before either writes. The concurrent test below should expose\n")
			b.WriteString("  this, and it is expected to fail until a unique constraint is added.\n")
		}
		if bd.Notes != "" {
			fmt.Fprintf(&b, "\n  notes: %s\n", bd.Notes)
		}
		b.WriteString("\n")
	}
	if len(k.Transitions) > 0 {
		b.WriteString("LEGAL TRANSITIONS\n")
		var types []string
		for t := range k.Transitions {
			types = append(types, t)
		}
		sort.Strings(types)
		for _, t := range types {
			fmt.Fprintf(&b, "  %s (initial: %s)\n", shortType(t), orNone(k.Transitions[t].InitialState))
			var froms []string
			for from := range k.Transitions[t].MayMoveTo {
				froms = append(froms, from)
			}
			sort.Strings(froms)
			for _, from := range froms {
				to := k.Transitions[t].MayMoveTo[from]
				if len(to) == 0 {
					fmt.Fprintf(&b, "    %-22s terminal\n", from)
					continue
				}
				fmt.Fprintf(&b, "    %-22s -> %s\n", from, strings.Join(to, ", "))
			}
		}
		b.WriteString("\n")
	}
	if len(k.ExternalEffects) > 0 {
		b.WriteString("WHAT EACH EXTERNAL CALL DOES TO THE WORLD\n")
		var targets []string
		for t := range k.ExternalEffects {
			targets = append(targets, t)
		}
		sort.Strings(targets)
		for _, t := range targets {
			fe := k.ExternalEffects[t]
			fmt.Fprintf(&b, "  %s\n", t)
			if fe.EscapesRollback() {
				b.WriteString("    survives a ROLLBACK — once it happens it cannot be taken back\n")
			} else {
				b.WriteString("    a rollback undoes it\n")
			}
			if askEntity.IsTrue(fe.MovesMoney) {
				b.WriteString("    MOVES MONEY, in this repository's meaning of the phrase\n")
			}
			if fe.Observable() {
				b.WriteString("    the outcome can be queried afterwards, so an unknown result is recoverable\n")
			} else {
				b.WriteString("    the outcome CANNOT be queried afterwards — a timeout leaves a permanent unknown\n")
			}
			if fe.Deduplicated() {
				fmt.Fprintf(&b, "    the far side deduplicates on %s\n", orNone(fe.DedupKeyArgument))
			}
			if fe.Undo.Exists {
				fmt.Fprintf(&b, "    undone by %s (%s)\n", fe.Undo.Symbol, fe.Undo.Proof)
			}
			if len(fe.FailureModes) > 0 {
				fmt.Fprintf(&b, "    fails as: %s\n", strings.Join(fe.FailureModes, ", "))
			}
			if fe.Notes != "" {
				fmt.Fprintf(&b, "    %s\n", fe.Notes)
			}
		}
		b.WriteString("\n")
	}
	return b.String()
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

// slug makes an ask ID safe as a filename on every filesystem, and short enough
// to read in a directory listing.
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

// needsEntityQuestion is false when the proof already settled it: no
// lifecycle entity at all, or a single identifier on a single entity, which is
// not a choice.
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

// Preamble is the text every prompt in a round shares, written once into
// PREAMBLE.md instead of into all of them.
func Preamble(f *flowEntity.Flow, round askEntity.Round) string {
	switch round {
	case askEntity.RoundUnderstand:
		return prompts.UnderstandPreamble(f, prompts.Paths(f))
	case askEntity.RoundGenerate:
		return prompts.Preamble
	}
	return ""
}

// worthDescribing reports whether a function has anything an agent could say
// something useful about.
func worthDescribing(n *flowEntity.Node) bool {
	if n.Position == flowEntity.NodePositionEntry {
		return true
	}
	f := n.Facts
	return f.OpensTx || f.CommitsTx || f.RollsBackTx || f.TouchesNet || f.TouchesDB ||
		f.ReadsClock || f.Randomness || f.SpawnsGoroutine || f.HandlesMoney ||
		f.HasDeferredTx || len(f.WritesStatus) > 0 || len(f.MoneyTypes) > 0
}
