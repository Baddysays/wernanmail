package main

import (
	"context"
	"log"
	"strings"
	"time"

	"github.com/Baddysays/wernanmail/server/internal/antispam"
	"github.com/Baddysays/wernanmail/server/internal/antivirus"
	"github.com/Baddysays/wernanmail/server/internal/dnsauth"
	"github.com/Baddysays/wernanmail/server/internal/greylist"
	"github.com/Baddysays/wernanmail/server/internal/mailcfg"
	"github.com/Baddysays/wernanmail/server/internal/mailtmpl"
	"github.com/Baddysays/wernanmail/server/internal/metrics"
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

	tlsCfg, err := cfg.LoadTLSConfig()
	if err != nil {
		log.Fatal("tls: ", err)
	}
	if tlsCfg == nil {
		log.Printf("mta: WARNING MAIL_TLS_CERT/KEY unset — AUTH allowed over plaintext (dev only)")
	}

	sm := settings.NewManager(st)
	qs := &queue.Service{Store: st}
	spam := antispam.New(
		&dnsauth.Checker{},
		float64(sm.GetInt(settings.KeySpamRejectAt, 10)),
		float64(sm.GetInt(settings.KeySpamQuarantineAt, 5)),
		splitCSV(sm.Get(settings.KeySpamRBLs)),
	)
	spam.Signals = st
	spam.SetConfig(antispam.Config{
		RejectAt:      float64(sm.GetInt(settings.KeySpamRejectAt, 10)),
		QuarantineAt:  float64(sm.GetInt(settings.KeySpamQuarantineAt, 5)),
		FlagAt:        float64(sm.GetInt(settings.KeySpamFlagAt, 3)),
		RBLs:          splitCSV(sm.Get(settings.KeySpamRBLs)),
		RejectMessage: sm.Get(settings.KeySpamRejectMessage),
	})
	var av antivirus.Scanner = antivirus.Noop{}
	if sm.GetBool(settings.KeyAVEnabled, true) {
		// Light attachment policy is the default AV — ClamAV stays optional/off-host.
		av = antivirus.Light{MaxBytes: sm.GetInt(settings.KeyMaxMessageBytes, 25<<20)}
	}
	pipe := &pipeline.Inbound{
		Store: st, Queue: qs, Spam: spam,
	}
	pipe.SetPolicy(av, sm.GetInt(settings.KeyMaxMessageBytes, 25<<20))

	// Hot-reload spam/AV settings so admin UI changes apply without MTA restart.
	go func() {
		t := time.NewTicker(30 * time.Second)
		defer t.Stop()
		for range t.C {
			if err := sm.Reload(context.Background()); err != nil {
				continue
			}
			spam.SetConfig(antispam.Config{
				RejectAt:      float64(sm.GetInt(settings.KeySpamRejectAt, 10)),
				QuarantineAt:  float64(sm.GetInt(settings.KeySpamQuarantineAt, 5)),
				FlagAt:        float64(sm.GetInt(settings.KeySpamFlagAt, 3)),
				RBLs:          splitCSV(sm.Get(settings.KeySpamRBLs)),
				RejectMessage: sm.Get(settings.KeySpamRejectMessage),
			})
			var next antivirus.Scanner = antivirus.Noop{}
			if sm.GetBool(settings.KeyAVEnabled, true) {
				next = antivirus.Light{MaxBytes: sm.GetInt(settings.KeyMaxMessageBytes, 25<<20)}
			}
			pipe.SetPolicy(next, sm.GetInt(settings.KeyMaxMessageBytes, 25<<20))
		}
	}()

	glSecs := sm.GetInt(settings.KeyGreylistSeconds, 0)
	gl := greylist.New(24 * time.Hour)
	sendPerHour := sm.GetInt(settings.KeyRateSendPerHour, 200)
	var sendLim *settings.Limiter
	if sendPerHour > 0 {
		sendLim = settings.NewLimiterWindow(sendPerHour, time.Hour)
	} else {
		sendLim = settings.NewLimiterWindow(100000, time.Hour)
	}
	authFailPerMin := sm.GetInt(settings.KeyRateAuthFailPerMin, 20)
	if authFailPerMin <= 0 {
		authFailPerMin = 20
	}
	authLim := settings.NewLimiter(authFailPerMin)

	superuser := func() bool {
		_ = sm.Reload(context.Background())
		return sm.GetBool(settings.KeySuperuserEnabled, false)
	}
	outboundPolicy := func() mailtmpl.Policy {
		_ = sm.Reload(context.Background())
		return mailtmpl.Policy{
			BodyPlain: sm.Get(settings.KeyBodyTemplatePlain),
			BodyHTML:  sm.Get(settings.KeyBodyTemplateHTML),
			FootPlain: sm.Get(settings.KeyFooterPlain),
			FootHTML:  sm.Get(settings.KeyFooterHTML),
			SkipReply: sm.GetBool(settings.KeyFooterSkipReplies, true),
		}
	}

	reg := metrics.New("mta")
	metrics.ListenEnv(reg)

	errCh := make(chan error, 2)
	go func() {
		be := &smtpd.Backend{
			Store: st, Pipeline: pipe, Queue: qs,
			Limiter:      settings.NewLimiter(sm.GetInt(settings.KeyRateSMTPConnPerMin, 120)),
			AuthLimiter:  authLim,
			Greylist:     gl,
			GreylistSecs: glSecs,
			RequireAuth:  false,
			Hostname:     cfg.Hostname,
			Metrics:      reg,
		}
		errCh <- smtpd.Listen(smtpd.ListenOpts{
			Addr:              cfg.SMTPAddr,
			Backend:           be,
			Domain:            cfg.Hostname,
			TLSConfig:         tlsCfg,
			AllowInsecureAuth: true,
		})
	}()
	go func() {
		be := &smtpd.Backend{
			Store: st, Pipeline: pipe, Queue: qs,
			Limiter:          settings.NewLimiter(sm.GetInt(settings.KeyRateSubmitPerMin, 60)),
			SendLimiter:      sendLim,
			AuthLimiter:      authLim,
			RequireAuth:      true,
			Hostname:         cfg.Hostname,
			MasterPassword:   cfg.MasterPassword,
			SuperuserEnabled: superuser,
			OutboundPolicy:   outboundPolicy,
			Metrics:          reg,
		}
		errCh <- smtpd.Listen(smtpd.ListenOpts{
			Addr:      cfg.SubmitAddr,
			Backend:   be,
			Domain:    cfg.Hostname,
			TLSConfig: tlsCfg,
			// stunnel :465 → plaintext :587 needs AUTH without STARTTLS
			AllowInsecureAuth: true,
		})
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
