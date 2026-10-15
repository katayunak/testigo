package scan

import (
	"errors"
	"fmt"
	"sort"

	"github.com/katayunak/gofacts/fact"
)

var ErrNoEntries = errors.New("no entry points: a payment flow has several (API handler, provider webhook, " +
	"reconciliation job, queue consumer), and following only one hides exactly the bugs worth finding")

// Flow is the part of the program reachable from entries, in the shape testigo
// stores as flow.json. Reachability follows resolved call edges between module
// functions, test code excluded; it is a set, so the order the walk runs in
// cannot be observed, and each node lists its calls in source order.
func (p *Program) Flow(entries []fact.EntryPoint) (*fact.Flow, error) {
	if len(entries) == 0 {
		return nil, ErrNoEntries
	}
	reached := map[string]bool{}
	var queue []string
	isEntry := map[string]bool{}
	for _, e := range entries {
		f, ok := p.funcs[e.ID()]
		if !ok {
			return nil, fmt.Errorf("entry point %q not found", e.ID())
		}
		isEntry[e.ID()] = true
		if !reached[e.ID()] {
			reached[e.ID()] = true
			queue = append(queue, f.Ref.ID())
		}
	}
	for len(queue) > 0 {
		cur := queue[0]
		queue = queue[1:]
		for _, e := range p.outEdges[cur] {
			if p.funcs[e.To].InTest || reached[e.To] {
				continue
			}
			reached[e.To] = true
			queue = append(queue, e.To)
		}
	}

	flow := fact.NewFlow(p.module)
	flow.Entries = entries
	for id := range reached {
		f := p.funcs[id]
		var calls []string
		seen := map[string]bool{}
		for _, e := range p.outEdges[id] {
			if !reached[e.To] || seen[e.To] {
				continue
			}
			seen[e.To] = true
			calls = append(calls, e.To)
		}
		pos := fact.NodePositionInternal
		switch {
		case isEntry[id]:
			pos = fact.NodePositionEntry
		case len(calls) == 0:
			pos = fact.NodePositionLeaf
		}
		flow.Nodes[id] = &fact.Node{Ref: f.Ref, Position: pos, Calls: calls, Facts: f.Facts}
	}

	for _, s := range p.seams {
		if reached[s.In.ID()] {
			flow.Seams = append(flow.Seams, s)
		}
	}
	for _, tx := range p.txs {
		if reached[tx.In.ID()] {
			flow.Transactions = append(flow.Transactions, tx)
		}
	}
	for _, f := range p.findings {
		if structural[f.ID] && !reached[f.Ref.ID()] {
			continue
		}
		flow.Findings = append(flow.Findings, f)
	}

	flow.States = p.States()
	flow.Infra = p.infra
	flow.TableWrites = p.TableWrites()
	flow.HashChains = p.HashChains()
	flow.IdempotencyKeys = p.IdempotencyKeys()
	flow.MoneyTypes = p.MoneyTypes()
	flow.Entities = map[string]string{}
	for k, v := range p.entities {
		flow.Entities[k] = v
	}
	flow.GeneratedFiles = len(p.generated)
	flow.GeneratedFindings = p.genFindings
	flow.LoadIssues = p.Issues()
	sort.SliceStable(flow.Seams, func(i, j int) bool {
		if flow.Seams[i].In.ID() != flow.Seams[j].In.ID() {
			return flow.Seams[i].In.ID() < flow.Seams[j].In.ID()
		}
		return flow.Seams[i].Line < flow.Seams[j].Line
	})
	return flow, nil
}
