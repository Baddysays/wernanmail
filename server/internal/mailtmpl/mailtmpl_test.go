package mailtmpl

import (
	"strings"
	"testing"
)

func TestTransformBodies_WrapAndFooter(t *testing.T) {
	p := Policy{
		BodyPlain: "Hello from {{.Domain}}\n\n{{.Body}}\n\n— {{.LocalPart}}",
		FootPlain: "--\nConfidential ({{.Year}})",
	}
	text, html := p.TransformBodies("alice@example.com", "Hi", "Please review", "")
	if !strings.Contains(text, "Hello from example.com") {
		t.Fatalf("wrap missing: %q", text)
	}
	if !strings.Contains(text, "Please review") {
		t.Fatalf("body missing: %q", text)
	}
	if !strings.Contains(text, "Confidential (") {
		t.Fatalf("footer missing: %q", text)
	}
	if html != "" {
		t.Fatalf("expected no html when html policy empty, got %q", html)
	}
}

func TestTransformBodies_SkipReply(t *testing.T) {
	p := Policy{FootPlain: "FOOTER", SkipReply: true}
	text, _ := p.TransformBodies("a@b.c", "Re: earlier", "body", "")
	if text != "body" {
		t.Fatalf("expected unchanged reply, got %q", text)
	}
}

func TestApply_Plain(t *testing.T) {
	raw := []byte("From: a@b.c\r\nTo: x@y.z\r\nSubject: Test\r\nContent-Type: text/plain; charset=utf-8\r\n\r\nHello\r\n")
	p := Policy{FootPlain: "DISCLAIMER {{.Domain}}"}
	out := Apply(raw, "a@b.c", p)
	if !strings.Contains(string(out), "DISCLAIMER b.c") {
		t.Fatalf("footer not applied: %s", out)
	}
	if !strings.Contains(string(out), appliedHeader+": 1") {
		t.Fatal("missing applied header")
	}
	// idempotent
	out2 := Apply(out, "a@b.c", p)
	if strings.Count(string(out2), "DISCLAIMER") != 1 {
		t.Fatalf("double applied: %s", out2)
	}
}

func TestApply_BodyTemplate(t *testing.T) {
	raw := []byte("From: a@b.c\r\nSubject: S\r\nContent-Type: text/plain; charset=utf-8\r\n\r\nUser text\r\n")
	p := Policy{BodyPlain: "[{{.Domain}}]\n{{.Body}}"}
	out := string(Apply(raw, "a@b.c", p))
	if !strings.Contains(out, "[b.c]") || !strings.Contains(out, "User text") {
		t.Fatalf("template failed: %s", out)
	}
}
