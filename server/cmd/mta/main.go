package main

import (
	"log"
	"strings"

	"github.com/Baddysays/wernanmail/server/internal/antispam"
	"github.com/Baddysays/wernanmail/server/internal/antivirus"
	"github.com/Baddysays/wernanmail/server/internal/dnsauth"
	"github.com/Baddysays/wernanmail/server/internal/mailcfg"
	"github.com/Baddysays/wernanmail/server/internal/pipeline"
	"github.com/Baddysays/wernanmail/server/internal/queue"
	"github.com/Baddysays/wernanmail/server/internal/settings"
	"github.com/Baddysays/wernanmail/server/internal/smtpd"
	"github.com/Baddysays/wernanmail/server/internal/store/sqlite"
)

func main() {
	cfg := mailcfg.Load()
	st, err := sqlite.Open(cfg.DataDir)
	if err != nil {
		log.Fatal(err)
	}
	defer st.Close()

	sm := settings.NewManager(st)
	qs := &queue.Service{Store: st}
	spam := &antispam.Engine{
		DNS:          &dnsauth.Checker{},
		RejectAt:     float64(sm.GetInt(settings.KeySpamRejectAt, 10)),
		QuarantineAt: float64(sm.GetInt(settings.KeySpamQuarantineAt, 5)),
		RBLs:         splitCSV(sm.Get(settings.KeySpamRBLs)),
	}
	var av antivirus.Scanner = antivirus.Light{}
	if sm.GetBool(settings.KeyAVEnabled, true) {
		if cfg.ClamAddr != "" {
			av = antivirus.ClamAV{Addr: cfg.ClamAddr}
		}
	} else {
		av = antivirus.Noop{}
	}
	pipe := &pipeline.Inbound{
		Store: st, Queue: qs, Spam: spam, AV: av,
		MaxBytes: sm.GetInt(settings.KeyMaxMessageBytes, 25<<20),
	}

	errCh := make(chan error, 2)
	go func() {
		be := &smtpd.Backend{
			Store: st, Pipeline: pipe,
			Limiter:     settings.NewLimiter(sm.GetInt(settings.KeyRateSMTPConnPerMin, 120)),
			RequireAuth: false,
		}
		errCh <- smtpd.ListenAndServe(cfg.SMTPAddr, be, cfg.Hostname)
	}()
	go func() {
		be := &smtpd.Backend{
			Store: st, Pipeline: pipe,
			Limiter:     settings.NewLimiter(sm.GetInt(settings.KeyRateSubmitPerMin, 60)),
			RequireAuth: true,
		}
		errCh <- smtpd.ListenAndServe(cfg.SubmitAddr, be, cfg.Hostname)
	}()
	log.Fatal(<-errCh)
}

func splitCSV(s string) []string {
	parts := strings.Split(s, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}
