package antispam

import (
	"context"
	"fmt"
	"net"
	"net/mail"
	"net/url"
	"regexp"
	"strings"
	"sync"
	"unicode"

	"github.com/Baddysays/wernanmail/server/internal/dnsauth"
	"github.com/Baddysays/wernanmail/server/internal/domain"
)

// Engine scores inbound mail.
type Engine struct {
	DNS *dnsauth.Checker
	// Signals supplies small, persistent weights learned from quarantine actions.
	// It is normally assigned once during process startup.
	Signals SignalStore

	mu            sync.RWMutex
	RejectAt      float64
	QuarantineAt  float64
	FlagAt        float64
	RBLs          []string
	RejectMessage string
}

// SignalStore looks up only the signal keys present in one message.
type SignalStore interface {
	LookupSpamSignals(ctx context.Context, keys []string) (map[string]float64, error)
}

// Config is a snapshot of tunable thresholds.
type Config struct {
	RejectAt      float64
	QuarantineAt  float64
	FlagAt        float64
	RBLs          []string
	RejectMessage string
}

// New constructs an Engine with thresholds applied once.
func New(dns *dnsauth.Checker, rejectAt, quarantineAt float64, rbls []string) *Engine {
	e := &Engine{DNS: dns}
	e.SetConfig(Config{
		RejectAt:     rejectAt,
		QuarantineAt: quarantineAt,
		FlagAt:       3,
		RBLs:         rbls,
	})
	return e
}

// SetConfig updates thresholds (safe for concurrent Check).
func (e *Engine) SetConfig(cfg Config) {
	if cfg.RejectAt <= 0 {
		cfg.RejectAt = 10
	}
	if cfg.QuarantineAt <= 0 {
		cfg.QuarantineAt = 5
	}
	if cfg.FlagAt <= 0 {
		cfg.FlagAt = 3
	}
	if cfg.RejectMessage == "" {
		cfg.RejectMessage = "Message rejected as spam"
	}
	rbls := append([]string(nil), cfg.RBLs...)
	e.mu.Lock()
	e.RejectAt = cfg.RejectAt
	e.QuarantineAt = cfg.QuarantineAt
	e.FlagAt = cfg.FlagAt
	e.RBLs = rbls
	e.RejectMessage = cfg.RejectMessage
	e.mu.Unlock()
}

func (e *Engine) config() Config {
	e.mu.RLock()
	defer e.mu.RUnlock()
	return Config{
		RejectAt:      e.RejectAt,
		QuarantineAt:  e.QuarantineAt,
		FlagAt:        e.FlagAt,
		RBLs:          append([]string(nil), e.RBLs...),
		RejectMessage: e.RejectMessage,
	}
}

