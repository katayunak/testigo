package planner

import "sort"

type Use int

const (
	Unread Use = iota
	ValidationOnly
	Dormant
	Live
)

func (u Use) String() string {
	switch u {
	case Unread:
		return "unread"
	case ValidationOnly:
		return "validation only"
	case Dormant:
		return "dormant"
	case Live:
		return "live"
	}
	return "?"
}

func (u Use) Worth() bool { return u == Live || u == Dormant }

type Field struct {
	Path   string
	Use    Use
	Reader string
}

var registry = []Field{
	{"moneyModel.money.type", Live, "plan.FactsFrom -> Facts.MoneyType"},
	{"moneyModel.money.proof", ValidationOnly, "MoneyModelAnswer.Validate"},
	{"moneyModel.money.amount_field", Unread, ""},
	{"moneyModel.money.representation", Unread, ""},
	{"moneyModel.money.currency", Unread, ""},
	{"moneyModel.transfer_func", Live, "plan.FactsFrom -> Facts.TransferFunc"},
	{"moneyModel.balance_func", Live, "plan.FactsFrom -> Facts.BalanceFunc"},
	{"moneyModel.idempotency.key_field", Live, "plan.FactsFrom -> Facts.IdempotencyKey"},
	{"moneyModel.idempotency.uniqueness", Live, "plan.FactsFrom -> Facts.Uniqueness"},
	{"moneyModel.idempotency.key_source", Unread, ""},
	{"moneyModel.idempotency.stored_in", Unread, ""},
	{"moneyModel.entity_id_field", Unread, ""},
	{"moneyModel.notes", Unread, ""},

	{"mainEntity.main_entity.struct", Live, "report.Report"},
	{"mainEntity.main_entity.package", Unread, ""},
	{"mainEntity.main_entity.proof", ValidationOnly, "MainEntityAnswer.Validate"},
	{"mainEntity.main_entity.why", Unread, ""},
	{"mainEntity.idempotency_key.field", Live, "report.Report, collect feed-forward into MoneyModel"},
	{"mainEntity.idempotency_key.proof", ValidationOnly, "MainEntityAnswer.Validate"},
	{"mainEntity.idempotency_key.supplied_by", ValidationOnly, "MainEntityAnswer.Validate"},
	{"mainEntity.idempotency_key.read_before_acting", Unread, ""},
	{"mainEntity.idempotency_key.confidence", Unread, ""},
	{"mainEntity.other_identifiers", ValidationOnly, "MainEntityAnswer.Validate contradiction check"},
	{"mainEntity.no_key_reason", ValidationOnly, "MainEntityAnswer.Validate"},
	{"mainEntity.notes", Unread, ""},

	{"stateRoles.roles", Dormant, "StateRolesAnswer.Transitions -> TransitionsAnswer.Illegal, which has no caller"},
	{"stateRoles.exceptions", Dormant, "TransitionsAnswer.FinalStateExceptions -> IsFinal, which has no caller"},
	{"stateRoles.notes", Unread, ""},

	{"externalEffect.changes_external_state", Dormant, "ExternalEffectAnswer.EscapesRollback, which has no caller"},
	{"externalEffect.reversible_by_rollback", Dormant, "ExternalEffectAnswer.Undoable, which has no caller"},
	{"externalEffect.outcome_observable", Dormant, "ExternalEffectAnswer.Observable, which has no caller"},
	{"externalEffect.accepts_dedup_key", Dormant, "ExternalEffectAnswer.Deduplicated, which has no caller"},
	{"externalEffect.moves_money", Dormant, "domain.IsTrue(MovesMoney), which has no caller"},
	{"externalEffect.dedup_key_argument", Unread, ""},
	{"externalEffect.undo", Unread, ""},
	{"externalEffect.failure_modes", Unread, ""},
	{"externalEffect.basis", Unread, ""},
	{"externalEffect.notes", Unread, ""},

	{"questions.verdict", Live, "collect -> AgentResponse.PaymentKind, the T/F matrix"},
	{"questions.info", Live, "collect -> AgentResponse.PaymentKind, the text answers"},

	{"paymentKind.answers", Unread, ""},
	{"paymentKind.notes", Unread, ""},

	{"notes.step", Live, "report/mermaid.go node label"},
	{"notes.purpose", Unread, ""},
	{"notes.effects", Unread, ""},
	{"notes.assumptions", Unread, ""},
	{"notes.confidence", Unread, ""},
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
