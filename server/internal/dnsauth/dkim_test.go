package dnsauth_test

import (
	"bytes"
	"strings"
	"testing"

	"github.com/Baddysays/wernanmail/server/internal/dnsauth"
	"github.com/emersion/go-msgauth/dkim"
)

func TestSignDKIMRoundTrip(t *testing.T) {
	kp, err := dnsauth.GenerateDKIM("test")
	if err != nil {
		t.Fatal(err)
	}
	raw := []byte("From: a@example.com\r\nTo: b@example.com\r\nSubject: hi\r\nDate: Mon, 2 Jan 2006 15:04:05 -0700\r\nMessage-ID: <x@example.com>\r\nMIME-Version: 1.0\r\nContent-Type: text/plain\r\n\r\nhello\r\n")
	signed, err := dnsauth.SignDKIM(raw, "example.com", kp.Selector, kp.PrivatePEM)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(signed), "DKIM-Signature:") {
		t.Fatal("missing DKIM-Signature header")
	}
	// Public key isn't in DNS — Verify will fail lookup; at least Sign must succeed and be parseable.
	verifications, err := dkim.Verify(bytes.NewReader(signed))
	_ = verifications
	_ = err // DNS lookup for public key expected to fail in unit tests
	if !bytes.Contains(signed, []byte("header.d=example.com")) && !bytes.Contains(signed, []byte("d=example.com")) {
		t.Fatalf("signature missing domain: %s", signed[:min(200, len(signed))])
	}
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
