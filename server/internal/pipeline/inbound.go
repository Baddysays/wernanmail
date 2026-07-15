package pipeline

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"mime"
	"net/mail"
	"strings"

	"github.com/Baddysays/wernanmail/server/internal/antispam"
	"github.com/Baddysays/wernanmail/server/internal/antivirus"
	"github.com/Baddysays/wernanmail/server/internal/domain"
	"github.com/Baddysays/wernanmail/server/internal/queue"
	"github.com/Baddysays/wernanmail/server/internal/received"
	"github.com/Baddysays/wernanmail/server/internal/store"
)

// Inbound processes accepted SMTP DATA.
type Inbound struct {
	Store    store.MessageStore
	Queue    *queue.Service
	Spam     *antispam.Engine
	AV       antivirus.Scanner
	MaxBytes int
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
	Action  domain.SpamAction
	Verdict domain.SpamVerdict
	Err     error
}

// Process runs spam → AV → enqueue or quarantine.
func (p *Inbound) Process(ctx context.Context, in ProcessInput) Result {
	raw := in.Raw
	if p.MaxBytes > 0 && len(raw) > p.MaxBytes {
		return Result{Action: domain.SpamReject, Err: fmt.Errorf("message too large")}
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
		return Result{Action: verdict.Action, Verdict: verdict, Err: fmt.Errorf("rejected by antispam")}
	}

	if p.AV != nil {
		res, err := p.AV.Scan(ctx, raw, headers["Subject"])
		if err == nil && !res.Clean {
			verdict.Action = domain.SpamReject
			verdict.Reasons = append(verdict.Reasons, domain.SpamReason{Code: "virus", Detail: res.Name + " " + res.Detail, Score: 100})
			return Result{Action: domain.SpamReject, Verdict: verdict, Err: fmt.Errorf("virus: %s", res.Name)}
		}
	}

	subj := headers["Subject"]
	if decoded, err := decodeHeader(subj); err == nil {
		subj = decoded
	}

	for _, rcpt := range in.Recipients {
		mid, err := p.Store.ResolveRecipient(ctx, rcpt)
		if err != nil {
			continue
		}
		if verdict.Action == domain.SpamQuarantine {
			vj, _ := json.Marshal(verdict)
			if err := p.Store.AddQuarantine(ctx, &domain.QuarantineItem{
				MailboxID:   mid,
				Subject:     subj,
				FromAddr:    in.From,
				VerdictJSON: string(vj),
			}, raw); err != nil {
				return Result{Action: verdict.Action, Verdict: verdict, Err: fmt.Errorf("quarantine: %w", err)}
			}
			continue
		}
		folder := domain.FolderInbox
		if verdict.Action == domain.SpamFlag {
			folder = domain.FolderSpam
		}
		payload := queue.InboundPayload{
			MailboxID: mid,
			Folder:    folder,
			RawB64:    base64.StdEncoding.EncodeToString(raw),
			Subject:   subj,
			From:      in.From,
			To:        rcpt,
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
