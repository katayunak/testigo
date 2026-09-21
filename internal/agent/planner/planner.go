package planner

import (
	"fmt"
	"sort"
)

type Candidate struct {
	Kind    string
	Subject string
	Title   string
	Prompt  string
	Asks    int
	Output  int
	Settled bool
	Stale   bool
}

type Choice struct {
	Candidate
	Method    Method
	Cost      Cost
	Trimmable Cost
	Value     int
	Chosen    bool
	Why       string
	By        []string
}

type Plan struct {
	Choices []Choice
	Budget  int
	Demand  Demand
}

func (p Plan) Chosen() []Choice {
	var out []Choice
	for _, c := range p.Choices {
		if c.Chosen {
			out = append(out, c)
		}
	}
	return out
}

func (p Plan) Total() Cost {
	var t Cost
	for _, c := range p.Choices {
		if c.Chosen {
			t = t.Add(c.Cost)
		}
	}
	return t
}

func (p Plan) Avoided() Cost {
	var t Cost
	for _, c := range p.Choices {
		if !c.Chosen {
			t = t.Add(c.asked())
		}
	}
	return t
}

func (c Choice) asked() Cost {
	fields := len(FieldsOf(c.Kind))
	return Cost{Input: InputTokens(c.Prompt), Output: OutputTokens(c.Kind, Open, fields)}
}

func Make(cands []Candidate, d Demand, budget int) Plan {
	p := Plan{Budget: budget, Demand: d}

	for _, c := range cands {
		p.Choices = append(p.Choices, decide(c, d))
	}

	sort.SliceStable(p.Choices, func(i, j int) bool {
		a, b := p.Choices[i], p.Choices[j]
		if a.Chosen != b.Chosen {
			return a.Chosen
		}
		return a.rank() > b.rank()
	})

	if budget > 0 {
		spent := 0
		for i := range p.Choices {
			if !p.Choices[i].Chosen {
				continue
			}
			w := p.Choices[i].Cost.Weighted()
			if spent+w > budget {
				p.Choices[i].Chosen = false
				p.Choices[i].Method = Skip
				p.Choices[i].Why = fmt.Sprintf("over the %s budget; %s already committed", short(budget), short(spent))
				continue
			}
			spent += w
		}
	}
	return p
}

func (c Choice) rank() float64 {
	w := c.Cost.Weighted()
	if w == 0 {
		return 0
	}
	return float64(c.Value) / float64(w) * 1000
}

func decide(c Candidate, d Demand) Choice {
	out := Choice{Candidate: c, Method: Open, Value: 1}

	all := FieldsOf(c.Kind)
	worth, use := Worth(c.Kind)

	if len(all) == 0 {
		out.Why = "not in the consumer registry — kept until someone records what reads it"
		out.Chosen = true
		out.Cost = Cost{Input: InputTokens(c.Prompt), Output: OutputTokens(c.Kind, Open, 1)}
		return out
	}
	if len(worth) == 0 {
		out.Method = Skip
		out.Why = "nothing reads any field of this answer"
		out.Cost = Cost{}
		return out
	}

	switch c.Kind {
	case "externalEffect":
		by, ok := d.NeedsSeam(c.Subject)
		if !ok {
			out.Method = Skip
			out.Why = "no runnable scenario touches this seam"
			return out
		}
		out.By = by
		out.Value = len(by)
		out.Method = Verify

	case "stateRoles":
		by, ok := d.NeedsState(c.Subject)
		if !ok {
			out.Method = Skip
			out.Why = "no runnable scenario uses this state machine"
			return out
		}
		out.By = by
		out.Value = len(by)

	case "moneyModel":
		if !d.Money {
			out.Method = Skip
			out.Why = "no runnable scenario needs the money model"
			return out
		}
		out.Value = d.Runnable
		if c.Settled {
			out.Method = Verify
		}

	case "mainEntity":
		if !d.MainEntity {
			out.Method = Skip
			out.Why = "no runnable scenario needs an idempotency key"
			return out
		}
		out.Value = d.Runnable

	case "notes":
		if !c.Stale {
			out.Method = Skip
			out.Why = "the note on disk still matches this function body"
			return out
		}
		out.Value = 1
	}

	if use == Dormant {
		out.Why = dormantReason(c.Kind)
	} else if out.Why == "" {
		out.Why = fmt.Sprintf("%d field(s) read downstream", len(worth))
	}

	asks := c.Asks
	if asks < 1 {
		asks = len(all)
	}

	out.Chosen = true
	outTok := c.Output
	if outTok == 0 {
		outTok = OutputTokens(c.Kind, out.Method, asks)
	}
	out.Cost = Cost{Input: InputTokens(c.Prompt), Output: outTok}
	if c.Output == 0 && asks > len(worth) {
		out.Trimmable = Cost{Output: OutputTokens(c.Kind, out.Method, asks-len(worth))}
	}
	return out
}

func dormantReason(kind string) string {
	for _, f := range FieldsOf(kind) {
		if f.Use == Dormant {
			return "kept, but " + f.Reader
		}
	}
	return "kept"
}
