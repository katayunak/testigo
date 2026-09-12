package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const RulesFileName = "rules.json"

type Rules struct {
	MoneyMovement MoneyMovement `json:"money_movement"`

	Skip []SkipRule `json:"skip,omitempty"`
}

type MoneyMovement struct {
	Symbols []string `json:"symbols,omitempty"`
}

type SkipRule struct {
	Scenario string `json:"scenario"`
	Why      string `json:"why"`
}

func LoadRules(root string) (*Rules, error) {
	b, err := os.ReadFile(filepath.Join(Dir(root), RulesFileName))
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var r Rules
	if err := json.Unmarshal(stripComments(b), &r); err != nil {
		return nil, fmt.Errorf("%s: %w", RulesFileName, err)
	}
	return &r, nil
}

func (r *Rules) Skipped(scenario string) (string, bool) {
	if r == nil {
		return "", false
	}
	for _, s := range r.Skip {
		if strings.EqualFold(s.Scenario, scenario) {
			why := s.Why
			if why == "" {
				why = "turned off in " + DirName + "/" + RulesFileName
			}
			return why, true
		}
	}
	return "", false
}

const RulesExample = `{
  // What does moving money MEAN in this repository?
  //
  // testigo cannot work this out from code, and guessing wrong makes every
  // generated test confidently wrong. Both fields are optional.

  "money_movement": {
    // The function(s) that actually commit money. testigo uses the first one
    // as the transfer function when it cannot find one itself.
    "symbols": ["example.com/pay/ledger#(*Ledger).Post"]
  },

  // Catalog scenarios that do not apply here, with the reason.
  "skip": [
    { "scenario": "MINOR-UNIT-CONVERSION", "why": "single currency, IRR, no conversion anywhere" }
  ]
}
`
