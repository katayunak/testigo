package storage

import (
	"github.com/katayunak/testigo/internal/codeRef"
	"github.com/katayunak/testigo/internal/scanningFlow/flowEntity"
)

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

			mergeStats.Stale++

		case codeRef.Moved:
			if n, ok := fresh.Nodes[current.ID()]; ok {
				n.Notes = old.Notes
				mergeStats.Moved++
			}

		case codeRef.Orphaned:

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
