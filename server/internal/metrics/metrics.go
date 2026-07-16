// Package metrics is a tiny Prometheus text exposition helper (no extra deps).
package metrics

import (
	"fmt"
	"io"
	"net/http"
	"runtime"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// Registry holds process-local counters and gauges.
type Registry struct {
	mu       sync.Mutex
	counters map[string]*atomic.Int64
	gauges   map[string]*atomic.Int64
	started  time.Time
	process  string
}

// New creates a registry labeled with the process name (mta, worker, admin, …).
func New(process string) *Registry {
	return &Registry{
		counters: make(map[string]*atomic.Int64),
		gauges:   make(map[string]*atomic.Int64),
		started:  time.Now().UTC(),
		process:  process,
	}
}

func (r *Registry) counter(name string) *atomic.Int64 {
	r.mu.Lock()
	defer r.mu.Unlock()
	if c, ok := r.counters[name]; ok {
		return c
	}
	c := &atomic.Int64{}
	r.counters[name] = c
	return c
}

func (r *Registry) gauge(name string) *atomic.Int64 {
	r.mu.Lock()
	defer r.mu.Unlock()
	if g, ok := r.gauges[name]; ok {
		return g
	}
	g := &atomic.Int64{}
	r.gauges[name] = g
	return g
}

// Inc increments a counter.
func (r *Registry) Inc(name string, delta int64) {
	if r == nil {
		return
	}
	r.counter(name).Add(delta)
}

// Set sets a gauge.
func (r *Registry) Set(name string, value int64) {
	if r == nil {
		return
	}
	r.gauge(name).Store(value)
}

// WritePrometheus writes text exposition format to w.
func (r *Registry) WritePrometheus(w io.Writer) {
	if r == nil {
		return
	}
	proc := sanitize(r.process)
	_, _ = fmt.Fprintf(w, "# HELP wernanmail_up Process is up.\n")
	_, _ = fmt.Fprintf(w, "# TYPE wernanmail_up gauge\n")
	_, _ = fmt.Fprintf(w, "wernanmail_up{process=%q} 1\n", proc)
	_, _ = fmt.Fprintf(w, "# HELP wernanmail_uptime_seconds Seconds since process start.\n")
	_, _ = fmt.Fprintf(w, "# TYPE wernanmail_uptime_seconds gauge\n")
	_, _ = fmt.Fprintf(w, "wernanmail_uptime_seconds{process=%q} %d\n", proc, int64(time.Since(r.started).Seconds()))

	var ms runtime.MemStats
	runtime.ReadMemStats(&ms)
	_, _ = fmt.Fprintf(w, "# HELP wernanmail_go_goroutines Number of goroutines.\n")
	_, _ = fmt.Fprintf(w, "# TYPE wernanmail_go_goroutines gauge\n")
	_, _ = fmt.Fprintf(w, "wernanmail_go_goroutines{process=%q} %d\n", proc, runtime.NumGoroutine())
	_, _ = fmt.Fprintf(w, "# HELP wernanmail_go_heap_alloc_bytes Heap bytes allocated.\n")
	_, _ = fmt.Fprintf(w, "# TYPE wernanmail_go_heap_alloc_bytes gauge\n")
	_, _ = fmt.Fprintf(w, "wernanmail_go_heap_alloc_bytes{process=%q} %d\n", proc, ms.HeapAlloc)

	r.mu.Lock()
	cnames := make([]string, 0, len(r.counters))
	for n := range r.counters {
		cnames = append(cnames, n)
	}
	gnames := make([]string, 0, len(r.gauges))
	for n := range r.gauges {
		gnames = append(gnames, n)
	}
	sort.Strings(cnames)
	sort.Strings(gnames)
	type pair struct {
		name string
		val  int64
	}
	cs := make([]pair, 0, len(cnames))
	for _, n := range cnames {
		cs = append(cs, pair{n, r.counters[n].Load()})
	}
	gs := make([]pair, 0, len(gnames))
	for _, n := range gnames {
		gs = append(gs, pair{n, r.gauges[n].Load()})
	}
	r.mu.Unlock()

	for _, p := range cs {
		metric := "wernanmail_" + sanitize(p.name) + "_total"
		_, _ = fmt.Fprintf(w, "# TYPE %s counter\n", metric)
		_, _ = fmt.Fprintf(w, "%s{process=%q} %d\n", metric, proc, p.val)
	}
	for _, p := range gs {
		metric := "wernanmail_" + sanitize(p.name)
		_, _ = fmt.Fprintf(w, "# TYPE %s gauge\n", metric)
		_, _ = fmt.Fprintf(w, "%s{process=%q} %d\n", metric, proc, p.val)
	}
}

// Handler returns an HTTP handler for GET /metrics.
func (r *Registry) Handler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/plain; version=0.0.4; charset=utf-8")
		r.WritePrometheus(w)
	})
}

func sanitize(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return "unknown"
	}
	var b strings.Builder
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '_':
			b.WriteRune(r)
		default:
			b.WriteByte('_')
		}
	}
	return b.String()
}
