package codeRef

import (
	"fmt"
	"go/ast"
	"go/token"
	"path/filepath"
	"strings"
)

// CodeRef identifies a function in the repository and lets us check whether
// the function has changed since the last scanningFlow.
//
// Pkg + Symbol identify the function. Together, they are its stable address:
// they tell us which function we are talking about. They change if the
// function is RENAMED or MOVED to another package.
//
// BodyHash tells us whether the function's CODE has changed. It hashes the
// function's structure rather than its source text, so comments and formatting
// changes do not change the hash.
//
// File and Line are only hints for displaying the function to the user. They
// are not part of its identity because edits elsewhere in the file can change
// the line number.
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

// as long as we're using structural hash matching, we could be deceived by one line-getter/setters
// these functions commonly share same naming convictions and structures

// for more precise understanding of moved/renamed, only functions having minMovableNodes
// which is referring to AST nodeCounts would be eligible for moved/rename scans
const minMovableNodes = 40

func Resolve(ix *Index, saved CodeRef) (CodeRef, MatchingStatus) {
	if current, ok := ix.byID[saved.ID()]; ok {
		if current.BodyHash == saved.BodyHash {
			return current, Fresh
		}

		return current, Stale
	}

	// The symbol at this point is gone, it may have been renamed or moved to another package
	if cands := ix.byHash[saved.BodyHash]; len(cands) == 1 {
		if ix.nodeCounts[cands[0].ID()] >= minMovableNodes {
			return cands[0], Moved
		}
	}

	// here this function is not found anymore
	// may it's renamed in a way we couldn't detect
	// or moved and changed at the same time
	// or deleted
	return saved, Orphaned
}
