package settings

import (
	"context"
	"strconv"
	"sync"
	"time"

	"github.com/Baddysays/wernanmail/server/internal/store"
)

// Keys for persisted settings.
const (
	KeyMaxMessageBytes     = "mail.max_message_bytes"
	KeyRateSubmitPerMin    = "mail.rate_submit_per_min"
	KeyRateSMTPConnPerMin  = "mail.rate_smtp_conn_per_min"
	KeySpamRejectAt        = "antispam.reject_at"
	KeySpamQuarantineAt    = "antispam.quarantine_at"
	KeySpamRBLs            = "antispam.rbls"
	KeyAVEnabled           = "antivirus.enabled"
	KeyDefaultQuotaBytes   = "mail.default_quota_bytes"
	KeyRetentionDays       = "mail.retention_days"
	KeyRelayHost           = "mail.relay_host"
	KeyGreylistSeconds     = "mail.greylist_seconds"
)

// Defaults returns built-in defaults.
func Defaults() map[string]string {
	return map[string]string{
		KeyMaxMessageBytes:    strconv.Itoa(25 << 20),
		KeyRateSubmitPerMin:   "60",
		KeyRateSMTPConnPerMin: "120",
		KeySpamRejectAt:       "10",
		KeySpamQuarantineAt:   "5",
		KeySpamRBLs:           "zen.spamhaus.org",
		KeyAVEnabled:          "true",
		KeyDefaultQuotaBytes:  strconv.FormatInt(200<<20, 10),
		KeyRetentionDays:      "0",
		KeyRelayHost:          "",
		KeyGreylistSeconds:    "0",
	}
}

// Manager merges DB overrides with defaults.
type Manager struct {
	Store store.MessageStore
	mu    sync.RWMutex
	cache map[string]string
}

func NewManager(st store.MessageStore) *Manager {
	m := &Manager{Store: st, cache: Defaults()}
	_ = m.Reload(context.Background())
	return m
}

func (m *Manager) Reload(ctx context.Context) error {
	list, err := m.Store.ListSettings(ctx)
	if err != nil {
		return err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.cache = Defaults()
	for _, s := range list {
		m.cache[s.Key] = s.Value
	}
	return nil
}

func (m *Manager) Get(key string) string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.cache[key]
}

func (m *Manager) GetInt(key string, fallback int) int {
	v := m.Get(key)
	n, err := strconv.Atoi(v)
	if err != nil {
		return fallback
	}
	return n
}

func (m *Manager) GetBool(key string, fallback bool) bool {
	v := m.Get(key)
	if v == "" {
		return fallback
	}
	b, err := strconv.ParseBool(v)
	if err != nil {
		return fallback
	}
	return b
}

func (m *Manager) Set(ctx context.Context, key, value string) error {
	if err := m.Store.SetSetting(ctx, key, value); err != nil {
		return err
	}
	m.mu.Lock()
	m.cache[key] = value
	m.mu.Unlock()
	return nil
}

func (m *Manager) All() map[string]string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make(map[string]string, len(m.cache))
	for k, v := range m.cache {
		out[k] = v
	}
	return out
}

// Limiter is a simple in-memory token bucket per key.
type Limiter struct {
	mu       sync.Mutex
	window   time.Duration
	limit    int
	hits     map[string][]time.Time
}

func NewLimiter(perMin int) *Limiter {
	if perMin <= 0 {
		perMin = 60
	}
	return &Limiter{
		window: time.Minute,
		limit:  perMin,
		hits:   map[string][]time.Time{},
	}
}

func (l *Limiter) Allow(key string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := time.Now()
	cut := now.Add(-l.window)
	arr := l.hits[key]
	n := 0
	for _, t := range arr {
		if t.After(cut) {
			arr[n] = t
			n++
		}
	}
	arr = arr[:n]
	if len(arr) >= l.limit {
		l.hits[key] = arr
		return false
	}
	l.hits[key] = append(arr, now)
	return true
}
