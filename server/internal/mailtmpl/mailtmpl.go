// Package mailtmpl applies outbound body templates and disclaimers.
// Designed for a small Go MTA: wrap/append text parts, leave attachments alone.
package mailtmpl

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"io"
	"mime"
	"mime/multipart"
	"mime/quotedprintable"
	"net/mail"
	"net/textproto"
	"strings"
	"text/template"
	"time"
)

const appliedHeader = "X-Wernanmail-Outbound"

// Data is available in body/footer templates.
type Data struct {
	Body      string
	From      string
	Domain    string
	LocalPart string
	Subject   string
	Date      string
	Year      int
}

// Policy is loaded from settings.
type Policy struct {
	BodyPlain string
	BodyHTML  string
	FootPlain string
	FootHTML  string
	SkipReply bool
}

// Empty reports whether nothing would change the message.
func (p Policy) Empty() bool {
	return strings.TrimSpace(p.BodyPlain) == "" &&
		strings.TrimSpace(p.BodyHTML) == "" &&
		strings.TrimSpace(p.FootPlain) == "" &&
		strings.TrimSpace(p.FootHTML) == ""
}

// TransformBodies applies wrap+footer to compose-time plain/HTML bodies.
func (p Policy) TransformBodies(from, subject, text, html string) (string, string) {
	if p.Empty() {
		return text, html
	}
	if p.SkipReply && looksLikeReply(subject, "", "") {
		return text, html
	}
	d := baseData(from, subject)
	if text != "" || html == "" {
		d.Body = text
		text = p.renderPlain(d)
	}
	if html != "" {
		d.Body = html
		html = p.renderHTML(d)
	} else if strings.TrimSpace(p.BodyHTML) != "" || strings.TrimSpace(p.FootHTML) != "" {
		// HTML policy set but client sent plain only — derive a simple HTML twin.
		d.Body = plainToHTML(text)
		html = p.renderHTML(d)
	}
	return text, html
}

// Apply mutates a raw RFC822 message for authenticated outbound submission.
// Idempotent via X-Wernanmail-Outbound. On failure returns the original raw.
func Apply(raw []byte, from string, p Policy) []byte {
	if p.Empty() || len(raw) == 0 {
		return raw
	}
	msg, err := mail.ReadMessage(bytes.NewReader(raw))
	if err != nil {
		return raw
	}
	if strings.EqualFold(msg.Header.Get(appliedHeader), "1") {
		return raw
	}
	subject := msg.Header.Get("Subject")
	if p.SkipReply && looksLikeReply(subject, msg.Header.Get("In-Reply-To"), msg.Header.Get("References")) {
		return raw
	}
	ct := msg.Header.Get("Content-Type")
	if ct == "" {
		ct = "text/plain; charset=utf-8"
	}
	media, params, err := mime.ParseMediaType(ct)
	if err != nil {
		return raw
	}
	body, err := io.ReadAll(msg.Body)
	if err != nil {
		return raw
	}
	d := baseData(from, subject)
	var newBody []byte
	var newCT string
	var newCTE string

	switch {
	case media == "text/plain":
		plain, err := decodePart(body, msg.Header.Get("Content-Transfer-Encoding"))
		if err != nil {
			return raw
		}
		d.Body = plain
		out := p.renderPlain(d)
		newBody = []byte(encodeQP(out))
		newCT = "text/plain; charset=UTF-8"
		newCTE = "quoted-printable"
	case media == "text/html":
		html, err := decodePart(body, msg.Header.Get("Content-Transfer-Encoding"))
		if err != nil {
			return raw
		}
		d.Body = html
		out := p.renderHTML(d)
		newBody = []byte(encodeQP(out))
		newCT = "text/html; charset=UTF-8"
		newCTE = "quoted-printable"
	case strings.HasPrefix(media, "multipart/"):
		boundary := params["boundary"]
		if boundary == "" {
			return raw
		}
		rewritten, ok := p.rewriteMultipart(body, boundary, d)
		if !ok {
			return raw
		}
		newBody = rewritten
		newCT = ct
		newCTE = msg.Header.Get("Content-Transfer-Encoding")
	default:
		return raw
	}

	var buf bytes.Buffer
	for k, vals := range msg.Header {
		lk := strings.ToLower(k)
		if lk == "content-type" || lk == "content-transfer-encoding" || lk == strings.ToLower(appliedHeader) {
			continue
		}
		for _, v := range vals {
			fmt.Fprintf(&buf, "%s: %s\r\n", k, v)
		}
	}
	fmt.Fprintf(&buf, "Content-Type: %s\r\n", newCT)
	if newCTE != "" {
		fmt.Fprintf(&buf, "Content-Transfer-Encoding: %s\r\n", newCTE)
	}
	fmt.Fprintf(&buf, "%s: 1\r\n\r\n", appliedHeader)
	buf.Write(newBody)
	return buf.Bytes()
}

