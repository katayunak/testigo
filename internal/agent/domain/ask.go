package domain

import (
	"fmt"
	"strings"

	"github.com/katayunak/testigo/internal/scanningFlow/flowEntity"
)

type Round int

const (
	RoundUnderstand Round = 1

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

type Kind string

const (
	KindMoneyModel Kind = "moneyModel"

	KindMainEntity Kind = "mainEntity"

	KindStateRoles Kind = "stateRoles"

	KindQuestions Kind = "questions"

	KindReport Kind = "report"

	KindExternalEffect Kind = "externalEffect"

	KindTestCase Kind = "testCase"
)

type Ask struct {
	Kind Kind `json:"kind"`

	Title string `json:"title"`

	Subject string `json:"subject,omitempty"`

	Questions []string `json:"questions,omitempty"`

	Prompt string `json:"-"`
}

func (a Ask) ID() string {
	if a.Subject == "" {
		return string(a.Kind)
	}
	return string(a.Kind) + "-" + Slug(a.Subject)
}

func (a Ask) AnswerFile() string { return a.ID() + ".json" }

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

func (a Ask) Ref(refs map[string]flowEntity.CodeRef) (flowEntity.CodeRef, bool) {
	ref, ok := refs[a.Subject]
	return ref, ok
}
