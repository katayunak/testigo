package askEntity

import (
	"fmt"
	"strings"
)

// Question is one thing testigo cannot work out for itself.
//
// The shape is chosen for what it costs to answer, not for what it costs to
// write. Three fields do the work:
//
//	Problem      the bug this question protects against. An agent told WHY a
//	             question is being asked answers the question; an agent told
//	             only the question describes the code back at you.
//	TrueOrFalse  a yes/no answer is a handful of output tokens, and output
//	             bills at roughly five times input. Where a boolean will do,
//	             ask for a boolean.
//	RelatedPaths where to look. This is the single largest saving in the whole
//	             tool: an agent that has to find the code first opens ten files
//	             to answer one question, and phase 1 already knows which line
//	             it is.
type Question struct {
	// ID is the key this question's answer is filed under. It has to be stable,
	// because a batch of answers is one JSON object keyed by ID and a renamed
	// key reads as a missing answer.
	ID string `json:"id"`

	Text        string `json:"text"`
	Problem     string `json:"problem,omitempty"`
	TrueOrFalse bool   `json:"true_or_false,omitempty"`

	// Hints are things testigo already knows that narrow the answer. They are
	// not the answer: an agent that disagrees with a hint should say so, because
	// a hint is phase 1's guess and phase 1 guesses wrong.
	Hints        []string `json:"hints,omitempty"`
	RelatedPaths []string `json:"related_paths,omitempty"`
}

func NewQuestion(id, text, problem string, tOrF bool) *Question {
	return &Question{ID: id, Text: text, Problem: problem, TrueOrFalse: tOrF}
}

// At records where the answer lives, so the agent does not go looking.
func (q *Question) At(paths ...string) *Question {
	q.RelatedPaths = append(q.RelatedPaths, paths...)
	return q
}

// Knowing records something phase 1 already proved.
func (q *Question) Knowing(hints ...string) *Question {
	q.Hints = append(q.Hints, hints...)
	return q
}

// Generate renders one question as a section of a prompt.
//
// Deliberately short. Every word here is paid for once per question, and on a
// pack with twenty questions a three-line preamble repeated twenty times is
// sixty lines of nothing.
func (q *Question) Generate() string {
	var b strings.Builder

	fmt.Fprintf(&b, "### `%s`\n\n%s\n", q.ID, strings.TrimSpace(q.Text))

	if q.Problem != "" {
		fmt.Fprintf(&b, "\nWhy it matters: %s\n", strings.TrimSpace(q.Problem))
	}
	if len(q.Hints) > 0 {
		b.WriteString("\nAlready known (disagree if the code says otherwise):\n")
		for _, h := range q.Hints {
			fmt.Fprintf(&b, "  - %s\n", h)
		}
	}
	if len(q.RelatedPaths) > 0 {
		// The instruction, not just the list. An agent given paths without being
		// told they are sufficient still opens the rest of the package.
		b.WriteString("\nThe answer is in these, and you should not need others:\n")
		for _, p := range q.RelatedPaths {
			fmt.Fprintf(&b, "  - %s\n", p)
		}
	}
	// The answer instruction is a marker, not a sentence.
	//
	// It used to spell out "answer in two sentences, unknown is a real answer"
	// under every question. That is a hundred and ten characters repeated once
	// per question, and a file with fifteen questions paid for it fifteen times
	// to say the same thing. The long form is stated once, at the top of the
	// file; this is the two words that say which of the two applies.
	if q.TrueOrFalse {
		b.WriteString("\n-> true/false + one line\n")
	} else {
		b.WriteString("\n-> two sentences, or `unknown`\n")
	}
	return b.String()
}

// AnswerShape is the JSON one Question is answered with. Uniform on purpose: a
// batch of thirty questions is one object of thirty of these, and a shape that
// varies per question is a shape that gets got wrong.
type QuestionAnswer struct {
	Answer  string `json:"answer"`
	Verdict *bool  `json:"verdict,omitempty"` // set for TrueOrFalse questions
	Proof   string `json:"proof,omitempty"`   // file:line, where there is one
}

// Validate checks the coherence of one answer, not its truth.
func (a *QuestionAnswer) Validate(q Question) error {
	switch {
	case q.TrueOrFalse && a.Verdict == nil:
		return fmt.Errorf("%s is a true/false question and `verdict` is missing", q.ID)
	case strings.TrimSpace(a.Answer) == "":
		return fmt.Errorf("%s has an empty answer: say `unknown` rather than nothing", q.ID)
	}
	return nil
}
