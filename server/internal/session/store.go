package session

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
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

// Store keeps a small in-memory index and can atomically persist it to disk.
type Store struct {
	mu     sync.RWMutex
	byID   map[string]*Session
	ttl    time.Duration
	secret string
	path   string
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

// NewFileStore opens a persistent session store. Credentials are encrypted
// with secret before they are written; callers should use a stable secret.
func NewFileStore(path string, ttl time.Duration, secret string) (*Store, error) {
	s := NewStoreWithSecret(ttl, secret)
	s.path = filepath.Clean(path)
	b, err := os.ReadFile(s.path)
	if errors.Is(err, os.ErrNotExist) {
		return s, nil
	}
	if err != nil {
		return nil, err
	}
	var sessions []*Session
	if err := json.Unmarshal(b, &sessions); err != nil {
		return nil, err
	}
	now := time.Now().UTC()
	for _, sess := range sessions {
		if sess != nil && sess.ID != "" && sess.ExpiresAt.After(now) {
			s.byID[sess.ID] = sess
		}
	}
	return s, nil
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
	if err := s.persistLocked(); err != nil {
		delete(s.byID, id)
		s.mu.Unlock()
		return nil, err
	}
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
	_ = s.persistLocked()
	s.mu.Unlock()
}

func (s *Store) persistLocked() error {
	if s.path == "" {
		return nil
	}
	sessions := make([]*Session, 0, len(s.byID))
	now := time.Now().UTC()
	for id, sess := range s.byID {
		if !sess.ExpiresAt.After(now) {
			delete(s.byID, id)
			continue
		}
		sessions = append(sessions, sess)
	}
	b, err := json.Marshal(sessions)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(s.path), 0o750); err != nil {
		return err
	}
	tmp := s.path + ".tmp"
	if err := os.WriteFile(tmp, b, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, s.path)
}

func newID() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}
