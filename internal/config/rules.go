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
	Domain string `json:"domain"`

	MoneyMovement MoneyMovement `json:"money_movement"`
	Reversal      Reversal      `json:"reversal"`
	RetryPolicy   RetryPolicy   `json:"retry_policy"`

	ExtraStateTypes []string `json:"extra_state_types,omitempty"`

	FinalStates []string `json:"final_states,omitempty"`

	Invariants []Invariant `json:"invariants,omitempty"`

	Skip []SkipRule `json:"skip,omitempty"`

	Notes string `json:"notes,omitempty"`
}

type MoneyMovement struct {
	Description string `json:"description"`

	Symbols []string `json:"symbols,omitempty"`

	ExternalSignal string `json:"external_signal,omitempty"`

	Ledger string `json:"ledger,omitempty"`
}

type Reversal struct {
	Possible bool     `json:"possible"`
	How      string   `json:"how,omitempty"`
	Symbols  []string `json:"symbols,omitempty"`

	WindowDays int `json:"window_days,omitempty"`
}

type RetryPolicy struct {
	Exists      bool   `json:"exists"`
	Where       string `json:"where,omitempty"`
	MaxAttempts int    `json:"max_attempts,omitempty"`
	Backoff     string `json:"backoff,omitempty"`
	Notes       string `json:"notes,omitempty"`
}

type Invariant struct {
	ID        string `json:"id"`
	Statement string `json:"statement"`
	Severity  string `json:"severity"`
	HowToTest string `json:"how_to_test,omitempty"`
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

func (r *Rules) MovesMoneyExternally() bool {
	return r != nil && strings.TrimSpace(r.MoneyMovement.ExternalSignal) != ""
}

const RulesExample = `{
  // What does moving money MEAN in this repository?
  //
  // testigo cannot work this out from code, and guessing wrong makes every
  // generated test confidently wrong. Two minutes here saves a lot of noise.
  //
  // Delete the fields that do not apply. Everything is optional.

  "domain": "wallet | switch | settlement | billing | activation | ledger | acquiring",

  "money_movement": {
    "description": "One or two sentences in your own words. When is money actually committed?",
    "symbols": ["example.com/pay/ledger#(*Ledger).Post"],

    // Set this ONLY if money moves because a message was sent rather than
    // because a row changed. In a service-activation system, reporting a
    // successful delivery to the settlement provider IS the money movement,
    // and no database rollback can take it back.
    "external_signal": "",

    "ledger": "ledger_entries"
  },

  "reversal": {
    "possible": true,
    "how": "a compensating entry, never an update",
    "symbols": [],
    // How long after the fact a reversal can arrive. This is why a state that
    // looks final may not be.
    "window_days": 40
  },

  // Does this system retry? Either answer is fine. Saying so stops testigo
  // asking retry-shaped questions of a system that deliberately has no retries,
  // and lets it TEST the policy of a system that does.
  "retry_policy": {
    "exists": false,
    "where": "",
    "max_attempts": 0,
    "backoff": "none",
    "notes": ""
  },

  // Enums that really are lifecycles but are not named like one.
  "extra_state_types": [],

  // States a payment cannot leave, if you already know them.
  "final_states": [],

  // Rules only your team knows. These become generated tests.
  "invariants": [
    {
      "id": "WALLET-NEVER-NEGATIVE",
      "statement": "A wallet balance may never go below zero, even during a reversal.",
      "severity": "critical",
      "how_to_test": "concurrent debits totalling more than the balance; assert one fails"
    }
  ],

  // Catalog scenarios that do not apply here, with the reason.
  "skip": [
    { "scenario": "MINOR-UNIT-CONVERSION", "why": "single currency, IRR, no conversion anywhere" }
  ],

  "notes": ""
}
`
