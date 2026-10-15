package domain

var Features = []Feature{
	{
		ID:       "money.amounts_stay_exact",
		Area:     AreaMoney,
		Claim:    "An amount keeps its exact value on every hop: it never passes through a float, a string or another unit.",
		Evidence: []Evidence{holdsIfFalse("MONEY-ROUND-TRIP-EXACT.representation")},
		Informs:  []string{"MONEY-ROUND-TRIP-EXACT", "MINOR-UNIT-CONVERSION", "SPLIT-SUMS-TO-TOTAL"},
	},
	{
		ID:    "money.one_currency_per_transaction",
		Area:  AreaMoney,
		Claim: "One transaction never holds more than one currency.",
		Evidence: []Evidence{
			holdsIfFalse("CURRENCY-MIXING-REFUSED.can_mix"),
			holdsIfFalse("ledger.multi_currency_legs"),
		},
		Informs: []string{"CURRENCY-MIXING-REFUSED", "DOUBLE-ENTRY-SUMS-TO-ZERO"},
	},
	{
		ID:    "money.balance_never_negative",
		Area:  AreaMoney,
		Claim: "No balance is ever allowed to go below zero.",
		Evidence: []Evidence{
			holdsIfFalse("BALANCE-NEVER-NEGATIVE.may_go_negative"),
			holdsIfFalse("market.negative_seller"),
		},
		Informs: []string{"BALANCE-NEVER-NEGATIVE", "LOST-UPDATE"},
	},
	{
		ID:    "money.balance_guard_is_atomic",
		Area:  AreaMoney,
		Claim: "Every balance check and the write it guards happen in one statement or under a row lock.",
		Evidence: []Evidence{
			holdsIfTrue("BALANCE-NEVER-NEGATIVE.guard_is_atomic"),
			holdsIfTrue("BALANCE-LIMIT-ENFORCED.checked_atomically"),
			holdsIfFalse("LOST-UPDATE.locking"),
		},
		Informs: []string{"BALANCE-NEVER-NEGATIVE", "BALANCE-LIMIT-ENFORCED", "LOST-UPDATE",
			"CONSERVATION-UNDER-CONCURRENCY", "READ-MODIFY-WRITE-NEEDS-A-LOCK"},
	},
	{
		ID:       "money.balance_has_one_writer",
		Area:     AreaMoney,
		Claim:    "Only the functions in the flow map write the balance.",
		Evidence: []Evidence{holdsIfFalse("wallet.other_writers")},
		Informs: []string{"CONSERVATION-UNDER-CONCURRENCY", "LOST-UPDATE", "BALANCE-NEVER-NEGATIVE",
			"BALANCE-LIMIT-ENFORCED"},
	},
	{
		ID:       "money.ledger_rule_outside_go",
		Area:     AreaMoney,
		Claim:    "The rule that ledger legs sum to zero is enforced outside the Go code too.",
		Evidence: []Evidence{holdsIfTrue("DOUBLE-ENTRY-SUMS-TO-ZERO.enforced_outside_go")},
		Informs:  []string{"DOUBLE-ENTRY-SUMS-TO-ZERO"},
	},
	{
		ID:    "money.captured_equals_authorized",
		Area:  AreaMoney,
		Claim: "The captured amount always equals the authorized amount.",
		Evidence: []Evidence{
			holdsIfFalse("authcap.partial"),
			holdsIfFalse("authcap.overcapture"),
		},
		Informs: []string{"ORPHANED-AUTHORIZATION"},
	},
	{
		ID:    "money.used_whole_or_not_at_all",
		Area:  AreaMoney,
		Claim: "A voucher or a hold is used in full or not at all; no remainder is left behind.",
		Evidence: []Evidence{
			holdsIfFalse("voucher.partial"),
			holdsIfFalse("escrow.partial_release"),
		},
		Informs: []string{"FINAL-STATE-IS-FINAL"},
	},

	{
		ID:    "seams.timeout_resolved_by_reading",
		Area:  AreaSeams,
		Claim: "After a timeout, the real outcome is found by asking the provider, and asking never sends the payment again.",
		Evidence: []Evidence{
			holdsIfTrue("TIMEOUT-UNKNOWN-OUTCOME.requery"),
			holdsIfFalse("TIMEOUT-UNKNOWN-OUTCOME.requery_resubmits"),
		},
		Informs: []string{"TIMEOUT-UNKNOWN-OUTCOME", "IDEM-CRASH-AT-STEP", "UNCLASSIFIED-ERROR-NOT-RETRIED"},
	},
	{
		ID:       "seams.unknown_errors_are_final",
		Area:     AreaSeams,
		Claim:    "A provider error the code does not recognise is treated as final, not retried.",
		Evidence: []Evidence{holdsIfFalse("UNCLASSIFIED-ERROR-NOT-RETRIED.default")},
		Informs:  []string{"UNCLASSIFIED-ERROR-NOT-RETRIED", "TIMEOUT-UNKNOWN-OUTCOME"},
	},
	{
		ID:       "seams.redelivery_is_harmless",
		Area:     AreaSeams,
		Claim:    "A message or callback delivered twice changes nothing the second time.",
		Evidence: []Evidence{holdsIfTrue("AT-LEAST-ONCE-DELIVERY-IS-SAFE.duplicate_message")},
		Informs:  []string{"AT-LEAST-ONCE-DELIVERY-IS-SAFE", "WEBHOOK-ORDER-INDEPENDENT", "IDEM-REPLAY"},
	},
	{
		ID:       "seams.callback_order_does_not_matter",
		Area:     AreaSeams,
		Claim:    "Provider callbacks can arrive in any order and the final state is still right.",
		Evidence: []Evidence{holdsIfTrue("WEBHOOK-ORDER-INDEPENDENT.out_of_order")},
		Informs:  []string{"WEBHOOK-ORDER-INDEPENDENT", "FINAL-STATE-IS-FINAL"},
	},
	{
		ID:    "seams.acts_only_on_settled_money",
		Area:  AreaSeams,
		Claim: "Money is only treated as done once the provider can no longer take it back.",
		Evidence: []Evidence{
			holdsIfFalse("refund.can_fail_late"),
			holdsIfFalse("escrow.reversible_inbound"),
			holdsIfFalse("market.transfer_before_settlement"),
		},
		Informs: []string{"FINAL-STATE-IS-FINAL", "FAILURE-IS-NOT-TERMINAL"},
	},

	{
		ID:    "flow.money_moves_in_one_transaction",
		Area:  AreaFlow,
		Claim: "Both sides of a money movement commit or roll back together.",
		Evidence: []Evidence{
			holdsIfTrue("TRANSFER-IS-ATOMIC.same_transaction"),
			holdsIfTrue("LEDGER-OUTSIDE-CALLER-TRANSACTION.shares_tx"),
		},
		Informs: []string{"TRANSFER-IS-ATOMIC", "LEDGER-OUTSIDE-CALLER-TRANSACTION",
			"CONSERVATION-UNDER-CONCURRENCY", "IDEM-CRASH-AT-STEP"},
	},
	{
		ID:       "flow.saved_before_reply",
		Area:     AreaFlow,
		Claim:    "The caller hears success only after the work is saved.",
		Evidence: []Evidence{holdsIfFalse("ASYNC-RESPONSE-BEFORE-DURABILITY.acks_early")},
		Informs:  []string{"ASYNC-RESPONSE-BEFORE-DURABILITY", "IDEM-CRASH-AT-STEP"},
	},
	{
		ID:       "flow.illegal_transitions_refused",
		Area:     AreaFlow,
		Claim:    "A status change the lifecycle does not allow is refused.",
		Evidence: []Evidence{holdsIfTrue("ILLEGAL-TRANSITION-REFUSED.enforced_where")},
		Informs:  []string{"ILLEGAL-TRANSITION-REFUSED", "FINAL-STATE-IS-FINAL", "WEBHOOK-ORDER-INDEPENDENT"},
	},
	{
		ID:       "flow.failed_is_terminal",
		Area:     AreaFlow,
		Claim:    "A failed payment stays failed; a retry starts a new payment.",
		Evidence: []Evidence{holdsIfTrue("oneshot.failure_terminal")},
		Informs:  []string{"FAILURE-IS-NOT-TERMINAL", "FINAL-STATE-IS-FINAL"},
	},
	{
		ID:       "flow.inactive_blocks_both_ways",
		Area:     AreaFlow,
		Claim:    "An account that is not active can neither send nor receive money.",
		Evidence: []Evidence{holdsIfTrue("WALLET-STATUS-GATES-MOVEMENT.blocks_credits")},
		Informs:  []string{"WALLET-STATUS-GATES-MOVEMENT"},
	},
	{
		ID:       "flow.self_transfer_refused",
		Area:     AreaFlow,
		Claim:    "A transfer to the same account is refused before anything is written.",
		Evidence: []Evidence{holdsIfTrue("SELF-TRANSFER-REFUSED.is_refused")},
		Informs:  []string{"SELF-TRANSFER-REFUSED", "TRANSFER-IS-ATOMIC"},
	},
	{
		ID:    "flow.jobs_safe_to_rerun",
		Area:  AreaFlow,
		Claim: "A batch job run twice over the same window changes nothing the second time.",
		Evidence: []Evidence{
			holdsIfFalse("RECONCILER-IS-IDEMPOTENT.rerun"),
			holdsIfTrue("recon.rerun_safe"),
			holdsIfTrue("usage.rerun_safe"),
		},
		Informs: []string{"RECONCILER-IS-IDEMPOTENT"},
	},

	{
		ID:       "entity.same_key_runs_once",
		Area:     AreaMainEntity,
		Claim:    "Two requests with the same key never both do the work.",
		Evidence: []Evidence{holdsIfTrue("IDEM-CONCURRENT.exactly_one")},
		Informs:  []string{"IDEM-CONCURRENT", "IDEM-REPLAY", "IDEM-CRASH-AT-STEP", "UPSERT-HIDES-A-SECOND-EFFECT"},
	},
	{
		ID:       "entity.repeat_gets_first_result",
		Area:     AreaMainEntity,
		Claim:    "Repeating a finished request returns the first result instead of doing the work again.",
		Evidence: []Evidence{holdsIfTrue("IDEM-REPLAY.stores_result")},
		Informs:  []string{"IDEM-REPLAY", "IDEM-CONCURRENT", "IDEM-PAYLOAD-MISMATCH"},
	},
	{
		ID:       "entity.key_bound_to_request",
		Area:     AreaMainEntity,
		Claim:    "Reusing a key with a different amount or recipient is refused.",
		Evidence: []Evidence{holdsIfTrue("IDEM-PAYLOAD-MISMATCH.rejects")},
		Informs:  []string{"IDEM-PAYLOAD-MISMATCH", "IDEM-REPLAY"},
	},
	{
		ID:       "entity.failure_is_replayed",
		Area:     AreaMainEntity,
		Claim:    "A retry after a failure gets the stored failure back, not a new attempt.",
		Evidence: []Evidence{holdsIfTrue("IDEM-REPLAY.replays_failures")},
		Informs:  []string{"IDEM-REPLAY", "FAILURE-IS-NOT-TERMINAL"},
	},
}

var NotRolledUp = map[string]string{
	"ledger.invariant_outside_go":              "two questions in one: a true could mean the rule is enforced elsewhere, or that another writer exists",
	"NETWORK-CALL-INSIDE-TRANSACTION.intended": "asks whether the design was on purpose, not what the code does",
	"payout.destination_snapshot":              "a choice between two designs, neither of them wrong",
	"payout.gross_or_net":                      "a choice between two designs, neither of them wrong",
	"voucher.bearer":                           "a choice between two designs, and about access rather than money",
	"recon.corrected_files":                    "a fact about the provider, not about this code",
	"sub.double_bill":                          "a billing rule about periods, not about one request or one key",
}
