package scanningFlow

import (
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/katayunak/testigo/internal/scanningFlow/flowEntity"
)

// FindDocs locates the repository's own markdown and ranks it by how much it
// talks about money.
//
// The team already wrote some of their business rules down. A README explaining
// that a settlement is only final after the nightly file is acknowledged is
// worth more than any inference a model can make from the code, and it is
// sitting in the repository for free.
//
// testigo does not read the contents into a prompt. It names the files and says
// why each looks relevant, and lets the agent — which is already sitting in this
// repository with file access — read them itself. Pasting a 40 KB README into
// every prompt would cost more than every other question combined.
func FindDocs(root string) []flowEntity.Doc {
	var out []flowEntity.Doc

	_ = filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			switch d.Name() {
			case ".git", "vendor", "node_modules", ".testigo", "_to_delete", "testdata":
				return filepath.SkipDir
			}
			return nil
		}
		name := strings.ToLower(d.Name())
		if !strings.HasSuffix(name, ".md") && !strings.HasSuffix(name, ".adoc") {
			return nil
		}
		info, err := d.Info()
		if err != nil || info.Size() > 512*1024 {
			return nil
		}
		body, err := os.ReadFile(path)
		if err != nil {
			return nil
		}
		rel, _ := filepath.Rel(root, path)
		rel = filepath.ToSlash(rel)

		// testigo's own output is not a source of business rules.
		if strings.Contains(rel, "internal/") && strings.Contains(strings.ToLower(string(body)), "testigo") {
			return nil
		}

		score, topics := relevance(string(body))
		if score == 0 {
			return nil
		}
		out = append(out, flowEntity.Doc{
			Path: rel, Score: score, Topics: topics,
			Bytes: int(info.Size()), Title: firstHeading(string(body)),
		})
		return nil
	})

	sort.SliceStable(out, func(i, j int) bool { return out[i].Score > out[j].Score })
	if len(out) > 8 {
		out = out[:8] // a prompt that lists thirty files is not a hint, it is noise
	}
	return out
}

// docTopics are the subjects that make a document worth reading before writing a
// payment test. Counted rather than merely detected, so a README that mentions
// settlement forty times outranks one that mentions it once in a changelog.
var docTopics = map[string][]string{
	"money movement": {"settle", "settlement", "payout", "disburse", "transfer", "capture", "authoriz", "authoris"},
	"reversal":       {"refund", "reverse", "reversal", "chargeback", "dispute", "recall", "cancel"},
	"lifecycle":      {"status", "state machine", "lifecycle", "transition", "pending", "final"},
	"idempotency":    {"idempot", "duplicate", "dedup", "retry", "replay"},
	"ledger":         {"ledger", "double entry", "double-entry", "journal", "balance", "debit", "credit"},
	"messaging":      {"kafka", "topic", "partition", "consumer", "webhook", "callback", "queue"},
	"reconciliation": {"reconcil", "coherence", "catch-up", "catchup", "mismatch"},
}

func relevance(body string) (int, []string) {
	low := strings.ToLower(body)
	score := 0
	var topics []string
	for topic, words := range docTopics {
		hits := 0
		for _, w := range words {
			hits += strings.Count(low, w)
		}
		if hits > 0 {
			topics = append(topics, topic)
			score += hits
			if hits >= 5 {
				score += 5 // sustained discussion, not a passing mention
			}
		}
	}
	sort.Strings(topics)
	return score, topics
}

func firstHeading(body string) string {
	for _, line := range strings.Split(body, "\n") {
		if strings.HasPrefix(line, "#") {
			return strings.TrimSpace(strings.TrimLeft(line, "# "))
		}
	}
	return ""
}
