package askEntity

import (
	"strings"
	"testing"

	"github.com/katayunak/testigo/internal/scanningFlow/flowEntity"
)

// flowWith builds the smallest flow that carries a vocabulary. Only the fields
// the classifier reads are set, so a test says exactly which signal it is about.
func flowWith(tables []flowEntity.Table, entities []string, fields []string, symbols []string) *flowEntity.Flow {
	f := flowEntity.NewFlow("example.com/x")
	f.Infra.Tables = tables
	f.Entities = map[string]string{}
	for _, e := range entities {
		f.Entities[e] = "pkg.Status"
	}
	for _, name := range fields {
		f.MoneyTypes = append(f.MoneyTypes, flowEntity.Candidate{Name: name, Owner: "Order"})
	}
	for i, s := range symbols {
		f.Nodes[string(rune('a'+i))] = &flowEntity.Node{Ref: flowEntity.CodeRefOf("p", s, "x.go", 1)}
	}
	return f
}

func table(name string, cols ...string) flowEntity.Table {
	t := flowEntity.Table{Name: name, File: "1.sql", Line: 1}
	for _, c := range cols {
		t.Columns = append(t.Columns, flowEntity.Column{Name: c, Type: "text"})
	}
	return t
}

// TestClassifyTellsTheKindsApart. The whole saving depends on this: a repository
// classified wrongly is asked a set of questions that cannot apply to it, which
// is worse than asking nothing, because a plausible answer to an irrelevant
// question becomes a test.
func TestClassifyTellsTheKindsApart(t *testing.T) {
	for _, c := range []struct {
		name      string
		flow      *flowEntity.Flow
		wantSpine PaymentType
		wantMotor PaymentType
		notMotor  []PaymentType
	}{
		{
			name: "double entry ledger",
			flow: flowWith(
				[]flowEntity.Table{
					table("ledger_accounts", "id", "normal_side", "currency"),
					table("entries", "id", "transaction_id", "amount", "direction"),
				},
				nil, nil, []string{"PostTransaction", "assertBalanced"}),
			wantSpine: SpineDoubleEntry,
			notMotor:  []PaymentType{MotionTopup, MotionSubscription},
		},
		{
			name: "wallet and one-shot purchase",
			flow: flowWith(
				[]flowEntity.Table{
					table("wallets", "user_id", "balance"),
					table("orders", "id", "amount", "status"),
				},
				[]string{"Order"}, nil, []string{"DeductBalance", "CreateOrder"}),
			wantSpine: SpineWallet,
			notMotor:  []PaymentType{MotionTopup, MotionEscrow, MotionMarketplace},
		},
		{
			name: "authorize then capture",
			flow: flowWith(
				[]flowEntity.Table{
					table("payments", "id", "authorized_amount", "captured_amount", "captured_at"),
				},
				nil, nil, []string{"Capture", "Void"}),
			wantMotor: MotionAuthCapture,
			notMotor:  []PaymentType{MotionTopup},
		},
		{
			name: "subscription billing",
			flow: flowWith(
				[]flowEntity.Table{
					table("subscriptions", "id", "current_period_end", "billing_cycle_anchor"),
					table("invoices", "id", "amount"),
				},
				nil, nil, []string{"ChargeSubscription"}),
			wantMotor: MotionSubscription,
			notMotor:  []PaymentType{MotionTopup, MotionEscrow},
		},
		{
			name: "airtime top-up with no migrations at all",
			// This is the real case. The recharge service this tool was first
			// run against has no .sql files, so every signal has to come off
			// Go struct fields and function names.
			flow: flowWith(nil,
				[]string{"Order", "Contact", "Package", "Provider", "RetryableOrder"},
				[]string{"Phone", "Price", "BasePrice", "Discount", "Fee", "ProviderTraceID"},
				[]string{"GetStatus", "goForRetryableOrder", "NewRetryableOrder"}),
			wantSpine: SpineStateless,
			wantMotor: MotionTopup,
			notMotor:  []PaymentType{MotionSubscription, MotionMarketplace, MotionEscrow, MotionInstallments},
		},
		{
			name: "marketplace split",
			flow: flowWith(
				[]flowEntity.Table{
					table("orders", "id", "amount", "seller_id", "platform_fee"),
					table("seller_transfers", "id", "amount"),
				},
				nil, nil, []string{"SplitPayment"}),
			wantMotor: MotionMarketplace,
			notMotor:  []PaymentType{MotionTopup, MotionSubscription},
		},
	} {
		t.Run(c.name, func(t *testing.T) {
			got := Classify(c.flow)
			if c.wantSpine != "" && got.Spine != c.wantSpine {
				t.Errorf("spine = %s, want %s\n  why: %v", got.Spine, c.wantSpine, got.Why)
			}
			if c.wantMotor != "" && !hasType(got.Motions, c.wantMotor) {
				t.Errorf("motions = %v, want them to include %s\n  why: %v", got.Motions, c.wantMotor, got.Why)
			}
			for _, no := range c.notMotor {
				if hasType(got.Motions, no) {
					t.Errorf("motions = %v, must NOT include %s: its questions cannot apply here\n  why: %v",
						got.Motions, no, got.Why[no])
				}
			}
		})
	}
}

