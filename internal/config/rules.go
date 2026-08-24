package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// RulesFileName is the rules file, inside config.DirName.
const RulesFileName = "rules.json"

// Rules is what this repository means by moving money.
//
// The single assumption testigo cannot make on its own. "Money movement" is not
// one thing across fintech:
//
//   - a wallet moves money when a balance row changes
//   - a switch moves money when it forwards an authorization to an acquirer
//   - a service-activation system moves money when it reports a status to a
//     settlement provider, and the provider settles later on that report
//   - a billing system moves money when it issues an invoice line, days before
//     anything is collected
//
// A tool that assumed the first definition would generate confident, wrong tests
// for the other three. So testigo asks the team to write it down once, in a file
// that lives beside the code and gets reviewed with it.
//
// Everything here is optional. With no rules file testigo falls back to code
// heuristics, and says so. With one, the scenario catalog gets sharper and
// several agent questions stop needing to be asked at all.
type Rules struct {
	// Domain is what kind of system this is. Free text; used in prompts so an
	// agent stops reasoning about card payments in a wallet repository.
	Domain string `json:"domain"`

	MoneyMovement MoneyMovement `json:"money_movement"`
	Reversal      Reversal      `json:"reversal"`
	RetryPolicy   RetryPolicy   `json:"retry_policy"`

	// ExtraStateTypes are enums that ARE lifecycles despite not being named like
	// one. patterns/state.go treats `SettlementMode` as a weak match and skips
	// it; naming it here promotes it.
	ExtraStateTypes []string `json:"extra_state_types,omitempty"`

	// FinalStates can be declared here when the team already knows them, which
	// removes that part of the round-one question.
	FinalStates []string `json:"final_states,omitempty"`

	// Invariants are rules only this team knows. They become generated tests
	// alongside the built-in catalog.
	Invariants []Invariant `json:"invariants,omitempty"`

	// Skip turns off catalog scenarios that do not apply, with a reason so the
	// next person knows it was a decision rather than an oversight.
	Skip []SkipRule `json:"skip,omitempty"`

	Notes string `json:"notes,omitempty"`
}

// MoneyMovement describes the moment money is committed in this system.
type MoneyMovement struct {
	// Description is one or two sentences, in your words.
	Description string `json:"description"`

	// Symbols are the functions where it happens.
	Symbols []string `json:"symbols,omitempty"`

	// ExternalSignal is for systems where money moves because a MESSAGE was
	// sent, not because a row changed. If this is set, an outbound call is the
	// money movement, and a rollback does not undo it.
	ExternalSignal string `json:"external_signal,omitempty"`

	// Ledger names the table or type holding the record of truth, if any.
	Ledger string `json:"ledger,omitempty"`
}

// Reversal is how money comes back.
type Reversal struct {
	Possible bool     `json:"possible"`
	How      string   `json:"how,omitempty"`
	Symbols  []string `json:"symbols,omitempty"`
	// WindowDays is how long a reversal can arrive after the fact. It is why a
	// "final" state may not be final.
	WindowDays int `json:"window_days,omitempty"`
}

// RetryPolicy records whether this system retries, which is a FACT about the
// code and not something testigo should assume either way.
//
// Its own field because it is a thing to TEST, not a thing to design. If a
// policy exists, testigo generates tests for it: that the cap is respected, that
// backoff grows, that non-retryable errors are not retried. If none exists, that
// is a legitimate choice and testigo stops asking retry-shaped questions.
type RetryPolicy struct {
	Exists      bool   `json:"exists"`
	Where       string `json:"where,omitempty"`
	MaxAttempts int    `json:"max_attempts,omitempty"`
	Backoff     string `json:"backoff,omitempty"` // none | fixed | exponential | jittered
	Notes       string `json:"notes,omitempty"`
}

// Invariant is a rule that must always hold in this system.
type Invariant struct {
	ID        string `json:"id"`
	Statement string `json:"statement"`
	Severity  string `json:"severity"`
	HowToTest string `json:"how_to_test,omitempty"`
}

// SkipRule turns off one catalog scenario.
type SkipRule struct {
	Scenario string `json:"scenario"`
	Why      string `json:"why"`
}

// LoadRules reads testigo.rules.json. A missing file is normal, not an error.
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

// Skipped reports whether a scenario is turned off, and why.
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

// MovesMoneyExternally reports whether money leaves by MESSAGE in this system.
// When it does, an outbound call is the money movement and no rollback reaches it.
func (r *Rules) MovesMoneyExternally() bool {
	return r != nil && strings.TrimSpace(r.MoneyMovement.ExternalSignal) != ""
}

// RulesExample is written by `testigo rules --init`.
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
