package codeRef

type Index struct {
	byID       map[string]CodeRef
	byHash     map[string][]CodeRef
	nodeCounts map[string]int
}

func NewIndex() *Index {
	return &Index{
		byID:       map[string]CodeRef{},
		byHash:     map[string][]CodeRef{},
		nodeCounts: map[string]int{},
	}
}

func (ix *Index) Add(ref CodeRef, nodes int) {
	ix.byID[ref.ID()] = ref
	ix.byHash[ref.BodyHash] = append(ix.byHash[ref.BodyHash], ref)
	ix.nodeCounts[ref.ID()] = nodes
}

func (ix *Index) Get(id string) (CodeRef, bool) {
	ref, ok := ix.byID[id]
	return ref, ok
}

func (ix *Index) Len() int { return len(ix.byID) }

// All returns every codeRef in the index, keyed by ID
func (ix *Index) All() map[string]CodeRef { return ix.byID }
