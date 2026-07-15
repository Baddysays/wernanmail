package session

import (
	"crypto/rand"
	"encoding/hex"
	"sync"
	"time"
)

// Credentials are mailbox connection settings kept server-side only.
// Password is stored encrypted at rest in memory when a SessionSecret is set.
type Credentials struct {
	IMAPHost string
	IMAPPort int
	SMTPHost string
	SMTPPort int
	Username string
	Password string // plaintext only after Get decrypts; never log
	TLS      bool
}

// Session is an authenticated client session.
type Session struct {
	ID             string
	Creds          Credentials
	CreatedAt      time.Time
	ExpiresAt      time.Time
	Impersonated   bool
	ImpersonatedBy string
}

// Store is an in-memory session map with encrypted passwords.
type Store struct {
	mu     sync.RWMutex
	byID   map[string]*Session
	ttl    time.Duration
	secret string
}

func NewStore(ttl time.Duration) *Store {
	return NewStoreWithSecret(ttl, "")
}

func NewStoreWithSecret(ttl time.Duration, secret string) *Store {
	if ttl <= 0 {
		ttl = 24 * time.Hour
	}
	return &Store{
		byID:   make(map[string]*Session),
		ttl:    ttl,
		secret: secret,
	}
}

type CreateOpts struct {
	Impersonated   bool
	ImpersonatedBy string
}

func (s *Store) Create(creds Credentials) (*Session, error) {
	return s.CreateWith(creds, CreateOpts{})
}

func (s *Store) CreateWith(creds Credentials, opts CreateOpts) (*Session, error) {
	id, err := newID()
	if err != nil {
		return nil, err
	}
	encPass, err := seal(s.secret, creds.Password)
	if err != nil {
		return nil, err
	}
	stored := creds
	stored.Password = encPass
	now := time.Now().UTC()
	sess := &Session{
		ID:             id,
		Creds:          stored,
		CreatedAt:      now,
		ExpiresAt:      now.Add(s.ttl),
		Impersonated:   opts.Impersonated,
		ImpersonatedBy: opts.ImpersonatedBy,
	}
	s.mu.Lock()
	s.byID[id] = sess
	s.mu.Unlock()
	// Return a copy with plaintext password for immediate use by caller if needed.
	out := *sess
	out.Creds.Password = creds.Password
	return &out, nil
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
	pass, err := open(s.secret, sess.Creds.Password)
	if err != nil {
		s.Delete(id)
		return nil, false
	}
	out := *sess
	out.Creds.Password = pass
	return &out, true
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
