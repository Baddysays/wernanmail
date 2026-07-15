package greylist

import (
	"context"
	"sync"
	"time"
)

// Key is one SMTP triplet (client IP + mail from + rcpt).
type Key struct {
	IP   string
	From string
	Rcpt string
}

// Store tracks first-seen times for greylisting.
type Store struct {
	mu   sync.Mutex
	seen map[string]time.Time
	ttl  time.Duration
}

func New(ttl time.Duration) *Store {
	if ttl <= 0 {
		ttl = 24 * time.Hour
	}
	return &Store{seen: map[string]time.Time{}, ttl: ttl}
}

func (s *Store) key(k Key) string {
	return k.IP + "|" + k.From + "|" + k.Rcpt
}

// Allow returns true if the triplet may proceed.
// On first sight returns false (caller should 451). After delaySeconds, returns true.
func (s *Store) Allow(k Key, delaySeconds int) bool {
	if delaySeconds <= 0 {
		return true
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.gcLocked()
	id := s.key(k)
	now := time.Now()
	first, ok := s.seen[id]
	if !ok {
		s.seen[id] = now
		return false
	}
	return now.Sub(first) >= time.Duration(delaySeconds)*time.Second
}

func (s *Store) gcLocked() {
	cut := time.Now().Add(-s.ttl)
	for k, t := range s.seen {
		if t.Before(cut) {
			delete(s.seen, k)
		}
	}
}

// Touch is unused helper for tests.
func (s *Store) Touch(ctx context.Context, k Key) {
	_ = ctx
	s.mu.Lock()
	s.seen[s.key(k)] = time.Now()
	s.mu.Unlock()
}
