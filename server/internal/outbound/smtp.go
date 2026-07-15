package outbound

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"log"
	"net"
	"net/smtp"
	"sort"
	"strings"
	"time"
)

// Transporter sends mail to remote MTAs.
type Transporter interface {
	Send(ctx context.Context, from string, to []string, raw []byte) error
}

// SMTPTransporter resolves MX and delivers.
type SMTPTransporter struct {
	RelayHost  string // optional host:port smarthost
	EHLOHost   string // our hostname for EHLO/HELO (must match PTR/A where possible)
	Timeout    time.Duration
	RequireTLS bool // refuse cleartext delivery when peer has no STARTTLS
}

func (t *SMTPTransporter) Send(ctx context.Context, from string, to []string, raw []byte) error {
	if t.Timeout == 0 {
		t.Timeout = 60 * time.Second
	}
	ehlo := strings.TrimSpace(t.EHLOHost)
	if ehlo == "" {
		ehlo = "localhost"
	}
	if t.RelayHost != "" {
		return sendToHost(ctx, t.RelayHost, ehlo, from, to, raw, t.Timeout, t.RequireTLS)
	}
	byDomain := map[string][]string{}
	for _, addr := range to {
		_, dom, ok := split(addr)
		if !ok {
			return fmt.Errorf("bad recipient %s", addr)
		}
		byDomain[dom] = append(byDomain[dom], addr)
	}
	for dom, recips := range byDomain {
		hosts, err := mxHosts(dom)
		if err != nil {
			return &DeliveryError{Code: 450, Message: fmt.Sprintf("mx lookup %s: %v", dom, err)}
		}
		if len(hosts) == 0 {
			return &DeliveryError{Code: 550, Message: fmt.Sprintf("no mx for %s", dom)}
		}
		var last error
		for _, h := range hosts {
			last = sendToHost(ctx, net.JoinHostPort(h, "25"), ehlo, from, recips, raw, t.Timeout, t.RequireTLS)
			if last == nil {
				break
			}
			// Don't try next MX on permanent recipient failures.
			if de := AsDeliveryError(last); de != nil && de.Permanent() {
				return last
			}
		}
		if last != nil {
			return last
		}
	}
	return nil
}

func split(addr string) (local, domain string, ok bool) {
	addr = strings.Trim(addr, "<>")
	i := strings.LastIndex(addr, "@")
	if i <= 0 {
		return "", "", false
	}
	return addr[:i], addr[i+1:], true
}

func mxHosts(domain string) ([]string, error) {
	mxs, err := net.LookupMX(domain)
	if err != nil {
		var dnsErr *net.DNSError
		if errors.As(err, &dnsErr) {
			if dnsErr.IsNotFound {
				// RFC 5321: no MX → try A/AAAA of the domain itself.
				return []string{domain}, nil
			}
			// Temporary DNS failure — retry later.
			return nil, err
		}
		return nil, err
	}
	sort.Slice(mxs, func(i, j int) bool { return mxs[i].Pref < mxs[j].Pref })
	out := make([]string, 0, len(mxs))
	for _, mx := range mxs {
		host := strings.TrimSuffix(mx.Host, ".")
		if host != "" {
			out = append(out, host)
		}
	}
	return out, nil
}

func sendToHost(ctx context.Context, addr, ehlo, from string, to []string, raw []byte, timeout time.Duration, requireTLS bool) error {
	host, _, _ := net.SplitHostPort(addr)
	err := dialAndSend(ctx, addr, host, ehlo, from, to, raw, timeout, false, requireTLS)
	if err == nil {
		log.Printf("outbound ok host=%s ehlo=%s to=%s tls=verify", host, ehlo, strings.Join(to, ","))
		return nil
	}
	// Opportunistic insecure retry only when TLS is not required.
	if !requireTLS && IsTLSError(err) {
		err2 := dialAndSend(ctx, addr, host, ehlo, from, to, raw, timeout, true, requireTLS)
		if err2 == nil {
			log.Printf("outbound ok host=%s ehlo=%s to=%s tls=insecure", host, ehlo, strings.Join(to, ","))
			return nil
		}
		return err2
	}
	return err
}

func dialAndSend(ctx context.Context, addr, host, ehlo, from string, to []string, raw []byte, timeout time.Duration, insecureTLS, requireTLS bool) error {
	d := net.Dialer{Timeout: timeout}
	conn, err := d.DialContext(ctx, "tcp", addr)
	if err != nil {
		return &DeliveryError{Code: 450, Host: host, Message: err.Error()}
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(timeout))

	c, err := smtp.NewClient(conn, host)
	if err != nil {
		return &DeliveryError{Code: 450, Host: host, Message: err.Error()}
	}
	defer c.Close()

	if err := c.Hello(ehlo); err != nil {
		return wrapSMTP(host, err, false)
	}
	okTLS, _ := c.Extension("STARTTLS")
	if requireTLS && !okTLS {
		return &DeliveryError{Code: 554, Host: host, Message: "TLS required but peer has no STARTTLS", TLS: true}
	}
	if okTLS {
		cfg := &tls.Config{ServerName: host, MinVersion: tls.VersionTLS12, InsecureSkipVerify: insecureTLS}
		if err := c.StartTLS(cfg); err != nil {
			return &DeliveryError{Code: 0, Host: host, Message: err.Error(), TLS: true}
		}
	}
	if err := c.Mail(from); err != nil {
		return wrapSMTP(host, err, false)
	}
	var accepted, rejected []string
	var lastRcpt error
	for _, rcpt := range to {
		if err := c.Rcpt(rcpt); err != nil {
			rejected = append(rejected, rcpt)
			lastRcpt = err
			continue
		}
		accepted = append(accepted, rcpt)
	}
	if len(accepted) == 0 {
		return wrapSMTP(host, lastRcpt, false)
	}
	if len(rejected) > 0 {
		// Partial: deliver to accepted, surface permanent fail for rejected via error after send.
		// We still send DATA for accepted recipients on this connection.
	}
	w, err := c.Data()
	if err != nil {
		return wrapSMTP(host, err, false)
	}
	if _, err := w.Write(raw); err != nil {
		return &DeliveryError{Code: 450, Host: host, Message: err.Error(), Partial: accepted}
	}
	if err := w.Close(); err != nil {
		return wrapSMTP(host, err, false)
	}
	_ = c.Quit()
	if len(rejected) > 0 {
		de := wrapSMTP(host, lastRcpt, false)
		de.Partial = accepted
		de.Message = fmt.Sprintf("%s (rejected: %s)", de.Message, strings.Join(rejected, ","))
		return de
	}
	return nil
}

func wrapSMTP(host string, err error, tlsFail bool) *DeliveryError {
	if err == nil {
		return &DeliveryError{Host: host, Message: "unknown smtp error", TLS: tlsFail}
	}
	msg := err.Error()
	code := 0
	if len(msg) >= 3 && msg[0] >= '2' && msg[0] <= '5' && msg[1] >= '0' && msg[1] <= '9' && msg[2] >= '0' && msg[2] <= '9' {
		code = int(msg[0]-'0')*100 + int(msg[1]-'0')*10 + int(msg[2]-'0')
	}
	return &DeliveryError{Code: code, Host: host, Message: msg, TLS: tlsFail}
}
