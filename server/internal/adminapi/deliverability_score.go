package adminapi

import (
	"context"
	"math"
	"strings"

	"github.com/Baddysays/wernanmail/server/internal/domain"
)

type dnsSnapshot struct {
	Domain   string
	MailHost string
	MX       map[string]string
	SPF      map[string]string
	DKIM     map[string]string
	DMARC    map[string]string
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
		MX:    checkResult("missing", "no domain"),
		SPF:   checkResult("missing", "no domain"),
		DKIM:  checkResult("missing", "no domain"),
		DMARC: checkResult("missing", "no domain"),
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
	return out
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

func buildDeliverabilityRating(dns dnsSnapshot, ptr, rbl map[string]string, spam map[string]any) deliverabilityRating {
	spamState, _ := spam["state"].(string)
	spamDetail, _ := spam["detail"].(string)
	items := []scoreItem{
		{ID: "mx", Label: "MX", State: dns.MX["state"], Max: 1, Points: scorePoints(dns.MX["state"], 1), Detail: dns.MX["detail"]},
		{ID: "spf", Label: "SPF", State: dns.SPF["state"], Max: 2, Points: scorePoints(dns.SPF["state"], 2), Detail: dns.SPF["detail"]},
		{ID: "dkim", Label: "DKIM", State: dns.DKIM["state"], Max: 2, Points: scorePoints(dns.DKIM["state"], 2), Detail: dns.DKIM["detail"]},
		{ID: "dmarc", Label: "DMARC", State: dns.DMARC["state"], Max: 1, Points: scorePoints(dns.DMARC["state"], 1), Detail: dns.DMARC["detail"]},
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