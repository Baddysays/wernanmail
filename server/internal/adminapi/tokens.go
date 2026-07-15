package adminapi

import (
	"crypto/rand"
	"encoding/hex"
	"sync"
	"time"
)

type tokenEntry struct {
	User      string
	ExpiresAt time.Time
}

// TokenStore holds short-lived admin bearer tokens.
type TokenStore struct {
	mu   sync.Mutex
	byID map[string]tokenEntry
	ttl  time.Duration
}

func NewTokenStore(ttl time.Duration) *TokenStore {
	if ttl <= 0 {
		ttl = 12 * time.Hour
	}
	return &TokenStore{byID: make(map[string]tokenEntry), ttl: ttl}
}

func (t *TokenStore) Issue(user string) (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	id := hex.EncodeToString(b)
	t.mu.Lock()
	t.byID[id] = tokenEntry{User: user, ExpiresAt: time.Now().UTC().Add(t.ttl)}
	t.mu.Unlock()
	return id, nil
}

func (t *TokenStore) Validate(token string) (string, bool) {
	t.mu.Lock()
	defer t.mu.Unlock()
	e, ok := t.byID[token]
	if !ok {
		return "", false
	}
	if time.Now().UTC().After(e.ExpiresAt) {
		delete(t.byID, token)
		return "", false
	}
	return e.User, true
}

func (t *TokenStore) Revoke(token string) {
	t.mu.Lock()
	delete(t.byID, token)
	t.mu.Unlock()
}
