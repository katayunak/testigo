package storage

import (
	"github.com/katayunak/testigo/internal/codeRef"
	"github.com/katayunak/testigo/internal/scanningFlow/flowEntity"
)

// Merge carries the previous run's NOTES onto a freshly scanned flow.
// Facts are never merged — they are recomputed from source on every run and the
// new ones always win. Only Notes travel.
func Merge(prev, fresh *flowEntity.Flow, ix *codeRef.Index) MergeStats {
	var mergeStats MergeStats
	if prev == nil {
		for _, node := range fresh.Nodes {
			if node.Notes == nil {
				mergeStats.New++
			}
		}

		return mergeStats
	}

	for _, old := range prev.Nodes {
		if old.Notes == nil {
			continue
		}
		current, matchingStatus := codeRef.Resolve(ix, old.Ref)

		switch matchingStatus {
		case codeRef.Fresh:
			if node, ok := fresh.Nodes[current.ID()]; ok {
				node.Notes = old.Notes
				mergeStats.Fresh++
			}

		case codeRef.Stale:
			// Deliberately do NOT carry the notes over. A note that describes
			// the old body is not evidence about the new one, and a plausible
			// but wrong note is more dangerous than a missing one.
			mergeStats.Stale++

		case codeRef.Moved:
			if n, ok := fresh.Nodes[current.ID()]; ok {
				n.Notes = old.Notes
				mergeStats.Moved++
			}

		case codeRef.Orphaned:
			// Orphaned notes are moved to Flow.Orphans rather than deleted:
			// user should decide whether a step was removed on purpose or renamed in a way
			// the resolver could not follow.
			fresh.Orphans = append(fresh.Orphans, old)
			mergeStats.Orphaned++
		}
	}

	for _, node := range fresh.Nodes {
		if node.Notes == nil {
			mergeStats.New++
		}
	}

	mergeStats.New -= mergeStats.Stale
	if mergeStats.New < 0 {
		mergeStats.New = 0
	}

	return mergeStats
}
