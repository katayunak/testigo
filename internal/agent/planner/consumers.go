package planner

import "sort"

type Use int

const (
	Unread Use = iota
	ValidationOnly
	Live
)

func (u Use) String() string {
	switch u {
	case Unread:
		return "unread"
	case ValidationOnly:
		return "validation only"
	case Live:
		return "live"
	}
	return "?"
}

func (u Use) Worth() bool { return u == Live }

type Field struct {
	Path   string
	Use    Use
	Reader string
}

var registry = []Field{
	{"moneyModel.money.type", Live, "plan.FactsFrom -> Facts.MoneyType"},
	{"moneyModel.money.amount_field", Live, "prompts.TestCase for money cases; report prompt"},
	{"moneyModel.money.representation", Live, "prompts.TestCase for money cases; report prompt"},
	{"moneyModel.money.currency", Live, "prompts.TestCase for money cases"},
	{"moneyModel.money.proof", Live, "prompts.TestCase and report prompt citation"},
	{"moneyModel.transfer_func", Live, "plan.FactsFrom -> Facts.TransferFunc"},
	{"moneyModel.balance_func", Live, "plan.FactsFrom -> Facts.BalanceFunc"},
	{"moneyModel.idempotency.key_field", Live, "plan.FactsFrom -> Facts.IdempotencyKey; prompts.TestCase for idempotency cases"},
	{"moneyModel.idempotency.uniqueness", Live, "plan.FactsFrom -> Facts.Uniqueness; prompts.TestCase for idempotency cases"},
	{"moneyModel.idempotency.proof", Live, "prompts.TestCase and report prompt citation"},

	{"mainEntity.main_entity.struct", Live, "report prompt"},
	{"mainEntity.main_entity.proof", Live, "report prompt citation"},
	{"mainEntity.idempotency_key.field", Live, "collect feed-forward into MoneyModel; report prompt"},
	{"mainEntity.idempotency_key.proof", ValidationOnly, "MainEntityAnswer.Validate"},
	{"mainEntity.idempotency_key.supplied_by", Live, "prompts.TestCase for idempotency cases"},
	{"mainEntity.idempotency_key.read_before_acting", Live, "prompts.TestCase for idempotency cases"},
	{"mainEntity.other_identifiers", ValidationOnly, "MainEntityAnswer.Validate contradiction check"},
	{"mainEntity.no_key_reason", ValidationOnly, "MainEntityAnswer.Validate"},

	{"stateRoles.roles", Live, "prompts.TestCase for state cases, via Transitions, IsFinal and Illegal"},
	{"stateRoles.exceptions", Live, "prompts.TestCase for state cases, as allowed exceptions"},

	{"externalEffect.changes_external_state", Live, "prompts.TestCase via EscapesRollback"},
	{"externalEffect.reversible_by_rollback", Live, "prompts.TestCase via Undoable"},
	{"externalEffect.outcome_observable", Live, "prompts.TestCase via Observable"},
	{"externalEffect.accepts_dedup_key", Live, "prompts.TestCase via Deduplicated"},
	{"externalEffect.moves_money", Live, "prompts.TestCase via IsTrue(MovesMoney)"},

	{"questions.verdict", Live, "prompts.TestCase per scenario and technique; report prompt"},
	{"questions.info", Live, "prompts.TestCase per scenario and technique; report prompt"},
}

func FieldsOf(kind string) []Field {
	var out []Field
	for _, f := range registry {
		if len(f.Path) > len(kind) && f.Path[:len(kind)+1] == kind+"." {
			out = append(out, f)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Path < out[j].Path })
	return out
}

func Worth(kind string) (fields []Field, best Use) {
	for _, f := range FieldsOf(kind) {
		if f.Use.Worth() {
			fields = append(fields, f)
			if f.Use > best {
				best = f.Use
			}
		}
	}
	return fields, best
}

func Registry() []Field { return registry }
