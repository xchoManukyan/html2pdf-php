package server

import (
	"crypto/rand"
	"encoding/hex"
	"sync"
	"time"
)

// results keeps recent PDFs in memory so the page can show and save them
// from a real URL ending in the file name (a blob: URL makes the browser's
// PDF viewer save under a random name).
type results struct {
	mu    sync.Mutex
	items map[string]*stored
	order []string
}

type stored struct {
	name string
	pdf  []byte
	at   time.Time
}

const (
	resultTTL      = 30 * time.Minute
	resultMaxCount = 50
	resultMaxBytes = 256 << 20
)

func newResults() *results { return &results{items: map[string]*stored{}} }

func (s *results) put(name string, pdf []byte) string {
	b := make([]byte, 16)
	rand.Read(b)
	id := hex.EncodeToString(b)
	s.mu.Lock()
	defer s.mu.Unlock()
	s.items[id] = &stored{name: name, pdf: pdf, at: time.Now()}
	s.order = append(s.order, id)
	s.evict()
	return id
}

func (s *results) get(id string) *stored {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.evict()
	return s.items[id]
}

// evict drops expired entries, then the oldest ones over the limits.
func (s *results) evict() {
	total := 0
	for _, it := range s.items {
		total += len(it.pdf)
	}
	for len(s.order) > 0 {
		it := s.items[s.order[0]]
		if time.Since(it.at) < resultTTL && len(s.order) <= resultMaxCount && total <= resultMaxBytes {
			break
		}
		total -= len(it.pdf)
		delete(s.items, s.order[0])
		s.order = s.order[1:]
	}
}
