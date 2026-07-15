package sqlite_test

import (
	"context"
	"errors"
	"testing"

	"github.com/Baddysays/wernanmail/server/internal/domain"
	"github.com/Baddysays/wernanmail/server/internal/store"
	"github.com/Baddysays/wernanmail/server/internal/store/sqlite"
)

func TestAppendMessageEnforcesMailboxQuota(t *testing.T) {
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
	mb := &domain.Mailbox{
		DomainID: d.ID, LocalPart: "user", PasswordHash: "unused",
		QuotaBytes: 5, Enabled: true,
	}
	if err := st.UpsertMailbox(ctx, mb); err != nil {
		t.Fatal(err)
	}
	if err := st.AppendMessage(ctx, &domain.Message{MailboxID: mb.ID}, []byte("12345")); err != nil {
		t.Fatalf("append at quota: %v", err)
	}
	err = st.AppendMessage(ctx, &domain.Message{MailboxID: mb.ID}, []byte("x"))
	if !errors.Is(err, store.ErrQuotaExceeded) {
		t.Fatalf("want ErrQuotaExceeded, got %v", err)
	}
}
