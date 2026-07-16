package metrics

import (
	"log/slog"
	"net/http"
	"os"
	"strings"
	"time"
)

// ListenEnv starts a tiny /metrics HTTP server when METRICS_ADDR is set
// (e.g. ":9101"). Safe to call from any daemon; no-op when unset.
func ListenEnv(reg *Registry) {
	addr := strings.TrimSpace(os.Getenv("METRICS_ADDR"))
	if addr == "" || reg == nil {
		return
	}
	mux := http.NewServeMux()
	mux.Handle("/metrics", reg.Handler())
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok\n"))
	})
	srv := &http.Server{Addr: addr, Handler: mux, ReadHeaderTimeout: 5 * time.Second}
	go func() {
		slog.Info("metrics listening", "addr", addr, "process", reg.process)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			slog.Error("metrics server stopped", "err", err)
		}
	}()
}
