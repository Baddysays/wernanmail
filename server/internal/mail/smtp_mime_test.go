package mail

import (
	"encoding/base64"
	"strings"
	"testing"
)

func TestBuildMIME_plain(t *testing.T) {
	msg := string(buildMIME("a@example.com", SendRequest{
		To:      []string{"b@example.com"},
		Subject: "Hi",
		Text:    "Hello",
	}, "mail.example.com", false))
	if !strings.Contains(msg, "Content-Type: text/plain") {
		t.Fatalf("expected text/plain, got:\n%s", msg)
	}
	if strings.Contains(msg, "multipart/") {
		t.Fatalf("unexpected multipart:\n%s", msg)
	}
}

func TestBuildMIME_htmlAndAttachment(t *testing.T) {
	payload := base64.StdEncoding.EncodeToString([]byte("file-bytes"))
	msg := string(buildMIME("a@example.com", SendRequest{
		To:      []string{"b@example.com"},
		Subject: "With file",
		Text:    "plain",
		HTML:    "<p><b>hi</b></p>",
		Attachments: []OutboundAttachment{{
			Filename:    "note.txt",
			ContentType: "text/plain",
			Content:     payload,
		}},
	}, "mail.example.com", false))
	if !strings.Contains(msg, "multipart/mixed") {
		t.Fatalf("expected mixed:\n%s", msg)
	}
	if !strings.Contains(msg, "multipart/alternative") {
		t.Fatalf("expected alternative:\n%s", msg)
	}
	if !strings.Contains(msg, "filename=\"note.txt\"") {
		t.Fatalf("expected filename:\n%s", msg)
	}
	if !strings.Contains(msg, "Content-Transfer-Encoding: base64") {
		t.Fatalf("expected base64 part:\n%s", msg)
	}
}

func TestDecodeOutboundAttachments(t *testing.T) {
	_, err := DecodeOutboundAttachments([]OutboundAttachment{{
		Filename: "x.bin",
		Content:  "!!!",
	}})
	if err == nil {
		t.Fatal("expected invalid base64 error")
	}
}
