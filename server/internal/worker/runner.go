package worker

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"log"
	"net/mail"
	"strings"
	"sync"
	"time"

	"github.com/Baddysays/wernanmail/server/internal/bounce"
	"github.com/Baddysays/wernanmail/server/internal/dnsauth"
	"github.com/Baddysays/wernanmail/server/internal/domain"
	"github.com/Baddysays/wernanmail/server/internal/outbound"
	"github.com/Baddysays/wernanmail/server/internal/queue"
	"github.com/Baddysays/wernanmail/server/internal/settings"
	"github.com/Baddysays/wernanmail/server/internal/store"
)

// Runner consumes queue jobs.
type Runner struct {
	Store         store.MessageStore
	Queue         store.QueueStore
	Transport     outbound.Transporter
	Settings      *settings.Manager
	PollEvery     time.Duration
	CleanupEvery  time.Duration
	WorkerID      string
	Hostname      string
	BounceEnabled bool
	RequireTLS    bool

	domMu    sync.RWMutex
	domCache map[int64]domain.Domain
	domAt    time.Time
}

func (r *Runner) Run(ctx context.Context) {
	if r.PollEvery == 0 {
		r.PollEvery = 2 * time.Second
	}
	if r.CleanupEvery == 0 {
		r.CleanupEvery = time.Hour
	}
	if r.WorkerID == "" {
		r.WorkerID = "worker-1"
	}
	t := time.NewTicker(r.PollEvery)
	defer t.Stop()
	cleanup := time.NewTicker(r.CleanupEvery)
	defer cleanup.Stop()
	r.cleanup(ctx) // run once at start
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			r.refreshPolicy()
			r.drain(ctx)
		case <-cleanup.C:
			r.cleanup(ctx)
		}
	}
}

func (r *Runner) refreshPolicy() {
	if r.Settings == nil {
		return
	}
	_ = r.Settings.Reload(context.Background())
	r.BounceEnabled = r.Settings.GetBool(settings.KeyBounceEnabled, true)
	r.RequireTLS = r.Settings.GetBool(settings.KeyRequireTLSOutbound, false)
	if tr, ok := r.Transport.(*outbound.SMTPTransporter); ok {
		tr.RequireTLS = r.RequireTLS
		if relay := strings.TrimSpace(r.Settings.Get(settings.KeyRelayHost)); relay != "" {
			tr.RelayHost = relay
		}
	}
}

func (r *Runner) cleanup(ctx context.Context) {
	if r.Settings == nil || r.Store == nil {
		return
	}
	_ = r.Settings.Reload(ctx)
	now := time.Now().UTC()

	if days := r.Settings.GetInt(settings.KeyQuarantineRetention, 14); days > 0 {
		older := now.AddDate(0, 0, -days)
		total := 0
		for {
			n, err := r.Store.PurgeQuarantineOlderThan(ctx, older, 200)
			if err != nil {
				log.Printf("quarantine purge: %v", err)
				break
			}
			total += n
			if n == 0 {
				break
			}
		}
		if total > 0 {
			log.Printf("quarantine purge: removed %d items older than %d days", total, days)
		}
	}

	if days := r.Settings.GetInt(settings.KeyRetentionDays, 0); days > 0 {
		older := now.AddDate(0, 0, -days)
		total := 0
		for {
			n, err := r.Store.DeleteMessagesOlderThan(ctx, older, 200)
			if err != nil {
				log.Printf("mail retention: %v", err)
				break
			}
			total += n
			if n == 0 {
				break
			}
		}
		if total > 0 {
			log.Printf("mail retention: deleted %d messages older than %d days", total, days)
		}
	}
}

func (r *Runner) drain(ctx context.Context) {
	defer func() {
		if rec := recover(); rec != nil {
			log.Printf("worker panic recovered: %v", rec)
		}
	}()
	for {
		job, err := r.Queue.Claim(ctx, r.WorkerID, 2*time.Minute)
		if err != nil {
			log.Printf("queue claim: %v", err)
			return
		}
		if job == nil {
			return
		}
		if err := r.handle(ctx, job); err != nil {
			msg := err.Error()
			dead := job.Attempts >= job.MaxAttempts || permanentFail(err)
			retry := queue.Backoff(job.Attempts)
			if failErr := r.Queue.Fail(ctx, job.ID, msg, retry, dead); failErr != nil {
				log.Printf("queue fail write: %v (job %d)", failErr, job.ID)
			}
			if dead && job.Kind == domain.JobOutboundSend && r.BounceEnabled {
				r.enqueueBounce(ctx, job, msg)
			}
			log.Printf("job %d failed: %v (dead=%v)", job.ID, err, dead)
			continue
		}
		if err := r.Queue.Complete(ctx, job.ID); err != nil {
			log.Printf("queue complete: %v (job %d)", err, job.ID)
		}
	}
}

func (r *Runner) enqueueBounce(ctx context.Context, job *domain.QueueJob, reason string) {
	var p queue.OutboundPayload
	if err := json.Unmarshal([]byte(job.PayloadJSON), &p); err != nil {
		return
	}
	failedTo := ""
	if len(p.To) > 0 {
		failedTo = p.To[0]
	}
	_ = (&queue.Service{Store: r.Queue}).EnqueueJSON(ctx, domain.JobBounce, queue.BouncePayload{
		OriginalFrom: p.From,
		FailedTo:     failedTo,
		Reason:       reason,
		RawB64:       p.RawB64,
	})
}

