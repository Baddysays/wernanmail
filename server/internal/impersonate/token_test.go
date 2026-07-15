package impersonate

import (
	"encoding/base64"
	"encoding/json"
	"testing"
	"time"
)

func TestIssueParse(t *testing.T) {
	secret := "test-secret-key"
	tok, err := Issue(secret, "user@example.com", "admin", time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	p, err := Parse(secret, tok)
	if err != nil {
		t.Fatal(err)
	}
	if p.Username != "user@example.com" || p.Actor != "admin" {
		t.Fatalf("payload %+v", p)
	}
	if _, err := Parse("wrong", tok); err == nil {
		t.Fatal("expected bad signature")
	}
}

func TestExpired(t *testing.T) {
	secret := "test-secret-key"
	// Craft an already-expired payload (Issue clamps ttl <= 0 to 2m).
	raw, _ := json.Marshal(TokenPayload{Username: "a@b.c", ExpiresAt: time.Now().UTC().Unix() - 10, Actor: "admin"})
	body := base64.RawURLEncoding.EncodeToString(raw)
	tok := body + "." + sign(secret, body)
	if _, err := Parse(secret, tok); err == nil {
		t.Fatal("expected expired")
	}
}
