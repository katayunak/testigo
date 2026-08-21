package codeRef

type Index struct {
	byID   map[string]CodeRef
	byHash map[string][]CodeRef
	nodes  map[string]int
}

func NewIndex() *Index {
	return &Index{
		byID:   map[string]CodeRef{},
		byHash: map[string][]CodeRef{},
		nodes:  map[string]int{},
	}
}

func (ix *Index) Add(a CodeRef, nodes int) {
	ix.byID[a.ID()] = a
	ix.byHash[a.BodyHash] = append(ix.byHash[a.BodyHash], a)
	ix.nodes[a.ID()] = nodes
}

func (ix *Index) Get(id string) (CodeRef, bool) {
	a, ok := ix.byID[id]
	return a, ok
}

func (ix *Index) Len() int { return len(ix.byID) }

// All returns every codeRef in the index, keyed by ID
func (ix *Index) All() map[string]CodeRef { return ix.byID }
