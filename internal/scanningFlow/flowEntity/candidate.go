package flowEntity

import (
	"fmt"
	"sort"
	"strings"
)

type Candidate struct {
	Name string `json:"name"`

	Owner string `json:"owner"`

	Type string `json:"type,omitempty"`

	File string `json:"file"`
	Line int    `json:"line"`

	Score int `json:"score"`

	Proof []string `json:"proof"`

	Declarative bool `json:"declarative,omitempty"`

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
	minDecisiveScore = 4

	minDecisiveGap = 3
)

func (c Candidate) Credible() bool { return c.Score >= minDecisiveScore && c.Declarative }

func (cs Candidates) Find(owner, name string) (Candidate, bool) {
	for _, c := range cs {
		if c.Owner == owner && c.Name == name {
			return c, true
		}
	}
	return Candidate{}, false
}

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

const maxRendered = 6

type Doc struct {
	Path   string   `json:"path"`
	Title  string   `json:"title,omitempty"`
	Score  int      `json:"score"`
	Topics []string `json:"topics,omitempty"`
	Bytes  int      `json:"bytes"`
}
