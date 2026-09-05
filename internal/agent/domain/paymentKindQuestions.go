package domain

var PaymentTypeQuestions = map[PaymentType][]Question{

	SpineDoubleEntry: {
		{
			ID:      "ledger.normal_side",
			Text:    "Which accounts hold money the business OWES to someone outside the system (liability), and which hold money the business OWNS (asset)? Give the exact values of the account type column.",
			Problem: "The balance sign convention is opposite for the two. One helper that always computes debits minus credits reports the negation of the truth for every customer balance, and the tests pass because the tests use asset accounts.",
		},
		{
			ID:          "ledger.invariant_outside_go",
			Text:        "Is the balanced-legs rule enforced anywhere except the Go function that writes entries — a CHECK constraint, a trigger, a stored procedure? And is there any OTHER writer to the entries table: an admin tool, a migration, a second service, a SQL runbook?",
			Problem:     "An invariant that lives only in one code path is not an invariant. It holds until the day someone runs an UPDATE by hand, and then it is false forever with nothing to detect it.",
			TrueOrFalse: true,
		},
		{
			ID:      "ledger.authoritative_balance",
			Text:    "Is the stored balance authoritative, or is the sum over entries authoritative? If the two disagree in production right now, which one does the business act on, and is there a job that notices the disagreement?",
			Problem: "A cached balance drifts the first time one path inserts an entry without updating the cache. If nobody has decided which is true, nobody can write the assertion.",
		},
		{
			ID:      "ledger.correction_shape",
			Text:    "What is a correction supposed to look like — a compensating transaction that references the original, or an amendment of the original rows? Describe one real historical correction.",
			Problem: "Corrections done as UPDATE or DELETE destroy the audit trail and make the ledger unreconstructable. The intended shape decides whether a reversal test asserts new rows or changed rows.",
		},
		{
			ID:      "ledger.external_counter_account",
			Text:    "Which account is the counterparty when money ENTERS the system from outside — a card capture, a bank credit? Is it a reconciled account or a suspense bucket, and who reconciles it?",
			Problem: "If the counterparty is an unreconciled black hole, conservation holds trivially and a conservation test proves nothing at all.",
		},
		{
			ID:          "ledger.multi_currency_legs",
			Text:        "Can one transaction legally contain legs in more than one currency?",
			Problem:     "Legs that sum to zero numerically and are nonsense financially. If the answer is no, that is a property test; if yes, something else has to make it balance and the test has to know what.",
			TrueOrFalse: true,
		},
	},

	SpineWallet: {
		{
			ID:      "wallet.negative_paths",
			Text:    "Can the balance go negative, and through which business paths? Name each one — chargeback clawback, fee settlement, manual adjustment, promo reversal.",
			Problem: "The purchase path checks for sufficient funds. The other paths usually do not, and they are the ones that produce a negative balance nobody expected.",
		},
		{
			ID:      "wallet.unit",
			Text:    "What is the unit of the balance column — minor units of one fixed currency, a currency that varies per row, or an abstract unit such as credits, points or data? Can one wallet hold more than one unit?",
			Problem: "Money units and product units in one column means rounding and conversion are applied inconsistently, and no test can tell a correct total from a wrong one.",
		},
		{
			ID:      "wallet.history_truth",
			Text:    "Is the movement history the source of truth for the balance, an audit log that should agree with it, or best-effort? Is there a job comparing the sum of history to the balance column, and what does the business do when they disagree?",
			Problem: "History written after the commit, or in a goroutine with the error ignored, makes the balance unauditable: you can see the number but not how it got there.",
		},
		{
			ID:      "wallet.in_flight_money",
			Text:    "When a purchase is in flight — debited, delivery unconfirmed — is that money considered spent, reserved, or still available? If there is no reserved concept, what happens today when delivery fails after the debit?",
			Problem: "A one-column wallet cannot express 'spent but not settled'. The money is then either double-spendable or it vanishes before the outcome is known, and which one it is decides the whole test.",
		},
		{
			ID:      "wallet.replayable_credits",
			Text:    "Which paths that ADD to a balance can be replayed by an external system — a provider webhook, a callback, a retried job — and what is supposed to make a replay a no-op?",
			Problem: "A duplicated debit generates a complaint. A duplicated credit is silent, so it survives in production until an audit finds it.",
		},
		{
			ID:          "wallet.other_writers",
			Text:        "Is there any writer to the balance column outside the functions in the flow map — an admin console, support tooling, a migration, another service?",
			Problem:     "Every guard tested here is bypassed by a path that is not here. If one exists, the test suite is measuring the wrong surface.",
			TrueOrFalse: true,
		},
	},

	SpineStateless: {
		{
			ID:      "stateless.source_of_truth",
			Text:    "If this system holds no balance, what IS the source of truth for what a customer is owed or has paid — the PSP, a bank file, another service? What happens when the local status column and that source disagree?",
			Problem: "A local status column with no balance behind it is a cache of somebody else's state. Tests that assert on it are asserting on a copy.",
		},
		{
			ID:      "stateless.reconstruct",
			Text:    "If this database were lost, could the money position be reconstructed from the external system alone? What would be missing?",
			Problem: "The answer names the fields that are only here, and those are exactly the ones whose loss is unrecoverable and whose writes must be durable.",
		},
	},

	MotionOneShot: {
		{
			ID:      "oneshot.money_leaves",
			Text:    "At exactly which line does the customer's money leave their account — the provider call, the webhook, or the status write? Is delivery of the goods before or after that point?",
			Problem: "Everything about ordering, retries and compensation depends on this one line, and it is the line people are most often wrong about.",
		},
		{
			ID:      "oneshot.key_conflict",
			Text:    "What is the client supposed to send as the idempotency key, is the client trusted to make it unique, and what should happen when the same key arrives with DIFFERENT parameters — a different amount, a different recipient?",
			Problem: "Same key, different amount is the interesting case and almost nobody handles it. Returning the first result silently charges the wrong amount; charging again defeats the key.",
		},
		{
			ID:      "oneshot.timeout_truth",
			Text:    "When the provider call times out or returns a network error, what is the truth about the money? Is there a query endpoint that resolves it, and is calling that repeatedly safe?",
			Problem: "A timeout is an UNKNOWN outcome, not a failed one. Treating it as failed and retrying double-charges; abandoning it charges for nothing.",
		},
		{
			ID:          "oneshot.failure_terminal",
			Text:        "Is a failed payment terminal, or can it be retried into the same row?",
			Problem:     "Stripe returns a PaymentIntent to requires_payment_method after a decline, so failure loops back. Code that treats failed as an end state breaks the first time a customer retries a declined card.",
			TrueOrFalse: true,
		},
		{
			ID:      "oneshot.webhook_race",
			Text:    "Can the provider webhook arrive BEFORE the synchronous call returns, or before the order row is committed? Which of the two is allowed to write the final status?",
			Problem: "Both paths write status. The loser overwrites a later state with an earlier one, and the row ends up permanently wrong.",
		},
	},

	MotionAuthCapture: {
		{
			ID:      "authcap.deadline",
			Text:    "What is the real capture deadline for each payment method this system authorizes, and where does that number come from — hardcoded, a provider field, config? What should happen to the order and to any reserved goods when it expires uncaptured?",
			Problem: "Authorization windows differ by card brand and method and are not in the code. An expiry nobody handles leaves goods reserved against money that no longer exists.",
		},
		{
			ID:          "authcap.partial",
			Text:        "Is partial capture ever intended? If so, is the remainder expected to stay capturable or be released?",
			Problem:     "Whether the remainder survives decides if a second capture is a legal operation or a bug, and the code usually does not say.",
			TrueOrFalse: true,
		},
		{
			ID:          "authcap.overcapture",
			Text:        "Can the captured amount ever exceed the authorized amount — tips, shipping, fuel, surcharge? By how much?",
			Problem:     "If overcapture is legal the amount assertion is a range, not an equality, and a test asserting equality is wrong in production.",
			TrueOrFalse: true,
		},
		{
			ID:      "authcap.amount_drift",
			Text:    "Between authorization and capture, what can change the amount — a removed line item, a discount, a weight adjustment? Who recomputes it, and against which stored value?",
			Problem: "An amount recomputed from a live cart rather than from the frozen order is the classic way a customer is charged for something they removed.",
		},
		{
			ID:      "authcap.void_or_refund",
			Text:    "When an authorization must be released, is the correct operation void or refund, and does the code choose based on the provider's settled state or on its own status column?",
			Problem: "Choosing from a local column that is stale sends a refund for money never captured, or a void for money already settled. Both fail, one silently.",
		},
	},

	MotionSubscription: {
		{
			ID:      "sub.access_by_status",
			Text:    "At which statuses should the customer still have access to the product, and at which should it be revoked? Specifically: past_due, unpaid, paused, incomplete.",
			Problem: "This is a business decision that lives in nobody's code. Getting it wrong either gives away the product or cuts off a paying customer over a temporary decline.",
		},
		{
			ID:      "sub.dunning",
			Text:    "What is the intended retry schedule after a failed charge — how many attempts, at what intervals — and what is the terminal action? Does that schedule live in this code, in the provider dashboard, or both?",
			Problem: "A schedule that lives in both places and differs means the system does something neither team believes it does.",
		},
		{
			ID:      "sub.proration",
			Text:    "When a plan changes mid-cycle, is the customer credited for unused time, charged immediately, or billed at the next cycle? Is a credit ever supposed to be refunded to the card rather than carried forward?",
			Problem: "Proration is arithmetic with a business rule on top, and the rule is the half that is never written down.",
		},
		{
			ID:      "sub.anchor_day",
			Text:    "What is the correct billing date when the anchor day does not exist in the target month — the 31st in February — and which timezone defines the billing day?",
			Problem: "Both produce a bug that only appears on specific calendar dates, which is exactly the kind a test can catch and a human review cannot.",
		},
		{
			ID:          "sub.double_bill",
			Text:        "Can a subscription legitimately be billed more than once for the same period — a manual re-run, a backfill, an ops retry?",
			Problem:     "If not, there is an intended uniqueness key for an invoice, and it should be asked for. If yes, no duplicate check can be written at all.",
			TrueOrFalse: true,
		},
	},

	MotionUsageBilling: {
		{
			ID:      "usage.clock",
			Text:    "Whose clock stamps a usage event — the client's or the server's? If the client's, how far into the past and future is a timestamp allowed to be?",
			Problem: "A client clock decides which billing period an event lands in, and a skewed one moves revenue between months.",
		},
		{
			ID:      "usage.late_event",
			Text:    "After a period is closed and invoiced, what should happen to an event timestamped INSIDE that period — drop it, credit-note it, or carry it into the next period?",
			Problem: "Late events always arrive. Without a rule the code picks one silently, and the choice is visible to the customer.",
		},
		{
			ID:          "usage.rerun_safe",
			Text:        "Is the aggregation job safe to re-run over a window already aggregated? If yesterday's window were re-run right now, would the total change?",
			Problem:     "A non-idempotent aggregation double-bills every time an operator re-runs it, and operators re-run things.",
			TrueOrFalse: true,
		},
		{
			ID:      "usage.credit_order",
			Text:    "In what exact order are credits, commitments and free tiers consumed, and what happens when a grant expires with a balance remaining?",
			Problem: "Consumption order changes the bill. It is a pricing decision encoded as a sort, and the sort is usually accidental.",
		},
	},

	MotionTopup: {
		{
			ID:      "topup.recoverable",
			Text:    "Is the value delivered by a successful top-up recoverable? If we deliver twice, or deliver to the wrong beneficiary, can it be clawed back — by us, by the provider, or not at all?",
			Problem: "This is the defining asymmetry of the kind. A duplicate card charge can be refunded; airtime on a prepaid SIM cannot be. If delivery is irreversible, then 'deliver twice' is strictly worse than 'never deliver', and every retry decision in the system has to be built around that.",
		},
		{
			ID:      "topup.requery_is_readonly",
			Text:    "When the provider call times out or returns ambiguously, what is the correct next action — retry the same request, call a status endpoint, or wait for a callback? Is the status/requery call guaranteed READ-ONLY at the provider, or can it re-submit the order?",
			Problem: "Some providers' status endpoints are read-only and some vendors' retry endpoints re-submit. A requery cron pointed at a mutating endpoint is a machine that double-recharges on a schedule.",
		},
		{
			ID:      "topup.status_semantics",
			Text:    "Map each value of the provider's status field to one of: we were charged and the subscriber got nothing / we were never charged / unknown. Which of these require crediting the customer back?",
			Problem: "FAILED and REFUNDED mean opposite things about whether money was taken — Reloadly's FAILED is 'no funds charged', its REFUNDED is 'charged and returned'. Collapsing them into one local status either double-credits the customer or never credits them at all.",
		},
		{
			ID:      "topup.debit_order",
			Text:    "Is the customer debited BEFORE or AFTER the provider is called? What restores their money when delivery fails after the debit, is that reversal automatic, and is it idempotent?",
			Problem: "The debit is in our database and the delivery is an HTTP call to a telco, so the two cannot be made atomic. Every implementation is a saga, and the direction chosen decides who loses money when it breaks.",
		},
		{
			ID:      "topup.beneficiary_validation",
			Text:    "How is the beneficiary identifier validated, and what should happen when the operator inferred from it is wrong — a ported number, a wrong prefix, an invalid length? Is there any correction path after delivery?",
			Problem: "A mistyped number delivers real value to a stranger, and unlike a bank transfer there is no recall. Validation is the only control that exists.",
		},
		{
			ID:      "topup.fulfilment_limit",
			Text:    "What limits our ability to fulfil — a float held with the provider, a per-operator quota, a daily cap? What should happen to an already-accepted order when that runs out mid-flight?",
			Problem: "Float exhaustion mid-day is routine. Orders accepted after it are either failed late or reversed hours later, after the customer's wallet was already debited.",
		},
		{
			ID:      "topup.amount_vs_delivered",
			Text:    "Where the amount charged and the amount delivered differ: which one is the customer charged, which does the subscriber receive, and what accounts for the difference — commission, discount, FX, or all three?",
			Problem: "A gap between the two is legitimate when it is commission and a bug when it is the wrong product code. One number cannot tell those apart, so the rule has to be stated.",
		},
	},

	MotionVoucher: {
		{
			ID:          "voucher.bearer",
			Text:        "Does possession of the code alone authorize redemption, or must it be tied to an account? What stops enumeration?",
			Problem:     "A bearer instrument with low-entropy codes and a redemption endpoint that distinguishes 'valid but used' from 'invalid' hands an attacker an oracle.",
			TrueOrFalse: true,
		},
		{
			ID:          "voucher.partial",
			Text:        "Can a voucher be partially redeemed? Where does the remainder live, is it re-redeemable, and does it inherit the original expiry?",
			Problem:     "A 50 voucher against a 30 order has to write the remaining 20 back atomically with the order, or the remainder is lost or duplicated.",
			TrueOrFalse: true,
		},
		{
			ID:      "voucher.lifecycle",
			Text:    "What is the intended lifecycle order, and which transitions are reversible? Can an activation be undone after a redemption?",
			Problem: "Redeemed-before-activated and activation-reversed-after-redemption are both reachable when the order is not enforced, and both create value from nothing.",
		},
		{
			ID:      "voucher.refund_destination",
			Text:    "When an order paid with a voucher is refunded, does value go back to the voucher, to a new voucher, or to a payment method? What if the original has expired?",
			Problem: "Refunding to a card what was paid in voucher value converts non-cash into cash, which is a real loss and usually unintended.",
		},
		{
			ID:      "voucher.breakage",
			Text:    "At what moment may the business treat an unredeemed voucher as revenue, and does that write anything that would block a later redemption?",
			Problem: "Recognising breakage while the code is still redeemable means the same value is counted twice.",
		},
	},

	MotionPayout: {
		{
			ID:      "payout.return_window",
			Text:    "After a payout reaches its success status, can the money still come back — an ACH return, a wire recall, a mobile-money reversal? How long is that window, and which event tells us?",
			Problem: "Success here is not final, which means the state machine has an edge out of a state everyone treats as terminal.",
		},
		{
			ID:      "payout.return_handling",
			Text:    "When a payout is returned or reversed, what must happen to the user's balance and to the payout row? Is that automatic today, or does an operator do it by hand?",
			Problem: "A manual path has none of the guards the tested path has, and it is the one that runs when money is already gone.",
		},
		{
			ID:          "payout.destination_snapshot",
			Text:        "Is the destination account snapshotted at request time or read at send time? If a user changes their bank details in between, which should be paid?",
			Problem:     "Reading at send time means an account change between request and send redirects money that was already approved.",
			TrueOrFalse: true,
		},
		{
			ID:      "payout.duplicate_rejection",
			Text:    "If a payout submission times out, is it safe to re-submit? What identifier does the provider use to reject a duplicate, and does it actually reject one?",
			Problem: "A provider that accepts the same reference twice makes retry unsafe no matter what our code does, and that has to be known before a retry test is written.",
		},
		{
			ID:          "payout.gross_or_net",
			Text:        "Is the fee deducted from the payout amount or charged separately — is the amount column gross or net?",
			Problem:     "Every reconciliation assertion is off by the fee if this is wrong, and off-by-a-fee looks like a rounding bug for months.",
			TrueOrFalse: true,
		},
	},

	MotionMarketplace: {
		{
			ID:      "market.liability",
			Text:    "Who is the merchant of record and who bears chargeback liability for each product on this platform — the platform or the seller? Is it the same for every transaction type here?",
			Problem: "Liability decides whose balance a dispute comes out of, and the code usually encodes one answer while the contract says another.",
		},
		{
			ID:      "market.remainder",
			Text:    "When a split does not divide evenly, where should the remainder go — the platform, the largest seller, the first seller, a rounding account? Is there a rule, or is it currently accidental?",
			Problem: "An accidental rule is still a rule, applied thousands of times a day, and it always favours the same party.",
		},
		{
			ID:      "market.partial_refund_fee",
			Text:    "On a partial refund, is the platform fee refunded proportionally, kept in full, or absorbed? Is the seller's transfer reversed automatically?",
			Problem: "Three parties and one refund. Any two of them can be made whole; the third is where the loss lands, and somebody has to have decided who.",
		},
		{
			ID:          "market.transfer_before_settlement",
			Text:        "Can the platform transfer to a seller before the customer's funds have settled? If a transfer is made and the charge is later disputed, how is money recovered from a seller who has already cashed out?",
			Problem:     "This is how marketplaces lose real money, and the recovery path is usually 'email them', which no test can assert on.",
			TrueOrFalse: true,
		},
		{
			ID:          "market.negative_seller",
			Text:        "Is a seller's balance allowed to go negative? What happens then — netted from future sales, debited from their bank, or written off?",
			Problem:     "A negative seller balance is the normal consequence of a dispute after a payout, so it is reachable whether or not anyone designed for it.",
			TrueOrFalse: true,
		},
	},

	MotionEscrow: {
		{
			ID:      "escrow.whose_balance",
			Text:    "While funds are held, whose balance are they on and can that party spend them? Where is 'available to spend' computed, and does it subtract open holds?",
			Problem: "If available does not subtract holds, held money is spendable, which is the entire failure this kind of system exists to prevent.",
		},
		{
			ID:      "escrow.all_endings",
			Text:    "What are ALL the ways a hold can end — release, return, expiry, partial release, split — and can two of them run concurrently? Which is supposed to win?",
			Problem: "Two endings racing is how the same held amount is paid out twice, and the resolution rule is never in the code.",
		},
		{
			ID:      "escrow.deadline",
			Text:    "Is there a deadline on a hold? What happens when it passes with no decision — auto-release to the beneficiary, auto-return to the payer, or stay held?",
			Problem: "The default here is a business and often a legal decision, and 'stay held forever' is the answer nobody chose but many systems implement.",
		},
		{
			ID:          "escrow.partial_release",
			Text:        "Can a hold be released for less than the held amount? Where does the remainder go, and is the hold then closed or still open?",
			Problem:     "A partially released hold that is marked closed strands the remainder with nothing pointing at it.",
			TrueOrFalse: true,
		},
		{
			ID:          "escrow.reversible_inbound",
			Text:        "Can a hold be placed against funds whose inbound payment can still be reversed? What happens if that inbound payment reverses while the funds are held, or after they were released?",
			Problem:     "Holding money that can be taken back means the guarantee the hold represents is not real, and the loss lands on the platform.",
			TrueOrFalse: true,
		},
	},

	MotionInstallments: {
		{
			ID:      "instal.allocation_order",
			Text:    "When a customer pays LESS than the instalment due, in what order is it applied — fees, interest, oldest principal, newest?",
			Problem: "Allocation order changes what the customer owes at the end. It is a regulated decision in many places and an accidental one in most code.",
		},
		{
			ID:      "instal.rounding",
			Text:    "Where does the rounding remainder go so the instalments sum exactly to the order total?",
			Problem: "Four instalments of a total that does not divide by four either lose a unit or create one, on every single order.",
		},
		{
			ID:      "instal.refund_midway",
			Text:    "If the underlying order is refunded when 2 of 4 instalments are paid — cancel the remaining schedule, refund what was paid, or both?",
			Problem: "Cancelling without refunding keeps money for goods returned; refunding without cancelling keeps charging for them.",
		},
		{
			ID:      "instal.early_settlement",
			Text:    "On early settlement, is the customer charged full remaining principal, a discounted amount, or given a rebate of unearned interest?",
			Problem: "The rebate calculation is the part that is regulated and the part that is guessed.",
		},
	},

	OverlayRefundReversal: {
		{
			ID:          "refund.can_fail_late",
			Text:        "Can a refund fail AFTER we have told the customer and our records that it succeeded? Which provider events represent that, and what should then happen to the balance and the order?",
			Problem:     "Bank-debit refunds can fail days later. A system with no state for 'refund failed after success' either loses the money or refunds twice.",
			TrueOrFalse: true,
		},
		{
			ID:      "refund.non_monetary",
			Text:    "What NON-monetary things must be reclaimed when money goes back — wallet credit, points, entitlement, delivered units? Which of those are actually reclaimable?",
			Problem: "Money is the easy half. The unreclaimable half is where the real loss is, and it is never in the refund function.",
		},
		{
			ID:      "refund.vs_chargeback",
			Text:    "What happens when a refund and a chargeback target the same payment? Is there a rule about which wins, and how is paying twice avoided?",
			Problem: "Refunding a payment that is already disputed pays the customer twice, and it is a race between a human and a network message.",
		},
		{
			ID:      "refund.dispute_deadline",
			Text:    "Is there a deadline in the dispute flow that something must respond to? What runs against it today, and what is the intended action when nobody responds?",
			Problem: "A missed deadline loses the dispute automatically. If nothing runs against it, the system's answer is always 'lose'.",
		},
		{
			ID:      "refund.terminal_statuses",
			Text:    "Can a decided dispute be re-opened — second chargeback, pre-arbitration, arbitration? Which status values are genuinely terminal?",
			Problem: "Adyen and the card networks model several rounds. A status treated as terminal that is not produces a state machine that cannot represent what happened.",
		},
	},

	OverlayReconciliation: {
		{
			ID:      "recon.authority",
			Text:    "Which side is authoritative when our record and the provider's file disagree — always the provider, always us, or does it depend on the field? Who decides in practice today?",
			Problem: "Without an answer the job either does nothing useful or silently overwrites correct data with someone else's mistake.",
		},
		{
			ID:      "recon.match_key",
			Text:    "What key is the match supposed to be on, and is it unique on BOTH sides? What is done when two rows match equally well?",
			Problem: "A key unique on one side only produces a match that is arbitrary, and arbitrary matches move money.",
		},
		{
			ID:          "recon.rerun_safe",
			Text:        "Can the reconciliation job be run twice over the same file or date range? What does it do the second time, and has that been tested against production data?",
			Problem:     "This job's corrective actions are writes. A non-idempotent one applies every correction twice the first time an operator re-runs it.",
			TrueOrFalse: true,
		},
		{
			ID:      "recon.autonomous_actions",
			Text:    "Which corrective actions may this job take on its own — credit a wallet, flip a status, create an adjustment, issue a refund — and which require a human?",
			Problem: "The boundary between what it may do alone and what it may only propose is the whole safety argument, and it is usually undocumented.",
		},
		{
			ID:      "recon.unmatched_ageing",
			Text:    "How long may an item sit unmatched before it is a real problem, and what happens to it then? Who looks at the exceptions?",
			Problem: "An exception queue nobody reads is the same as no reconciliation at all, and the test that matters is whether anything ages out of it.",
		},
		{
			ID:          "recon.corrected_files",
			Text:        "Do providers ever re-send a CORRECTED file for a period already reconciled? How should that be handled?",
			Problem:     "A corrected file replayed as a new one applies the same movements a second time.",
			TrueOrFalse: true,
		},
	},

	OverlayFX: {
		{
			ID:      "fx.currencies_and_exponents",
			Text:    "Which currencies does this system actually handle, and are minor-unit exponents taken from a generic table or from the provider's own list? Have any of the deviating ones appeared in production — CLP, ISK, IDR, CVE, HUF, TWD, JPY, KRW?",
			Problem: "The zero-decimal and three-decimal currencies are where a generic times-100 turns into a hundredfold error.",
		},
		{
			ID:      "fx.rate_validity",
			Text:    "How long is a quoted rate valid, and what should happen when it expires between quote and execution — re-quote, honour the old rate, or fail?",
			Problem: "Honouring an expired rate is a loss the business chose; failing is a customer complaint. The code picks one whether or not anybody decided.",
		},
		{
			ID:      "fx.exact_side",
			Text:    "Which side is exact and which is derived — does the customer specify what they pay or what the recipient receives? Which is allowed to move?",
			Problem: "Both sides fixed and a rate applied is an over-determined system, and something has to give: usually the rounding, silently.",
		},
		{
			ID:      "fx.rounding_direction",
			Text:    "In which direction is rounding supposed to go, and is it the same for a purchase and a refund?",
			Problem: "Rounding up on both sides leaks money on every round trip, in the business's favour, which makes it a compliance problem rather than a bug.",
		},
	},
}
