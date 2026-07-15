package antispam

import (
	"context"
	"fmt"
	"net"
	"strings"

	"github.com/Baddysays/wernanmail/server/internal/domain"
	"github.com/Baddysays/wernanmail/server/internal/dnsauth"
)

// Engine scores inbound mail.
type Engine struct {
	DNS          *dnsauth.Checker
	RejectAt     float64
	QuarantineAt float64
	RBLs         []string
}

// New constructs an Engine with thresholds applied once (no concurrent mutation).
func New(dns *dnsauth.Checker, rejectAt, quarantineAt float64, rbls []string) *Engine {
	if rejectAt == 0 {
		rejectAt = 10
	}
	if quarantineAt == 0 {
		quarantineAt = 5
	}
	return &Engine{
		DNS:          dns,
		RejectAt:     rejectAt,
		QuarantineAt: quarantineAt,
		RBLs:         rbls,
	}
}

// Input is one message under evaluation.
type Input struct {
	From       string
	Helo       string
	RemoteIP   string
	Recipients []string
	Raw        []byte
	Headers    map[string]string
}

// Check returns a verdict.
func (e *Engine) Check(ctx context.Context, in Input) domain.SpamVerdict {
	rejectAt := e.RejectAt
	quarantineAt := e.QuarantineAt
	if rejectAt == 0 {
		rejectAt = 10
	}
	if quarantineAt == 0 {
		quarantineAt = 5
	}
	var v domain.SpamVerdict
	add := func(code, detail string, score float64) {
		v.Score += score
		v.Reasons = append(v.Reasons, domain.SpamReason{Code: code, Detail: detail, Score: score})
	}

	if in.From == "" {
		add("empty_from", "missing MAIL FROM", 2)
	}
	if strings.Contains(strings.ToLower(in.From), "viagra") {
		add("keyword", "suspicious keyword in from", 4)
	}

	subj := strings.ToLower(in.Headers["Subject"])
	if strings.Count(subj, "!") > 3 {
		add("subj_bang", "excessive exclamation marks", 1.5)
	}
	if len(in.Raw) > 0 {
		body := string(in.Raw)
		if strings.Count(body, "http://")+strings.Count(body, "https://") > 15 {
			add("url_density", "many URLs in body", 2)
		}
		if strings.Contains(body, "Content-Transfer-Encoding: base64") && len(in.Raw) > 200_000 {
			add("base64_bomb", "large base64 payload", 2)
		}
	}

	if e.DNS != nil && in.From != "" {
		spf := e.DNS.CheckSPF(ctx, in.From, in.RemoteIP)
		switch spf {
		case "fail":
			add("spf_fail", "SPF fail", 3)
		case "softfail":
			add("spf_softfail", "SPF softfail", 1.5)
		case "pass":
			add("spf_pass", "SPF pass", -1)
		}
		if dkim := e.DNS.CheckDKIM(ctx, in.Raw); dkim == "pass" {
			add("dkim_pass", "DKIM pass", -1)
		} else if dkim == "fail" {
			add("dkim_fail", "DKIM fail", 2)
		}
	}

	if in.RemoteIP != "" && len(e.RBLs) > 0 {
		if hit := checkRBL(in.RemoteIP, e.RBLs); hit != "" {
			add("rbl", hit, 4)
		}
	}

	switch {
	case v.Score >= rejectAt:
		v.Action = domain.SpamReject
	case v.Score >= quarantineAt:
		v.Action = domain.SpamQuarantine
	case v.Score >= 2:
		v.Action = domain.SpamFlag
	default:
		v.Action = domain.SpamDeliver
	}
	return v
}

func checkRBL(ip string, zones []string) string {
	parsed := net.ParseIP(ip)
	if parsed == nil {
		return ""
	}
	b := parsed.To4()
	if b == nil {
		return "" // IPv6 RBLs not wired yet
	}
	rev := fmt.Sprintf("%d.%d.%d.%d", b[3], b[2], b[1], b[0])
	for _, zone := range zones {
		name := rev + "." + zone
		if _, err := net.LookupHost(name); err == nil {
			return zone
		}
	}
	return ""
}
