package sqlite_test

import (
	"context"
	"testing"

	"github.com/Baddysays/wernanmail/server/internal/domain"
	"github.com/Baddysays/wernanmail/server/internal/store/sqlite"
)

func TestMailboxContentRevBumpsOnAppend(t *testing.T) {
	ctx := context.Background()
	st, err := sqlite.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	d := &domain.Domain{Name: "example.test", Enabled: true}
	if err := st.UpsertDomain(ctx, d); err != nil {
		t.Fatal(err)
	}
	mb := &domain.Mailbox{DomainID: d.ID, LocalPart: "user", PasswordHash: "x", Enabled: true}
	if err := st.UpsertMailbox(ctx, mb); err != nil {
		t.Fatal(err)
	}

	r0, err := st.MailboxContentRev(ctx, mb.ID)
	if err != nil {
		t.Fatal(err)
	}
	if err := st.AppendMessage(ctx, &domain.Message{MailboxID: mb.ID, Subject: "a"}, []byte("body-a")); err != nil {
		t.Fatal(err)
	}
	r1, err := st.MailboxContentRev(ctx, mb.ID)
	if err != nil {
		t.Fatal(err)
	}
	if r1 <= r0 {
		t.Fatalf("content_rev did not bump: %d -> %d", r0, r1)
	}
}