// TestClassificationCutsTheQuestionSet is the cost argument, asserted.
func TestClassificationCutsTheQuestionSet(t *testing.T) {
	all := 0
	for _, qs := range PaymentTypeQuestions {
		all += len(qs)
	}
	if all < 60 {
		t.Fatalf("only %d questions in the catalog; the test below proves nothing", all)
	}

	f := flowWith(nil,
		[]string{"Order", "Contact", "Package", "Provider"},
		[]string{"Phone", "Price", "BasePrice", "Discount"},
		[]string{"GetStatus", "goForRetryableOrder"})

	asked := Classify(f).Questions()
	if len(asked) == 0 {
		t.Fatal("a classified repository was asked nothing")
	}
	// The number matters. Asking all of them would mean the classification
	// bought nothing, and asking one would mean it is not covering the kind.
	if len(asked) > all/3 {
		t.Errorf("asked %d of %d questions; classification is not narrowing anything", len(asked), all)
	}
	t.Logf("asked %d of %d", len(asked), all)
}

// TestEveryQuestionIsWellFormed. These are shipped as data, so nothing at
// compile time checks them. A duplicate ID silently overwrites an answer, and an
// empty problem field wastes the one line that makes the answer usable.
func TestEveryQuestionIsWellFormed(t *testing.T) {
	seen := map[string]PaymentType{}
	for kind, qs := range PaymentTypeQuestions {
		if !kind.Known() {
			t.Errorf("%s has questions but no axis", kind)
		}
		if len(qs) < 2 {
			t.Errorf("%s has %d question(s): either it does not deserve to be a kind, or it is unfinished", kind, len(qs))
		}
		for _, q := range qs {
			switch {
			case q.ID == "":
				t.Errorf("%s: a question with no ID cannot be answered in a batch", kind)
			case seen[q.ID] != "" && seen[q.ID] != kind:
				t.Errorf("duplicate question ID %q in %s and %s: one answer would overwrite the other", q.ID, seen[q.ID], kind)
			case q.Problem == "":
				t.Errorf("%s/%s has no Problem: an agent told why answers, an agent told only the question describes the code back", kind, q.ID)
			case len(q.Text) < 40:
				t.Errorf("%s/%s is too short to be a real question", kind, q.ID)
			case !strings.Contains(q.Text, "?"):
				t.Errorf("%s/%s does not ask anything", kind, q.ID)
			}
			seen[q.ID] = kind
		}
	}
}

// TestMissingOverlayIsAFinding. The absence of a flow is information. A top-up
// service with no reversal path cannot give money back when a provider takes it
// and delivers nothing, and that happens every day.
func TestMissingOverlayIsAFinding(t *testing.T) {
	c := Classification{Spine: SpineStateless, Motions: []PaymentType{MotionTopup}}
	missing := c.MissingOverlays()
	if !hasType(missing, OverlayRefundReversal) {
		t.Errorf("a top-up with no refund path must be reported, got %v", missing)
	}

	withRefund := Classification{
		Spine: SpineStateless, Motions: []PaymentType{MotionTopup},
		Overlays: []PaymentType{OverlayRefundReversal, OverlayReconciliation},
	}
	if len(withRefund.MissingOverlays()) != 0 {
		t.Errorf("nothing is missing here, got %v", withRefund.MissingOverlays())
	}
}

func hasType(list []PaymentType, want PaymentType) bool {
	for _, t := range list {
		if t == want {
			return true
		}
	}
	return false
}
