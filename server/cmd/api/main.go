package main

import (
	"log"
	"net/http"
	"os"
	"time"

	"github.com/joho/godotenv"

	"github.com/Baddysays/wernanmail/server/internal/api"
	"github.com/Baddysays/wernanmail/server/internal/config"
	"github.com/Baddysays/wernanmail/server/internal/session"
)

func main() {
	_ = godotenv.Load() // optional .env; ignored if missing

	cfg := config.Load()
	store := session.NewStore(time.Duration(cfg.SessionTTLHours) * time.Hour)
	h := &api.Handler{Cfg: cfg, Store: store}
	router := api.NewRouter(h)

	log.Printf("wernanmail api listening on %s", cfg.Addr)
	if err := http.ListenAndServe(cfg.Addr, router); err != nil {
		log.Printf("server stopped: %v", err)
		os.Exit(1)
	}
}
