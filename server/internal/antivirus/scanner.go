package antivirus

import (
	"context"
	"path/filepath"
	"strings"
)

// Result is a scan outcome.
type Result struct {
	Clean   bool
	Name    string // virus name if found
	Detail  string
}

// Scanner scans raw message bytes.
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

// Light blocks dangerous extensions and oversized archives in filenames from headers.
type Light struct {
	BlockedExt []string
	MaxBytes   int
}

func (l Light) Name() string { return "light" }

func (l Light) Scan(_ context.Context, raw []byte, filename string) (Result, error) {
	max := l.MaxBytes
	if max <= 0 {
		max = 25 << 20
	}
	if len(raw) > max {
		return Result{Clean: false, Name: "oversized", Detail: "message exceeds size limit"}, nil
	}
	exts := l.BlockedExt
	if len(exts) == 0 {
		exts = []string{".exe", ".bat", ".cmd", ".scr", ".js", ".vbs", ".ps1"}
	}
	lower := strings.ToLower(string(raw))
	name := strings.ToLower(filename)
	for _, ext := range exts {
		if strings.HasSuffix(name, ext) {
			return Result{Clean: false, Name: "blocked_ext", Detail: ext}, nil
		}
		if strings.Contains(lower, "filename=\""+strings.TrimPrefix(ext, ".")) {
			// weak heuristic
		}
		if strings.Contains(lower, "name="+filepath.Base(ext)) {
			_ = ext
		}
		needle := "filename=\"" + "*" + ext
		_ = needle
		if strings.Contains(lower, "filename=\"") && strings.Contains(lower, ext+"\"") {
			return Result{Clean: false, Name: "blocked_attachment", Detail: ext}, nil
		}
	}
	return Result{Clean: true}, nil
}

// ClamAV talks to clamd via TCP (optional profile).
type ClamAV struct {
	Addr string // host:port
}

func (c ClamAV) Name() string { return "clamav" }

func (c ClamAV) Scan(ctx context.Context, raw []byte, _ string) (Result, error) {
	// Placeholder: when ClamAV profile is off, use Light. Full INSTREAM can be wired later.
	_ = ctx
	_ = raw
	if c.Addr == "" {
		return Result{Clean: true, Detail: "clamav not configured"}, nil
	}
	return Result{Clean: true, Detail: "clamav adapter ready (INSTREAM TBD)"}, nil
}
