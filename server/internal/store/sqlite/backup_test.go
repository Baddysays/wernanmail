package sqlite_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/Baddysays/wernanmail/server/internal/domain"
	"github.com/Baddysays/wernanmail/server/internal/store/sqlite"
)

func TestBackupDatabase(t *testing.T) {
	dir := t.TempDir()
	st, err := sqlite.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	if err := st.UpsertDomain(context.Background(), &domain.Domain{Name: "ex.com", Enabled: true}); err != nil {
		t.Fatal(err)
	}

	snapDir := t.TempDir()
	dest := filepath.Join(snapDir, "mail.db")
	if err := st.BackupDatabase(context.Background(), dest); err != nil {
		t.Fatal(err)
	}
	fi, err := os.Stat(dest)
	if err != nil || fi.Size() < 1024 {
		t.Fatalf("snapshot missing or tiny: %v size=%v", err, fi)
	}

	st2, err := sqlite.Open(snapDir)
	if err != nil {
		t.Fatal(err)
	}
	defer st2.Close()
	list, err := st2.ListDomains(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 1 || list[0].Name != "ex.com" {
		t.Fatalf("domains=%+v", list)
	}
}
