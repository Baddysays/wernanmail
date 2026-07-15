package session_test

import (
	"testing"
	"time"

	"github.com/Baddysays/wernanmail/server/internal/session"
)

func TestStoreEncryptsPassword(t *testing.T) {
	s := session.NewStoreWithSecret(time.Hour, "test-secret-key-please-change")
	created, err := s.Create(session.Credentials{
		IMAPHost: "localhost", Username: "a@b.c", Password: "s3cret",
	})
	if err != nil {
		t.Fatal(err)
	}
	if created.Creds.Password != "s3cret" {
		t.Fatal("Create should return plaintext to caller")
	}
	got, ok := s.Get(created.ID)
	if !ok {
		t.Fatal("missing session")
	}
	if got.Creds.Password != "s3cret" {
		t.Fatalf("decrypt got %q", got.Creds.Password)
	}
}
