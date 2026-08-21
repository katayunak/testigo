package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/katayunak/testigo/internal/models"
)

const FileName = "testigo.json"

type Config struct {
	// Patterns limits which packages are loaded. Defaults to ["./..."].
	//
	// Q: what is this?
	// A: These are Go package patterns — the same strings you type after
	//    `go build` or `go list`. "./..." means "this module and everything
	//    under it".
	//
	//    It exists for speed. testigo type-checks and builds SSA for every
	//    package it loads, and on a large service most of them have nothing to
	//    do with payments. Narrowing the set is the difference between a scanningFlow
	//    that takes four seconds and one that takes two minutes:
	//
	//        "patterns": ["./service/...", "./domain/...", "./client/..."]
	//
	//    Be careful narrowing it too far. If you exclude a package the flow
	//    actually calls into, that call becomes invisible — the function is not
	//    "local" any more, so the graph stops there and you get a flow that
	//    looks complete but is not. Start with ./... and only narrow once you
	//    know which packages the flow touches.
	Patterns []string `json:"patterns,omitempty"`
	// Entries are the starting points of the payment flow. Plural on purpose.
	Entries []Entry `json:"entries"`
}

type Entry struct {
	Pkg    string `json:"pkg"`
	Symbol string `json:"symbol"`
	Label  string `json:"label,omitempty"`
}

func Load(root string) (*Config, error) {
	b, err := os.ReadFile(filepath.Join(root, FileName))
	if err != nil {
		return nil, err
	}
	var c Config
	if err := json.Unmarshal(stripComments(b), &c); err != nil {
		return nil, fmt.Errorf("%s: %w", FileName, err)
	}
	if len(c.Entries) == 0 {
		return nil, fmt.Errorf("%s: no entries configured", FileName)
	}
	for i, e := range c.Entries {
		if e.Pkg == "" || e.Symbol == "" {
			return nil, fmt.Errorf("%s: entry %d needs both pkg and symbol", FileName, i)
		}
	}
	return &c, nil
}

func stripComments(b []byte) []byte {
	lines := strings.Split(string(b), "\n")
	for i, ln := range lines {
		if strings.HasPrefix(strings.TrimSpace(ln), "//") {
			lines[i] = ""
		}
	}
	return []byte(strings.Join(lines, "\n"))
}

func (c *Config) EntryPoints() []models.EntryPoint {
	out := make([]models.EntryPoint, 0, len(c.Entries))
	for _, e := range c.Entries {
		out = append(out, models.EntryPoint{Pkg: e.Pkg, Symbol: e.Symbol, Label: e.Label})
	}
	return out
}

// Example is written by `testigo init` so a first-time user has something to
// edit rather than a blank file and a manual to read.
const Example = `{
  // A payment flow has more than one entry point. The API handler starts it,
  // but the provider webhook, the reconciliation job and the queue consumer all
  // rejoin the same state machine. Listing only the first one hides the bugs
  // that live in the others.
  //
  // Symbol syntax: Func, Type.Method, or (*Type).Method
  // Run 'testigo entries' to see candidates found in this repository.

  "patterns": ["./..."],

  "entries": [
    { "pkg": "example.com/pay/internal/api", "symbol": "(*Server).CreatePayment", "label": "API create" }
  ]
}
`
