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
	"github.com/Baddysays/wernanmail/server/internal/store"
)

// Inbound processes accepted SMTP DATA.
type Inbound struct {
	Store   store.MessageStore
	Queue   *queue.Service
	Spam    *antispam.Engine
	AV      antivirus.Scanner
	MaxBytes int
}

// Result of processing one inbound message.
type Result struct {
	Action  domain.SpamAction
	Verdict domain.SpamVerdict
	Err     error
}

// Process runs spam → AV → enqueue or quarantine.
func (p *Inbound) Process(ctx context.Context, from string, recipients []string, remoteIP, helo string, raw []byte) Result {
	if p.MaxBytes > 0 && len(raw) > p.MaxBytes {
		return Result{Action: domain.SpamReject, Err: fmt.Errorf("message too large")}
	}
	headers := parseHeaders(raw)
	verdict := domain.SpamVerdict{Action: domain.SpamDeliver}
	if p.Spam != nil {
		verdict = p.Spam.Check(ctx, antispam.Input{
			From: from, Helo: helo, RemoteIP: remoteIP, Recipients: recipients, Raw: raw, Headers: headers,
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

	for _, rcpt := range recipients {
		mid, err := p.Store.ResolveRecipient(ctx, rcpt)
		if err != nil {
			continue
		}
		if verdict.Action == domain.SpamQuarantine {
			vj, _ := json.Marshal(verdict)
			_ = p.Store.AddQuarantine(ctx, &domain.QuarantineItem{
				MailboxID:   mid,
				Subject:     subj,
				FromAddr:    from,
				VerdictJSON: string(vj),
			}, raw)
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
			From:      from,
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
