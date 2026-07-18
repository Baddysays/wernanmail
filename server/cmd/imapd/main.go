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

	errCh := make(chan error, 2)
	go func() {
		// AllowInsecureAuth must stay true while stunnel terminates TLS on :993
		// and forwards plaintext here — otherwise LOGIN/AUTH stay disabled.
		// STARTTLS on :143 still works when TLSConfig is set.
		errCh <- imapd.Listen(imapd.ListenOpts{
			Addr:              cfg.IMAPAddr,
			Backend:           be,
			TLSConfig:         tlsCfg,
			AllowInsecureAuth: true,
		})
	}()
	if cfg.IMAPSAddr != "" {
		if tlsCfg == nil {
			log.Printf("imapd: IMAPS_ADDR=%s set but MAIL_TLS_CERT/KEY unset — skipping implicit TLS", cfg.IMAPSAddr)
		} else {
			go func() {
				errCh <- imapd.Listen(imapd.ListenOpts{
					Addr:              cfg.IMAPSAddr,
					Backend:           be,
					TLSConfig:         tlsCfg,
					AllowInsecureAuth: false,
					ImplicitTLS:       true,
				})
			}()
		}
	}
	log.Fatal(<-errCh)
}
