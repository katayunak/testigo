package prompts

import (
	"fmt"
	"sort"
	"strings"

	"github.com/katayunak/testigo/internal/scanningFlow/flowEntity"
)

// MainEntity asks the one question the scanner provably cannot answer.
//
// Everything up to here is derivable. testigo knows which named types are
// lifecycles, which structs carry them, which fields arrive from outside the
// process, which are handed to a database call, and which columns a migration
// makes unique. That narrows a real recharge service from eight equally-scored
// fields — Phone, Name, three config structs — down to a handful of order
// identifiers on the entity that carries Status.
//
// It cannot get further, and the reason is not a missing heuristic. On that
// service the entity carries ID, UserID, OrderID, ProviderID, RRN and TID. All
// six are identifiers. All six are strings or UUIDs on the struct the payment
// moves through. Which one a CLIENT repeats when it retries is a fact about the
// contract between two systems, and no amount of AST walking reads a contract.
//
// So the question is narrow by construction: the candidates are listed, the
// proof is attached, and the agent chooses among them rather than searching.
// A narrow question with proof gets a far better answer than "find the
// idempotency key", and it costs a fraction of the tokens.
func MainEntity(f *flowEntity.Flow, entities []Entity) *Prompt {
	p := New("testigo — which entity does this flow move, and what identifies a repeat?").
		Goal("Two questions about the same struct. Both are contract questions: the analyser can see every field and still not know which one two systems agreed on.").
		Rule("Answer only from this repository. Read the files and lines named below.",
			"`null` is a real answer for the key. Plenty of payment systems have no deduplication key at all — saying so is a finding, and a wrong key is worse than an admitted absence, because it produces a replay test that passes while real duplicates get through.",
			"Do not pick a field because its name looks right. `OrderID` appears on the entity AND on three request types here; only one of them is the value a retry repeats.")

	var b strings.Builder

	if len(f.States) > 0 {
		b.WriteString("Lifecycle states found in this module:\n\n")
		for _, m := range f.States {
			b.WriteString(fmt.Sprintf("  %s  —  %d states, written in %d place(s), field `%s`\n",
				lastSegment(m.Type), len(m.States), len(m.Writes), m.Field))
		}
		b.WriteString("\n")
	}

	b.WriteString(`A struct carrying one of those types is an object this flow moves through
states, which is what makes it a candidate for the main entity.

`)

	for _, e := range entities {
		b.WriteString(fmt.Sprintf("### %s   (%s)\n\n", e.Name, e.File))
		if e.CarriesState != "" {
			b.WriteString(fmt.Sprintf("carries `%s`, so the flow moves it\n\n", e.CarriesState))
		}
		if len(e.Money) > 0 {
			b.WriteString("money fields: " + strings.Join(e.Money, ", ") + "\n\n")
		}
		b.WriteString("identifier fields, with what the analyser could prove about each:\n\n")
		for _, c := range e.IDs {
			b.WriteString(fmt.Sprintf("  %-22s %-14s line %d\n", c.Name, c.Type, c.Line))
			for _, ev := range c.Proof {
				b.WriteString("      + " + ev + "\n")
			}
			for _, ag := range c.Against {
				b.WriteString("      - " + ag + "\n")
			}
		}
		b.WriteString("\n")
	}

	p.Fact(Proof, "What the analyser proved", b.String())

	var work strings.Builder
	work.WriteString(`**1. Which struct is THE main entity of this flow?**

The one whose change IS the business event. When a row of it moves to a new
state, money has moved or is committed to moving. Everything else — requests,
responses, notifications, config — describes or reports that change without
being it. Usually one struct; say so if this flow genuinely has two.

**2. On that entity, which field is the idempotency key?**

The value a CALLER supplies and REPEATS on retry, so this system can recognise a
second delivery of the same request and refuse to do the work twice.

Three tests it has to pass, all three:

  - it arrives from outside — a client, a provider callback, a queue message.
    A value this process generates cannot deduplicate anything, because a retry
    generates a different one.
  - it is the same on a retry of the same business request, and different for a
    genuinely new one.
  - the code READS it before acting — a lookup, a WHERE clause, an ON CONFLICT.
    A field only ever written is an audit trail, not a guard.

**3. Say what the other identifiers are for.**

This is the part that stops a guess. A payment entity normally carries several
IDs for different reasons: a surrogate primary key, a user reference, a
provider's own reference, a bank retrieval number that only exists AFTER the
call, a terminal identifier. Naming each one is how you demonstrate the key was
chosen rather than picked.

A reference that arrives back FROM a provider is worth special care. It looks
exactly like a key and cannot be one, because this system does not have it at
the moment it must decide whether to act.

`)
	p.Fact(Subject, "What to work out", work.String())

	return p.Answers(`Reply with one JSON object and nothing else.

` + "```" + `
{
  "main_entity": {
    "struct": "Order",
    "package": "example.com/pay/domain/entity",
    "proof": "domain/entity/order.go:9 — carries Status, written in 122 places",
    "why": "one sentence: what real-world thing one row of it is"
  },
  "idempotency_key": {
    "field": "OrderID" | null,
    "proof": "domain/entity/order.go:12 — arrives in the request payload at controller/x.go:31 and is read back at repository/y.go:88 before the charge",
    "supplied_by": "client" | "provider" | "queue" | "unknown",
    "read_before_acting": true | false | "unknown",
    "confidence": "high" | "medium" | "low"
  },
  "other_identifiers": [
    { "field": "ID",     "purpose": "surrogate primary key, generated here", "could_be_key": false },
    { "field": "RRN",    "purpose": "bank retrieval number, arrives after authorization", "could_be_key": false },
    { "field": "UserID", "purpose": "who owns the order", "could_be_key": false }
  ],
  "no_key_reason": "fill this in ONLY if idempotency_key.field is null: say what stops duplicates instead, or say nothing does",
  "notes": ""
}
` + "```" + `

If ` + "`idempotency_key.field`" + ` is null, ` + "`no_key_reason`" + ` is required. "There is no
deduplication key and nothing else prevents a repeat" is a legitimate answer and
a serious finding — it is the shape of a double-charge, and testigo would rather
record it than invent a key that hides it.`).Where("testigo/flow.json")
}

