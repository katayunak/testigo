package agent

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/katayunak/testigo/internal/agent/domain"
)

const (
	asksDir    = "asks"
	answersDir = "answers"
)

type Pack struct {
	Round domain.Round `json:"round"`
	Asks  []domain.Ask `json:"asks"`
	Dir   string       `json:"-"`
}

type Cost struct {
	Prompts       int
	Chars         int
	SharedChars   int
	EstTokens     int
	SavedByShared int
}

func Estimate(round domain.Round, asks []domain.Ask, preamble string) Cost {
	c := Cost{Prompts: len(asks)}
	for _, a := range asks {
		c.Chars += len(a.Prompt)
	}

	c.SharedChars = len(preamble)
	if len(asks) > 1 {
		c.SavedByShared = len(preamble) * (len(asks) - 1) / 4
	}
	c.EstTokens = (c.Chars + c.SharedChars) / 4
	return c
}

func Write(sidecarDir string, round domain.Round, asks []domain.Ask, preamble string) (*Pack, error) {
	adir := filepath.Join(sidecarDir, asksDir)
	rdir := filepath.Join(sidecarDir, answersDir)
	for _, d := range []string{adir, rdir} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			return nil, err
		}
	}

	old, _ := filepath.Glob(filepath.Join(adir, "*.md"))
	for _, f := range old {
		if err := os.Remove(f); err != nil {
			return nil, err
		}
	}

	for _, b := range Batches(asks) {
		path := filepath.Join(adir, b.ID()+".md")
		if err := os.WriteFile(path, []byte(b.Prompt()), 0o644); err != nil {
			return nil, err
		}
	}

	pack := &Pack{Round: round, Asks: asks, Dir: adir}
	manifest, err := json.MarshalIndent(pack, "", "  ")
	if err != nil {
		return nil, err
	}
	if err := os.WriteFile(filepath.Join(adir, "manifest.json"), append(manifest, '\n'), 0o644); err != nil {
		return nil, err
	}
	if err := os.WriteFile(filepath.Join(adir, "INSTRUCTIONS.md"), []byte(instructions(round, asks)), 0o644); err != nil {
		return nil, err
	}

	if preamble != "" {
		if err := os.WriteFile(filepath.Join(adir, "PREAMBLE.md"), []byte(preamble), 0o644); err != nil {
			return nil, err
		}
	}
	return pack, nil
}

func instructions(round domain.Round, asks []domain.Ask) string {
	var b strings.Builder
	fmt.Fprintf(&b, `# testigo — round %d (%s)

You are being asked %d question(s) about this repository, grouped into %d files.
Everything a Go compiler could already prove has been proved and is stated as
fact. You are only being asked for the context a compiler cannot read.

## How to do this

Read PREAMBLE.md once. Then, for each file below:

1. Read `+"`asks/<name>.md`"+`. It holds every question of that kind.
2. Read the source it points at. Every question names files and lines, and
   those are sufficient — you should not need to search the repository.
3. Write ONE answer file, `+"`answers/<name>.json`"+`, covering all of them.

**Answer each file in a single pass.** One file at a time is fine; one QUESTION
at a time is not. Every turn re-reads everything said so far, so answering
sixty questions in sixty turns costs far more than the questions do — that is
where the money goes, not in the prompts.

The answer must be **one JSON object and nothing else** — no prose around it, no
markdown fence. A file with several questions is answered by one object whose
keys are the answer keys printed above each question.

## Rules that apply to every answer

- **Never contradict the facts section.** It came from the type checker and SSA.
  If your reading disagrees with it, you have misread the code.
- **Every claim carries a file:line.** If you cannot point at one, the answer is
  null.
- **"unknown" is a real answer.** Prefer it to a guess. Downstream treats unknown
  as the unsafe case, which is the correct default when money is involved.
- **Do not fix anything.** This round is read-only. Do not edit source, do not
  suggest patches.
- **Do not answer the example.** Every prompt shows a filled-in example of the
  JSON shape. It uses placeholder names like `+"`example.com/pay`"+`. If those appear
  in your answer, you described the example instead of this repository.

## The questions

`, int(round), round, len(asks), len(Batches(asks)))

	for _, batch := range Batches(asks) {
		fmt.Fprintf(&b, "`asks/%s.md`  ->  `answers/%s`   (%d question(s))\n",
			batch.ID(), batch.AnswerFile(), len(batch.Asks))
	}
	b.WriteString("\n")

	b.WriteString(`## Read PREAMBLE.md once, then keep it in front of you

Every question below is written to be short because the shared half — the flow
map, the rules for each kind of question, the JSON shapes — lives in
PREAMBLE.md instead of being repeated in all of them.

If your tooling caches prompt prefixes, put PREAMBLE.md FIRST and identical in
every call, and the question last. The preamble is then paid for once rather
than once per question, and on a pack this size that is most of the bill.

`)
	b.WriteString("## When you are done\n\nRun:\n\n```sh\ntestigo collect\n```\n\n")
	b.WriteString(`It validates every answer against the facts — that the states you listed are
the states the compiler found, that named symbols carry proof, that nothing
came back as the placeholder example. Anything malformed is reported with the
exact problem so you can fix that one file rather than redo the round.
`)
	if round == domain.RoundUnderstand {
		b.WriteString("\nThen `testigo ask --round 2` uses these answers to generate tests.\n")
	}
	return b.String()
}

func ReadPack(sidecarDir string) (*Pack, error) {
	b, err := os.ReadFile(filepath.Join(sidecarDir, asksDir, "manifest.json"))
	if err != nil {
		return nil, err
	}
	var p Pack
	if err := json.Unmarshal(b, &p); err != nil {
		return nil, fmt.Errorf("manifest is corrupt: %w", err)
	}
	p.Dir = filepath.Join(sidecarDir, asksDir)
	return &p, nil
}
