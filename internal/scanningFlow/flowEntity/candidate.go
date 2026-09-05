package flowEntity

import (
	"fmt"
	"sort"
	"strings"
)

type Candidate struct {
	// Name is the field or type being proposed.
	Name string `json:"name"`
	// Owner is the struct or package it belongs to.
	Owner string `json:"owner"`
	// Type is its Go type.
	Type string `json:"type,omitempty"`

	File string `json:"file"`
	Line int    `json:"line"`

	// Score is the sum of the proof below. Comparable only against other
	// candidates for the same question.
	Score int `json:"score"`

	// Proof is why, in words a person can check. Every entry names a fact.
	Proof []string `json:"proof"`

	// Declarative is true when something DECLARES this field to be a key: its
	// name is in the vocabulary, or a migration puts a unique constraint on its
	// column.
	//
	// It exists because behavioural proof alone describes far too much. "The
	// value arrives from outside" and "the value is passed to a database call"
	// are both true of a phone number, a product code and every other query
	// parameter in a request — on a real recharge service that pair scored
	// Order.Phone a 6 and would have named it the idempotency key. Behaviour
	// says the value COULD be a key. Only a declaration says anyone INTENDED it
	// to be one.
	Declarative bool `json:"declarative,omitempty"`

	// Against is the proof pointing the other way. Kept rather than dropped:
	// a candidate that scored well DESPITE something suspicious is worth a
	// second look, and hiding the doubt would make the score look more certain
	// than it is.
	Against []string `json:"against,omitempty"`
}

func (c Candidate) String() string {
	return fmt.Sprintf("%s.%s (%s:%d) score=%d", c.Owner, c.Name, c.File, c.Line, c.Score)
}

type Candidates []Candidate

func (cs Candidates) Sort() {
	sort.SliceStable(cs, func(i, j int) bool {
		if cs[i].Declarative != cs[j].Declarative {
			return cs[i].Declarative
		}
		return cs[i].Score > cs[j].Score
	})
}

// Decided reports whether one candidate wins clearly enough to skip the question.
//
// The margin is the whole design. A clear winner is used as a fact and costs
// nothing. A close call becomes a narrow question with the proof attached.
// Requiring both a minimum score and a gap over the runner-up means a field that
// only matched on its name never wins by itself.
func (cs Candidates) Decided() (Candidate, bool) {
	if len(cs) == 0 {
		return Candidate{}, false
	}

	if len(cs) == 1 {
		return cs[0], cs[0].Credible()
	}

	top, next := cs[0], cs[1]
	return top, top.Credible() && top.Score-next.Score >= minDecisiveGap
}

const (
	// minDecisiveScore keeps a name-only match from winning. Name proof is
	// worth 1; anything decisive needs at least one behavioural fact behind it.
	minDecisiveScore = 4
	// minDecisiveGap is how far ahead the winner must be. Two candidates within
	// two points of each other are a real question, not a decision.
	minDecisiveGap = 3
)

// Credible reports whether the proof behind this candidate is strong enough
// to raise a finding about it without asking anyone first.
//
// Same bar as Decided, for the same reason. Name proof is worth 1, so a
// field that only matched the vocabulary never clears it. A finding is an
// accusation, and an accusation built on a name match is the coin flip this
// whole type exists to avoid.
func (c Candidate) Credible() bool { return c.Score >= minDecisiveScore && c.Declarative }

// Find returns the candidate for one owner and field, if it was scored at all.
func (cs Candidates) Find(owner, name string) (Candidate, bool) {
	for _, c := range cs {
		if c.Owner == owner && c.Name == name {
			return c, true
		}
	}
	return Candidate{}, false
}

// Render writes the ranked list for a prompt.
//
// Capped, because the ranking is the message and the tail is not. On a real
// payment service the uncapped list was 28 KB inside a single prompt — eighty
// candidates whose proof lines were near-identical copies of each other, and
// nobody, human or model, reads to number eighty. What matters is the top few
// and how far ahead the leader is.
func (cs Candidates) Render() string {
	shown := cs
	if len(shown) > maxRendered {
		shown = shown[:maxRendered]
	}
	var b strings.Builder
	for i, c := range shown {
		fmt.Fprintf(&b, "  %d. %s.%s  %s\n", i+1, c.Owner, c.Name, c.Type)
		fmt.Fprintf(&b, "     %s:%d   score %d\n", c.File, c.Line, c.Score)
		for _, e := range c.Proof {
			fmt.Fprintf(&b, "     + %s\n", e)
		}
		for _, a := range c.Against {
			fmt.Fprintf(&b, "     - %s\n", a)
		}
		b.WriteString("\n")
	}
	if len(cs) > len(shown) {
		fmt.Fprintf(&b, "  ...and %d lower-scoring candidate(s), in testigo/flow.json.\n"+
			"  They are not printed here: none of them beats the ones above, and\n"+
			"  printing them would cost more than reading them is worth.\n\n",
			len(cs)-len(shown))
	}
	return b.String()
}

// maxRendered is how many candidates a prompt is allowed to print. Past this
// the list stops informing a decision and starts being a data dump.
const maxRendered = 6

// Doc is one of the repository's own markdown files, ranked by how much it talks
// about money movement.
type Doc struct {
	Path   string   `json:"path"`
	Title  string   `json:"title,omitempty"`
	Score  int      `json:"score"`
	Topics []string `json:"topics,omitempty"`
	Bytes  int      `json:"bytes"`
}
