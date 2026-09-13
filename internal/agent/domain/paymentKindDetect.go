package domain

import (
	"sort"
	"strings"

	"github.com/katayunak/testigo/internal/scanningFlow/flowEntity"
)

func Classify(f *flowEntity.Flow) Classification {
	e := gather(f)
	c := Classification{Why: map[PaymentType][]string{}}

	score := map[PaymentType]int{}
	note := func(t PaymentType, n int, why string) {
		score[t] += n
		c.Why[t] = append(c.Why[t], why)
	}

	if e.anyTable("entries", "postings", "ledger_entries", "journal_entries", "book_entries") {
		note(SpineDoubleEntry, 3, "a table of entries or postings exists")
	}
	if e.anyTable("accounts", "ledger_accounts", "books") && e.anyColumn("normal_side", "account_type", "side", "direction") {
		note(SpineDoubleEntry, 3, "accounts carry a side or an account type")
	}
	if e.anySymbol("assertbalanced", "postentries", "posttransaction", "balancedlegs") {
		note(SpineDoubleEntry, 3, "a function posts a set of legs together")
	}
	if e.anyColumn("debit", "credit", "debit_amount", "credit_amount") {
		note(SpineDoubleEntry, 2, "columns named debit and credit")
	}

	if e.anyColumn("balance", "balance_cents", "credits", "points", "wallet_balance", "available_balance",
		"blocked_balance", "pending_balance") {
		note(SpineWallet, 3, "a mutable balance column")
	}
	if e.anyTable("wallets", "wallet", "balances", "user_balances") {
		note(SpineWallet, 2, "a wallets or balances table")
	}
	if e.anySymbol("deductbalance", "addbalance", "creditwallet", "debitwallet", "updatebalance", "chargewallet",
		"coretransfer", "singletransfer", "deposit", "withdraw", "topup", "reclaim", "ensurewallet") {
		note(SpineWallet, 2, "functions that add to and subtract from a balance")
	}

	switch {
	case score[SpineDoubleEntry] >= score[SpineWallet]+2:
		c.Spine = SpineDoubleEntry
	case score[SpineWallet] >= score[SpineDoubleEntry]+2:
		c.Spine = SpineWallet
	case score[SpineWallet] > 0 || score[SpineDoubleEntry] > 0:

		c.Spine = SpineWallet
		if score[SpineDoubleEntry] > score[SpineWallet] {
			c.Spine = SpineDoubleEntry
		}
		c.Unsure = append(c.Unsure, SpineDoubleEntry, SpineWallet)
	default:
		c.Spine = SpineStateless
		c.Why[SpineStateless] = []string{"no balance column and no entries table was found"}
	}

	motion := func(t PaymentType, n int, why string) {
		note(t, n, why)
	}

	if e.anyColumn("phone", "phone_number", "msisdn", "mobile", "meter_number",
		"decoder_number", "smartcard", "subscriber", "beneficiary", "sim", "sim_type") {
		motion(MotionTopup, 4, "a beneficiary identifier that is not the payer")
	}
	if e.anyColumn("operator_id", "biller_code", "product_code", "item_code", "bundle",
		"denomination", "plan_id", "charge_type", "purchase_type", "service_code",
		"package", "product") {
		motion(MotionTopup, 2, "a catalog or product dimension")
	}
	if e.anySymbol("recharge", "topup", "airtime", "getstatus", "get_status", "requery", "inquiry") {
		motion(MotionTopup, 2, "functions named for recharge or provider requery")
	}

	if e.anyColumn("provider_trace_id", "rrn", "stan", "tid", "provider_ref", "external_ref") &&
		e.anySymbol("status", "inquiry", "retry", "requery") {
		motion(MotionTopup, 3, "a provider reference that arrives late, and something that goes back to ask about it")
	}

	if e.anyColumn("price", "amount") &&
		e.anyColumn("base_price", "discount", "commission", "markup", "face_value",
			"delivered_amount", "payment_price") {
		motion(MotionTopup, 2, "a customer-facing amount and a second amount beside it")
	}

	if e.anyColumn("captured_at", "authorized_amount", "captured_amount", "amount_captured",
		"authorization_id", "auth_code") {
		motion(MotionAuthCapture, 4, "separate authorized and captured amounts")
	}
	if e.anySymbol("capture", "void", "authorize") && !e.anySymbol("capturelog") {
		motion(MotionAuthCapture, 2, "capture, void or authorize functions")
	}

	if e.anyTable("subscriptions", "plans", "invoices") || e.anyColumn("billing_cycle_anchor", "current_period_end", "trial_end") {
		motion(MotionSubscription, 4, "subscription or billing-period tables")
	}
	if e.anyTable("usage_events", "meter_events", "usage_records") || e.anyColumn("metered", "usage_quantity") {
		motion(MotionUsageBilling, 4, "metered usage events")
	}
	if e.anyTable("vouchers", "gift_cards", "codes", "pins") || e.anyColumn("voucher_code", "redeemed_at", "initial_value") {
		motion(MotionVoucher, 4, "a bearer-code table")
	}
	if e.anyTable("payouts", "disbursements", "withdrawals", "transfers_out") ||
		e.anySymbol("payout", "disburse", "withdraw") {
		motion(MotionPayout, 4, "payout or withdrawal paths")
	}

	if e.anyTable("splits", "seller_transfers", "sub_merchants") {
		motion(MotionMarketplace, 3, "a table of splits or per-seller transfers")
	}
	if e.anyColumn("platform_fee", "application_fee", "seller_amount") {
		motion(MotionMarketplace, 3, "a fee the platform keeps out of another party's money")
	}
	if e.anyColumn("seller_id", "merchant_id", "vendor_id") {
		motion(MotionMarketplace, 2, "a per-seller dimension on the money")
	}
	if e.anyTable("holds", "escrows", "reservations") ||
		e.anyColumn("held_amount", "pending_balance", "reserved_amount", "hold_expires_at") {
		motion(MotionEscrow, 4, "held or reserved funds")
	}
	if e.anyTable("installments", "instalments", "schedules", "repayments") ||
		e.anyColumn("installment_number", "due_date", "principal", "interest") {
		motion(MotionInstallments, 4, "an instalment schedule")
	}

	for t, s := range score {
		if t.Axis() == AxisMotion && s >= 4 {
			c.Motions = append(c.Motions, t)
		}
	}
	if len(c.Motions) == 0 && len(f.States) > 0 {
		c.Motions = []PaymentType{MotionOneShot}
		c.Why[MotionOneShot] = []string{"a status machine with no capture, balance, schedule or beneficiary dimension"}
	}

	if e.anySymbol("refund", "reverse", "reversal", "chargeback", "dispute", "cancel") ||
		e.anyState("REFUND", "REFUNDED", "REVERSED", "CHARGEBACK") ||
		e.anyTable("refunds", "reversals", "disputes") {
		note(OverlayRefundReversal, 4, "a refund, reversal or dispute path exists")
		c.Overlays = append(c.Overlays, OverlayRefundReversal)
	}
	if e.anySymbol("reconcile", "reconciliation", "settlement", "settle") ||
		e.anyTable("settlements", "reconciliations") {
		note(OverlayReconciliation, 4, "a reconciliation or settlement path exists")
		c.Overlays = append(c.Overlays, OverlayReconciliation)
	} else if e.anyColumn("retry_config", "retry_cron_input", "inquiry_cron_input") ||
		(e.anySymbol("cron", "task", "job", "sweep") && e.anySymbol("status", "retry", "inquiry")) {

		note(OverlayReconciliation, 4, "a scheduled job re-checks outcomes against a provider, which is reconciliation under another name")
		c.Overlays = append(c.Overlays, OverlayReconciliation)
	}
	if e.anyColumn("currency", "fx_rate", "exchange_rate", "base_currency", "target_currency") &&
		e.distinctCurrencies() {
		note(OverlayFX, 3, "more than one currency is represented")
		c.Overlays = append(c.Overlays, OverlayFX)
	}

	sort.Slice(c.Motions, func(i, j int) bool { return score[c.Motions[i]] > score[c.Motions[j]] })
	return c
}