// RejectText returns the SMTP reject message.
func (e *Engine) RejectText() string {
	e.mu.RLock()
	defer e.mu.RUnlock()
	if e.RejectMessage == "" {
		return "Message rejected as spam"
	}
	return e.RejectMessage
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
	cfg := e.config()
	var v domain.SpamVerdict
	add := func(code, detail string, score float64) {
		v.Score += score
		v.Reasons = append(v.Reasons, domain.SpamReason{Code: code, Detail: detail, Score: score})
	}

	if in.From == "" {
		add("empty_from", "missing MAIL FROM", 2)
	}

	subj := strings.ToLower(headerOr(in.Headers, "Subject"))
	if strings.Count(subj, "!") > 3 {
		add("subj_bang", "excessive exclamation marks", 1.5)
	}
	if looksLikePhishSubject(subj) {
		add("subj_phish", "suspicious subject pattern", 2)
	}

	if headerOr(in.Headers, "Message-Id") == "" && headerOr(in.Headers, "Message-ID") == "" {
		add("no_msgid", "missing Message-ID", 1)
	}
	if headerOr(in.Headers, "Date") == "" {
		add("no_date", "missing Date header", 0.5)
	}

	if mismatch := replyToMismatch(in.From, headerOr(in.Headers, "Reply-To")); mismatch != "" {
		add("reply_to_mismatch", mismatch, 1.5)
	}

	if in.Helo != "" && looksLikeIPHelo(in.Helo) {
		add("helo_ip", "HELO is an IP literal", 1)
	}

	if len(in.Raw) > 0 {
		// Count URLs only in the textual prefix (headers + early body) to avoid
		// huge base64 blobs inflating scores; still catches HTML link farms.
		sample := in.Raw
		if len(sample) > 64_000 {
			sample = sample[:64_000]
		}
		body := strings.ToLower(string(sample))
		urlN := strings.Count(body, "http://") + strings.Count(body, "https://")
		if urlN > 20 {
			add("url_density", "many URLs in body", 2)
		} else if urlN > 12 {
			add("url_density", "elevated URL count", 1)
		}
		if strings.Contains(body, "content-transfer-encoding: base64") && len(in.Raw) > 400_000 {
			add("base64_bomb", "large base64 payload", 2)
		}
		uri := inspectURIs(body)
		if uri.IPLiteral {
			add("uri_ip", "URL uses an IP address instead of a domain", 2)
		}
		if uri.Shortener {
			add("uri_shortener", "URL uses a link-shortening service", 1)
		}
		if uri.UserInfo {
			add("uri_userinfo", "URL contains misleading user information", 2)
		}
		if uri.CredentialLure {
			add("credential_lure", "credential request combined with an external link", 2)
		}
	}

	spfRes, dkimRes := "none", "none"
	if e.DNS != nil && in.From != "" {
		spfRes = e.DNS.CheckSPF(ctx, in.From, in.RemoteIP)
		switch spfRes {
		case "fail":
			add("spf_fail", "SPF fail", 3)
		case "softfail":
			add("spf_softfail", "SPF softfail", 1.5)
		case "pass":
			add("spf_pass", "SPF pass", -1)
		}
		dkimRes = e.DNS.CheckDKIM(ctx, in.Raw)
		switch dkimRes {
		case "pass":
			add("dkim_pass", "DKIM pass", -1)
		case "fail":
			add("dkim_fail", "DKIM fail", 2)
		}
	}

	// Soft DMARC alignment: both auth paths failed → higher risk (no full DMARC parser).
	if spfRes == "fail" && (dkimRes == "fail" || dkimRes == "none") {
		add("auth_fail", "SPF fail and no valid DKIM", 2)
	}

	if in.RemoteIP != "" && len(cfg.RBLs) > 0 {
		if hit := checkRBL(in.RemoteIP, cfg.RBLs); hit != "" {
			add("rbl", hit, 4)
		}
	}

	if e.Signals != nil {
		keys := SignalKeys(in.From, subj)
		if weights, err := e.Signals.LookupSpamSignals(ctx, keys); err == nil {
			var learned float64
			for _, key := range keys {
				learned += weights[key]
			}
			if learned > 4 {
				learned = 4
			} else if learned < -4 {
				learned = -4
			}
			if learned != 0 {
				add("learned_signals", "quarantine feedback", learned)
			}
		}
	}

	switch {
	case v.Score >= cfg.RejectAt:
		v.Action = domain.SpamReject
	case v.Score >= cfg.QuarantineAt:
		v.Action = domain.SpamQuarantine
	case v.Score >= cfg.FlagAt:
		v.Action = domain.SpamFlag
	default:
		v.Action = domain.SpamDeliver
	}
	return v
}

// SignalKeys returns a bounded set of stable keys for lightweight learning.
func SignalKeys(from, subject string) []string {
	keys := make([]string, 0, 7)
	if addr, err := mail.ParseAddress(strings.TrimSpace(from)); err == nil {
		from = addr.Address
	} else {
		from = extractEmail(from)
	}
	if d := strings.Trim(strings.TrimSpace(domainOf(from)), ".>"); d != "" {
		keys = append(keys, "from_domain:"+d)
	}

	seen := make(map[string]struct{}, 6)
	for _, token := range subjectTokens(subject) {
		if _, ok := seen[token]; ok {
			continue
		}
		seen[token] = struct{}{}
		keys = append(keys, "subject_token:"+token)
		if len(seen) == 6 {
			break
		}
	}
	return keys
}

