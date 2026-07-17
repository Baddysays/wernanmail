package adminapi

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/Baddysays/wernanmail/server/internal/antispam"
	"github.com/Baddysays/wernanmail/server/internal/domain"
	"github.com/Baddysays/wernanmail/server/internal/settings"
)

var expectedMailProcs = []string{"mta", "imapd", "worker", "admin", "api"}

// readyz is a public liveness/readiness probe for monitoring (no auth).
// Public clients get a slim {status} payload. Loopback / SCRAPE_ALLOW get details.
func (h *Handler) readyz(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	pending, dead, qErr := h.queueCounts(ctx)
	running, missing, stackMode := h.stackProcs()
	status := "ok"
	code := http.StatusOK
	if qErr != nil || dead > 0 {
		status = "degraded"
		code = http.StatusServiceUnavailable
	}
	if stackMode == "proc" && len(missing) > 0 {
		status = "degraded"
		code = http.StatusServiceUnavailable
	}
	out := map[string]any{"status": status}
	if scrapeAllowed(r) {
		out["queuePending"] = pending
		out["queueDead"] = dead
		out["procsRunning"] = len(running)
		out["procsExpected"] = len(expectedMailProcs)
		out["missing"] = missing
		out["stackMode"] = stackMode
		if qErr != nil {
			out["queueError"] = qErr.Error()
		}
	}
	writeJSON(w, code, out)
}

// posture returns outbound IP cleanliness, antispam self-test, and stack/queue health.
func (h *Handler) posture(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 8*time.Second)
	defer cancel()
	_ = h.Settings.Reload(ctx)

	pending, dead, qErr := h.queueCounts(ctx)
	running, missing, stackMode := h.stackProcs()
	stackState := "ok"
	switch {
	case stackMode == "skip":
		stackState = "ok" // container / explicit skip — process list not meaningful
	case len(missing) > 0:
		stackState = "missing"
	case len(running) == 0:
		stackState = "warn"
	}
	queueState := "ok"
	if qErr != nil {
		queueState = "bad"
	} else if dead > 0 {
		queueState = "bad"
	} else if pending >= 50 {
		queueState = "warn"
	}

	ip, ipSource := h.resolveOutboundIP(ctx)
	ehlo := strings.TrimSpace(h.Cfg.EHLOHost)
	if ehlo == "" {
		ehlo = strings.TrimSpace(h.Cfg.Hostname)
	}

	ptr := checkResult("warn", "no outbound IP")
	rbl := checkResult("warn", "no outbound IP")
	if ip != "" {
		ptr = h.checkPTR(ctx, ip, ehlo)
		rbl = h.checkOutboundRBL(ip)
	}

	spam := h.antispamPosture(ctx)
	dns := h.collectDNSStatus(ctx, r.URL.Query().Get("domain"))
	rating := buildDeliverabilityRating(dns, ptr, rbl, spam)

	overall := "ok"
	for _, st := range []string{ptr["state"], rbl["state"], spam["state"].(string), stackState, queueState, dns.MX["state"], dns.SPF["state"], dns.DKIM["state"], dns.DMARC["state"]} {
		if st == "bad" || st == "missing" {
			overall = "bad"
			break
		}
		if st == "warn" && overall == "ok" {
			overall = "warn"
		}
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"status":    overall,
		"checkedAt": time.Now().UTC(),
		"ip":        ip,
		"ipSource":  ipSource,
		"ehlo":      ehlo,
		"ptr":       ptr,
		"rbl":       rbl,
		"antispam":  spam,
		"dns": map[string]any{
			"domain":   dns.Domain,
			"mailHost": dns.MailHost,
			"mx":       dns.MX,
			"spf":      dns.SPF,
			"dkim":     dns.DKIM,
			"dmarc":    dns.DMARC,
		},
		"rating": rating,
		"stack": map[string]any{
			"state":    stackState,
			"mode":     stackMode,
			"expected": expectedMailProcs,
			"running":  running,
			"missing":  missing,
		},
		"queue": map[string]any{
			"state":   queueState,
			"pending": pending,
			"dead":    dead,
			"error": func() string {
				if qErr != nil {
					return qErr.Error()
				}
				return ""
			}(),
		},
	})
}

