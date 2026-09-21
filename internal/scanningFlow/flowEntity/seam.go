package flowEntity

import "github.com/katayunak/testigo/internal/codeRef"

type Seam struct {
	In         codeRef.CodeRef `json:"in"`
	Kind       SeamKind        `json:"kind"`
	Target     string          `json:"target"`
	Line       int             `json:"line"`
	Injectable bool            `json:"injectable"`
	Iface      string          `json:"iface,omitempty"`
}

type SeamKind string

const (
	SeamDB     SeamKind = "db"
	SeamHTTP   SeamKind = "http"
	SeamQueue  SeamKind = "queue"
	SeamCache  SeamKind = "cache"
	SeamClock  SeamKind = "clock"
	SeamRandom SeamKind = "random"
)
