package flowEntity

import "github.com/katayunak/testigo/internal/codeRef"

// Node is one function in the payment flow.
type Node struct {
	Ref      codeRef.CodeRef `json:"ref"`
	Position NodePosition    `json:"position"`
	Calls    []string        `json:"calls,omitempty"` // reference IDs of nodes it calls
	Facts    Facts           `json:"facts"`
	Notes    *Notes          `json:"notes,omitempty"`
}

// NodePosition describes what a function does in the flow
type NodePosition string

const (
	NodePositionEntry    NodePosition = "entry"
	NodePositionInternal NodePosition = "internal"
	NodePositionLeaf     NodePosition = "leaf"
)

// Facts come from the scanner and are always computed from the source
type Facts struct {
	TouchesDB       bool     `json:"touches_db,omitempty"`
	TouchesNet      bool     `json:"touches_net,omitempty"`
	OpensTx         bool     `json:"opens_tx,omitempty"`
	CommitsTx       bool     `json:"commits_tx,omitempty"`
	RollsBackTx     bool     `json:"rolls_back_tx,omitempty"`
	ReadsClock      bool     `json:"reads_clock,omitempty"`
	Randomness      bool     `json:"randomness,omitempty"`
	HandlesMoney    bool     `json:"handles_money,omitempty"`
	MoneyTypes      []string `json:"money_types,omitempty"`
	WritesStatus    []string `json:"writes_status,omitempty"`
	SpawnsGoroutine bool     `json:"spawns_goroutine,omitempty"`
	HasDeferredTx   bool     `json:"has_deferred_tx,omitempty"`
}

// Notes come from an agent and are NOT guaranteed to be correct
// They are saved so we do not need to ask the agent again if the code has not changed
type Notes struct {
	Step        string   `json:"step"`
	Purpose     string   `json:"purpose,omitempty"`     // what this function does
	Effects     []string `json:"effects,omitempty"`     // side effects it causes
	Assumptions []string `json:"assumptions,omitempty"` // what callers must guarantee
	ForHash     string   `json:"for_hash"`              // BodyHash these notes describe
	Model       string   `json:"model,omitempty"`       // agent that wrote the notes
	Timestamp   string   `json:"timestamp,omitempty"`
}
