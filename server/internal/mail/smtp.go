package mail

import (
	"bytes"
	"crypto/rand"
	"crypto/tls"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"io"
	"mime"
	"mime/quotedprintable"
	"net"
	"net/smtp"
	"net/url"
	"path"
	"strings"
	"time"
	"unicode"

	"github.com/Baddysays/wernanmail/server/internal/mailtmpl"
	"github.com/Baddysays/wernanmail/server/internal/session"
)

const (
	maxAttachmentBytes      = 15 << 20 // 15 MiB per file
	maxTotalAttachmentBytes = 25 << 20 // 25 MiB total
)

// SaveDraft builds MIME and APPEND to the Drafts mailbox.
func SaveDraft(creds session.Credentials, req SendRequest) error {
	if _, err := DecodeOutboundAttachments(req.Attachments); err != nil {
		return err
	}
	from := creds.Username
	raw := buildMIME(from, req, creds.SMTPHost, false)
	return AppendDraft(creds, raw)
}

// SendMessageWithPolicy applies body templates/footers before MIME build so Sent matches outbound.
func SendMessageWithPolicy(creds session.Credentials, req SendRequest, policy mailtmpl.Policy) error {
	if len(req.To) == 0 {
		return fmt.Errorf("missing recipients")
	}
	if _, err := DecodeOutboundAttachments(req.Attachments); err != nil {
		return err
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

// DecodeOutboundAttachments validates and decodes base64 attachment payloads.
func DecodeOutboundAttachments(atts []OutboundAttachment) ([]decodedAttachment, error) {
	if len(atts) == 0 {
		return nil, nil
	}
	out := make([]decodedAttachment, 0, len(atts))
	var total int
	for i, a := range atts {
		name := strings.TrimSpace(a.Filename)
		if name == "" {
			name = fmt.Sprintf("attachment-%d", i+1)
		}
		name = path.Base(strings.ReplaceAll(name, "\\", "/"))
		if name == "." || name == ".." || name == "" {
			name = fmt.Sprintf("attachment-%d", i+1)
		}
		raw, err := base64.StdEncoding.DecodeString(strings.TrimSpace(a.Content))
		if err != nil {
			// Some clients omit padding.
			raw, err = base64.RawStdEncoding.DecodeString(strings.TrimSpace(a.Content))
			if err != nil {
				return nil, fmt.Errorf("attachment %q: invalid base64", name)
			}
		}
		if len(raw) == 0 {
			return nil, fmt.Errorf("attachment %q: empty", name)
		}
		if len(raw) > maxAttachmentBytes {
			return nil, fmt.Errorf("attachment %q: exceeds size limit", name)
		}
		total += len(raw)
		if total > maxTotalAttachmentBytes {
			return nil, fmt.Errorf("attachments exceed total size limit")
		}
		ct := strings.TrimSpace(a.ContentType)
		if ct == "" || !strings.Contains(ct, "/") {
			ct = "application/octet-stream"
		}
		ct = strings.Split(ct, ";")[0]
		out = append(out, decodedAttachment{
			Filename:    name,
			ContentType: ct,
			Data:        raw,
		})
	}
	return out, nil
}

type decodedAttachment struct {
	Filename    string
	ContentType string
	Data        []byte
}

func buildMIME(from string, req SendRequest, smtpHost string, policyApplied bool) []byte {
	decoded, err := DecodeOutboundAttachments(req.Attachments)
	if err != nil {
		// SendMessage validates earlier; keep build resilient.
		decoded = nil
	}

	var b strings.Builder
	b.WriteString("From: " + from + "\r\n")
	b.WriteString("To: " + strings.Join(req.To, ", ") + "\r\n")
	if len(req.CC) > 0 {
		b.WriteString("Cc: " + strings.Join(req.CC, ", ") + "\r\n")
	}
	b.WriteString("Subject: " + encodeHeader(req.Subject) + "\r\n")
	b.WriteString("Date: " + time.Now().Format(time.RFC1123Z) + "\r\n")
	b.WriteString("Message-ID: <" + newMessageID(smtpHost) + ">\r\n")
	if irt := strings.TrimSpace(req.InReplyTo); irt != "" {
		if !strings.HasPrefix(irt, "<") {
			irt = "<" + irt + ">"
		}
		b.WriteString("In-Reply-To: " + irt + "\r\n")
	}
	if refs := strings.TrimSpace(req.References); refs != "" {
		b.WriteString("References: " + refs + "\r\n")
	}
	b.WriteString("MIME-Version: 1.0\r\n")
	if policyApplied {
		b.WriteString("X-Wernanmail-Outbound: 1\r\n")
	}

	hasHTML := strings.TrimSpace(req.HTML) != ""
	hasAtt := len(decoded) > 0

	switch {
	case hasAtt:
		const mixed = "wernan-mixed"
		b.WriteString("Content-Type: multipart/mixed; boundary=\"" + mixed + "\"\r\n\r\n")
		b.WriteString("--" + mixed + "\r\n")
		writeBodyParts(&b, req.Text, req.HTML, hasHTML)
		for _, att := range decoded {
			b.WriteString("--" + mixed + "\r\n")
			writeAttachmentPart(&b, att)
		}
		b.WriteString("--" + mixed + "--\r\n")
	case hasHTML:
		writeBodyParts(&b, req.Text, req.HTML, true)
	default:
		writeTextPart(&b, "text/plain", req.Text)
	}
	return []byte(b.String())
}

func writeBodyParts(b *strings.Builder, text, html string, hasHTML bool) {
	if !hasHTML {
		writeTextPart(b, "text/plain", text)
		return
	}
	const alt = "wernan-alt"
	b.WriteString("Content-Type: multipart/alternative; boundary=\"" + alt + "\"\r\n\r\n")
	b.WriteString("--" + alt + "\r\n")
	writeTextPart(b, "text/plain", text)
	b.WriteString("--" + alt + "\r\n")
	writeTextPart(b, "text/html", html)
	b.WriteString("--" + alt + "--\r\n")
}

func writeTextPart(b *strings.Builder, contentType, body string) {
	b.WriteString("Content-Type: " + contentType + "; charset=UTF-8\r\n")
	b.WriteString("Content-Transfer-Encoding: quoted-printable\r\n\r\n")
	b.WriteString(encodeQP(body))
	if !strings.HasSuffix(body, "\n") {
		b.WriteString("\r\n")
	}
}

func writeAttachmentPart(b *strings.Builder, att decodedAttachment) {
	ct := att.ContentType
	if ct == "" {
		ct = "application/octet-stream"
	}
	b.WriteString("Content-Type: " + ct + "\r\n")
	b.WriteString("Content-Transfer-Encoding: base64\r\n")
	b.WriteString("Content-Disposition: " + attachmentDisposition(att.Filename) + "\r\n\r\n")
	b.WriteString(wrapBase64(base64.StdEncoding.EncodeToString(att.Data)))
	b.WriteString("\r\n")
}

func attachmentDisposition(filename string) string {
	ascii := make([]rune, 0, len(filename))
	needsStar := false
	for _, r := range filename {
		if r > 127 || r == '"' || r == '\\' || unicode.IsControl(r) {
			needsStar = true
			if r > 127 {
				ascii = append(ascii, '_')
			}
			continue
		}
		ascii = append(ascii, r)
	}
	fallback := string(ascii)
	if fallback == "" {
		fallback = "attachment"
	}
	disp := fmt.Sprintf("attachment; filename=\"%s\"", fallback)
	if needsStar || fallback != filename {
		disp += "; filename*=UTF-8''" + url.PathEscape(filename)
	}
	return disp
}

func wrapBase64(s string) string {
	const line = 76
	if len(s) <= line {
		return s
	}
	var b strings.Builder
	b.Grow(len(s) + len(s)/line)
	for len(s) > line {
		b.WriteString(s[:line])
		b.WriteString("\r\n")
		s = s[line:]
	}
	b.WriteString(s)
	return b.String()
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
