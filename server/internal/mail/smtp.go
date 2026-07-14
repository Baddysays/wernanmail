package mail

import (
	"crypto/tls"
	"fmt"
	"net"
	"net/smtp"
	"strings"

	"github.com/Baddysays/wernanmail/server/internal/session"
)

// SendMessage sends mail via SMTP using session credentials.
func SendMessage(creds session.Credentials, req SendRequest) error {
	if len(req.To) == 0 {
		return fmt.Errorf("missing recipients")
	}
	from := creds.Username
	recipients := append([]string{}, req.To...)
	recipients = append(recipients, req.CC...)
	recipients = append(recipients, req.BCC...)

	msg := buildMIME(from, req)

	addr := fmt.Sprintf("%s:%d", creds.SMTPHost, creds.SMTPPort)
	auth := smtp.PlainAuth("", creds.Username, creds.Password, creds.SMTPHost)

	// Port 465 = implicit TLS; 587/25 = plain then STARTTLS when available.
	if creds.TLS && creds.SMTPPort == 465 {
		return sendSMTPS(addr, auth, from, recipients, msg, creds.SMTPHost)
	}
	return sendSMTPStartTLS(addr, auth, from, recipients, msg, creds.SMTPHost, creds.TLS)
}

func sendSMTPS(addr string, auth smtp.Auth, from string, to []string, msg []byte, host string) error {
	tlsCfg := &tls.Config{ServerName: host}
	conn, err := tls.Dial("tcp", addr, tlsCfg)
	if err != nil {
		return err
	}
	defer conn.Close()

	c, err := smtp.NewClient(conn, host)
	if err != nil {
		return err
	}
	defer func() { _ = c.Close() }()

	if err := c.Auth(auth); err != nil {
		return err
	}
	return writeMail(c, from, to, msg)
}

func sendSMTPStartTLS(addr string, auth smtp.Auth, from string, to []string, msg []byte, host string, preferTLS bool) error {
	conn, err := net.Dial("tcp", addr)
	if err != nil {
		return err
	}
	c, err := smtp.NewClient(conn, host)
	if err != nil {
		_ = conn.Close()
		return err
	}
	defer func() { _ = c.Close() }()

	if ok, _ := c.Extension("STARTTLS"); ok {
		tlsCfg := &tls.Config{ServerName: host}
		if err := c.StartTLS(tlsCfg); err != nil {
			return err
		}
	} else if preferTLS {
		return fmt.Errorf("smtp starttls unavailable")
	}

	if err := c.Auth(auth); err != nil {
		return err
	}
	return writeMail(c, from, to, msg)
}

func writeMail(c *smtp.Client, from string, to []string, msg []byte) error {
	if err := c.Mail(from); err != nil {
		return err
	}
	for _, rcpt := range to {
		rcpt = strings.TrimSpace(rcpt)
		if rcpt == "" {
			continue
		}
		if err := c.Rcpt(rcpt); err != nil {
			return err
		}
	}
	w, err := c.Data()
	if err != nil {
		return err
	}
	if _, err := w.Write(msg); err != nil {
		return err
	}
	if err := w.Close(); err != nil {
		return err
	}
	return c.Quit()
}

func buildMIME(from string, req SendRequest) []byte {
	var b strings.Builder
	b.WriteString("From: " + from + "\r\n")
	b.WriteString("To: " + strings.Join(req.To, ", ") + "\r\n")
	if len(req.CC) > 0 {
		b.WriteString("Cc: " + strings.Join(req.CC, ", ") + "\r\n")
	}
	b.WriteString("Subject: " + sanitizeHeader(req.Subject) + "\r\n")
	b.WriteString("MIME-Version: 1.0\r\n")

	if req.HTML != "" {
		b.WriteString("Content-Type: multipart/alternative; boundary=\"wernan-boundary\"\r\n\r\n")
		b.WriteString("--wernan-boundary\r\n")
		b.WriteString("Content-Type: text/plain; charset=UTF-8\r\n\r\n")
		b.WriteString(req.Text)
		b.WriteString("\r\n--wernan-boundary\r\n")
		b.WriteString("Content-Type: text/html; charset=UTF-8\r\n\r\n")
		b.WriteString(req.HTML)
		b.WriteString("\r\n--wernan-boundary--\r\n")
	} else {
		b.WriteString("Content-Type: text/plain; charset=UTF-8\r\n\r\n")
		b.WriteString(req.Text)
		b.WriteString("\r\n")
	}
	return []byte(b.String())
}

func sanitizeHeader(s string) string {
	s = strings.ReplaceAll(s, "\r", "")
	s = strings.ReplaceAll(s, "\n", "")
	return s
}
