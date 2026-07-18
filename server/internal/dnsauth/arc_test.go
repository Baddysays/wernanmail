package dnsauth

import (
	"strings"
	"testing"
)

func TestSealAndCheckARC(t *testing.T) {
	kp, err := GenerateDKIM("wernan")
	if err != nil {
		t.Fatal(err)
	}
	raw := []byte("From: a@example.com\r\n" +
		"To: b@example.com\r\n" +
		"Subject: ARC test\r\n" +
		"Date: Sat, 18 Jul 2026 00:00:00 +0000\r\n" +
		"Message-ID: <arc-test@example.com>\r\n" +
		"MIME-Version: 1.0\r\n" +
		"Content-Type: text/plain; charset=utf-8\r\n" +
		"\r\n" +
		"hello arc\r\n")
	signed, err := SignDKIM(raw, "example.com", kp.Selector, kp.PrivatePEM)
	if err != nil {
		t.Fatal(err)
	}
	sealed, err := SealARC(signed, "example.com", kp.Selector, kp.PrivatePEM, "mail.example.com", nil)
	if err != nil {
		t.Fatal(err)
	}
	s := string(sealed)
	for _, h := range []string{"ARC-Seal:", "ARC-Message-Signature:", "ARC-Authentication-Results:", "DKIM-Signature:"} {
		if !strings.Contains(s, h) {
			t.Fatalf("missing %s in:\n%s", h, s)
		}
	}
	// Without published DNS key, crypto verify cannot pass — but chain must be present.
	c := &Checker{}
	if got := c.CheckARC(sealed); got == "" {
		t.Fatal("empty arc result")
	}
}

func TestFormatAuthResults(t *testing.T) {
	s := FormatAuthResults("mail.example.com", "pass", "pass", "none")
	if !strings.Contains(s, "mail.example.com") || !strings.Contains(s, "spf=pass") || !strings.Contains(s, "dkim=pass") || !strings.Contains(s, "arc=none") {
		t.Fatalf("got %q", s)
	}
}

func TestPrependAuthResults(t *testing.T) {
	raw := []byte("From: a@b.c\r\n\r\nbody\r\n")
	out := PrependAuthResults(raw, "mail.test", "pass", "none", "none")
	if !strings.HasPrefix(string(out), "Authentication-Results:") {
		t.Fatalf("prefix missing: %q", out[:80])
	}
}
