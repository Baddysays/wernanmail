package mail

import (
	"bytes"
	"crypto/rand"
	"crypto/tls"
	"encoding/hex"
	"fmt"
	"io"
	"mime"
	"mime/quotedprintable"
	"net"
	"net/smtp"
	"strings"
	"time"

	"github.com/Baddysays/wernanmail/server/internal/session"
	"github.com/Baddysays/wernanmail/server/internal/mailtmpl"
)

// SendMessage sends mail via SMTP and best-effort saves a copy to Sent.
func SendMessage(creds session.Credentials, req SendRequest) error {
	return SendMessageWithPolicy(creds, req, mailtmpl.Policy{})
}

// SendMessageWithPolicy applies body templates/footers before MIME build so Sent matches outbound.
func SendMessageWithPolicy(creds session.Credentials, req SendRequest, policy mailtmpl.Policy) error {
	if len(req.To) == 0 {
		return fmt.Errorf("missing recipients")
	}
	from := creds.Username
	recipients := append([]string{}, req.To...)
	recipients = append(recipients, req.CC...)
	recipients = append(recipients, req.BCC...)

	applied := false
	if !policy.Empty() {
		req.Text, req.HTML = policy.TransformBodies(from, req.Subject, req.Text, req.HTML)
		applied = true
	}

	msg := buildMIME(from, req, creds.SMTPHost, applied)

	addr := fmt.Sprintf("%s:%d", creds.SMTPHost, creds.SMTPPort)
	auth := smtp.PlainAuth("", creds.Username, creds.Password, creds.SMTPHost)

	var err error
	if creds.TLS && creds.SMTPPort == 465 {
		err = sendSMTPS(addr, auth, from, recipients, msg, creds.SMTPHost)
	} else {
		err = sendSMTPStartTLS(addr, auth, from, recipients, msg, creds.SMTPHost, creds.TLS)
		// Common self-hosted layout: submission :587 without STARTTLS, SMTPS on :465 (stunnel).
		if err != nil && creds.TLS && creds.SMTPPort == 587 {
			if err2 := sendSMTPS(fmt.Sprintf("%s:465", creds.SMTPHost), auth, from, recipients, msg, creds.SMTPHost); err2 == nil {
				err = nil
			}
		}
	}
	if err != nil {
		return err
	}

	// Filing to Sent must not undo a successful SMTP delivery.
	_ = AppendToSent(creds, msg)
	return nil
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
		return fmt.Errorf("smtp starttls unavailable on %s (use port 465 SMTPS)", addr)
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

func buildMIME(from string, req SendRequest, smtpHost string, policyApplied bool) []byte {
	var b strings.Builder
	b.WriteString("From: " + from + "\r\n")
	b.WriteString("To: " + strings.Join(req.To, ", ") + "\r\n")
	if len(req.CC) > 0 {
		b.WriteString("Cc: " + strings.Join(req.CC, ", ") + "\r\n")
	}
	b.WriteString("Subject: " + encodeHeader(req.Subject) + "\r\n")
	b.WriteString("Date: " + time.Now().Format(time.RFC1123Z) + "\r\n")
	b.WriteString("Message-ID: <" + newMessageID(smtpHost) + ">\r\n")
	b.WriteString("MIME-Version: 1.0\r\n")
	if policyApplied {
		b.WriteString("X-Wernanmail-Outbound: 1\r\n")
	}

	if req.HTML != "" {
		b.WriteString("Content-Type: multipart/alternative; boundary=\"wernan-boundary\"\r\n\r\n")
		b.WriteString("--wernan-boundary\r\n")
		writeTextPart(&b, "text/plain", req.Text)
		b.WriteString("--wernan-boundary\r\n")
		writeTextPart(&b, "text/html", req.HTML)
		b.WriteString("--wernan-boundary--\r\n")
	} else {
		writeTextPart(&b, "text/plain", req.Text)
	}
	return []byte(b.String())
}

func writeTextPart(b *strings.Builder, contentType, body string) {
	b.WriteString("Content-Type: " + contentType + "; charset=UTF-8\r\n")
	b.WriteString("Content-Transfer-Encoding: quoted-printable\r\n\r\n")
	b.WriteString(encodeQP(body))
	if !strings.HasSuffix(body, "\n") {
		b.WriteString("\r\n")
	}
}

func encodeHeader(s string) string {
	s = strings.ReplaceAll(s, "\r", "")
	s = strings.ReplaceAll(s, "\n", "")
	if s == "" {
		return s
	}
	needs := false
	for _, r := range s {
		if r > 127 || r < 32 {
			needs = true
			break
		}
	}
	if !needs {
		return s
	}
	return mime.QEncoding.Encode("utf-8", s)
}

func encodeQP(s string) string {
	var buf bytes.Buffer
	w := quotedprintable.NewWriter(&buf)
	_, _ = io.WriteString(w, s)
	_ = w.Close()
	// quotedprintable uses \r\n; ensure trailing newline for MIME part body
	out := buf.String()
	if out != "" && !strings.HasSuffix(out, "\n") {
		out += "\r\n"
	}
	return out
}

func newMessageID(host string) string {
	host = strings.TrimSpace(host)
	if host == "" {
		host = "localhost"
	}
	var b [12]byte
	_, _ = rand.Read(b[:])
	return fmt.Sprintf("%s.%d@%s", hex.EncodeToString(b[:]), time.Now().UnixNano(), host)
}
