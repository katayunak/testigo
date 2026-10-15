package domain

type Area string

const (
	AreaMoney      Area = "money"
	AreaSeams      Area = "seams"
	AreaFlow       Area = "flow"
	AreaMainEntity Area = "entity"
)

var Areas = []Area{AreaMoney, AreaSeams, AreaFlow, AreaMainEntity}

func (a Area) Human() string {
	switch a {
	case AreaMoney:
		return "Money"
	case AreaSeams:
		return "Seams"
	case AreaFlow:
		return "Flow"
	case AreaMainEntity:
		return "Main entity"
	}
	return string(a)
}

type Evidence struct {
	Question  string
	HoldsWhen bool
}

func holdsIfTrue(question string) Evidence  { return Evidence{Question: question, HoldsWhen: true} }
func holdsIfFalse(question string) Evidence { return Evidence{Question: question, HoldsWhen: false} }

type Feature struct {
	ID       string
	Area     Area
	Claim    string
	Evidence []Evidence
	Informs  []string
}

func (f Feature) InformsScenario(id string) bool {
	for _, s := range f.Informs {
		if s == id {
			return true
		}
	}
	return false
}

type Sign int

const (
	SignUnknown Sign = iota
	SignSupports
	SignContradicts
)

type Mark struct {
	Question string
	Sign     Sign
	Info     string
}

func (e Evidence) mark(a *QuestionAnswer) Mark {
	m := Mark{Question: e.Question, Info: a.Info}
	switch {
	case a.Verdict == nil:
		m.Sign = SignUnknown
	case *a.Verdict == e.HoldsWhen:
		m.Sign = SignSupports
	default:
		m.Sign = SignContradicts
	}
	return m
}

type Standing string

const (
	StandingHolds       Standing = "holds"
	StandingDoesNotHold Standing = "does not hold"
	StandingUnsettled   Standing = "unsettled"
)

type Conclusion struct {
	Feature Feature
	Marks   []Mark
}

func (c Conclusion) Standing() Standing {
	unsettled := false
	for _, m := range c.Marks {
		switch m.Sign {
		case SignContradicts:
			return StandingDoesNotHold
		case SignUnknown:
			unsettled = true
		}
	}
	if unsettled {
		return StandingUnsettled
	}
	return StandingHolds
}

func RollUp(answers map[string]*QuestionAnswer) []Conclusion {
	var out []Conclusion
	for _, area := range Areas {
		for _, f := range Features {
			if f.Area != area {
				continue
			}
			c := Conclusion{Feature: f}
			for _, e := range f.Evidence {
				if a := answers[e.Question]; a != nil {
					c.Marks = append(c.Marks, e.mark(a))
				}
			}
			if len(c.Marks) > 0 {
				out = append(out, c)
			}
		}
	}
	return out
}
