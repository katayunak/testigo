package prompts

import (
	"fmt"
	"github.com/katayunak/testigo/internal/testPlan/planEntity"
	"strings"

	"github.com/katayunak/testigo/internal/scanningFlow/flowEntity"
)

// Preamble is written once per pack and read once by the agent.
//
// Everything here used to be repeated inside every prompt. On a pack of twelve
// test cases that is eleven redundant copies of the same eight hundred tokens,
// paid for on every request. Hoisting it into one file the agent reads at the
// start cuts a pack roughly in half, and it costs nothing structurally: these
// rules are identical for every case by construction, which is exactly the
// property that makes a shared prefix safe.
//
// It is also the piece a caching transport would mark as a stable prefix, so the
// same split pays twice if testigo ever gains an API path.
const Preamble = `# testigo — how to write these tests

Each ` + "`case-*.md`" + ` file describes ONE test. They share these rules. Read this
once; the case files will not repeat it.

## The rule that matters most

**Never derive an expected value from the implementation.**

The way generated tests fail is quiet: you read the code, infer what it produces,
and assert that. If the code has a bug, the test now asserts the bug and passes
forever. Green over broken code is worse than no test, because it stops anyone
looking.

Each case names its ORACLE — where "correct" comes from:

| Oracle | Meaning |
|---|---|
| ` + "`specification`" + ` | a published contract (Stripe's idempotency rules, Adyen's currency exponents) |
| ` + "`invariant`" + ` | a property of the domain, true whatever the code does (debits equal credits) |
| ` + "`metamorphic`" + ` | a relation between two runs, where neither answer need be known |
| ` + "`reference`" + ` | a second, deliberately simple implementation |

None of those come from the code under test. That is the point of naming them.

## Every test must prove it did something

A concurrency test whose goroutines never collided passes. A fault-injection test
whose fault never fired passes. Both are green and both checked nothing — and
this is the normal result of a fake wired up slightly wrong, not a rare one.

So every test asserts the interesting situation was REACHED:

    if fake.Calls() == 0 {
        t.Fatal("the provider fake was never called — this test proved nothing")
    }
    if collisions == 0 {
        t.Fatal("no two goroutines collided on the key — raise n or fix the barrier")
    }

This is part of the test, not an extra.

## Size is enforced, not described

A case marked ` + "`small`" + ` is checked mechanically after you write it. A small test
must not: sleep, read the wall clock, open a socket, touch the disk, start a
process, open a database, or use unseeded randomness. Violations are rejected
and sent back.

A case marked ` + "`medium`" + ` may use a real database and localhost. Emit it with
` + "`//go:build integration`" + ` as the first line so a bare ` + "`go test ./...`" + ` stays fast.

Never use ` + "`time.Sleep`" + ` in either. For goroutines, release from a barrier:

    var start sync.WaitGroup; start.Add(1)
    var done sync.WaitGroup
    for i := 0; i < n; i++ {
        done.Add(1)
        go func() { defer done.Done(); start.Wait(); attempt() }()
    }
    start.Done()
    done.Wait()

## Constraints

- Standard library ` + "`testing`" + ` only. No new module dependencies. If a case truly
  needs one, mark it blocked and name the dependency instead of adding it.
- The file must compile and pass ` + "`go vet`" + `. It is built and run automatically;
  anything that does not compile is discarded and sent back.
- Fakes must satisfy the repository's own interfaces. Where a case says a seam is
  NOT injectable, you cannot fake it — mark that case blocked and name the
  interface that would need extracting.

## Blocked is a real answer

If a case cannot be written honestly, say so and say what is missing. Do not
write a weaker test that looks like it covers the case. "We could not test this
because the provider is called on a concrete type" is a useful result. A test
that quietly asserts something easier is not.

## Output

One JSON object per case file, written to ` + "`answers/<case-id>.json`" + `. Nothing else
— no prose around it, no markdown fence.

` + "```" + `
{
  "status": "written" | "blocked",
  "blocked_reason": "required when blocked",
  "needed": "what would unblock it",
  "file": { "path": "api/idempotency_testigo_test.go", "package": "api",
            "content": "package api\n\nimport (\n\t\"testing\"\n)\n..." },
  "func_name": "TestIdemConcurrent",
  "fakes_added": ["fakeGateway"],
  "reached_assertions": ["fails if the provider fake was never called"],
  "expected_to_fail": "why this should go red on the current code, or empty",
  "oracle_used": "specification"
}
` + "```" + `

` + "`expected_to_fail`" + ` is not a disclaimer. If your reading says the property does
not hold, write the test anyway and declare it. A test that goes red on the first
run and names a real gap is the most valuable thing this produces. Weakening an
assertion so the suite comes back green is the least.
`

