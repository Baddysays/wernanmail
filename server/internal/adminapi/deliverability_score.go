package adminapi

import (
	"context"
	"io"
	"math"
	"net/http"
	"strings"
	"time"

	"github.com/Baddysays/wernanmail/server/internal/domain"
)

type dnsSnapshot struct {
	Domain   string
	MailHost string
	MX       map[string]string
	SPF      map[string]string
	DKIM     map[string]string
	DMARC    map[string]string
	MTASTS   map[string]string
	TLSRPT   map[string]string
	BIMI     map[string]string
}

type scoreItem struct {
	ID     string  `json:"id"`
	Label  string  `json:"label"`
	State  string  `json:"state"`
	Points float64 `json:"points"`
	Max    float64 `json:"max"`
	Detail string  `json:"detail,omitempty"`
}

type deliverabilityRating struct {
	Score   float64     `json:"score"`
	Max     float64     `json:"max"`
	Verdict string      `json:"verdict"` // perfect | good | attention | critical
	Items   []scoreItem `json:"items"`
}

func (h *Handler) collectDNSStatus(ctx context.Context, wantName string) dnsSnapshot {
	out := dnsSnapshot{
		MX:     checkResult("missing", "no domain"),
		SPF:    checkResult("missing", "no domain"),
		DKIM:   checkResult("missing", "no domain"),
		DMARC:  checkResult("missing", "no domain"),
		MTASTS: checkResult("missing", "no domain"),
		TLSRPT: checkResult("missing", "no domain"),
		BIMI:   checkResult("missing", "no domain"),
	}
	if h.Store == nil {
		return out
	}
	domains, err := h.Store.ListDomains(ctx)
	if err != nil || len(domains) == 0 {
		return out
	}
	var d *domain.Domain
	name := strings.ToLower(strings.TrimSpace(wantName))
	for i := range domains {
		if name != "" && domains[i].Name == name {
			d = &domains[i]
			break
		}
	}
	if d == nil {
		d = &domains[0]
		name = d.Name
	}
	out.Domain = name
	mailHost := strings.TrimSpace(h.Cfg.Hostname)
	if mailHost == "" {
		mailHost = "mail." + name
	}
	out.MailHost = mailHost
	res := publicDNSResolver()

	mxState, mxDetail := "missing", "no MX"
	if mxs, err := res.LookupMX(ctx, name); err == nil && len(mxs) > 0 {
		found := false
		parts := make([]string, 0, len(mxs))
		want := strings.TrimSuffix(strings.ToLower(mailHost), ".")
		for _, mx := range mxs {
			host := strings.TrimSuffix(strings.ToLower(mx.Host), ".")
			parts = append(parts, host)
			if host == want {
				found = true
			}
		}
		if found {
			mxState, mxDetail = "ok", strings.Join(parts, ", ")
		} else {
			mxState, mxDetail = "warn", "MX: "+strings.Join(parts, ", ")
		}
	}
	out.MX = checkResult(mxState, mxDetail)

	spfState, spfDetail := "missing", "no TXT"
	if txts, err := res.LookupTXT(ctx, name); err == nil {
		for _, t := range txts {
			tt := strings.TrimSpace(t)
			if strings.HasPrefix(tt, "v=spf1") {
				spfState, spfDetail = "ok", tt
				break
			}
		}
	}
	out.SPF = checkResult(spfState, spfDetail)

	selector := d.DKIMSelector
	if selector == "" {
		selector = "wernan"
	}
	dkimHost := selector + "._domainkey." + name
	dkimState, dkimDetail := "missing", "not published"
	if d.DKIMPublic == "" {
		dkimState, dkimDetail = "warn", "no local key"
	} else if txts, err := res.LookupTXT(ctx, dkimHost); err == nil && len(txts) > 0 {
		pub := strings.Join(txts, "")
		want := extractDKIMP(d.DKIMPublic)
		got := extractDKIMP(pub)
		if want != "" && got != "" && want == got {
			dkimState, dkimDetail = "ok", "published"
		} else if strings.Contains(pub, "v=DKIM1") || strings.Contains(pub, "p=") {
			dkimState, dkimDetail = "warn", "TXT found, key mismatch"
		} else {
			dkimState, dkimDetail = "warn", "unexpected TXT"
		}
	} else if d.DKIMPublic != "" {
		dkimState, dkimDetail = "warn", "key ready, not in DNS"
	}
	out.DKIM = checkResult(dkimState, dkimDetail)

	dmarcState, dmarcDetail := "missing", "no _dmarc"
	if txts, err := res.LookupTXT(ctx, "_dmarc."+name); err == nil {
		for _, t := range txts {
			if strings.Contains(strings.ToLower(t), "v=dmarc1") {
				dmarcState, dmarcDetail = "ok", t
				break
			}
		}
	}
	out.DMARC = checkResult(dmarcState, dmarcDetail)

	out.MTASTS = checkMTASTS(ctx, res, name)
	out.TLSRPT = checkTLSRPT(ctx, res, name)
	out.BIMI = checkBIMI(ctx, res, name)
	return out
}

