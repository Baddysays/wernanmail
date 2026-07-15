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
		log.Printf("api: WARNING SESSION_SECRET unset — session passwords stored without encryption")
	}
	store := session.NewStoreWithSecret(time.Duration(cfg.SessionTTLHours)*time.Hour, cfg.SessionSecret)
	h := &api.Handler{Cfg: cfg, Store: store}

	dataDir := strings.TrimSpace(os.Getenv("DATA_DIR"))
	if dataDir == "" {
		dataDir = "./data"
	}
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
