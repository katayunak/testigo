package gocmd

import (
	"fmt"
	"sync"
)

// boundedBuffer keeps the head and the tail of a stream. Build errors appear at
// the start of go's output and test summaries at the end; the middle of a
// runaway log is the part nobody reads.
type boundedBuffer struct {
	mu      sync.Mutex
	limit   int
	head    []byte
	tail    []byte
	dropped int
}

func newBoundedBuffer(limit int) *boundedBuffer { return &boundedBuffer{limit: limit} }

func (b *boundedBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	n := len(p)
	half := b.limit / 2
	if room := half - len(b.head); room > 0 {
		take := min(room, len(p))
		b.head = append(b.head, p[:take]...)
		p = p[take:]
	}
	if len(p) == 0 {
		return n, nil
	}
	b.tail = append(b.tail, p...)
	if over := len(b.tail) - (b.limit - half); over > 0 {
		b.dropped += over
		b.tail = append(b.tail[:0], b.tail[over:]...)
	}
	return n, nil
}

func (b *boundedBuffer) Truncated() bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.dropped > 0
}

func (b *boundedBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.dropped == 0 {
		return string(b.head) + string(b.tail)
	}
	return fmt.Sprintf("%s\n... %d bytes omitted ...\n%s", b.head, b.dropped, b.tail)
}
