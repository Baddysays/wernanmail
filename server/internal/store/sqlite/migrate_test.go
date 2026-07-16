package sqlite_test

import (
	"context"
	"testing"

	"github.com/Baddysays/wernanmail/server/internal/domain"
	"github.com/Baddysays/wernanmail/server/internal/store/sqlite"
)

func TestSchemaMigrationsApply(t *testing.T) {
	dir := t.TempDir()
	st, err := sqlite.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	v, err := st.SchemaVersion()
	if err != nil {
		t.Fatal(err)
	}
	if v != sqlite.CurrentSchemaVersion {
		t.Fatalf("schema version=%d want %d", v, sqlite.CurrentSchemaVersion)
	}
	st.Close()

	// Re-open: migrations must be idempotent.
	st2, err := sqlite.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer st2.Close()
	v2, err := st2.SchemaVersion()
	if err != nil {
		t.Fatal(err)
	}
	if v2 != sqlite.CurrentSchemaVersion {
		t.Fatalf("after reopen version=%d", v2)
	}
}

func TestSchemaMigrationsPreserveData(t *testing.T) {
	dir := t.TempDir()
	st, err := sqlite.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	d := &domain.Domain{Name: "example.test", Enabled: true}
	if err := st.UpsertDomain(ctx, d); err != nil {
		t.Fatal(err)
	}
	st.Close()

	st2, err := sqlite.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer st2.Close()
	got, err := st2.GetDomainByName(ctx, "example.test")
	if err != nil || got == nil {
		t.Fatalf("domain lost after remigrate: %v", err)
	}
}
