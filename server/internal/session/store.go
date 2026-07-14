package session

import (
	"crypto/rand"
	"encoding/hex"
	"sync"
	"time"
)

// Credentials are mailbox connection settings kept server-side only.
type Credentials struct {
	IMAPHost string
	IMAPPort int
	SMTPHost string
	SMTPPort int
	Username string
	Password string
	TLS      bool
}

// Session is an authenticated client session.
type Session struct {
	ID        string
	Creds     Credentials
	CreatedAt time.Time
	ExpiresAt time.Time
}

// Store is an in-memory session map (MVP).
// Later: persist sessions in SQLite and avoid holding passwords in process memory longer than needed.
type Store struct {
	mu      sync.RWMutex
	byID    map[string]*Session
	ttl     time.Duration
}

func NewStore(ttl time.Duration) *Store {
	if ttl <= 0 {
		ttl = 24 * time.Hour
	}
	return &Store{
		byID: make(map[string]*Session),
		ttl:  ttl,
	}
}

func (s *Store) Create(creds Credentials) (*Session, error) {
	id, err := newID()
	if err != nil {
		return nil, err
	}
	now := time.Now().UTC()
	sess := &Session{
		ID:        id,
		Creds:     creds,
		CreatedAt: now,
		ExpiresAt: now.Add(s.ttl),
	}
	s.mu.Lock()
	s.byID[id] = sess
	s.mu.Unlock()
	return sess, nil
}

func (s *Store) Get(id string) (*Session, bool) {
	s.mu.RLock()
	sess, ok := s.byID[id]
	s.mu.RUnlock()
	if !ok {
		return nil, false
	}
	if time.Now().UTC().After(sess.ExpiresAt) {
		s.Delete(id)
		return nil, false
	}
	return sess, true
}

func (s *Store) Delete(id string) {
	s.mu.Lock()
	delete(s.byID, id)
	s.mu.Unlock()
}

func newID() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}
