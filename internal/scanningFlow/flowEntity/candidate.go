package flowEntity

import (
	"fmt"
	"sort"
	"strings"
)

// Candidate is a guess with its reasons attached.
//
// This type exists because of a mistake worth naming. An earlier version matched
// field names against a vocabulary — anything called `idem_key`, `request_id`,
// `reference_id`, `order_id` was "the idempotency key". On an Order struct that
// carries three of those at once, that is not a heuristic, it is a coin flip.
//
// Handing the coin flip to an agent does not fix it either. The agent sees the
// same three names and has the same information. A confident answer from a model
// is still a guess; it is just harder to audit.
//
// So testigo does not guess. It collects EVIDENCE — where the value comes from,
// what the code does with it, what the database enforces about it — scores each
// candidate, and shows its work. Most of the time one candidate wins clearly and
// no question is asked at all. When two tie, the agent gets a narrow
// multiple-choice question with the evidence attached, which is a far better
// question than "find the idempotency key".
type Candidate struct {
	// Name is the field or type being proposed.
	Name string `json:"name"`
	// Owner is the struct or package it belongs to.
	Owner string `json:"owner"`
	// Type is its Go type.
	Type string `json:"type,omitempty"`

	File string `json:"file"`
	Line int    `json:"line"`

	// Score is the sum of the evidence below. Comparable only against other
	// candidates for the same question.
	Score int `json:"score"`

	// Evidence is why, in words a person can check. Every entry names a fact.
	Evidence []string `json:"evidence"`

	// Against is the evidence pointing the other way. Kept rather than dropped:
	// a candidate that scored well DESPITE something suspicious is worth a
	// second look, and hiding the doubt would make the score look more certain
	// than it is.
	Against []string `json:"against,omitempty"`
}

func (c Candidate) String() string {
	return fmt.Sprintf("%s.%s (%s:%d) score=%d", c.Owner, c.Name, c.File, c.Line, c.Score)
}

// Candidates is a ranked list.
type Candidates []Candidate

func (cs Candidates) Sort() {
	sort.SliceStable(cs, func(i, j int) bool { return cs[i].Score > cs[j].Score })
}

// Decided reports whether one candidate wins clearly enough to skip the question.
//
// The margin is the whole design. A clear winner is used as a fact and costs
// nothing. A close call becomes a narrow question with the evidence attached.
// Requiring both a minimum score and a gap over the runner-up means a field that
// only matched on its name never wins by itself.
func (cs Candidates) Decided() (Candidate, bool) {
	if len(cs) == 0 {
		return Candidate{}, false
	}
	if len(cs) == 1 {
		return cs[0], cs[0].Score >= minDecisiveScore
	}
	top, next := cs[0], cs[1]
	return top, top.Score >= minDecisiveScore && top.Score-next.Score >= minDecisiveGap
}

const (
	// minDecisiveScore keeps a name-only match from winning. Name evidence is
	// worth 1; anything decisive needs at least one behavioural fact behind it.
	minDecisiveScore = 4
	// minDecisiveGap is how far ahead the winner must be. Two candidates within
	// two points of each other are a real question, not a decision.
	minDecisiveGap = 3
)

// Credible reports whether the evidence behind this candidate is strong enough
// to raise a finding about it without asking anyone first.
//
// Same bar as Decided, for the same reason. Name evidence is worth 1, so a
// field that only matched the vocabulary never clears it. A finding is an
// accusation, and an accusation built on a name match is the coin flip this
// whole type exists to avoid.
func (c Candidate) Credible() bool { return c.Score >= minDecisiveScore }

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
func (cs Candidates) Render() string {
	var b strings.Builder
	for i, c := range cs {
		fmt.Fprintf(&b, "  %d. %s.%s  %s\n", i+1, c.Owner, c.Name, c.Type)
		fmt.Fprintf(&b, "     %s:%d   score %d\n", c.File, c.Line, c.Score)
		for _, e := range c.Evidence {
			fmt.Fprintf(&b, "     + %s\n", e)
		}
		for _, a := range c.Against {
			fmt.Fprintf(&b, "     - %s\n", a)
		}
		b.WriteString("\n")
	}
	return b.String()
}

// Doc is one of the repository's own markdown files, ranked by how much it talks
// about money movement.
type Doc struct {
	Path   string   `json:"path"`
	Title  string   `json:"title,omitempty"`
	Score  int      `json:"score"`
	Topics []string `json:"topics,omitempty"`
	Bytes  int      `json:"bytes"`
}