type evidence struct {
	tables  map[string]bool
	columns map[string]bool

	fields map[string]bool

	symbols    string
	states     map[string]bool
	currencies map[string]bool
}

func gather(f *flowEntity.Flow) evidence {
	e := evidence{
		tables: map[string]bool{}, columns: map[string]bool{}, fields: map[string]bool{},
		states: map[string]bool{}, currencies: map[string]bool{},
	}
	for _, t := range f.Infra.Tables {
		e.tables[strings.ToLower(t.Name)] = true
		for _, c := range t.Columns {
			e.columns[strings.ToLower(c.Name)] = true
		}
	}

	for _, c := range f.Infra.Constraints {
		e.tables[strings.ToLower(c.Table)] = true
		for _, col := range c.Columns {
			e.columns[strings.ToLower(col)] = true
		}
	}

	addField := func(name string) {
		if name == "" {
			return
		}
		e.fields[strings.ToLower(name)] = true
		e.fields[snakeLower(name)] = true
	}
	for _, c := range f.IdempotencyKeys {
		addField(c.Name)
		addField(c.Owner)
	}
	for _, c := range f.MoneyTypes {
		addField(c.Name)
		addField(c.Owner)
	}
	for entity := range f.Entities {
		addField(entity)
	}

	var syms strings.Builder
	for _, n := range f.Nodes {
		syms.WriteString(strings.ToLower(n.Ref.Symbol))
		syms.WriteByte(' ')
	}
	e.symbols = syms.String()

	for _, m := range f.States {
		for _, s := range m.States {
			e.states[strings.ToUpper(s)] = true
		}
	}
	for _, ch := range f.Infra.Checks {
		for _, v := range ch.Values {
			if len(v) == 3 && v == strings.ToUpper(v) {
				e.currencies[v] = true
			}
		}
	}
	return e
}

func (e evidence) anyTable(names ...string) bool {
	for _, n := range names {
		if e.tables[n] || e.fields[n] || e.fields[strings.TrimSuffix(n, "s")] {
			return true
		}
	}
	return false
}

func (e evidence) anyColumn(names ...string) bool {
	for _, n := range names {
		if e.columns[n] || e.fields[n] {
			return true
		}
	}
	return false
}

func snakeLower(s string) string {
	var b strings.Builder
	for i, r := range s {
		if r >= 'A' && r <= 'Z' {
			if i > 0 {
				b.WriteByte('_')
			}
			r += 32
		}
		b.WriteRune(r)
	}
	return b.String()
}

func (e evidence) anySymbol(subs ...string) bool {
	for _, s := range subs {
		if strings.Contains(e.symbols, s) {
			return true
		}
	}
	return false
}

func (e evidence) anyState(names ...string) bool {
	for _, n := range names {
		if e.states[n] {
			return true
		}
	}
	for s := range e.states {
		for _, n := range names {
			if strings.Contains(s, n) {
				return true
			}
		}
	}
	return false
}

func (e evidence) distinctCurrencies() bool { return len(e.currencies) > 1 }
