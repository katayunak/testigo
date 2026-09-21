package planner

import (
	"strings"
	"testing"
)

func candidate(kind, subject string, fields int) Candidate {
	return Candidate{
		Kind:    kind,
		Subject: subject,
		Prompt:  strings.Repeat("x", 4000),
		Asks:    fields,
	}
}

func find(p Plan, kind, subject string) (Choice, bool) {
	for _, c := range p.Choices {
		if c.Kind == kind && c.Subject == subject {
			return c, true
		}
	}
	return Choice{}, false
}

func TestOutputIsWeightedFiveTimesInput(t *testing.T) {
	c := Cost{Input: 100, Output: 100}
	if got, want := c.Weighted(), 600; got != want {
		t.Fatalf("weighted = %d, want %d: an answer costs five times what the question does", got, want)
	}
}

func TestASeamNoScenarioTouchesIsNotAsked(t *testing.T) {
	d := Demand{
		Runnable: 3,
		States:   map[string][]string{},
		Seams:    map[string][]string{"psp.Authorize": {"IDEM-REPLAY"}},
	}
	p := Make([]Candidate{
		candidate("externalEffect", "psp.Authorize", 5),
		candidate("externalEffect", "grpc/status.New", 5),
	}, d, 0)

	used, _ := find(p, "externalEffect", "psp.Authorize")
	if !used.Chosen {
		t.Error("a seam a runnable scenario needs must be asked about")
	}
	if used.Method != Verify {
		t.Errorf("an external effect is five verdicts, got %q", used.Method)
	}

	unused, _ := find(p, "externalEffect", "grpc/status.New")
	if unused.Chosen {
		t.Error("asked about a seam no runnable scenario touches")
	}
}

func TestValuePerTokenDecidesWhatFitsTheBudget(t *testing.T) {
	d := Demand{
		Runnable: 9,
		States:   map[string][]string{},
		Seams: map[string][]string{
			"cheap.Wanted": {"A", "B", "C", "D", "E", "F"},
			"dear.Fringe":  {"G"},
		},
	}
	full := Make([]Candidate{
		candidate("externalEffect", "cheap.Wanted", 5),
		candidate("externalEffect", "dear.Fringe", 5),
	}, d, 0)
	if len(full.Chosen()) != 2 {
		t.Fatalf("without a budget both are worth asking, got %d", len(full.Chosen()))
	}

	one := full.Choices[0].Cost.Weighted()
	tight := Make([]Candidate{
		candidate("externalEffect", "cheap.Wanted", 5),
		candidate("externalEffect", "dear.Fringe", 5),
	}, d, one+10)

	if got := len(tight.Chosen()); got != 1 {
		t.Fatalf("a budget for one ask bought %d", got)
	}
	kept := tight.Chosen()[0]
	if kept.Subject != "cheap.Wanted" {
		t.Errorf("the budget kept %q; the one serving six scenarios should win", kept.Subject)
	}
	if tight.Total().Weighted() > one+10 {
		t.Errorf("spent %s over a budget of %d", tight.Total(), one+10)
	}
}

func TestAnUnregisteredKindIsKeptRatherThanSilentlyDropped(t *testing.T) {
	d := Demand{Runnable: 1, States: map[string][]string{}, Seams: map[string][]string{}}
	p := Make([]Candidate{candidate("somethingNew", "x", 3)}, d, 0)

	c, _ := find(p, "somethingNew", "x")
	if !c.Chosen {
		t.Fatal("an unknown kind must fail open: dropping it silently loses a question nobody decided to drop")
	}
	if !strings.Contains(c.Why, "registry") {
		t.Errorf("the reason should point at the registry, got %q", c.Why)
	}
}

func TestEveryRegisteredFieldNamesItsReader(t *testing.T) {
	for _, f := range Registry() {
		if f.Use != Unread && f.Reader == "" {
			t.Errorf("%s claims to be %s but names no reader", f.Path, f.Use)
		}
		if f.Use == Unread && f.Reader != "" {
			t.Errorf("%s names reader %q but is marked unread", f.Path, f.Reader)
		}
	}
}

func withRegistry(t *testing.T, fields []Field) {
	t.Helper()
	saved := registry
	registry = fields
	t.Cleanup(func() { registry = saved })
}

func TestAnAnswerNothingReadsIsNotBought(t *testing.T) {
	withRegistry(t, []Field{{Path: "sample.detail", Use: Unread}})
	d := Demand{Runnable: 5, States: map[string][]string{}, Seams: map[string][]string{}}
	p := Make([]Candidate{candidate("sample", "x", 1)}, d, 0)

	c, ok := find(p, "sample", "x")
	if !ok {
		t.Fatal("candidate vanished")
	}
	if c.Chosen {
		t.Error("bought an answer no code path reads")
	}
	if !strings.Contains(c.Why, "nothing reads") {
		t.Errorf("the reason has to say why, got %q", c.Why)
	}
	if c.Cost.Weighted() != 0 {
		t.Errorf("a skipped ask costs nothing, got %s", c.Cost)
	}
}

func TestUnreadFieldsAreReportedAsTrimmable(t *testing.T) {
	withRegistry(t, []Field{
		{Path: "sample.label", Use: Live, Reader: "report"},
		{Path: "sample.purpose", Use: Unread},
		{Path: "sample.effects", Use: Unread},
	})
	d := Demand{Runnable: 1, States: map[string][]string{}, Seams: map[string][]string{}}
	p := Make([]Candidate{candidate("sample", "x", 3)}, d, 0)

	c, _ := find(p, "sample", "x")
	if !c.Chosen {
		t.Fatal("sample.label is read, so the ask is worth making")
	}
	if c.Trimmable.Weighted() == 0 {
		t.Error("two of the three fields are unread; that should show as trimmable")
	}
	if !strings.Contains(p.Explain(), "STILL PAID FOR") {
		t.Error("explain must surface what is bought and never read")
	}
}
