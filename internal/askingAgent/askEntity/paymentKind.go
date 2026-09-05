package askEntity

import "sort"

// PaymentType is one shape a payment system can have.
//
// A repository is almost never ONE of these. An airtime recharge service keeps a
// customer balance (a wallet), delivers value to a phone number (a top-up), and
// runs a nightly reconciler — three different sets of bugs in one binary. So the
// types are split across three axes and a repository gets several:
//
//	spine    how value is held at rest.        Exactly one.
//	motion   how money moves.                  One or more.
//	overlay  flows that sit alongside a motion One or more, often zero, and a
//	         and have their own failures.      zero here is itself a finding.
//
// The point of the split is what it saves. There are about ninety questions
// below. A wallet + top-up + reconciliation service is asked nineteen of them
// and never sees the other seventy — no marketplace question, no subscription
// proration question, no escrow question. Questions that cannot apply are never
// written to a file, never read, and never paid for.
type PaymentType string

// Spine — how value is held at rest.
const (
	// SpineDoubleEntry: money moves as balanced legs. Two or more rows with
	// opposing signs that must sum to zero, and a balance that is derived.
	SpineDoubleEntry PaymentType = "double_entry"

	// SpineWallet: one mutable balance column that goes up and down.
	SpineWallet PaymentType = "wallet"

	// SpineStateless: no balance is held here at all. The money lives at the
	// PSP or the bank, and this system only records what it asked for.
	SpineStateless PaymentType = "stateless"
)

// Motion — how money moves.
const (
	MotionOneShot      PaymentType = "one_shot"
	MotionAuthCapture  PaymentType = "auth_capture"
	MotionSubscription PaymentType = "subscription"
	MotionUsageBilling PaymentType = "usage_billing"
	MotionTopup        PaymentType = "topup"
	MotionVoucher      PaymentType = "voucher"
	MotionPayout       PaymentType = "payout"
	MotionMarketplace  PaymentType = "marketplace"
	MotionEscrow       PaymentType = "escrow"
	MotionInstallments PaymentType = "installments"
)

// Overlay — flows that live alongside a motion.
const (
	OverlayRefundReversal PaymentType = "refund_reversal"
	OverlayReconciliation PaymentType = "reconciliation"
	OverlayFX             PaymentType = "fx_conversion"
)

// Axis says which of the three a type belongs to.
type Axis string

const (
	AxisSpine   Axis = "spine"
	AxisMotion  Axis = "motion"
	AxisOverlay Axis = "overlay"
)

var axisOf = map[PaymentType]Axis{
	SpineDoubleEntry: AxisSpine, SpineWallet: AxisSpine, SpineStateless: AxisSpine,

	MotionOneShot: AxisMotion, MotionAuthCapture: AxisMotion,
	MotionSubscription: AxisMotion, MotionUsageBilling: AxisMotion,
	MotionTopup: AxisMotion, MotionVoucher: AxisMotion, MotionPayout: AxisMotion,
	MotionMarketplace: AxisMotion, MotionEscrow: AxisMotion, MotionInstallments: AxisMotion,

	OverlayRefundReversal: AxisOverlay, OverlayReconciliation: AxisOverlay,
	OverlayFX: AxisOverlay,
}

func (p PaymentType) Axis() Axis { return axisOf[p] }

// Known reports whether this is a type testigo has questions for. Guards against
// a hand-edited config naming something that does not exist.
func (p PaymentType) Known() bool { _, ok := axisOf[p]; return ok }

// Human is the name to print. The slug is for files and JSON; this is for people.
func (p PaymentType) Human() string {
	if h, ok := humanName[p]; ok {
		return h
	}
	return string(p)
}

var humanName = map[PaymentType]string{
	SpineDoubleEntry:      "double-entry ledger",
	SpineWallet:           "single-balance wallet",
	SpineStateless:        "no balance held here",
	MotionOneShot:         "one-shot purchase",
	MotionAuthCapture:     "authorize then capture",
	MotionSubscription:    "subscription billing",
	MotionUsageBilling:    "usage-based billing",
	MotionTopup:           "top-up / recharge",
	MotionVoucher:         "voucher or gift card",
	MotionPayout:          "payout / disbursement",
	MotionMarketplace:     "marketplace split",
	MotionEscrow:          "escrow / hold and release",
	MotionInstallments:    "instalments / BNPL",
	OverlayRefundReversal: "refunds, reversals and chargebacks",
	OverlayReconciliation: "reconciliation against a provider",
	OverlayFX:             "currency conversion",
}

// Classification is what testigo decided this repository is.
type Classification struct {
	Spine    PaymentType   `json:"spine"`
	Motions  []PaymentType `json:"motions,omitempty"`
	Overlays []PaymentType `json:"overlays,omitempty"`

	// Why records the evidence for each decision, so a person can disagree with
	// it. A classification without its reasons is a number nobody can argue
	// with, and the classifier is a pile of heuristics that will be wrong.
	Why map[PaymentType][]string `json:"why,omitempty"`

	// Unsure lists the types that scored close to something else. These are
	// what the classification question asks about, when it is asked at all.
	Unsure []PaymentType `json:"unsure,omitempty"`
}

// All returns every type in the classification, spine first.
func (c Classification) All() []PaymentType {
	out := make([]PaymentType, 0, 1+len(c.Motions)+len(c.Overlays))
	if c.Spine != "" {
		out = append(out, c.Spine)
	}
	out = append(out, c.Motions...)
	return append(out, c.Overlays...)
}

// Questions returns the questions worth asking about this repository, and only
// those.
func (c Classification) Questions() []Question {
	var out []Question
	seen := map[string]bool{}
	for _, t := range c.All() {
		for _, q := range PaymentTypeQuestions[t] {
			if seen[q.ID] {
				continue
			}
			seen[q.ID] = true
			out = append(out, q)
		}
	}
	return out
}

// MissingOverlays names the overlays this kind of system normally has and this
// repository does not.
//
// Absence of a flow is a finding, not a reason to skip a question. A top-up
// service with no reversal path is not a simple system: it is a system that
// cannot give money back when a provider takes it and delivers nothing, which
// happens every day.
func (c Classification) MissingOverlays() []PaymentType {
	expected := map[PaymentType][]PaymentType{
		MotionTopup:       {OverlayRefundReversal, OverlayReconciliation},
		MotionOneShot:     {OverlayRefundReversal},
		MotionAuthCapture: {OverlayRefundReversal},
		MotionPayout:      {OverlayReconciliation},
		MotionMarketplace: {OverlayRefundReversal},
		SpineDoubleEntry:  {OverlayReconciliation},
	}
	have := map[PaymentType]bool{}
	for _, o := range c.Overlays {
		have[o] = true
	}
	missing := map[PaymentType]bool{}
	for _, t := range c.All() {
		for _, want := range expected[t] {
			if !have[want] {
				missing[want] = true
			}
		}
	}
	out := make([]PaymentType, 0, len(missing))
	for m := range missing {
		out = append(out, m)
	}
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out
}
