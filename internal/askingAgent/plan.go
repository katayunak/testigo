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
func Plan(f *flowEntity.Flow, k *askEntity.Knowledge, round askEntity.Round) []askEntity.Ask {
	paths := prompts.Paths(f)

	switch round {
	case askEntity.RoundUnderstand:
		return planUnderstand(f, paths)
	case askEntity.RoundGenerate:
		return planGenerate(f, k, paths)
	}
	return nil
}

func planUnderstand(f *flowEntity.Flow, paths []prompts.Path) []askEntity.Ask {
	var asks []askEntity.Ask

	// Ask about the money model only where the evidence did not settle it. On a
	// repository with one obvious money type and a client-supplied key, most of
	// this question disappears.
	_, unresolved := askEntity.FromEvidence(f)
	asks = append(asks, askEntity.Ask{
		Kind:   askEntity.KindBinding,
		Round:  askEntity.RoundUnderstand,
		Title:  bindingTitle(unresolved),
		Prompt: prompts.Binding(f, paths, unresolved),
	})

	for _, m := range f.Machines {
		asks = append(asks, askEntity.Ask{
			Kind:    askEntity.KindTransitions,
			Round:   askEntity.RoundUnderstand,
			Title:   fmt.Sprintf("Which transitions of %s must be impossible? (%d states)", shortType(m.Type), len(m.States)),
			Subject: m.Type,
			Prompt:  prompts.Transitions(f, m, paths),
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
			Prompt:  prompts.ExternalEffect(f, target, seamsByTarget[target], paths),
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
			Prompt:  prompts.Notes(f, node, ctx.step, ctx.path),
		})
	}
	return asks
}

// bindingTitle says what is actually left to decide, so a person scanning the
// manifest can see at a glance whether phase 1 did its job.
func bindingTitle(unresolved []string) string {
	if len(unresolved) == 0 {
		return "Confirm the money model (both candidates decided by evidence) and name the transfer"
	}
	return "Decide " + strings.Join(unresolved, " and ") + ", then name the transfer"
}

func planGenerate(f *flowEntity.Flow, k *askEntity.Knowledge, paths []prompts.Path) []askEntity.Ask {
	var asks []askEntity.Ask
	for _, c := range testPlan.Select(f, bindingsOf(k)) {
		if !c.Runnable() {
			continue // reported in the summary, not paid for as a prompt
		}
		asks = append(asks, askEntity.Ask{
			Kind:    askEntity.KindTestCase,
			Round:   askEntity.RoundGenerate,
			Title:   c.Scenario.Name,
			Subject: c.Scenario.ID,
			Prompt:  prompts.TestCase(c, f),
		})
	}
	return asks
}

// Cases exposes the full selection, including the blocked ones, so the CLI can
// report what could not be tested and why. A short list with no explanation
// reads as "there was not much to test"; the same list with eleven blocked
// entries reads as "your seams are concrete", which is the actionable version.
func Cases(f *flowEntity.Flow, k *askEntity.Knowledge, rules *config.Rules) []planEntity.TestCase {
	return testPlan.Select(f, BindingsFrom(k, rules))
}

// bindingsOf narrows round one's answers to the fields the catalog filters on.
func bindingsOf(k *askEntity.Knowledge) planEntity.Bindings {
	return BindingsFrom(k, nil)
}

// BindingsFrom merges what round one answered with what the team wrote down.
//
// The rules file wins where the two disagree, because a person wrote it on
// purpose and an agent inferred the other one.
func BindingsFrom(k *askEntity.Knowledge, rules *config.Rules) planEntity.Bindings {
	b := planEntity.Bindings{Skipped: map[string]string{}}
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
	if k == nil || k.Binding == nil {
		return b
	}
	fromAgent := planEntity.Bindings{
		Known:          true,
		MoneyType:      k.Binding.Money.Type,
		BalanceFunc:    k.Binding.BalanceFunc.Symbol,
		TransferFunc:   k.Binding.TransferFunc.Symbol,
		IdempotencyKey: k.Binding.Idempotency.KeyField,
		Uniqueness:     k.Binding.Idempotency.Uniqueness,
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
func BlockedReason(f *flowEntity.Flow, k *askEntity.Knowledge) string {
	if k == nil || k.Binding == nil {
		return "the money model has not been named yet — answer binding.json first"
	}
	var missing []string
	for _, m := range f.Machines {
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

func renderKnowledge(k *askEntity.Knowledge) string {
	if k == nil {
		return "(none)\n"
	}
	var b strings.Builder
	if k.Binding != nil {
		bd := k.Binding
		b.WriteString("MONEY MODEL\n")
		fmt.Fprintf(&b, "  money type:      %s (%s)\n", orNone(bd.Money.Type), orNone(bd.Money.Representation))
		fmt.Fprintf(&b, "  amount field:    %s\n", orNone(bd.Money.AmountField))
		fmt.Fprintf(&b, "  currency:        %s\n", orNone(bd.Money.Currency))
		fmt.Fprintf(&b, "  transfer:        %s   %s\n", orNone(bd.TransferFunc.Symbol), bd.TransferFunc.Evidence)
		fmt.Fprintf(&b, "  balance read:    %s   %s\n", orNone(bd.BalanceFunc.Symbol), bd.BalanceFunc.Evidence)
		fmt.Fprintf(&b, "  entity id field: %s\n", orNone(bd.EntityIDField))
		b.WriteString("\nIDEMPOTENCY\n")
		fmt.Fprintf(&b, "  key source:      %s\n", orNone(bd.Idempotency.KeySource))
		fmt.Fprintf(&b, "  key field:       %s\n", orNone(bd.Idempotency.KeyField))
		fmt.Fprintf(&b, "  stored in:       %s\n", orNone(bd.Idempotency.StoredIn))
		fmt.Fprintf(&b, "  uniqueness:      %s   %s\n", orNone(bd.Idempotency.Uniqueness), bd.Idempotency.Evidence)
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
				fmt.Fprintf(&b, "    undone by %s (%s)\n", fe.Undo.Symbol, fe.Undo.Evidence)
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
