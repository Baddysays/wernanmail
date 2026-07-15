package main

import (
	"context"
	"log"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/joho/godotenv"

	"github.com/Baddysays/wernanmail/server/internal/api"
	"github.com/Baddysays/wernanmail/server/internal/config"
	"github.com/Baddysays/wernanmail/server/internal/mailtmpl"
	"github.com/Baddysays/wernanmail/server/internal/session"
	"github.com/Baddysays/wernanmail/server/internal/settings"
	"github.com/Baddysays/wernanmail/server/internal/store/sqlite"
)

func main() {
	_ = godotenv.Load() // optional .env; ignored if missing

	cfg := config.Load()
	if cfg.SessionSecret == "" {
		log.Printf("api: WARNING SESSION_SECRET unset — persisted session passwords are not encrypted")
	}
	dataDir := strings.TrimSpace(os.Getenv("DATA_DIR"))
	if dataDir == "" {
		dataDir = "./data"
	}
	sessionStore, err := session.NewFileStore(
		dataDir+"/api-sessions.json",
		time.Duration(cfg.SessionTTLHours)*time.Hour,
		cfg.SessionSecret,
	)
	if err != nil {
		log.Fatalf("api: open persistent session store: %v", err)
	}
	h := &api.Handler{Cfg: cfg, Store: sessionStore}

	if st, err := sqlite.Open(dataDir); err != nil {
		log.Printf("api: WARNING settings store unavailable (%v) — outbound templates disabled in webmail", err)
	} else {
		defer st.Close()
		sm := settings.NewManager(st)
		h.OutboundPolicy = func() mailtmpl.Policy {
			_ = sm.Reload(context.Background())
			return mailtmpl.Policy{
				BodyPlain: sm.Get(settings.KeyBodyTemplatePlain),
				BodyHTML:  sm.Get(settings.KeyBodyTemplateHTML),
				FootPlain: sm.Get(settings.KeyFooterPlain),
				FootHTML:  sm.Get(settings.KeyFooterHTML),
				SkipReply: sm.GetBool(settings.KeyFooterSkipReplies, true),
			}
		}
	}

	router := api.NewRouter(h)

	log.Printf("wernanmail api listening on %s", cfg.Addr)
	if err := http.ListenAndServe(cfg.Addr, router); err != nil {
		log.Printf("server stopped: %v", err)
		os.Exit(1)
	}
}
