package worker

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"log"
	"net/mail"
	"strings"
	"time"

	"github.com/Baddysays/wernanmail/server/internal/dnsauth"
	"github.com/Baddysays/wernanmail/server/internal/domain"
	"github.com/Baddysays/wernanmail/server/internal/outbound"
	"github.com/Baddysays/wernanmail/server/internal/queue"
	"github.com/Baddysays/wernanmail/server/internal/store"
)

var errQuota = errors.New("mailbox quota exceeded")

// Runner consumes queue jobs.
type Runner struct {
	Store      store.MessageStore
	Queue      store.QueueStore
	Transport  outbound.Transporter
	PollEvery  time.Duration
	WorkerID   string
}

func (r *Runner) Run(ctx context.Context) {
	if r.PollEvery == 0 {
		r.PollEvery = 2 * time.Second
	}
	if r.WorkerID == "" {
		r.WorkerID = "worker-1"
	}
	t := time.NewTicker(r.PollEvery)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			r.drain(ctx)
		}
	}
}

func (r *Runner) drain(ctx context.Context) {
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
			dead := job.Attempts >= job.MaxAttempts
			retry := queue.Backoff(job.Attempts)
			_ = r.Queue.Fail(ctx, job.ID, err.Error(), retry, dead)
			log.Printf("job %d failed: %v (dead=%v)", job.ID, err, dead)
			continue
		}
		_ = r.Queue.Complete(ctx, job.ID)
	}
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
		}
		mb, err := r.Store.GetMailboxByID(ctx, p.MailboxID)
		if err != nil {
			return err
		}
		if mb != nil && mb.QuotaBytes > 0 {
			used, _ := r.Store.UsageBytes(ctx, p.MailboxID)
			if used+int64(len(raw)) > mb.QuotaBytes {
				return errQuota
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
		if p.DomainID > 0 {
			domains, _ := r.Store.ListDomains(ctx)
			for _, d := range domains {
				if d.ID == p.DomainID && d.DKIMPrivate != "" {
					signed, err := dnsauth.SignDKIM(raw, d.Name, d.DKIMSelector, d.DKIMPrivate)
					if err == nil {
						raw = signed
					}
					break
				}
			}
		}
		if r.Transport == nil {
			return nil
		}
		return r.Transport.Send(ctx, p.From, p.To, raw)

	case domain.JobBounce:
		return nil
	default:
		return nil
	}
}
