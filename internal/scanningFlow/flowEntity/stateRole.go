package flowEntity

import (
	"fmt"
	"sort"
	"strings"
)

type StateRole struct {
	State string `json:"state"`

	Initializing bool `json:"initializing,omitempty"`
	InProgress   bool `json:"in_progress,omitempty"`
	Pending      bool `json:"pending,omitempty"`
	Final        bool `json:"final,omitempty"`

	Compensating bool `json:"compensating,omitempty"`
	Retryable    bool `json:"retryable,omitempty"`

	RetryEntersAt string `json:"retry_enters_at,omitempty"`

	Foreign  bool `json:"foreign,omitempty"`
	Sentinel bool `json:"sentinel,omitempty"`
	Unclear  bool `json:"unclear,omitempty"`

	Proof string `json:"proof,omitempty"`
}

func (r StateRole) phase() string {
	switch {
	case r.Foreign:
		return "foreign"
	case r.Sentinel:
		return "sentinel"
	case r.Unclear:
		return ""
	case r.Initializing:
		return "initializing"
	case r.InProgress:
		return "inProgress"
	case r.Pending:
		return "pending"
	case r.Final:
		return "final"
	}
	return ""
}

func (r StateRole) Validate() error {
	var set []string
	for name, on := range map[string]bool{
		"initializing": r.Initializing, "in_progress": r.InProgress,
		"pending": r.Pending, "final": r.Final,
		"foreign": r.Foreign, "sentinel": r.Sentinel, "unclear": r.Unclear,
	} {
		if on {
			set = append(set, name)
		}
	}
	sort.Strings(set)

	switch {
	case len(set) == 0:
		return fmt.Errorf("%s: no role given — every state is somewhere in the lifecycle, "+
			"outside it (foreign/sentinel), or genuinely unclear", r.State)
	case len(set) > 1:
		return fmt.Errorf("%s: %s are mutually exclusive, pick one", r.State, strings.Join(set, " and "))
	}

	if (r.Compensating || r.Retryable) && (r.Foreign || r.Sentinel || r.Unclear) {
		return fmt.Errorf("%s: compensating/retryable describe how a payment moves through the "+
			"lifecycle, so they cannot apply to a state outside it", r.State)
	}

	if r.Retryable && !r.Final {
		return fmt.Errorf("%s: retryable means a payment can leave a state it otherwise could not, "+
			"which only says something about a FINAL state", r.State)
	}
	if r.RetryEntersAt != "" && !r.Retryable {
		return fmt.Errorf("%s: retry_enters_at names where a retry lands, so it means nothing "+
			"unless the state is retryable", r.State)
	}
	if r.Proof == "" && !r.Unclear {
		return fmt.Errorf("%s: no proof — name the file and line that shows this", r.State)
	}
	return nil
}

type StateRoles []StateRole

func (rs StateRoles) Derive(neverAssigned []string) map[string][]string {
	unreachable := map[string]bool{}
	for _, s := range neverAssigned {

		unreachable[s] = true
	}

	byState := map[string]StateRole{}
	var active, finals, inits, foreign []string
	for _, r := range rs {
		byState[r.State] = r
		switch r.phase() {
		case "foreign":
			foreign = append(foreign, r.State)
		case "initializing":
			inits = append(inits, r.State)
		case "inProgress", "pending":

			active = append(active, r.State)
		case "final":
			finals = append(finals, r.State)
		}
	}
	sort.Strings(active)
	sort.Strings(finals)
	sort.Strings(foreign)

	reachable := func(list []string) []string {
		var out []string
		for _, s := range list {
			if !unreachable[s] {
				out = append(out, s)
			}
		}
		return out
	}

	out := map[string][]string{}
	for _, r := range rs {
		out[r.State] = []string{}
	}

	for _, from := range inits {
		out[from] = append(reachable(active), reachable(finals)...)
		sort.Strings(out[from])
	}
	for _, from := range active {
		var to []string
		for _, s := range reachable(active) {
			if s != from || byState[from].Pending {

				to = append(to, s)
			}
		}
		to = append(to, reachable(finals)...)
		sort.Strings(to)
		out[from] = to
	}

	for _, from := range finals {
		fr := byState[from]
		var to []string
		for _, cand := range finals {
			if cand != from && byState[cand].Compensating && !unreachable[cand] {
				to = append(to, cand)
			}
		}
		switch {
		case fr.Retryable && fr.RetryEntersAt != "":
			if !unreachable[fr.RetryEntersAt] {
				to = append(to, fr.RetryEntersAt)
			}
		case fr.Retryable:

			to = append(to, reachable(active)...)
		}
		sort.Strings(to)
		out[from] = to
	}

	for _, from := range foreign {
		var to []string
		for _, cand := range foreign {
			if cand != from {
				to = append(to, cand)
			}
		}
		sort.Strings(to)
		out[from] = to
	}

	return out
}

func (rs StateRoles) Initial() (string, bool) {
	var found []string
	for _, r := range rs {
		if r.Initializing {
			found = append(found, r.State)
		}
	}
	if len(found) == 1 {
		return found[0], true
	}
	return "", false
}

func (rs StateRoles) Finals() []string {
	var out []string
	for _, r := range rs {
		if r.Final && !r.Retryable {
			out = append(out, r.State)
		}
	}
	sort.Strings(out)
	return out
}

func (rs StateRoles) Shape() (string, []string) {
	counts := map[string]int{}
	for _, r := range rs {
		counts[r.phase()]++
	}
	line := fmt.Sprintf("%d initializing, %d in progress, %d pending, %d final, %d compensating, %d foreign, %d sentinel, %d unclear",
		counts["initializing"], counts["inProgress"], counts["pending"], counts["final"],
		rs.countCompensating(), counts["foreign"], counts["sentinel"], counts[""])

	var problems []string
	if counts["initializing"] == 0 {
		problems = append(problems, "no state is the one a new row starts in, so no test can set one up")
	}
	if counts["initializing"] > 1 {
		problems = append(problems, "more than one state claims to be the start — usually a type shared between entities")
	}
	if counts["final"] == 0 {
		problems = append(problems, "nothing is final, so the strongest available test — drive it to the end and refuse every further move — cannot be written")
	}
	if counts[""] > 0 {
		problems = append(problems, fmt.Sprintf("%d state(s) unclear; every one is a transition nobody can test", counts[""]))
	}
	return line, problems
}

func (rs StateRoles) countCompensating() int {
	n := 0
	for _, r := range rs {
		if r.Compensating {
			n++
		}
	}
	return n
}
