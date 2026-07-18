package mail

import (
	"strings"
	"testing"
)

func TestSnippetFromBytesPlain(t *testing.T) {
	got := SnippetFromBytes([]byte("Hello world, this is a test message body."))
	if got != "Hello world, this is a test message body." {
		t.Fatalf("got %q", got)
	}
}

func TestSnippetFromBytesHTML(t *testing.T) {
	got := SnippetFromBytes([]byte(`<html><body><p>Hi <b>there</b></p><script>x()</script></body></html>`))
	if !strings.Contains(got, "Hi there") {
		t.Fatalf("got %q", got)
	}
	if strings.Contains(got, "script") || strings.Contains(got, "<") {
		t.Fatalf("html leak: %q", got)
	}
}

func TestSnippetFromBytesTruncates(t *testing.T) {
	long := strings.Repeat("а", 200)
	got := SnippetFromBytes([]byte(long))
	runes := []rune(got)
	if len(runes) != 141 { // 140 + ellipsis
		t.Fatalf("len=%d got %q", len(runes), got)
	}
	if runes[len(runes)-1] != '…' {
		t.Fatalf("missing ellipsis: %q", got)
	}
}
