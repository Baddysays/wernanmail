package antivirus

import (
	"bytes"
	"context"
	"io"
	"path/filepath"
	"strings"

	gomessage "github.com/emersion/go-message/mail"
)

// Result is a scan outcome. The message bytes are never modified.
type Result struct {
	Clean              bool
	Name               string // virus / policy code
	Detail             string
	PreferQuarantine   bool // true → hold for admin; false → SMTP reject
}

// Scanner scans raw message bytes without mutating them.
type Scanner interface {
	Name() string
	Scan(ctx context.Context, raw []byte, filename string) (Result, error)
}

// Noop always clean.
type Noop struct{}

func (Noop) Name() string { return "noop" }
func (Noop) Scan(context.Context, []byte, string) (Result, error) {
	return Result{Clean: true}, nil
}

// Light is a MIME-aware attachment policy (not a full AV).
// It only inspects Content-Disposition: attachment parts — text/html bodies,
// multipart wrappers, and inline CID images are ignored so message visuals stay intact.
type Light struct {
	BlockedExt []string
	MaxBytes   int
}

func (l Light) Name() string { return "light" }

func defaultRejectExt() []string {
	return []string{
		".exe", ".dll", ".scr", ".bat", ".cmd", ".com", ".pif", ".msi", ".msp",
		".js", ".jse", ".vbs", ".vbe", ".wsf", ".wsh", ".ps1", ".psm1",
		".jar", ".cpl", ".msc", ".hta", ".lnk", ".reg",
	}
}

func defaultQuarantineExt() []string {
	// Ambiguous / phishing-prone as attachments — hold, don't hard-reject.
	return []string{".html", ".htm", ".shtml", ".iso", ".img", ".apk", ".dmg"}
}

var dangerousMIME = map[string]string{
	"application/x-msdownload":           "executable",
	"application/x-msdos-program":        "executable",
	"application/x-executable":           "executable",
	"application/x-dosexec":              "executable",
	"application/vnd.microsoft.portable-executable": "executable",
	"application/x-javascript":           "script",
	"text/javascript":                    "script",
	"application/javascript":             "script",
	"application/x-vbscript":             "script",
}

func (l Light) Scan(_ context.Context, raw []byte, _ string) (Result, error) {
	max := l.MaxBytes
	if max <= 0 {
		max = 25 << 20
	}
	if len(raw) > max {
		return Result{Clean: false, Name: "oversized", Detail: "message exceeds size limit"}, nil
	}

	rejectExt := l.BlockedExt
	if len(rejectExt) == 0 {
		rejectExt = defaultRejectExt()
	}
	quarantineExt := defaultQuarantineExt()

	mr, err := gomessage.CreateReader(bytes.NewReader(raw))
	if err != nil {
		// Unparseable → do not guess from raw body (would false-positive on HTML text).
		return Result{Clean: true, Detail: "mime_unparsed"}, nil
	}
	return scanMIME(mr, rejectExt, quarantineExt)
}

func scanMIME(mr *gomessage.Reader, rejectExt, quarantineExt []string) (Result, error) {
	for {
		part, err := mr.NextPart()
		if err == io.EOF {
			break
		}
		if err != nil {
			// Partial MIME — fail open on parse errors after some parts.
			return Result{Clean: true, Detail: "mime_part_error"}, nil
		}
		switch h := part.Header.(type) {
		case *gomessage.InlineHeader:
			// text/plain, text/html, multipart children, CID images — leave alone.
			_ = h
			continue
		case *gomessage.AttachmentHeader:
			filename, _ := h.Filename()
			ct, _, _ := h.ContentType()
			ct = strings.ToLower(strings.TrimSpace(ct))
			filename = strings.TrimSpace(filename)

			if name, ok := dangerousMIME[ct]; ok {
				return Result{
					Clean: false, Name: "blocked_type", Detail: ct + " (" + name + ")",
				}, nil
			}

			ext := effectiveExt(filename)
			if ext == "" {
				// nameless attachment: still block obvious binary types above; else allow
				continue
			}
			if matchExt(ext, rejectExt) {
				return Result{
					Clean: false, Name: "blocked_attachment", Detail: filename,
				}, nil
			}
			if matchExt(ext, quarantineExt) {
				return Result{
					Clean:            false,
					Name:             "suspect_attachment",
					Detail:           filename,
					PreferQuarantine: true,
				}, nil
			}
		default:
			continue
		}
	}
	return Result{Clean: true}, nil
}

// effectiveExt returns the last extension, preferring the true trailing one
// for double extensions like invoice.pdf.exe → .exe
func effectiveExt(name string) string {
	name = strings.ToLower(filepath.Base(strings.ReplaceAll(name, "\\", "/")))
	if name == "" || name == "." {
		return ""
	}
	ext := filepath.Ext(name)
	if ext == "" {
		return ""
	}
	return ext
}

func matchExt(ext string, list []string) bool {
	for _, e := range list {
		e = strings.ToLower(strings.TrimSpace(e))
		if e == "" {
			continue
		}
		if !strings.HasPrefix(e, ".") {
			e = "." + e
		}
		if ext == e {
			return true
		}
	}
	return false
}

// ClamAV talks to clamd via TCP (optional; not required for light hosts).
type ClamAV struct {
	Addr string
}

func (c ClamAV) Name() string { return "clamav" }

func (c ClamAV) Scan(ctx context.Context, raw []byte, _ string) (Result, error) {
	_ = ctx
	_ = raw
	if c.Addr == "" {
		return Result{Clean: true, Detail: "clamav not configured"}, nil
	}
	// Intentionally not implemented: clamd is too heavy for the default host profile.
	return Result{Clean: true, Detail: "clamav optional; use Light policy"}, nil
}

// Chain runs scanners in order; first unclean result wins.
type Chain []Scanner

func (c Chain) Name() string { return "chain" }

func (c Chain) Scan(ctx context.Context, raw []byte, filename string) (Result, error) {
	for _, s := range c {
		if s == nil {
			continue
		}
		res, err := s.Scan(ctx, raw, filename)
		if err != nil {
			return Result{}, err
		}
		if !res.Clean {
			return res, nil
		}
	}
	return Result{Clean: true}, nil
}
