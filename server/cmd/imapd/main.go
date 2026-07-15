package main

import (
	"context"
	"log"

	"github.com/Baddysays/wernanmail/server/internal/imapd"
	"github.com/Baddysays/wernanmail/server/internal/mailcfg"
	"github.com/Baddysays/wernanmail/server/internal/settings"
	"github.com/Baddysays/wernanmail/server/internal/store/sqlite"
)

func main() {
	cfg := mailcfg.Load()
	st, err := sqlite.Open(cfg.DataDir)
	if err != nil {
		log.Fatal(err)
	}
	defer st.Close()
	tlsCfg, err := cfg.LoadTLSConfig()
	if err != nil {
		log.Fatal("tls: ", err)
	}
	if tlsCfg == nil {
		log.Printf("imapd: WARNING MAIL_TLS_CERT/KEY unset — AUTH allowed over plaintext (dev only)")
	}
	sm := settings.NewManager(st)
	be := &imapd.Backend{
		Store:          st,
		MasterPassword: cfg.MasterPassword,
		SuperuserEnabled: func() bool {
			_ = sm.Reload(context.Background())
			return sm.GetBool(settings.KeySuperuserEnabled, false)
		},
	}
	if cfg.MasterPassword != "" {
		log.Printf("imapd: master password configured (admin impersonation)")
	}
	// AllowInsecureAuth must stay true while stunnel terminates TLS on :993/:465
	// and forwards plaintext to this process — otherwise LOGIN/AUTH stay disabled
	// (Outlook IMAPS/SMTPS). STARTTLS on :143/:587 still works when TLSConfig is set.
	log.Fatal(imapd.Listen(imapd.ListenOpts{
		Addr:              cfg.IMAPAddr,
		Backend:           be,
		TLSConfig:         tlsCfg,
		AllowInsecureAuth: true,
	}))
}