// Entity is one candidate main entity, already narrowed by the scanner.
type Entity struct {
	Name         string
	Pkg          string
	File         string
	CarriesState string
	Money        []string
	IDs          flowEntity.Candidates

	// weight is how many places the lifecycle it carries is written. It ranks
	// the entities: a status written in 122 places is the flow's spine, one
	// written twice is an enum that happens to be called Status.
	weight int
}

// Entities groups the ranked candidates by the struct they belong to, keeping
// only structs that carry a lifecycle state.
//
// Grouping is what makes the prompt small. Listing 40 ranked fields across the
// repository asks the agent to do the narrowing testigo already did; listing
// two structs with their identifier fields asks it to do only the part that
// needs judgement.
func Entities(f *flowEntity.Flow, carriers map[string]string) []Entity {
	// How many places each lifecycle type is written. A status written in 122
	// places is the flow's spine; one written twice is an enum that happens to
	// be called Status.
	weight := map[string]int{}
	for _, m := range f.States {
		weight[m.Type] = len(m.Writes)
	}

	byOwner := map[string]*Entity{}
	for _, c := range f.IdempotencyKeys {
		state, isEntity := carriers[c.Owner]
		if !isEntity {
			continue
		}
		e, ok := byOwner[c.Owner]
		if !ok {
			e = &Entity{Name: c.Owner, File: c.File, CarriesState: state, weight: weight[state]}
			byOwner[c.Owner] = e
		}
		e.IDs = append(e.IDs, c)
	}
	for _, c := range f.MoneyTypes {
		if e, ok := byOwner[c.Owner]; ok {
			e.Money = append(e.Money, c.Name)
		}
	}

	out := make([]Entity, 0, len(byOwner))
	for _, e := range byOwner {
		e.IDs.Sort()

		// An entity with nothing DECLARING any of its fields a key is not a
		// candidate for this question, it is a struct that happens to carry a
		// status. On the recharge service that rule is the difference between
		// four entities worth asking about and fifteen — Provider, Package,
		// Contact and Type all carry Status and offer nothing but Name, Lang
		// and Message.
		declared := 0
		for _, c := range e.IDs {
			if c.Declarative {
				declared++
			}
		}
		if declared == 0 {
			continue
		}

		e.IDs = trimIDs(e.IDs)
		out = append(out, *e)
	}

	// Heaviest lifecycle first: the entity the flow actually moves should be the
	// first thing read, not the tenth.
	sort.Slice(out, func(i, j int) bool {
		if out[i].weight != out[j].weight {
			return out[i].weight > out[j].weight
		}
		if len(out[i].IDs) != len(out[j].IDs) {
			return len(out[i].IDs) > len(out[j].IDs)
		}
		return out[i].Name < out[j].Name
	})
	if len(out) > maxEntities {
		out = out[:maxEntities]
	}
	return out
}

const (
	// maxEntities caps the prompt. Beyond a handful the question stops being
	// "choose between these" and becomes "search this list", which is the
	// expensive open question this prompt exists to avoid.
	maxEntities = 4
	// maxOtherIDs is how many behaviour-only fields ride along per entity. They
	// are there so the agent can see what it is rejecting; they are not the
	// question, so they are capped.
	maxOtherIDs = 3
)

// trimIDs keeps every declared candidate and only a few of the rest.
//
// The behaviour-only fields matter — question 3 asks what the other identifiers
// are for, and an agent cannot answer that about fields it was never shown —
// but Price, Lang and Message are not identifiers by any reading, and paying to
// print them is paying for noise.
func trimIDs(ids flowEntity.Candidates) flowEntity.Candidates {
	var kept flowEntity.Candidates
	others := 0
	for _, c := range ids {
		if c.Declarative {
			kept = append(kept, c)
			continue
		}
		if others < maxOtherIDs {
			kept = append(kept, c)
			others++
		}
	}
	return kept
}

// lastSegment trims a fully qualified type down to pkg.Type, which is how a
// person refers to it and how it appears in the source the agent will read.
func lastSegment(qualified string) string {
	if i := strings.LastIndex(qualified, "/"); i >= 0 {
		return qualified[i+1:]
	}
	return qualified
}
