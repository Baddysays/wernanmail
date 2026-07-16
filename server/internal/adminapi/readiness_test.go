package adminapi

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/Baddysays/wernanmail/server/internal/domain"
	"github.com/Baddysays/wernanmail/server/internal/settings"
	"github.com/Baddysays/wernanmail/server/internal/store/sqlite"
)

func TestParseFloatSetting(t *testing.T) {
	if parseFloatSetting("5.5", 1) != 5.5 {
		t.Fatal("parse")
	}
	if parseFloatSetting("x", 3) != 3 {
		t.Fatal("fallback")
	}
}

func TestSplitSettingCSV(t *testing.T) {
	got := splitSettingCSV(" zen.spamhaus.org , bl.example.com ")
	if len(got) != 2 || got[0] != "zen.spamhaus.org" {
		t.Fatalf("%v", got)
	}
}

func TestAntispamPostureProbe(t *testing.T) {
	st, err := sqlite.Open(filepath.Join(t.TempDir(), "data"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	h := &Handler{Settings: settings.NewManager(st)}
	out := h.antispamPosture(context.Background())
	probe, _ := out["probe"].(map[string]any)
	if probe == nil || probe["ok"] != true {
		t.Fatalf("probe not ok: %+v", out)
	}
	clean, _ := probe["clean"].(map[string]any)
	spammy, _ := probe["spammy"].(map[string]any)
	if clean["action"] != string(domain.SpamDeliver) {
		t.Fatalf("clean action=%v", clean["action"])
	}
	act, _ := spammy["action"].(string)
	if act != string(domain.SpamQuarantine) && act != string(domain.SpamReject) && act != string(domain.SpamFlag) {
		t.Fatalf("spammy action=%v score=%v", act, spammy["score"])
	}
}
