package planner

import "fmt"

type Method string

const (
	Skip   Method = "skip"
	Verify Method = "verify"
	Narrow Method = "narrow"
	Open   Method = "open"
)

func (m Method) Human() string {
	switch m {
	case Skip:
		return "not asked"
	case Verify:
		return "true/false"
	case Narrow:
		return "pick one"
	case Open:
		return "free text"
	}
	return string(m)
}

const (
	CharsPerToken    = 4
	OutputMultiplier = 5
)

type Cost struct {
	Input  int
	Output int
}

func (c Cost) Weighted() int { return c.Input + c.Output*OutputMultiplier }

func (c Cost) Add(o Cost) Cost { return Cost{Input: c.Input + o.Input, Output: c.Output + o.Output} }

func (c Cost) String() string {
	return fmt.Sprintf("%s in + %s out = %s", short(c.Input), short(c.Output), short(c.Weighted()))
}

func short(n int) string {
	if n < 1000 {
		return fmt.Sprintf("%d", n)
	}
	return fmt.Sprintf("%.1fk", float64(n)/1000)
}

func InputTokens(prompt string) int { return len(prompt) / CharsPerToken }

const (
	verifyPerField  = 5
	narrowPerField  = 15
	defaultPerField = 40
)

var measuredPerField = map[string]int{
	"externalEffect": 21,
	"stateRoles":     131,
	"mainEntity":     128,
	"moneyModel":     77,
}

func OutputTokens(kind string, m Method, fields int) int {
	if m == Skip {
		return 0
	}
	if fields < 1 {
		fields = 1
	}
	switch m {
	case Verify:
		return verifyPerField * fields
	case Narrow:
		return narrowPerField * fields
	}
	per, ok := measuredPerField[kind]
	if !ok {
		per = defaultPerField
	}
	return per * fields
}
