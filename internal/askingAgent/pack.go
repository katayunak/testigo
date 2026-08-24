package askingAgent

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/katayunak/testigo/internal/askingAgent/askEntity"
)

const (
	asksDir    = "asks"
	answersDir = "answers"
)

// Pack is what a run of `testigo ask` produced.
type Pack struct {
	Round askEntity.Round `json:"round"`
	Asks  []askEntity.Ask `json:"asks"`
	Dir   string          `json:"-"`
}

// Cost is a rough token estimate for a pack, reported before anything is spent.
//
// Four characters per token is a crude approximation and deliberately so — the
// number is here to make a decision visible, not to bill anyone. Seeing "this
// pack is about 9k tokens" before handing it to an agent is the difference
// between choosing to spend it and finding out afterwards.
type Cost struct {
	Prompts       int
	Chars         int
	SharedChars   int
	EstTokens     int
	SavedByShared int
}

func Estimate(round askEntity.Round, asks []askEntity.Ask, preamble string) Cost {
	c := Cost{Prompts: len(asks)}
	for _, a := range asks {
		c.Chars += len(a.Prompt)
	}
	// Both rounds hoist. Round one hoists the flow map, which is far larger than
	// round two's shared rules and was the whole reason a 73-function service
	// cost a million tokens to ask about.
	c.SharedChars = len(preamble)
	if len(asks) > 1 {
		c.SavedByShared = len(preamble) * (len(asks) - 1) / 4
	}
	c.EstTokens = (c.Chars + c.SharedChars) / 4
	return c
}

// Write lays the pack out on disk under .testigo/.
//
// Files rather than an API call, so testigo does not care which agent you use,
// does not need a key, and does not need the network — which matters more than
// it sounds when the network is the thing that has been failing. The prompts are
// plain markdown a person can read and correct before spending anything on them,
// and the answers are plain JSON a person can hand-write when the agent gets one
// wrong. Every other transport can be added later behind the same two
// directories.
func Write(sidecarDir string, round askEntity.Round, asks []askEntity.Ask, preamble string) (*Pack, error) {
	adir := filepath.Join(sidecarDir, asksDir)
	rdir := filepath.Join(sidecarDir, answersDir)
	for _, d := range []string{adir, rdir} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			return nil, err
		}
	}

	// Old asks are removed so the directory always describes THIS run. A stale
	// prompt left behind is worse than a missing one: an agent will answer it,
	// and the answer will be applied to code that has since changed.
	old, _ := filepath.Glob(filepath.Join(adir, "*.md"))
	for _, f := range old {
		if err := os.Remove(f); err != nil {
			return nil, err
		}
	}

	for _, a := range asks {
		path := filepath.Join(adir, a.ID()+".md")
		if err := os.WriteFile(path, []byte(a.Prompt), 0o644); err != nil {
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
	// Both rounds get a preamble now. Round one's carries the flow map, which
	// used to be pasted into all 105 prompts.
	if preamble != "" {
		if err := os.WriteFile(filepath.Join(adir, "PREAMBLE.md"), []byte(preamble), 0o644); err != nil {
			return nil, err
		}
	}
	return pack, nil
}

func instructions(round askEntity.Round, asks []askEntity.Ask) string {
	var b strings.Builder
	fmt.Fprintf(&b, `# testigo — round %d (%s)

You are being asked %d question(s) about this repository. Everything a Go
compiler could already prove has been proved and is pasted into each prompt as
fact. You are only being asked for the context a compiler cannot read.

## How to do this

Work through the files below in the order listed. For each one:

1. Read `+"`asks/<id>.md`"+`.
2. Read the actual source it points at. Every prompt names files and lines.
3. Write your answer to `+"`answers/<id>.json`"+`.

The answer must be **one JSON object and nothing else** — no prose around it, no
markdown fence, no explanation. Each prompt ends with the exact shape expected.

## Rules that apply to every answer

- **Never contradict the facts section.** It came from the type checker and SSA.
  If your reading disagrees with it, you have misread the code.
- **Every claim carries a file:line.** If you cannot point at one, the answer is
  null.
- **"unknown" is a real answer.** Prefer it to a guess. Downstream treats unknown
  as the unsafe case, which is the correct default when money is involved.
- **Do not fix anything.** askEntity.Round %d is read-only. Do not edit source, do not
  suggest patches.
- **Do not answer the example.** Every prompt shows a filled-in example of the
  JSON shape. It uses placeholder names like `+"`example.com/pay`"+`. If those appear
  in your answer, you described the example instead of this repository.

## The questions

`, int(round), round, len(asks), int(round))

	// Grouped by kind, not listed one by one.
	//
	// This used to print all 106 questions with their titles: 21 KB of file
	// names in a file whose job is to explain the procedure. manifest.json
	// already holds the list in a form a program can read, and `ls asks/` holds
	// it in a form a person can read. A prose index of a directory is the
	// directory, retyped and paid for.
	byKind := map[askEntity.Kind][]askEntity.Ask{}
	var order []askEntity.Kind
	for _, a := range asks {
		if _, seen := byKind[a.Kind]; !seen {
			order = append(order, a.Kind)
		}
		byKind[a.Kind] = append(byKind[a.Kind], a)
	}
	for _, k := range order {
		group := byKind[k]
		fmt.Fprintf(&b, "**%s** — %d question(s)\n", k, len(group))
		// Name one so the shape of the filename is obvious, then stop.
		fmt.Fprintf(&b, "  `asks/%s.md`  →  `answers/%s`\n", group[0].ID(), group[0].AnswerFile())
		if len(group) > 1 {
			fmt.Fprintf(&b, "  ...and %d more of the same shape. `manifest.json` lists them all.\n", len(group)-1)
		}
		b.WriteString("\n")
	}

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
the states the compiler found, that named symbols carry evidence, that nothing
came back as the placeholder example. Anything malformed is reported with the
exact problem so you can fix that one file rather than redo the round.
`)
	if round == askEntity.RoundUnderstand {
		b.WriteString("\nThen `testigo ask --round 2` uses these answers to generate tests.\n")
	}
	return b.String()
}

// ReadPack loads the manifest written by the last `testigo ask`.
//
// The prompts are not reloaded, only the identities. Collect needs to know which
// questions were asked and what shape each answer should be — it does not need
// to re-read a megabyte of prompt text to check a JSON file.
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
