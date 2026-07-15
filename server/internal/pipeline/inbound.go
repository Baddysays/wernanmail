package pipeline

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"mime"
	"net/mail"
	"strings"
	"sync"

	"github.com/Baddysays/wernanmail/server/internal/antispam"
	"github.com/Baddysays/wernanmail/server/internal/antivirus"
	"github.com/Baddysays/wernanmail/server/internal/dmarc"
	"github.com/Baddysays/wernanmail/server/internal/domain"
	"github.com/Baddysays/wernanmail/server/internal/mailfilter"
	"github.com/Baddysays/wernanmail/server/internal/queue"
	"github.com/Baddysays/wernanmail/server/internal/received"
	"github.com/Baddysays/wernanmail/server/internal/store"
)

// Inbound processes accepted SMTP DATA.
type Inbound struct {
	Store store.MessageStore
	Queue *queue.Service
	Spam  *antispam.Engine

	mu       sync.RWMutex
	AV       antivirus.Scanner
	MaxBytes int
}

// SetPolicy updates AV scanner and size limit (hot-reload safe).
func (p *Inbound) SetPolicy(av antivirus.Scanner, maxBytes int) {
	p.mu.Lock()
	p.AV = av
	p.MaxBytes = maxBytes
	p.mu.Unlock()
}

func (p *Inbound) policy() (antivirus.Scanner, int) {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.AV, p.MaxBytes
}

// ProcessInput is one inbound SMTP DATA.
type ProcessInput struct {
	From       string
	Recipients []string
	RemoteIP   string
	Helo       string
	Hostname   string
	AuthUser   string
	Raw        []byte
}

// Result of processing one inbound message.
type Result struct {
	Action      domain.SpamAction
	Verdict     domain.SpamVerdict
	Err         error
	SMTPMessage string
}

// Process runs spam → AV → enqueue or quarantine.
func (p *Inbound) Process(ctx context.Context, in ProcessInput) Result {
	av, maxBytes := p.policy()
	raw := in.Raw
	if maxBytes > 0 && len(raw) > maxBytes {
		return Result{Action: domain.SpamReject, Err: fmt.Errorf("message too large"), SMTPMessage: "message too large"}
	}
	raw = received.Prepend(raw, in.Helo, in.RemoteIP, in.Hostname, in.AuthUser)

	headers := parseHeaders(raw)
	verdict := domain.SpamVerdict{Action: domain.SpamDeliver}
	if p.Spam != nil {
		verdict = p.Spam.Check(ctx, antispam.Input{
			From: in.From, Helo: in.Helo, RemoteIP: in.RemoteIP, Recipients: in.Recipients, Raw: raw, Headers: headers,
		})
	}
	if verdict.Action == domain.SpamReject {
		msg := "rejected by antispam"
		if p.Spam != nil {
			msg = p.Spam.RejectText()
		}
		return Result{Action: verdict.Action, Verdict: verdict, Err: fmt.Errorf("%s", msg), SMTPMessage: msg}
	}

	if av != nil {
		res, err := av.Scan(ctx, raw, "")
		if err == nil && !res.Clean {
			verdict.Reasons = append(verdict.Reasons, domain.SpamReason{
				Code: "virus", Detail: strings.TrimSpace(res.Name + " " + res.Detail), Score: 100,
			})
			if res.PreferQuarantine {
				verdict.Action = domain.SpamQuarantine
				verdict.Score += 100
			} else {
				verdict.Action = domain.SpamReject
				msg := "blocked attachment policy: " + res.Name
				return Result{
					Action:      domain.SpamReject,
					Verdict:     verdict,
					Err:         fmt.Errorf("%s", msg),
					SMTPMessage: msg,
				}
			}
		}
	}

	subj := headers["Subject"]
	if decoded, err := decodeHeader(subj); err == nil {
		subj = decoded
	}

	type delivery struct {
		mailboxID int64
		recipient string
		folder    string
	}
	deliveries := make([]delivery, 0, len(in.Recipients))
	for _, rcpt := range in.Recipients {
		mid, err := p.Store.ResolveRecipient(ctx, rcpt)
		if err != nil {
			continue
		}
		folder := domain.FolderInbox
		if verdict.Action == domain.SpamFlag {
			folder = domain.FolderSpam
		} else if verdict.Action == domain.SpamDeliver {
			rules, err := p.Store.ListMailFilters(ctx, mid)
			// A filter lookup failure must not turn accepted mail into message loss.
			var rule *domain.MailFilter
			if err == nil {
				rule = mailfilter.FirstMatch(rules, mailfilter.MessageFields{From: in.From, Subject: subj, To: rcpt})
			}
			if rule != nil {
				switch rule.Action {
				case "reject":
					msg := "rejected by mailbox filter"
					return Result{Action: domain.SpamReject, Verdict: verdict, Err: fmt.Errorf("%s", msg), SMTPMessage: msg}
				case "flag_spam":
					folder = domain.FolderSpam
				case "fileinto":
					folder = strings.TrimSpace(rule.ActionArg)
				}
			}
		}
		deliveries = append(deliveries, delivery{mailboxID: mid, recipient: rcpt, folder: folder})
	}

	if reports, err := dmarc.ParseMessage(raw); err == nil {
		for _, d := range deliveries {
			copyReports := append([]domain.DMARCReport(nil), reports...)
			for i := range copyReports {
				copyReports[i].MailboxID = d.mailboxID
			}
			// Report indexing is supplemental and must never delay mail delivery.
			_ = p.Store.AddDMARCReports(ctx, copyReports)
		}
	}

	for _, d := range deliveries {
		if verdict.Action == domain.SpamQuarantine {
			vj, _ := json.Marshal(verdict)
			if err := p.Store.AddQuarantine(ctx, &domain.QuarantineItem{
				MailboxID:   d.mailboxID,
				Subject:     subj,
				FromAddr:    in.From,
				VerdictJSON: string(vj),
			}, raw); err != nil {
				return Result{Action: verdict.Action, Verdict: verdict, Err: fmt.Errorf("quarantine: %w", err)}
			}
			continue
		}
		payload := queue.InboundPayload{
			MailboxID: d.mailboxID,
			Folder:    d.folder,
			RawB64:    base64.StdEncoding.EncodeToString(raw),
			Subject:   subj,
			From:      in.From,
			To:        d.recipient,
			SpamScore: verdict.Score,
		}
		if err := p.Queue.EnqueueJSON(ctx, domain.JobInboundDeliver, payload); err != nil {
			return Result{Action: verdict.Action, Verdict: verdict, Err: err}
		}
	}
	return Result{Action: verdict.Action, Verdict: verdict}
}

func parseHeaders(raw []byte) map[string]string {
	out := map[string]string{}
	msg, err := mail.ReadMessage(strings.NewReader(string(raw)))
	if err != nil {
		return out
	}
	for k, vals := range msg.Header {
		if len(vals) > 0 {
			out[k] = vals[0]
		}
	}
	return out
}

func decodeHeader(s string) (string, error) {
	dec := new(mime.WordDecoder)
	return dec.DecodeHeader(s)
}
