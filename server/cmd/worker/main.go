package main

import (
	"context"
	"log"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/Baddysays/wernanmail/server/internal/mailcfg"
	"github.com/Baddysays/wernanmail/server/internal/metrics"
	"github.com/Baddysays/wernanmail/server/internal/outbound"
	"github.com/Baddysays/wernanmail/server/internal/settings"
	"github.com/Baddysays/wernanmail/server/internal/store/sqlite"
	"github.com/Baddysays/wernanmail/server/internal/worker"
)

func main() {
	cfg := mailcfg.Load()
	st, err := sqlite.Open(cfg.DataDir)
	if err != nil {
		log.Fatal(err)
	}
	defer st.Close()
	sm := settings.NewManager(st)
	relay := cfg.RelayHost
	if relay == "" {
		relay = sm.Get(settings.KeyRelayHost)
	}
	requireTLS := sm.GetBool(settings.KeyRequireTLSOutbound, false)
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	reg := metrics.New("worker")
	metrics.ListenEnv(reg)
	r := &worker.Runner{
		Store:         st,
		Queue:         st,
		Settings:      sm,
		Transport:     &outbound.SMTPTransporter{RelayHost: relay, EHLOHost: cfg.EHLOHost, RequireTLS: requireTLS},
		WorkerID:      "worker-1",
		Hostname:      cfg.EHLOHost,
		BounceEnabled: sm.GetBool(settings.KeyBounceEnabled, true),
		RequireTLS:    requireTLS,
		Metrics:       reg,
	}
	slog.Info("queue worker started", "bounce", r.BounceEnabled, "require_tls", r.RequireTLS)
	log.Printf("queue worker started (bounce=%v require_tls=%v)", r.BounceEnabled, r.RequireTLS)
	r.Run(ctx)
}
