package planner

import (
	"sort"

	"github.com/katayunak/testigo/internal/scanningFlow/flowEntity"
	"github.com/katayunak/testigo/internal/testPlan"
	"github.com/katayunak/testigo/internal/testPlan/planEntity"
)

type Demand struct {
	Money      bool
	MainEntity bool
	States     map[string][]string
	Seams      map[string][]string

	Scenarios  []string
	Techniques []string

	Runnable int
	Blocked  int
}

func (d Demand) NeedsSeam(target string) ([]string, bool) {
	by, ok := d.Seams[target]
	return by, ok
}

func (d Demand) NeedsState(machine string) ([]string, bool) {
	by, ok := d.States[machine]
	return by, ok
}

func Demanded(f *flowEntity.Flow, facts planEntity.Facts) Demand {
	d := Demand{
		States: map[string][]string{},
		Seams:  map[string][]string{},
	}

	for _, c := range testPlan.Select(f, facts) {
		if !c.Runnable() {
			d.Blocked++
			continue
		}
		d.Runnable++
		d.Scenarios = appendOnce(d.Scenarios, c.Scenario.ID)
		if c.Technique != "" {
			d.Techniques = appendOnce(d.Techniques, string(c.Technique))
		}

		r := c.Scenario.Requires
		if r.MoneyFlows || r.TransferFunc || r.BalanceFunc {
			d.Money = true
		}
		if r.IdempotencyKey {
			d.Money = true
			d.MainEntity = true
		}
		if r.StateMachine {
			for _, m := range f.States {
				if len(m.States) < 2 {
					continue
				}
				d.States[m.Type] = appendOnce(d.States[m.Type], c.Scenario.ID)
			}
		}
		for _, s := range c.Seams {
			d.Seams[s.Target] = appendOnce(d.Seams[s.Target], c.Scenario.ID)
		}
	}

	sort.Strings(d.Scenarios)
	sort.Strings(d.Techniques)
	for k := range d.States {
		sort.Strings(d.States[k])
	}
	for k := range d.Seams {
		sort.Strings(d.Seams[k])
	}
	return d
}

func appendOnce(list []string, s string) []string {
	for _, x := range list {
		if x == s {
			return list
		}
	}
	return append(list, s)
}