func (h *Handler) queueCounts(ctx context.Context) (pending, dead int, err error) {
	if h.Queue == nil {
		return 0, 0, fmt.Errorf("queue store unavailable")
	}
	p, d, err := h.Queue.Count(ctx)
	if err != nil {
		return 0, 0, err
	}
	return p, d, nil
}

// stackProcs returns running/missing daemon names and the check mode:
//   - "proc": host /proc scan (native install)
//   - "skip": containers / WERNANMAIL_STACK_CHECK=skip (cross-process /proc is meaningless)
func (h *Handler) stackProcs() (running, missing []string, mode string) {
	mode = stackCheckMode()
	if mode == "skip" {
		return []string{}, []string{}, mode
	}
	found := map[string]bool{}
	for _, p := range findMailPIDs() {
		found[p.name] = true
	}
	for _, name := range expectedMailProcs {
		if found[name] {
			running = append(running, name)
		} else {
			missing = append(missing, name)
		}
	}
	if running == nil {
		running = []string{}
	}
	if missing == nil {
		missing = []string{}
	}
	return running, missing, mode
}

func stackCheckMode() string {
	switch strings.ToLower(strings.TrimSpace(os.Getenv("WERNANMAIL_STACK_CHECK"))) {
	case "skip", "none", "off", "0", "false":
		return "skip"
	case "proc", "host":
		return "proc"
	}
	if _, err := os.Stat("/.dockerenv"); err == nil {
		return "skip"
	}
	// Common container markers when /.dockerenv is absent.
	if _, err := os.Stat("/run/.containerenv"); err == nil {
		return "skip"
	}
	return "proc"
}

func (h *Handler) resolveOutboundIP(ctx context.Context) (ip, source string) {
	if v := strings.TrimSpace(h.Cfg.PublicIP); v != "" {
		if net.ParseIP(v) != nil {
			return v, "MAIL_PUBLIC_IP"
		}
	}
	hosts := []string{h.Cfg.Hostname, h.Cfg.EHLOHost}
	res := publicDNSResolver()
	for _, host := range hosts {
		host = strings.TrimSpace(strings.TrimSuffix(host, "."))
		if host == "" || strings.EqualFold(host, "localhost") {
			continue
		}
		ips, err := res.LookupIP(ctx, "ip4", host)
		if err != nil || len(ips) == 0 {
			continue
		}
		for _, cand := range ips {
			if v4 := cand.To4(); v4 != nil && !v4.IsLoopback() && !v4.IsPrivate() {
				return v4.String(), "dns:" + host
			}
		}
		// Fall back to first IPv4 even if private (dev).
		for _, cand := range ips {
			if v4 := cand.To4(); v4 != nil {
				return v4.String(), "dns:" + host
			}
		}
	}
	if local := detectLocalOutboundIP(); local != "" {
		return local, "local-route"
	}
	return "", ""
}

func detectLocalOutboundIP() string {
	conn, err := net.Dial("udp", "8.8.8.8:80")
	if err != nil {
		return ""
	}
	defer conn.Close()
	addr, ok := conn.LocalAddr().(*net.UDPAddr)
	if !ok || addr.IP == nil {
		return ""
	}
	if v4 := addr.IP.To4(); v4 != nil {
		return v4.String()
	}
	return ""
}

func (h *Handler) checkPTR(ctx context.Context, ip, ehlo string) map[string]string {
	res := publicDNSResolver()
	names, err := res.LookupAddr(ctx, ip)
	if err != nil || len(names) == 0 {
		return checkResult("missing", "no PTR for "+ip)
	}
	want := strings.TrimSuffix(strings.ToLower(ehlo), ".")
	got := make([]string, 0, len(names))
	match := false
	for _, n := range names {
		n = strings.TrimSuffix(strings.ToLower(n), ".")
		got = append(got, n)
		if want != "" && (n == want || strings.HasSuffix(n, "."+want)) {
			match = true
		}
	}
	detail := strings.Join(got, ", ")
	if want == "" {
		return checkResult("warn", detail)
	}
	if match {
		return checkResult("ok", detail)
	}
	return checkResult("warn", fmt.Sprintf("%s (want %s)", detail, want))
}

