package main

import (
	"context"
	"log"
	"os"
	"os/signal"
	"syscall"

	"github.com/Baddysays/wernanmail/server/internal/mailcfg"
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
	r := &worker.Runner{
		Store:         st,
		Queue:         st,
		Settings:      sm,
		Transport:     &outbound.SMTPTransporter{RelayHost: relay, EHLOHost: cfg.EHLOHost, RequireTLS: requireTLS},
		WorkerID:      "worker-1",
		Hostname:      cfg.EHLOHost,
		BounceEnabled: sm.GetBool(settings.KeyBounceEnabled, true),
		RequireTLS:    requireTLS,
	}
	log.Printf("queue worker started (bounce=%v require_tls=%v)", r.BounceEnabled, r.RequireTLS)
	r.Run(ctx)
}
