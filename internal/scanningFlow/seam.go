package scanningFlow

import "github.com/katayunak/testigo/internal/codeRef"

// Seam is a place where the program talks to something outside the process
// These are important test points because real failures often happen here:
// for example, a payment provider times out, COMMIT fails, or the clock reaches
// an expiry time.
//
// Injectable tells us whether a test can replace the real implementation.
// An interface call can usually be replaced with a fake implementation.
// A direct call on a concrete value, such as *sql.DB, cannot be replaced
// this way, so that is useful information too.
type Seam struct {
	In         codeRef.CodeRef `json:"in"` // the function containing the call
	Kind       SeamKind        `json:"kind"`
	Target     string          `json:"target"` // e.g. "(*database/sql.DB).QueryContext"
	Line       int             `json:"line"`
	Injectable bool            `json:"injectable"`
	Iface      string          `json:"iface,omitempty"` // interface type when injectable
}

// SeamKind describes the kind of external system a function talks to.
type SeamKind string

const (
	SeamDB     SeamKind = "db"
	SeamHTTP   SeamKind = "http"
	SeamQueue  SeamKind = "queue"
	SeamCache  SeamKind = "cache"
	SeamClock  SeamKind = "clock"
	SeamRandom SeamKind = "random"
)