func (p Policy) rewriteMultipart(body []byte, boundary string, d Data) ([]byte, bool) {
	mr := multipart.NewReader(bytes.NewReader(body), boundary)
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	_ = mw.SetBoundary(boundary)
	changed := false
	for {
		part, err := mr.NextPart()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, false
		}
		payload, err := io.ReadAll(part)
		if err != nil {
			return nil, false
		}
		pct := part.Header.Get("Content-Type")
		media, _, _ := mime.ParseMediaType(pct)
		disposition, _, _ := mime.ParseMediaType(part.Header.Get("Content-Disposition"))
		isAttach := strings.EqualFold(disposition, "attachment")

		hdr := textproto.MIMEHeader{}
		for k, vals := range part.Header {
			hdr[k] = vals
		}

		if !isAttach && (media == "text/plain" || media == "text/html") {
			decoded, err := decodePart(payload, part.Header.Get("Content-Transfer-Encoding"))
			if err == nil {
				d.Body = decoded
				var out string
				if media == "text/html" {
					out = p.renderHTML(d)
				} else {
					out = p.renderPlain(d)
				}
				payload = []byte(encodeQP(out))
				hdr.Set("Content-Transfer-Encoding", "quoted-printable")
				if media == "text/plain" {
					hdr.Set("Content-Type", "text/plain; charset=UTF-8")
				} else {
					hdr.Set("Content-Type", "text/html; charset=UTF-8")
				}
				changed = true
			}
		}

		w, err := mw.CreatePart(hdr)
		if err != nil {
			return nil, false
		}
		if _, err := w.Write(payload); err != nil {
			return nil, false
		}
	}
	if err := mw.Close(); err != nil || !changed {
		return nil, false
	}
	return buf.Bytes(), true
}

func (p Policy) renderPlain(d Data) string {
	body := d.Body
	if tpl := strings.TrimSpace(p.BodyPlain); tpl != "" {
		body = execTemplate(ensureBodySlot(tpl), d)
	}
	if foot := strings.TrimSpace(p.FootPlain); foot != "" {
		d.Body = body
		foot = execTemplate(foot, d)
		if body != "" && !strings.HasSuffix(body, "\n") {
			body += "\n"
		}
		body = body + "\n" + foot
	}
	return body
}

func (p Policy) renderHTML(d Data) string {
	body := d.Body
	if tpl := strings.TrimSpace(p.BodyHTML); tpl != "" {
		body = execTemplate(ensureBodySlot(tpl), d)
	}
	if foot := strings.TrimSpace(p.FootHTML); foot != "" {
		d.Body = body
		foot = execTemplate(foot, d)
		body = body + "\n" + foot
	}
	return body
}

func ensureBodySlot(tpl string) string {
	if strings.Contains(tpl, ".Body") {
		return tpl
	}
	return strings.TrimRight(tpl, "\r\n") + "\n\n{{.Body}}"
}

func execTemplate(tpl string, d Data) string {
	t, err := template.New("mail").Option("missingkey=zero").Parse(tpl)
	if err != nil {
		return d.Body
	}
	var b strings.Builder
	if err := t.Execute(&b, d); err != nil {
		return d.Body
	}
	return b.String()
}

func baseData(from, subject string) Data {
	local, domain := splitAddr(from)
	now := time.Now()
	return Data{
		From:      from,
		Domain:    domain,
		LocalPart: local,
		Subject:   subject,
		Date:      now.Format(time.RFC1123Z),
		Year:      now.Year(),
	}
}

func splitAddr(addr string) (local, domain string) {
	addr = strings.Trim(strings.TrimSpace(addr), "<>")
	i := strings.LastIndex(addr, "@")
	if i <= 0 {
		return addr, ""
	}
	return addr[:i], addr[i+1:]
}

func looksLikeReply(subject, inReplyTo, references string) bool {
	if strings.TrimSpace(inReplyTo) != "" || strings.TrimSpace(references) != "" {
		return true
	}
	s := strings.TrimSpace(subject)
	for strings.HasPrefix(strings.ToLower(s), "re:") || strings.HasPrefix(strings.ToLower(s), "re：") {
		return true
	}
	return false
}

func decodePart(body []byte, cte string) (string, error) {
	cte = strings.ToLower(strings.TrimSpace(cte))
	switch cte {
	case "", "7bit", "8bit", "binary":
		return string(body), nil
	case "base64":
		decoded, err := base64.StdEncoding.DecodeString(stripWS(string(body)))
		if err != nil {
			decoded, err = base64.RawStdEncoding.DecodeString(stripWS(string(body)))
			if err != nil {
				return "", err
			}
		}
		return string(decoded), nil
	case "quoted-printable":
		r := quotedprintable.NewReader(bytes.NewReader(body))
		b, err := io.ReadAll(r)
		if err != nil {
			return "", err
		}
		return string(b), nil
	default:
		return string(body), nil
	}
}

func stripWS(s string) string {
	var b strings.Builder
	for _, r := range s {
		if r != ' ' && r != '\t' && r != '\r' && r != '\n' {
			b.WriteRune(r)
		}
	}
	return b.String()
}

func encodeQP(s string) string {
	var buf bytes.Buffer
	w := quotedprintable.NewWriter(&buf)
	_, _ = io.WriteString(w, s)
	_ = w.Close()
	out := buf.String()
	if out != "" && !strings.HasSuffix(out, "\n") {
		out += "\r\n"
	}
	return out
}

func plainToHTML(s string) string {
	esc := htmlEscape(s)
	esc = strings.ReplaceAll(esc, "\r\n", "\n")
	esc = strings.ReplaceAll(esc, "\n", "<br>\n")
	return esc
}

func htmlEscape(s string) string {
	repl := strings.NewReplacer(
		"&", "&amp;",
		"<", "&lt;",
		">", "&gt;",
		`"`, "&quot;",
	)
	return repl.Replace(s)
}
