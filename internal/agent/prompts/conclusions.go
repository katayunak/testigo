package prompts

import (
	"fmt"
	"strings"

	"github.com/katayunak/testigo/internal/agent/domain"
	"github.com/katayunak/testigo/internal/testPlan"
)

const markLegend = "+ the answer supports the claim, - it contradicts it, ? it could not be told.\n"

func markSymbol(s domain.Sign) string {
	switch s {
	case domain.SignSupports:
		return "+"
	case domain.SignContradicts:
		return "-"
	}
	return "?"
}

func conclusionsBlock(cs []domain.Conclusion, reasonShownElsewhere func(question string) bool) string {
	if len(cs) == 0 {
		return ""
	}
	var b strings.Builder
	var area domain.Area
	for _, c := range cs {
		if c.Feature.Area != area {
			area = c.Feature.Area
			fmt.Fprintf(&b, "%s\n", area.Human())
		}
		fmt.Fprintf(&b, "  %-13s  %s\n", c.Standing(), c.Feature.Claim)
		for _, m := range c.Marks {
			fmt.Fprintf(&b, "      %s  %s", markSymbol(m.Sign), m.Question)
			if m.Sign != domain.SignSupports && m.Info != "" && !reasonShownElsewhere(m.Question) {
				fmt.Fprintf(&b, " — %s", m.Info)
			}
			b.WriteString("\n")
		}
	}
	b.WriteString("\n")
	b.WriteString(markLegend)
	return b.String()
}

func seesOwnAnswer(c testPlan.TestCase, question string) bool {
	return strings.HasPrefix(question, c.Scenario.ID+".") ||
		strings.HasPrefix(question, "technique."+string(c.Technique)+".")
}

func caseConclusions(c testPlan.TestCase, k *domain.AgentResponse) string {
	if k == nil {
		return ""
	}
	var picked []domain.Conclusion
	for _, con := range domain.RollUp(k.PaymentKind) {
		if !con.Feature.InformsScenario(c.Scenario.ID) {
			continue
		}
		for _, m := range con.Marks {
			if !seesOwnAnswer(c, m.Question) {
				picked = append(picked, con)
				break
			}
		}
	}
	return conclusionsBlock(picked, func(q string) bool { return seesOwnAnswer(c, q) })
}
