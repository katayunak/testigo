package domain

import "sort"

type PaymentType string

const (
	SpineDoubleEntry PaymentType = "double_entry"

	SpineWallet PaymentType = "wallet"

	SpineStateless PaymentType = "stateless"
)

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

const (
	OverlayRefundReversal PaymentType = "refund_reversal"
	OverlayReconciliation PaymentType = "reconciliation"
	OverlayFX             PaymentType = "fx_conversion"
)

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

func (p PaymentType) Known() bool { _, ok := axisOf[p]; return ok }

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

type Classification struct {
	Spine    PaymentType   `json:"spine"`
	Motions  []PaymentType `json:"motions,omitempty"`
	Overlays []PaymentType `json:"overlays,omitempty"`

	Why map[PaymentType][]string `json:"why,omitempty"`

	Unsure []PaymentType `json:"unsure,omitempty"`
}

func (c Classification) All() []PaymentType {
	out := make([]PaymentType, 0, 1+len(c.Motions)+len(c.Overlays))
	if c.Spine != "" {
		out = append(out, c.Spine)
	}
	out = append(out, c.Motions...)
	return append(out, c.Overlays...)
}

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
