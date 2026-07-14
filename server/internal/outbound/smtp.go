package outbound

import (
	"context"
	"crypto/tls"
	"fmt"
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
	RelayHost string // optional host:port smarthost
	Timeout   time.Duration
}

func (t *SMTPTransporter) Send(ctx context.Context, from string, to []string, raw []byte) error {
	if t.Timeout == 0 {
		t.Timeout = 60 * time.Second
	}
	if t.RelayHost != "" {
		return sendToHost(ctx, t.RelayHost, from, to, raw, t.Timeout)
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
		if err != nil || len(hosts) == 0 {
			return fmt.Errorf("mx lookup %s: %w", dom, err)
		}
		var last error
		for _, h := range hosts {
			last = sendToHost(ctx, net.JoinHostPort(h, "25"), from, recips, raw, t.Timeout)
			if last == nil {
				break
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
		// fallback A record
		return []string{domain}, nil
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

func sendToHost(ctx context.Context, addr, from string, to []string, raw []byte, timeout time.Duration) error {
	d := net.Dialer{Timeout: timeout}
	conn, err := d.DialContext(ctx, "tcp", addr)
	if err != nil {
		return err
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(timeout))

	host, _, _ := net.SplitHostPort(addr)
	c, err := smtp.NewClient(conn, host)
	if err != nil {
		return err
	}
	defer c.Close()

	if ok, _ := c.Extension("STARTTLS"); ok {
		_ = c.StartTLS(&tls.Config{ServerName: host, MinVersion: tls.VersionTLS12})
	}
	if err := c.Mail(from); err != nil {
		return err
	}
	for _, rcpt := range to {
		if err := c.Rcpt(rcpt); err != nil {
			return err
		}
	}
	w, err := c.Data()
	if err != nil {
		return err
	}
	if _, err := w.Write(raw); err != nil {
		return err
	}
	if err := w.Close(); err != nil {
		return err
	}
	return c.Quit()
}
