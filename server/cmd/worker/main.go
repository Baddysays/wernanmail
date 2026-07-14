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
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	r := &worker.Runner{
		Store:     st,
		Queue:     st,
		Transport: &outbound.SMTPTransporter{RelayHost: relay},
		WorkerID:  "worker-1",
	}
	log.Printf("queue worker started")
	r.Run(ctx)
}