// TestCase renders one case.
//
// The shape follows the slots that actually change an answer: what the outcome
// is, what is fixed, what counts as done, and what a wrong answer looks like.
// The last slot is the one usually skipped and the one that does the most work —
// a bad generated test is bad in a predictable way, and naming the way in advance
// beats any amount of "be careful".
func TestCase(c planEntity.TestCase, f *flowEntity.Flow) *Prompt {
	var b strings.Builder
	props := c.Technique.Props()

	fmt.Fprintf(&b, "GOAL       %s\n", c.Scenario.LookingFor)
	fmt.Fprintf(&b, "FUNC       %s\n", c.FuncName)
	fmt.Fprintf(&b, "FILE       %s   (package %s)\n", c.TargetFile, c.TargetPkg)
	fmt.Fprintf(&b, "TECHNIQUE  %s\n", c.Technique)
	fmt.Fprintf(&b, "SIZE       %s", c.Size)
	if tag := c.Size.BuildTag(); tag != "" {
		fmt.Fprintf(&b, "   (emit `//go:build %s` as the first line)", tag)
	}
	fmt.Fprintf(&b, "\nORACLE     %s\n", c.Scenario.Oracle)
	fmt.Fprintf(&b, "SEVERITY   %s\n\n", c.Scenario.Severity)

	fmt.Fprintf(&b, "Why this technique: %s.\n", props.Uniquely)
	fmt.Fprintf(&b, "Its limit: it cannot %s.\n", props.Cannot)
	fmt.Fprintf(&b, "How it usually goes wrong: %s.\n\n", props.FailureMode)

	b.WriteString("## SCENARIO\n\n")
	b.WriteString(c.Scenario.CaseScenario)
	b.WriteString("\n\n")

	b.WriteString("## DONE — the test passes when all of these hold\n\n")
	for _, a := range c.Scenario.Acceptance {
		fmt.Fprintf(&b, "- %s\n", a)
	}

	b.WriteString("\n## NOT — wrong versions of this test, do not write these\n\n")
	for _, a := range c.Scenario.AntiGoals {
		fmt.Fprintf(&b, "- %s\n", a)
	}

	p := NewGenerate(fmt.Sprintf("%s · %s", c.Scenario.ID, c.Scenario.Name)).
		Goal("Write ONE test. Read `PREAMBLE.md` first: it holds the rules, this file holds only this case.").
		Fact(Proof, "This case", b.String()).
		Where("testigo/asks/PREAMBLE.md")

	// Proof priority, and the reason is the whole design of round two: these
	// are the facts THIS case needs and nothing else. Cutting them would make
	// the agent go and re-derive them from source, which is what including
	// them was meant to prevent.
	return p.Fact(Proof, "FACTS — proved by the compiler, do not re-derive", caseFacts(c, f))
}

// caseFacts includes only what THIS case needs.
//
// The temptation is to paste the whole flow into every prompt so the agent
// "has context". That is the most expensive habit available: on a twelve-case
// pack it multiplies the largest block in the prompt by twelve, and most of it
// is irrelevant to any given case. A currency-conversion test does not need the
// call graph. A concurrency test does not need the state machine.
func caseFacts(c planEntity.TestCase, f *flowEntity.Flow) string {
	var b strings.Builder

	if len(c.Seams) > 0 {
		b.WriteString("Boundaries this test must fake:\n\n")
		for _, s := range c.Seams {
			inj := "NOT INJECTABLE — concrete type, cannot be substituted"
			if s.Injectable {
				inj = "fake via " + s.Iface
			}
			fmt.Fprintf(&b, "  %s\n    called in %s at %s:%d\n    %s\n\n",
				s.Target, s.In.Symbol, s.In.File, s.Line, inj)
		}
	}

	if c.States != nil {
		fmt.Fprintf(&b, "State machine %s (field %s), complete set of declared states:\n\n",
			c.States.Type, c.States.Field)
		for _, s := range c.States.States {
			fmt.Fprintf(&b, "  %s\n", s)
		}
		b.WriteString("\nWhere the status is assigned:\n\n")
		for _, w := range c.States.Writes {
			tx := "outside any transaction"
			if w.InTx {
				tx = "inside a transaction"
			}
			fmt.Fprintf(&b, "  -> %-20s in %s (%s:%d, %s)\n", w.To, w.In.Symbol, w.In.File, w.Line, tx)
		}
		b.WriteString("\n")
	}

	if c.Entry != nil {
		fmt.Fprintf(&b, "Entry point under test: %s#%s", c.Entry.Pkg, c.Entry.Symbol)
		if c.Entry.Label != "" {
			fmt.Fprintf(&b, "   (%s)", c.Entry.Label)
		}
		b.WriteString("\n")
	}

	return b.String()
}