func checkMTASTS(ctx context.Context, res interface {
	LookupTXT(context.Context, string) ([]string, error)
}, name string) map[string]string {
	txtOK := false
	txtDetail := "no _mta-sts TXT"
	if txts, err := res.LookupTXT(ctx, "_mta-sts."+name); err == nil {
		for _, t := range txts {
			tt := strings.ToLower(strings.TrimSpace(t))
			if strings.Contains(tt, "v=stsv1") {
				txtOK = true
				txtDetail = strings.TrimSpace(t)
				break
			}
		}
	}
	policyURL := "https://mta-sts." + name + "/.well-known/mta-sts.txt"
	policyOK, policyDetail := fetchMTASTSPolicy(ctx, policyURL)
	switch {
	case txtOK && policyOK:
		return checkResult("ok", "TXT + policy ("+policyDetail+")")
	case txtOK && !policyOK:
		return checkResult("warn", "TXT ok; policy: "+policyDetail)
	case !txtOK && policyOK:
		return checkResult("warn", "policy ok; missing _mta-sts TXT")
	default:
		return checkResult("missing", txtDetail+"; "+policyDetail)
	}
}

func fetchMTASTSPolicy(ctx context.Context, url string) (ok bool, detail string) {
	reqCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(reqCtx, http.MethodGet, url, nil)
	if err != nil {
		return false, "bad url"
	}
	client := &http.Client{Timeout: 3 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return false, "HTTPS unreachable"
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
	if resp.StatusCode >= 300 {
		return false, "HTTP " + http.StatusText(resp.StatusCode)
	}
	text := strings.ToLower(string(body))
	if !strings.Contains(text, "version: stsv1") {
		return false, "invalid policy body"
	}
	mode := "unknown"
	for _, line := range strings.Split(string(body), "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(strings.ToLower(line), "mode:") {
			mode = strings.TrimSpace(line[5:])
			break
		}
	}
	return true, "mode=" + mode
}

func checkTLSRPT(ctx context.Context, res interface {
	LookupTXT(context.Context, string) ([]string, error)
}, name string) map[string]string {
	if txts, err := res.LookupTXT(ctx, "_smtp._tls."+name); err == nil {
		for _, t := range txts {
			tt := strings.ToLower(strings.TrimSpace(t))
			if strings.Contains(tt, "v=tlsrptv1") {
				return checkResult("ok", strings.TrimSpace(t))
			}
		}
	}
	return checkResult("missing", "no _smtp._tls TXT")
}

func checkBIMI(ctx context.Context, res interface {
	LookupTXT(context.Context, string) ([]string, error)
}, name string) map[string]string {
	var record string
	if txts, err := res.LookupTXT(ctx, "default._bimi."+name); err == nil {
		for _, t := range txts {
			tt := strings.ToLower(strings.TrimSpace(t))
			if strings.Contains(tt, "v=bimi1") {
				record = strings.TrimSpace(t)
				break
			}
		}
	}
	if record == "" {
		return checkResult("missing", "no default._bimi TXT")
	}
	logoURL := extractBIMILogo(record)
	if logoURL == "" {
		return checkResult("warn", "TXT without l= logo URL")
	}
	if ok, detail := probeBIMILogo(ctx, logoURL); ok {
		return checkResult("ok", "logo "+detail)
	} else {
		return checkResult("warn", "TXT ok; logo: "+detail)
	}
}

