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

func TestFileStoreSurvivesRestart(t *testing.T) {
	path := t.TempDir() + "/sessions.json"
	secret := "persistent-test-secret"
	first, err := session.NewFileStore(path, time.Hour, secret)
	if err != nil {
		t.Fatal(err)
	}
	created, err := first.Create(session.Credentials{
		IMAPHost: "mail.example.test", Username: "user@example.test", Password: "s3cret",
	})
	if err != nil {
		t.Fatal(err)
	}

	second, err := session.NewFileStore(path, time.Hour, secret)
	if err != nil {
		t.Fatal(err)
	}
	got, ok := second.Get(created.ID)
	if !ok {
		t.Fatal("session was not restored")
	}
	if got.Creds.Password != "s3cret" || got.Creds.Username != "user@example.test" {
		t.Fatalf("restored wrong credentials: %+v", got.Creds)
	}
}

func TestFileStoreDropsExpiredSessions(t *testing.T) {
	path := t.TempDir() + "/sessions.json"
	first, err := session.NewFileStore(path, time.Nanosecond, "persistent-test-secret")
	if err != nil {
		t.Fatal(err)
	}
	created, err := first.Create(session.Credentials{Username: "expired", Password: "secret"})
	if err != nil {
		t.Fatal(err)
	}
	time.Sleep(time.Millisecond)

	second, err := session.NewFileStore(path, time.Hour, "persistent-test-secret")
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := second.Get(created.ID); ok {
		t.Fatal("expired session was restored")
	}
}
