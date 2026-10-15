package fact

// SchemaVersion of the Flow encoding. 5 adds transactions, load issues, edge
// certainty and write provenance to testigo's version 4; every version 4 field
// keeps its name and meaning.
const SchemaVersion = 5

// Flow is the part of a program reachable from declared entry points: the view
// testigo plans tests from and stores as flow.json.
type Flow struct {
	SchemaVersion int              `json:"schema_version"`
	Module        string           `json:"module"`
	GeneratedAt   string           `json:"generated_at,omitempty"`
	Entries       []EntryPoint     `json:"entries"`
	Nodes         map[string]*Node `json:"nodes"`
	Seams         []Seam           `json:"seams"`
	States        []StateMachine   `json:"states"`
	Findings      []Finding        `json:"findings"`
	Infra         Infra            `json:"infra,omitempty"`

	TableWrites  []TableWrites `json:"table_writes,omitempty"`
	HashChains   []HashChain   `json:"hash_chains,omitempty"`
	Transactions []Tx          `json:"transactions,omitempty"`

	IdempotencyKeys Candidates        `json:"idempotency_keys,omitempty"`
	MoneyTypes      Candidates        `json:"money_types,omitempty"`
	Entities        map[string]string `json:"entities,omitempty"`

	GeneratedFiles    int `json:"generated_files,omitempty"`
	GeneratedFindings int `json:"generated_findings,omitempty"`

	LoadIssues []LoadIssue `json:"load_issues,omitempty"`
	Docs       []Doc       `json:"docs,omitempty"`
}

func NewFlow(module string) *Flow {
	return &Flow{SchemaVersion: SchemaVersion, Module: module, Nodes: map[string]*Node{}}
}

type EntryPoint struct {
	Pkg    string `json:"pkg"`
	Symbol string `json:"symbol"`
	Label  string `json:"label,omitempty"`
}

func (e EntryPoint) ID() string { return e.Pkg + "#" + e.Symbol }

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

// HashChain is a type whose method hashes its own previous instance: the shape
// of a tamper-evident log.
type HashChain struct {
	Type   string  `json:"type"`
	Method CodeRef `json:"method"`
}

// Doc is markdown found in the repository. The analyser does not produce it;
// the field exists so a Flow written by testigo keeps its shape.
type Doc struct {
	Path   string   `json:"path"`
	Title  string   `json:"title,omitempty"`
	Score  int      `json:"score"`
	Topics []string `json:"topics,omitempty"`
}

func (f *Flow) WritesTo(table string) (TableWrites, bool) {
	for _, w := range f.TableWrites {
		if w.Table == table {
			return w, true
		}
	}
	return TableWrites{}, false
}

func (f *Flow) HasWritePattern(p WritePattern) bool {
	for _, w := range f.TableWrites {
		if w.Pattern == p {
			return true
		}
	}
	return false
}
