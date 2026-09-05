package codeRef

import (
	"fmt"
	"go/ast"
	"go/token"
	"path/filepath"
	"strings"
)

type CodeRef struct {
	Pkg      string `json:"pkg"`
	Symbol   string `json:"symbol"`
	BodyHash string `json:"body_hash"`
	File     string `json:"file"`
	Line     int    `json:"line"`
}

func (c CodeRef) ID() string { return c.Pkg + "#" + c.Symbol }

func (c CodeRef) String() string { return fmt.Sprintf("%s (%s:%d)", c.ID(), c.File, c.Line) }

func NewCodeRef(pkgPath, repoRoot string, fset *token.FileSet, decl *ast.FuncDecl) CodeRef {
	pos := fset.Position(decl.Pos())
	file := pos.Filename

	if rel, err := filepath.Rel(repoRoot, file); err == nil && !strings.HasPrefix(rel, "..") {
		file = rel
	}
	hash, _ := StructuralHash(decl)

	return CodeRef{
		Pkg:      pkgPath,
		Symbol:   Symbol(decl),
		File:     filepath.ToSlash(file),
		Line:     pos.Line,
		BodyHash: hash,
	}
}

const minMovableNodes = 40

func Resolve(ix *Index, saved CodeRef) (CodeRef, MatchingStatus) {
	if current, ok := ix.byID[saved.ID()]; ok {
		if current.BodyHash == saved.BodyHash {
			return current, Fresh
		}

		return current, Stale
	}

	if cands := ix.byHash[saved.BodyHash]; len(cands) == 1 {
		if ix.nodeCounts[cands[0].ID()] >= minMovableNodes {
			return cands[0], Moved
		}
	}

	return saved, Orphaned
}
