package domain

import (
	"strings"
	"testing"
)

func boolp(b bool) *bool { return &b }

func TestAnswerShapeFollowsTheMode(t *testing.T) {
	cases := []struct {
		name string
		q    *Question
		want string
	}{
		{"recheck is bounded and says when to explain",
			Recheck("r", "is it?", "why"), "true/false (one line only if false)"},
		{"a discovered true/false costs one token",
			Discover("d", "is it?", "why").Bool(), "true/false"},
		{"an open question is capped at one sentence",
			Discover("o", "what is it?", "why"), "one sentence, or `unknown`"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := c.q.AnswerShape(); got != c.want {
				t.Errorf("got %q, want %q", got, c.want)
			}
		})
	}
}

func TestATrueVerdictNeedsNoExplanation(t *testing.T) {
	q := *Recheck("money.type", "is Money the money type?", "everything downstream reads this")
	a := &QuestionAnswer{Verdict: boolp(true)}
	if err := a.Validate(q); err != nil {
		t.Fatalf("a bare true must be a complete answer, got %v", err)
	}
}

func TestAFalseRecheckMustSayWhatIsTrueInstead(t *testing.T) {
	q := *Recheck("money.type", "is Money the money type?", "everything downstream reads this",
		Proof{Symbol: "domain.Money", At: "domain/payment.go:12"})

	bare := &QuestionAnswer{Verdict: boolp(false)}
	err := bare.Validate(q)
	if err == nil {
		t.Fatal("a false verdict overturns a proved fact; refusing it without a reason is the whole point")
	}
	if !strings.Contains(err.Error(), "what is true instead") {
		t.Errorf("wrong reason: %v", err)
	}

	explained := &QuestionAnswer{Verdict: boolp(false), Info: "the amount is on Order.Cents"}
	if err := explained.Validate(q); err != nil {
		t.Errorf("an explained false must be accepted: %v", err)
	}
}

func TestADiscoveredFalseNeedsNoExplanation(t *testing.T) {
	q := *Discover("multi_currency", "can one transaction hold two currencies?", "legs that sum to nonsense").Bool()
	a := &QuestionAnswer{Verdict: boolp(false)}
	if err := a.Validate(q); err != nil {
		t.Fatalf("only a recheck has a fact to overturn, got %v", err)
	}
}

func TestAMissingVerdictIsNotASilentFalse(t *testing.T) {
	q := *Discover("q", "is it?", "why").Bool()
	a := &QuestionAnswer{Info: "I could not tell"}
	if err := a.Validate(q); err == nil || !strings.Contains(err.Error(), "verdict") {
		t.Fatalf("a missing verdict must be reported, not defaulted, got %v", err)
	}
}

func TestAnOpenQuestionCannotComeBackEmpty(t *testing.T) {
	q := *Discover("q", "what is it?", "why")
	a := &QuestionAnswer{}
	if err := a.Validate(q); err == nil || !strings.Contains(err.Error(), "unknown") {
		t.Fatalf("silence and `unknown` must not look identical, got %v", err)
	}
}

func TestReferencesReachThePrompt(t *testing.T) {
	q := Recheck("idem.key", "is OrderID the key?", "a retry test needs a key to retry with",
		Proof{Symbol: "Order.OrderID", At: "domain/order.go:12"}).
		At("api/server.go").
		Knowing("the value arrives in the request body")

	out := q.Generate()
	for _, want := range []string{
		"Order.OrderID", "domain/order.go:12",
		"api/server.go", "arrives in the request body",
		"-> true/false (one line only if false)",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("the agent would have to go looking for %q:\n%s", want, out)
		}
	}
}