func (h *Handler) checkOutboundRBL(ip string) map[string]string {
	zones := splitSettingCSV(h.Settings.Get(settings.KeySpamRBLs))
	if len(zones) == 0 {
		return checkResult("warn", "no RBL zones configured")
	}
	var listed []string
	var inconclusive []string
	checked := 0
	for _, zone := range zones {
		res := antispam.QueryDNSBL(ip, zone)
		checked++
		if res.Listed {
			listed = append(listed, zone)
		} else if res.Inconclusive {
			inconclusive = append(inconclusive, zone+": "+res.Detail)
		}
	}
	if len(listed) > 0 {
		return checkResult("bad", "listed on "+strings.Join(listed, ", "))
	}
	if len(inconclusive) > 0 && len(inconclusive) == checked {
		return checkResult("warn", strings.Join(inconclusive, "; "))
	}
	if len(inconclusive) > 0 {
		return checkResult("warn", "clean on most zones; "+strings.Join(inconclusive, "; "))
	}
	return checkResult("ok", "not listed on "+strings.Join(zones, ", "))
}

func (h *Handler) antispamPosture(ctx context.Context) map[string]any {
	rejectAt := parseFloatSetting(h.Settings.Get(settings.KeySpamRejectAt), 10)
	quarantineAt := parseFloatSetting(h.Settings.Get(settings.KeySpamQuarantineAt), 5)
	flagAt := parseFloatSetting(h.Settings.Get(settings.KeySpamFlagAt), 3)
	rbls := splitSettingCSV(h.Settings.Get(settings.KeySpamRBLs))
	greylist := h.Settings.GetInt(settings.KeyGreylistSeconds, 0)

	// Probe without network RBLs so the self-test stays offline-stable.
	eng := antispam.New(nil, rejectAt, quarantineAt, nil)
	eng.SetConfig(antispam.Config{
		RejectAt:     rejectAt,
		QuarantineAt: quarantineAt,
		FlagAt:       flagAt,
		RBLs:         nil,
	})

	clean := eng.Check(ctx, antispam.Input{
		From:       "alice@example.com",
		Helo:       "mail.example.com",
		RemoteIP:   "203.0.113.10",
		Recipients: []string{"bob@example.com"},
		Headers: map[string]string{
			"Subject":    "Project update",
			"Message-ID": "<clean@example.com>",
			"Date":       time.Now().UTC().Format(time.RFC1123Z),
		},
		Raw: []byte("From: alice@example.com\r\nSubject: Project update\r\n\r\nHello, see you tomorrow.\r\n"),
	})
	spammy := eng.Check(ctx, antispam.Input{
		From:       "",
		Helo:       "1.2.3.4",
		RemoteIP:   "203.0.113.50",
		Recipients: []string{"bob@example.com"},
		Headers: map[string]string{
			"Subject":  "URGENT!!!! verify your account!!!!",
			"Reply-To": "evil@phish.example",
		},
		Raw: []byte("http://1.2.3.4/login http://bit.ly/x https://tinyurl.com/y verify password\r\n"),
	})

	probeOK := clean.Action == domain.SpamDeliver &&
		(spammy.Action == domain.SpamQuarantine || spammy.Action == domain.SpamReject || spammy.Action == domain.SpamFlag)

	state := "ok"
	detail := "engine separates clean vs spammy samples"
	if !probeOK {
		state = "warn"
		detail = "probe unexpected — check thresholds"
	}
	if len(rbls) == 0 {
		if state == "ok" {
			state = "warn"
		}
		detail = "no inbound RBL zones configured"
	}

	return map[string]any{
		"state":           state,
		"detail":          detail,
		"rbls":            rbls,
		"flagAt":          flagAt,
		"quarantineAt":    quarantineAt,
		"rejectAt":        rejectAt,
		"greylistSeconds": greylist,
		"probe": map[string]any{
			"ok": probeOK,
			"clean": map[string]any{
				"score":  clean.Score,
				"action": string(clean.Action),
			},
			"spammy": map[string]any{
				"score":  spammy.Score,
				"action": string(spammy.Action),
			},
		},
	}
}

func splitSettingCSV(s string) []string {
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

func parseFloatSetting(s string, fallback float64) float64 {
	s = strings.TrimSpace(s)
	if s == "" {
		return fallback
	}
	v, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return fallback
	}
	return v
}
