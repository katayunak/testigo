package domain

import (
	"fmt"
	"strings"
)

type Question struct {
	ID string `json:"id"`

	Text        string `json:"text"`
	Problem     string `json:"problem,omitempty"`
	TrueOrFalse bool   `json:"true_or_false,omitempty"`

	References   []Proof  `json:"references,omitempty"`
	Hints        []string `json:"hints,omitempty"`
	RelatedPaths []string `json:"related_paths,omitempty"`
}

func Discover(id, text, problem string) *Question {
	return &Question{
		ID:      id,
		Text:    text,
		Problem: problem,
	}
}

func (q *Question) Bool() *Question {
	q.TrueOrFalse = true
	return q
}

func (q *Question) At(paths ...string) *Question {
	q.RelatedPaths = append(q.RelatedPaths, paths...)
	return q
}

func (q *Question) Knowing(hints ...string) *Question {
	q.Hints = append(q.Hints, hints...)
	return q
}

func (q *Question) Citing(refs ...Proof) *Question {
	q.References = append(q.References, refs...)
	return q
}

func (q Question) Rechecking() bool { return len(q.References) > 0 }

func (q Question) AnswerShape() string {
	switch {
	case q.Rechecking():
		return "true/false (one line only if false)"
	case q.TrueOrFalse:
		return "true/false"
	}
	return "one sentence, or `unknown`"
}

func (q *Question) Generate() string {
	var b strings.Builder

	fmt.Fprintf(&b, "### `%s`\n\n%s\n", q.ID, strings.TrimSpace(q.Text))

	if q.Problem != "" {
		fmt.Fprintf(&b, "\nProblem: %s\n", strings.TrimSpace(q.Problem))
	}
	if len(q.References) > 0 {
		b.WriteString("\nFound in the source, and what this question re-checks:\n")
		for _, r := range q.References {
			fmt.Fprintf(&b, "  - %s\n", r)
		}
	}
	if len(q.Hints) > 0 {
		b.WriteString("\nAlready known (disagree if the code says otherwise):\n")
		for _, h := range q.Hints {
			fmt.Fprintf(&b, "  - %s\n", h)
		}
	}
	if len(q.RelatedPaths) > 0 {
		b.WriteString("\nThe answer is in these, and you should not need others:\n")
		for _, p := range q.RelatedPaths {
			fmt.Fprintf(&b, "  - %s\n", p)
		}
	}

	fmt.Fprintf(&b, "\n-> %s\n", q.AnswerShape())
	return b.String()
}

type QuestionAnswer struct {
	Verdict *bool  `json:"verdict,omitempty"`
	Info    string `json:"info,omitempty"`
}

func (a *QuestionAnswer) True() bool  { return a.Verdict != nil && *a.Verdict }
func (a *QuestionAnswer) False() bool { return a.Verdict != nil && !*a.Verdict }

func (a *QuestionAnswer) Validate(q Question) error {
	switch {
	case q.TrueOrFalse && a.Verdict == nil:
		return fmt.Errorf("%s is a true/false question and `verdict` is missing", q.ID)
	case q.Rechecking() && a.False() && strings.TrimSpace(a.Info) == "":
		return fmt.Errorf("%s came back false, which overturns something testigo proved from the source: "+
			"say in one line what is true instead", q.ID)
	case !q.TrueOrFalse && strings.TrimSpace(a.Info) == "":
		return fmt.Errorf("%s has an empty answer: say `unknown` rather than nothing", q.ID)
	}
	return nil
}
