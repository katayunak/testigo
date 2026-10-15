package fact

import (
	"fmt"
	"strconv"
	"strings"
)

// CodeRef names a declaration. Pkg+"#"+Symbol is the identity used as a key
// everywhere; File and Line are where a person goes to look.
type CodeRef struct {
	Pkg    string `json:"pkg"`
	Symbol string `json:"symbol"`
	File   string `json:"file"`
	Line   int    `json:"line"`
}

func (c CodeRef) ID() string { return c.Pkg + "#" + c.Symbol }

func (c CodeRef) String() string { return fmt.Sprintf("%s (%s:%d)", c.ID(), c.File, c.Line) }

func CodeRefOf(pkg, symbol, file string, line int) CodeRef {
	return CodeRef{Pkg: pkg, Symbol: symbol, File: file, Line: line}
}

// Pos is a site inside a file, repo-relative and slash-separated.
type Pos struct {
	File string `json:"file"`
	Line int    `json:"line"`
	Col  int    `json:"col,omitempty"`
}

func (p Pos) String() string {
	if p.File == "" {
		return ""
	}
	return p.File + ":" + strconv.Itoa(p.Line)
}

func (p Pos) IsValid() bool { return p.File != "" && p.Line > 0 }

func (p Pos) Before(q Pos) bool {
	if p.File != q.File {
		return p.File < q.File
	}
	if p.Line != q.Line {
		return p.Line < q.Line
	}
	return p.Col < q.Col
}

func SplitID(id string) (pkg, symbol string) {
	pkg, symbol, _ = strings.Cut(id, "#")
	return pkg, symbol
}