func (r *Runner) handle(ctx context.Context, job *domain.QueueJob) error {
	switch job.Kind {
	case domain.JobInboundDeliver:
		var p queue.InboundPayload
		if err := json.Unmarshal([]byte(job.PayloadJSON), &p); err != nil {
			return err
		}
		raw, err := base64.StdEncoding.DecodeString(p.RawB64)
		if err != nil {
			return err
		}
		msg := &domain.Message{
			MailboxID: p.MailboxID,
			Folder:    p.Folder,
			Subject:   p.Subject,
			FromAddr:  p.From,
			ToAddrs:   p.To,
			SpamScore: p.SpamScore,
			Date:      time.Now().UTC(),
			Flags:     nil,
		}
		if parsed, err := mail.ReadMessage(strings.NewReader(string(raw))); err == nil {
			if msg.MessageID == "" {
				msg.MessageID = parsed.Header.Get("Message-Id")
			}
			if msg.Subject == "" {
				msg.Subject = parsed.Header.Get("Subject")
			}
			if dh := parsed.Header.Get("Date"); dh != "" {
				if t, err := mail.ParseDate(dh); err == nil {
					msg.Date = t.UTC()
				}
			}
		}
		return r.Store.AppendMessage(ctx, msg, raw)

	case domain.JobOutboundSend:
		var p queue.OutboundPayload
		if err := json.Unmarshal([]byte(job.PayloadJSON), &p); err != nil {
			return err
		}
		raw, err := base64.StdEncoding.DecodeString(p.RawB64)
		if err != nil {
			return err
		}
		idHost := r.Hostname
		if p.DomainID > 0 {
			if d, ok := r.domainByID(ctx, p.DomainID); ok {
				if idHost == "" {
					idHost = d.Name
				}
				raw = outbound.EnsureRFCHeaders(raw, idHost)
				if d.DKIMPrivate != "" {
					signed, err := dnsauth.SignDKIM(raw, d.Name, d.DKIMSelector, d.DKIMPrivate)
					if err == nil {
						raw = signed
					} else {
						log.Printf("dkim sign: %v", err)
					}
				}
			} else {
				raw = outbound.EnsureRFCHeaders(raw, idHost)
			}
		} else {
			raw = outbound.EnsureRFCHeaders(raw, idHost)
		}
		if r.Transport == nil {
			return nil
		}
		err = r.Transport.Send(ctx, p.From, p.To, raw)
		if err == nil {
			return nil
		}
		if de := outbound.AsDeliveryError(err); de != nil && len(de.Partial) > 0 {
			rejected := recipDiff(p.To, de.Partial)
			if len(rejected) == 0 {
				return nil
			}
			if qerr := (&queue.Service{Store: r.Queue}).EnqueueJSON(ctx, domain.JobOutboundSend, queue.OutboundPayload{
				From: p.From, To: rejected, RawB64: p.RawB64, DomainID: p.DomainID,
			}); qerr != nil {
				log.Printf("partial requeue: %v", qerr)
				return err
			}
			log.Printf("partial delivery: accepted %v, requeued %v", de.Partial, rejected)
			return nil
		}
		return err

	case domain.JobBounce:
		var p queue.BouncePayload
		if err := json.Unmarshal([]byte(job.PayloadJSON), &p); err != nil {
			return err
		}
		rawOrig, _ := base64.StdEncoding.DecodeString(p.RawB64)
		dsn := bounce.BuildDSN(p.OriginalFrom, p.FailedTo, r.Hostname, p.Reason, rawOrig)
		mid, err := r.Store.ResolveRecipient(ctx, p.OriginalFrom)
		if err != nil {
			log.Printf("bounce: original sender %s not local (%v) — drop DSN", p.OriginalFrom, err)
			return nil
		}
		msg := &domain.Message{
			MailboxID: mid,
			Folder:    domain.FolderInbox,
			Subject:   "Undelivered Mail Returned to Sender",
			FromAddr:  "MAILER-DAEMON@" + r.Hostname,
			ToAddrs:   p.OriginalFrom,
			Date:      time.Now().UTC(),
		}
		return r.Store.AppendMessage(ctx, msg, dsn)

	default:
		return nil
	}
}

func recipDiff(all, accepted []string) []string {
	set := map[string]struct{}{}
	for _, a := range accepted {
		set[strings.ToLower(strings.Trim(a, "<>"))] = struct{}{}
	}
	var out []string
	for _, t := range all {
		key := strings.ToLower(strings.Trim(t, "<>"))
		if _, ok := set[key]; !ok {
			out = append(out, t)
		}
	}
	return out
}

func (r *Runner) domainByID(ctx context.Context, id int64) (domain.Domain, bool) {
	r.domMu.RLock()
	if r.domCache != nil && time.Since(r.domAt) < 60*time.Second {
		d, ok := r.domCache[id]
		r.domMu.RUnlock()
		return d, ok
	}
	r.domMu.RUnlock()

	r.domMu.Lock()
	defer r.domMu.Unlock()
	if r.domCache != nil && time.Since(r.domAt) < 60*time.Second {
		d, ok := r.domCache[id]
		return d, ok
	}
	list, err := r.Store.ListDomains(ctx)
	if err != nil {
		return domain.Domain{}, false
	}
	m := make(map[int64]domain.Domain, len(list))
	for _, d := range list {
		m[d.ID] = d
	}
	r.domCache = m
	r.domAt = time.Now()
	d, ok := m[id]
	return d, ok
}

func permanentFail(err error) bool {
	if de := outbound.AsDeliveryError(err); de != nil {
		return de.Permanent()
	}
	msg := strings.TrimSpace(err.Error())
	if msg == "" {
		return false
	}
	if len(msg) >= 3 && msg[0] == '5' && msg[1] >= '0' && msg[1] <= '9' && msg[2] >= '0' && msg[2] <= '9' {
		return true
	}
	return strings.Contains(msg, "5.1.1") || strings.Contains(msg, "user unknown") || strings.Contains(msg, "NoSuchUser")
}
