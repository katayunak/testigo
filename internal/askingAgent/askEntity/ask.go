// Package askingAgent turns what testigo could not prove into questions for an
// agent, and turns the agent's answers back into checked data.
//
// The design rule for every prompt in this package: ask a BOUNDED question with
// a VERIFIABLE answer.
//
// "Read this code and tell me what it does" is unbounded. The answer cannot be
// checked, so a wrong answer is indistinguishable from a right one, and the
// model has no reason not to invent. "Here are six declared states and four
// write sites that testigo found in the source; which transitions between them
// should be impossible?" is bounded — the answer space is a finite matrix, and
// every claim has to point at a file and line that either exists or does not.
//
// Phase 1 exists to make these questions bounded. Everything it proved is
// pasted into the prompt as fact, so the agent is never asked to re-derive
// something a compiler already knows, and can never contradict it.
package askEntity

import (
	"fmt"
	"strings"

	"github.com/katayunak/testigo/internal/codeRef"
)

// Round separates context from generation.
//
// They are separate on purpose. A test generated in the same breath as the
// understanding it depends on has no chance to be corrected: if the agent
// decides the wrong call is the money-moving one, the test it writes in the
// same response will faithfully encode that mistake. Splitting the rounds gives
// a person one place to look and one thing to fix.
type Round int

const (
	// RoundUnderstand collects only context. No code is written.
	RoundUnderstand Round = 1

	// RoundGenerate asks for tests, using round one's answers as given.
	RoundGenerate Round = 2
)

func (r Round) String() string {
	switch r {
	case RoundUnderstand:
		return "understand"
	case RoundGenerate:
		return "generate"
	}
	return fmt.Sprintf("round(%d)", int(r))
}

// Kind is what a single ask is about.
type Kind string

const (
	// KindMoneyModel names the money model: which type is money, which function
	// moves it, which field carries the idempotency key. Everything else in
	// both rounds depends on this, so it is asked first and asked alone.
	KindMoneyModel Kind = "moneyModel"

	// KindMainEntity asks which struct the flow actually moves, and which of its
	// several identifiers a client repeats on a retry.
	//
	// It exists because the scanner narrows this question well and cannot close
	// it. Which value two systems agreed to repeat is a contract, not a
	// property of the syntax.
	KindMainEntity Kind = "mainEntity"

	// KindStateRoles asks what part each state plays. testigo derives the
	// transition matrix from the answer instead of paying for N x N cells.
	KindStateRoles Kind = "stateRoles"

	// KindPaymentKind asks the questions that only matter for THIS kind of
	// payment system.
	//
	// Which kind it is was decided in Go, from the migrations and the struct
	// names, so nobody pays to be told "this is a top-up service". What is left
	// is the part no static analysis reaches: whether delivered airtime can be
	// clawed back, whether the provider's requery endpoint re-submits the order,
	// which of its status values mean money was actually taken. Seventy-nine
	// questions exist; a repository sees the fifteen that can apply to it.
	KindPaymentKind Kind = "paymentKind"

	// KindNotes asks what one function does in business terms.
	KindNotes Kind = "notes"

	// KindTransitions asks which state transitions should be impossible.
	KindTransitions Kind = "transitions"

	// KindExternalEffect asks what one call outside this process actually does
	// to the world, and whether that can be undone.
	//
	// This used to ask "is a retry of this call safe?", which was the wrong
	// question in two ways. It presumed the repository HAS a retry policy, and
	// many do not. And it asked an agent to make a DESIGN decision — should this
	// be retried — when what testigo needs is an OBSERVATION about the code:
	// does the effect survive a rollback, and can the outcome be checked
	// afterwards.
	//
	// Those two facts are what a duplicate-effect test is built from, and unlike
	// a retry policy they are true whether or not anyone wrote a retry loop. A
	// client hitting refresh is a retry the server never opted into.
	KindExternalEffect Kind = "externalEffect"

	// KindTestCase asks for one executable test, described by one TestCase.
	//
	// One ask per case rather than one ask for everything. A single prompt
	// asking for four tests gets four shallow tests and no way to retry just
	// the one that failed to compile; one prompt per case is retryable,
	// cacheable, and reviewable on its own.
	KindTestCase Kind = "testCase"
)

// Ask is one question, rendered and ready to hand to an agent.
//
// There is no ID field. An ask IS its kind plus its subject, so storing an
// identity alongside them would be a third thing that can disagree with the
// other two. Kind says what shape the answer has; Subject says which specific
// thing it is about. ID() derives the name from both, which makes it impossible
// to write a manifest whose filenames do not match its contents.
type Ask struct {
	Kind  Kind  `json:"kind"`
	Round Round `json:"round"`

	// Title is one line, shown to a person choosing what to review.
	Title string `json:"title"`

	// Subject is what the ask is about: a node ID, a state machine type, a seam
	// target. Empty for asks about the whole flow.
	Subject string `json:"subject,omitempty"`

	// ForHash pins the ask to the exact code it was generated from. If the
	// function changes, the answer is stale and the ask is regenerated — the
	// same rule the sidecar uses for notes, applied to questions.
	ForHash string `json:"for_hash,omitempty"`

	// Prompt is the full text handed to the agent.
	Prompt string `json:"-"`
}

// ID names this question. Stable across runs for the same subject, so an answer
// file can be matched back without consulting the manifest.
func (a Ask) ID() string {
	if a.Subject == "" {
		return string(a.Kind)
	}
	return string(a.Kind) + "-" + Slug(a.Subject)
}

// AnswerFile is where the agent writes the reply.
func (a Ask) AnswerFile() string { return a.ID() + ".json" }

// Slug makes a string safe as a filename on every filesystem, and short enough
// to read in a directory listing.
func Slug(s string) string {
	var b strings.Builder
	lastDash := false
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
			b.WriteRune(r)
			lastDash = false
		default:
			if !lastDash {
				b.WriteByte('-')
				lastDash = true
			}
		}
	}
	out := strings.Trim(b.String(), "-")
	if len(out) > 72 {
		out = strings.Trim(out[len(out)-72:], "-")
	}
	return out
}

// Ref returns the code this ask is about, when it is about one function.
func (a Ask) Ref(refs map[string]codeRef.CodeRef) (codeRef.CodeRef, bool) {
	ref, ok := refs[a.Subject]
	return ref, ok
}
