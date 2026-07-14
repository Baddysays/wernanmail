package main

import (
	"log"

	"github.com/Baddysays/wernanmail/server/internal/imapd"
	"github.com/Baddysays/wernanmail/server/internal/mailcfg"
	"github.com/Baddysays/wernanmail/server/internal/store/sqlite"
)

func main() {
	cfg := mailcfg.Load()
	st, err := sqlite.Open(cfg.DataDir)
	if err != nil {
		log.Fatal(err)
	}
	defer st.Close()
	be := &imapd.Backend{Store: st}
	log.Fatal(imapd.ListenAndServe(cfg.IMAPAddr, be))
}
