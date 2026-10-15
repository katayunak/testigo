package fact

// Seam is a call that crosses the process boundary. Injectable means the call
// goes through an interface the module defines, so a test can replace it and
// make it fail; that part is proved by SSA. Kind comes from package tables and
// is a heuristic.
type Seam struct {
	In         CodeRef  `json:"in"`
	Kind       SeamKind `json:"kind"`
	Target     string   `json:"target"`
	Line       int      `json:"line"`
	Injectable bool     `json:"injectable"`
	Iface      string   `json:"iface,omitempty"`
}

func (s Seam) At() Pos { return Pos{File: s.In.File, Line: s.Line} }

type SeamKind string

const (
	SeamDB     SeamKind = "db"
	SeamHTTP   SeamKind = "http"
	SeamQueue  SeamKind = "queue"
	SeamCache  SeamKind = "cache"
	SeamClock  SeamKind = "clock"
	SeamRandom SeamKind = "random"
)

// Network reports whether the kind is a round trip to another process that a
// database transaction should not wait on.
func (k SeamKind) Network() bool { return k == SeamHTTP || k == SeamQueue }
