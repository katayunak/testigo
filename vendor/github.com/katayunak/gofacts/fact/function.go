package fact

// Function is one declared function or method of the module. Closures belong
// to the declaration they appear in: their calls, seams and writes are
// reported against it.
type Function struct {
	Ref       CodeRef `json:"ref"`
	EndLine   int     `json:"end_line"`
	Exported  bool    `json:"exported,omitempty"`
	InTest    bool    `json:"in_test,omitempty"`
	Generated bool    `json:"generated,omitempty"`
	Facts     Facts   `json:"facts"`
}

// Contains reports whether a line of file falls inside the declaration, which
// is how a changed hunk is mapped to the functions it touches.
func (f Function) Contains(file string, line int) bool {
	return f.Ref.File == file && line >= f.Ref.Line && line <= f.EndLine
}

type Facts struct {
	TouchesDB       bool       `json:"touches_db,omitempty"`
	TouchesNet      bool       `json:"touches_net,omitempty"`
	OpensTx         bool       `json:"opens_tx,omitempty"`
	CommitsTx       bool       `json:"commits_tx,omitempty"`
	RollsBackTx     bool       `json:"rolls_back_tx,omitempty"`
	ReadsClock      bool       `json:"reads_clock,omitempty"`
	Randomness      bool       `json:"randomness,omitempty"`
	HandlesMoney    bool       `json:"handles_money,omitempty"`
	MoneyTypes      []string   `json:"money_types,omitempty"`
	WritesStatus    []string   `json:"writes_status,omitempty"`
	SpawnsGoroutine bool       `json:"spawns_goroutine,omitempty"`
	Reaches         []SeamKind `json:"reaches,omitempty"`
}

// Certainty grades a call edge by what proved it.
type Certainty string

const (
	// Certain: a static call. The compiler names exactly one callee.
	Certain Certainty = "certain"
	// Likely: a dynamic call narrowed by the values the program is seen to
	// put into the interface or function variable (VTA).
	Likely Certainty = "likely"
	// Possible: a dynamic call where no value was seen flowing in, so every
	// implementation of the interface or every function of the signature is a
	// candidate (CHA). Over-approximates on purpose.
	Possible Certainty = "possible"
)

func (c Certainty) Rank() int {
	switch c {
	case Certain:
		return 0
	case Likely:
		return 1
	}
	return 2
}

// Weakest returns the less certain of two grades, which is the grade of a
// path that uses both edges.
func Weakest(a, b Certainty) Certainty {
	if a.Rank() >= b.Rank() {
		return a
	}
	return b
}

type CallKind string

const (
	CallStatic   CallKind = "static"
	CallDispatch CallKind = "dispatch"
	CallDynamic  CallKind = "dynamic"
)

// Edge is one resolved call from one function of the module to another.
// Go marks a goroutine hand-off and Defer a call that runs at exit: both change
// what "after this call" means, so a reader of a call chain must see them.
type Edge struct {
	From      string    `json:"from"`
	To        string    `json:"to"`
	Site      Pos       `json:"site"`
	Kind      CallKind  `json:"kind"`
	Certainty Certainty `json:"certainty"`
	Go        bool      `json:"go,omitempty"`
	Defer     bool      `json:"defer,omitempty"`
}

// TestLink says a test reaches a function, how far away it is, and how sure
// that is.
type TestLink struct {
	Test      CodeRef   `json:"test"`
	Target    string    `json:"target"`
	Hops      int       `json:"hops"`
	Certainty Certainty `json:"certainty"`
}

// LoadIssue is a package that did not load or type-check. Everything behind it
// is invisible, so facts about it are missing, not absent.
type LoadIssue struct {
	Pkg  string `json:"pkg"`
	Pos  Pos    `json:"pos,omitempty"`
	Kind string `json:"kind"`
	Msg  string `json:"msg"`
}
