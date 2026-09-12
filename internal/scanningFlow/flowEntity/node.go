package flowEntity

type Node struct {
	Ref      CodeRef      `json:"ref"`
	Position NodePosition `json:"position"`
	Calls    []string     `json:"calls,omitempty"`
	Facts    Facts        `json:"facts"`
}

type NodePosition string

const (
	NodePositionEntry    NodePosition = "entry"
	NodePositionInternal NodePosition = "internal"
	NodePositionLeaf     NodePosition = "leaf"
)

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
}
