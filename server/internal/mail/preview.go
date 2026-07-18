package mail

import (
	"bytes"
	"io"
	"mime/quotedprintable"
	"regexp"
	"strings"
	"unicode"

	"github.com/emersion/go-imap"
)

const previewFetchBytes = 4096
const previewMaxRunes = 140

var (
	htmlTagRe   = regexp.MustCompile(`(?is)<(script|style)[^>]*>.*?</(script|style)>`)
	htmlStripRe = regexp.MustCompile(`(?is)<[^>]+>`)
	spaceRe     = regexp.MustCompile(`\s+`)
)

func previewBodySection() *imap.BodySectionName {
	return &imap.BodySectionName{
		BodyPartName: imap.BodyPartName{Specifier: imap.TextSpecifier},
		Peek:         true,
		Partial:      []int{0, previewFetchBytes},
	}
}

func previewFromMessage(msg *imap.Message, section *imap.BodySectionName) string {
	if msg == nil || section == nil {
		return ""
	}
	lit := msg.GetBody(section)
	if lit == nil {
		return ""
	}
	raw, err := io.ReadAll(io.LimitReader(lit, previewFetchBytes+512))
	if err != nil || len(raw) == 0 {
		return ""
	}
	return SnippetFromBytes(raw)
}

// SnippetFromBytes turns a partial MIME/text body into a short list preview.
func SnippetFromBytes(raw []byte) string {
	raw = bytes.TrimSpace(raw)
	if len(raw) == 0 {
		return ""
	}
	if bytes.Contains(raw, []byte("=\r\n")) || bytes.Contains(raw, []byte("=3D")) {
		if decoded, err := io.ReadAll(quotedprintable.NewReader(bytes.NewReader(raw))); err == nil && len(decoded) > 0 {
			raw = decoded
		}
	}
	s := string(raw)
	lower := strings.ToLower(s)
	if strings.Contains(lower, "<html") || strings.Contains(lower, "<body") ||
		strings.Contains(lower, "<div") || strings.Contains(lower, "<p") {
		s = htmlTagRe.ReplaceAllString(s, " ")
		s = htmlStripRe.ReplaceAllString(s, " ")
		s = strings.NewReplacer(
			"&nbsp;", " ",
			"&amp;", "&",
			"&lt;", "<",
			"&gt;", ">",
			"&quot;", "\"",
		).Replace(s)
	}
	if i := strings.Index(s, "\r\n\r\n"); i >= 0 && i < 400 && strings.Contains(strings.ToLower(s[:i]), "content-type:") {
		s = s[i+4:]
	}
	s = spaceRe.ReplaceAllString(s, " ")
	s = strings.TrimSpace(s)
	s = strings.Map(func(r rune) rune {
		if unicode.IsControl(r) && r != '\t' {
			return -1
		}
		return r
	}, s)
	runes := []rune(s)
	if len(runes) > previewMaxRunes {
		return string(runes[:previewMaxRunes]) + "…"
	}
	return s
}