func subjectTokens(subject string) []string {
	const maxTokenRunes = 32
	stop := map[string]struct{}{
		"and": {}, "are": {}, "for": {}, "from": {}, "have": {}, "the": {},
		"this": {}, "that": {}, "with": {}, "your": {},
	}
	var tokens []string
	var token []rune
	flush := func() {
		if len(token) >= 3 {
			s := string(token)
			if _, skip := stop[s]; !skip {
				tokens = append(tokens, s)
			}
		}
		token = token[:0]
	}
	for _, r := range strings.ToLower(subject) {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			if len(token) < maxTokenRunes {
				token = append(token, r)
			}
			continue
		}
		flush()
	}
	flush()
	return tokens
}

var uriPattern = regexp.MustCompile(`https?://[^\s<>"']+`)

type uriSignals struct {
	IPLiteral      bool
	Shortener      bool
	UserInfo       bool
	CredentialLure bool
}

func inspectURIs(body string) uriSignals {
	var out uriSignals
	matches := uriPattern.FindAllString(body, 32)
	shorteners := map[string]struct{}{
		"bit.ly": {}, "tinyurl.com": {}, "t.co": {}, "is.gd": {},
		"cutt.ly": {}, "rebrand.ly": {}, "tiny.cc": {},
	}
	for _, raw := range matches {
		raw = strings.TrimRight(raw, ".,);]}")
		u, err := url.Parse(raw)
		if err != nil {
			continue
		}
		host := strings.ToLower(u.Hostname())
		if net.ParseIP(host) != nil {
			out.IPLiteral = true
		}
		if _, ok := shorteners[host]; ok {
			out.Shortener = true
		}
		if u.User != nil {
			out.UserInfo = true
		}
	}
	if len(matches) > 0 {
		lures := []string{
			"verify your password", "confirm your password", "enter your password",
			"sign in immediately", "login immediately", "подтвердите пароль",
		}
		for _, lure := range lures {
			if strings.Contains(body, lure) {
				out.CredentialLure = true
				break
			}
		}
	}
	return out
}

func headerOr(h map[string]string, key string) string {
	if h == nil {
		return ""
	}
	if v := h[key]; v != "" {
		return v
	}
	// case variants from parseHeaders
	for k, v := range h {
		if strings.EqualFold(k, key) {
			return v
		}
	}
	return ""
}

func looksLikePhishSubject(subj string) bool {
	needles := []string{
		"account suspended", "verify your account", "confirm your identity",
		"password expire", "urgent action", "wire transfer", "gift card",
		"you have won", "lottery", "bitcoin", "crypto wallet",
		"документы на подпись", "срочн", "подтвердите",
	}
	for _, n := range needles {
		if strings.Contains(subj, n) {
			return true
		}
	}
	return false
}

func replyToMismatch(from, replyTo string) string {
	replyTo = strings.TrimSpace(replyTo)
	if replyTo == "" {
		return ""
	}
	fromAddr, err1 := mail.ParseAddress(from)
	replyAddr, err2 := mail.ParseAddress(replyTo)
	if err1 != nil || err2 != nil {
		// bare addresses
		fromAddr = &mail.Address{Address: extractEmail(from)}
		replyAddr = &mail.Address{Address: extractEmail(replyTo)}
	}
	fd := domainOf(fromAddr.Address)
	rd := domainOf(replyAddr.Address)
	if fd == "" || rd == "" || strings.EqualFold(fd, rd) {
		return ""
	}
	return fmt.Sprintf("Reply-To domain %s ≠ From %s", rd, fd)
}

func extractEmail(s string) string {
	s = strings.TrimSpace(s)
	if i := strings.LastIndex(s, "<"); i >= 0 {
		s = s[i+1:]
		s = strings.TrimSuffix(s, ">")
	}
	return strings.TrimSpace(s)
}

func domainOf(addr string) string {
	at := strings.LastIndex(addr, "@")
	if at < 0 {
		return ""
	}
	return strings.ToLower(addr[at+1:])
}

func looksLikeIPHelo(helo string) bool {
	helo = strings.Trim(helo, "[]")
	return net.ParseIP(helo) != nil
}

func checkRBL(ip string, zones []string) string {
	return ListedOnRBL(ip, zones)
}