func extractBIMILogo(record string) string {
	lower := strings.ToLower(record)
	idx := strings.Index(lower, "l=")
	if idx < 0 {
		return ""
	}
	rest := record[idx+2:]
	end := len(rest)
	for i, r := range rest {
		if r == ';' || r == ' ' {
			end = i
			break
		}
	}
	return strings.TrimSpace(rest[:end])
}

func probeBIMILogo(ctx context.Context, url string) (ok bool, detail string) {
	if !strings.HasPrefix(strings.ToLower(url), "https://") {
		return false, "l= must be https"
	}
	reqCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(reqCtx, http.MethodGet, url, nil)
	if err != nil {
		return false, "bad url"
	}
	client := &http.Client{Timeout: 3 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return false, "unreachable"
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 64<<10))
	if resp.StatusCode >= 300 {
		return false, "HTTP status"
	}
	ct := strings.ToLower(resp.Header.Get("Content-Type"))
	text := strings.ToLower(string(body))
	if strings.Contains(ct, "svg") || strings.Contains(text, "<svg") {
		return true, "svg ok"
	}
	return false, "not SVG"
}

func scorePoints(state string, max float64) float64 {
	switch state {
	case "ok":
		return max
	case "warn":
		return math.Round(max*5) / 10 // half, one decimal
	default:
		return 0
	}
}

// optionalScorePoints: missing does not punish (record not published yet).
func optionalScorePoints(state string, max float64) float64 {
	if state == "" {
		state = "missing"
	}
	switch state {
	case "ok", "missing":
		return max
	case "warn":
		return math.Round(max*5) / 10
	default:
		return 0
	}
}

func buildDeliverabilityRating(dns dnsSnapshot, ptr, rbl map[string]string, spam map[string]any) deliverabilityRating {
	spamState, _ := spam["state"].(string)
	spamDetail, _ := spam["detail"].(string)
	items := []scoreItem{
		{ID: "mx", Label: "MX", State: dns.MX["state"], Max: 1, Points: scorePoints(dns.MX["state"], 1), Detail: dns.MX["detail"]},
		{ID: "spf", Label: "SPF", State: dns.SPF["state"], Max: 2, Points: scorePoints(dns.SPF["state"], 2), Detail: dns.SPF["detail"]},
		{ID: "dkim", Label: "DKIM", State: dns.DKIM["state"], Max: 2, Points: scorePoints(dns.DKIM["state"], 2), Detail: dns.DKIM["detail"]},
		{ID: "dmarc", Label: "DMARC", State: dns.DMARC["state"], Max: 1, Points: scorePoints(dns.DMARC["state"], 1), Detail: dns.DMARC["detail"]},
		{ID: "mtasts", Label: "STS", State: dns.MTASTS["state"], Max: 0.5, Points: optionalScorePoints(dns.MTASTS["state"], 0.5), Detail: dns.MTASTS["detail"]},
		{ID: "tlsrpt", Label: "TLS-RPT", State: dns.TLSRPT["state"], Max: 0.5, Points: optionalScorePoints(dns.TLSRPT["state"], 0.5), Detail: dns.TLSRPT["detail"]},
		{ID: "bimi", Label: "BIMI", State: dns.BIMI["state"], Max: 0.5, Points: optionalScorePoints(dns.BIMI["state"], 0.5), Detail: dns.BIMI["detail"]},
		{ID: "ptr", Label: "PTR", State: ptr["state"], Max: 1, Points: scorePoints(ptr["state"], 1), Detail: ptr["detail"]},
		{ID: "rbl", Label: "IP", State: rbl["state"], Max: 2, Points: scorePoints(rbl["state"], 2), Detail: rbl["detail"]},
		{ID: "spam", Label: "Spam", State: spamState, Max: 1, Points: scorePoints(spamState, 1), Detail: spamDetail},
	}
	var sum, max float64
	for _, it := range items {
		sum += it.Points
		max += it.Max
	}
	// Present as x/10 even if weights change slightly.
	score := 0.0
	if max > 0 {
		score = math.Round((sum/max)*100) / 10 // one decimal on 0–10
	}
	verdict := "critical"
	switch {
	case score >= 9.5:
		verdict = "perfect"
	case score >= 8:
		verdict = "good"
	case score >= 5:
		verdict = "attention"
	}
	return deliverabilityRating{Score: score, Max: 10, Verdict: verdict, Items: items}
}