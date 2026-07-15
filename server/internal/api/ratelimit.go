package api

import (
	"net"
	"net/http"
	"strings"
	"sync"
	"time"
)

// loginGuard limits failed-ish login attempts per client IP.
type loginGuard struct {
	mu          sync.Mutex
	attempts    map[string][]time.Time
	bannedUntil map[string]time.Time
	window      time.Duration
	banFor      time.Duration
	max         int
}

func newLoginGuard() *loginGuard {
	return &loginGuard{
		attempts:    make(map[string][]time.Time),
		bannedUntil: make(map[string]time.Time),
		window:      15 * time.Minute,
		banFor:      30 * time.Minute,
		max:         10,
	}
}

func (g *loginGuard) allow(r *http.Request) bool {
	ip := clientIP(r)
	now := time.Now()
	g.mu.Lock()
	defer g.mu.Unlock()
	if until := g.bannedUntil[ip]; until.After(now) {
		return false
	}
	delete(g.bannedUntil, ip)
	cut := now.Add(-g.window)
	list := g.attempts[ip]
	n := 0
	for _, t := range list {
		if t.After(cut) {
			list[n] = t
			n++
		}
	}
	list = list[:n]
	g.attempts[ip] = list
	return len(list) < g.max
}

func (g *loginGuard) fail(r *http.Request) {
	ip := clientIP(r)
	now := time.Now()
	g.mu.Lock()
	defer g.mu.Unlock()
	g.attempts[ip] = append(g.attempts[ip], now)
	if len(g.attempts[ip]) >= g.max {
		g.bannedUntil[ip] = now.Add(g.banFor)
		delete(g.attempts, ip)
	}
}

func (g *loginGuard) succeed(r *http.Request) {
	ip := clientIP(r)
	g.mu.Lock()
	delete(g.attempts, ip)
	g.mu.Unlock()
}

func clientIP(r *http.Request) string {
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		parts := strings.Split(xff, ",")
		return strings.TrimSpace(parts[0])
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}
