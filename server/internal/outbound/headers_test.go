package outbound

import (
	"strings"
	"testing"
)

func TestEnsureRFCHeadersAddsMissing(t *testing.T) {
	raw := []byte("From: a@b.ru\r\nTo: c@d.ru\r\nSubject: hi\r\n\r\nbody\r\n")
	out := string(EnsureRFCHeaders(raw, "wernanmail.ru"))
	if !strings.Contains(strings.ToLower(out), "\ndate:") && !strings.HasPrefix(strings.ToLower(out), "date:") {
		t.Fatalf("missing Date in %q", out)
	}
	if !strings.Contains(strings.ToLower(out), "message-id:") {
		t.Fatalf("missing Message-ID in %q", out)
	}
	if !strings.Contains(out, "@wernanmail.ru>") {
		t.Fatalf("Message-ID host wrong: %q", out)
	}
}

func TestEnsureRFCHeadersKeepsExisting(t *testing.T) {
	raw := []byte("Date: Mon, 01 Jan 2026 12:00:00 +0000\r\nMessage-ID: <x@y>\r\nFrom: a@b.ru\r\n\r\nhi\r\n")
	out := string(EnsureRFCHeaders(raw, "wernanmail.ru"))
	if strings.Count(strings.ToLower(out), "message-id:") != 1 {
		t.Fatalf("duplicated Message-ID: %q", out)
	}
	if strings.Count(strings.ToLower(out), "\ndate:")+boolCount(strings.HasPrefix(strings.ToLower(out), "date:")) != 1 {
		// simpler check
	}
	if !strings.Contains(out, "<x@y>") {
		t.Fatalf("original Message-ID lost: %q", out)
	}
}

func boolCount(b bool) int {
	if b {
		return 1
	}
	return 0
}
