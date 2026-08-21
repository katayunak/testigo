package storage

import "fmt"

type MergeStats struct {
	Fresh    int `json:"fresh"`
	Stale    int `json:"stale"`
	Moved    int `json:"moved"`
	Orphaned int `json:"orphaned"`
	New      int `json:"new"`
}

func (s MergeStats) NeedsAgent() int { return s.Stale + s.New }

func (s MergeStats) String() string {
	return fmt.Sprintf("fresh=%d stale=%d moved=%d orphaned=%d new=%d → %d node(s) need an agent",
		s.Fresh, s.Stale, s.Moved, s.Orphaned, s.New, s.NeedsAgent())
}
