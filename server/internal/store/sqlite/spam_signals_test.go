package sqlite_test

import (
	"context"
	"math"
	"testing"

	"github.com/Baddysays/wernanmail/server/internal/store/sqlite"
)

func TestSpamSignalsAccumulateAndPersist(t *testing.T) {
	dir := t.TempDir()
	ctx := context.Background()
	st, err := sqlite.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	keys := []string{"from_domain:example.com", "subject_token:offer"}
	if err := st.LearnSpamSignals(ctx, keys, 0.5); err != nil {
		t.Fatal(err)
	}
	if err := st.LearnSpamSignals(ctx, keys, -0.25); err != nil {
		t.Fatal(err)
	}
	got, err := st.LookupSpamSignals(ctx, append(keys, "subject_token:missing"))
	if err != nil {
		t.Fatal(err)
	}
	for _, key := range keys {
		if math.Abs(got[key]-0.25) > 0.0001 {
			t.Fatalf("%s: want 0.25, got %v", key, got[key])
		}
	}
	if _, ok := got["subject_token:missing"]; ok {
		t.Fatal("missing signal should not be returned")
	}
	if err := st.Close(); err != nil {
		t.Fatal(err)
	}

	st, err = sqlite.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	got, err = st.LookupSpamSignals(ctx, keys)
	if err != nil {
		t.Fatal(err)
	}
	if got[keys[0]] != 0.25 {
		t.Fatalf("signal did not persist: %v", got)
	}
}
