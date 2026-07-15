package antispam_test

import (
	"context"
	"testing"

	"github.com/Baddysays/wernanmail/server/internal/antispam"
	"github.com/Baddysays/wernanmail/server/internal/domain"
)

type signalStore map[string]float64

func (s signalStore) LookupSpamSignals(_ context.Context, keys []string) (map[string]float64, error) {
	out := make(map[string]float64, len(keys))
	for _, key := range keys {
		out[key] = s[key]
	}
	return out, nil
}

func TestEngineThresholds(t *testing.T) {
	e := antispam.New(nil, 10, 5, nil)
	v := e.Check(context.Background(), antispam.Input{From: "", Raw: []byte("x")})
	if v.Action == domain.SpamReject {
		t.Fatalf("empty from alone should not reject, score=%v action=%v", v.Score, v.Action)
	}
	e2 := antispam.New(nil, 1, 0.5, nil)
	v2 := e2.Check(context.Background(), antispam.Input{From: ""})
	if v2.Action != domain.SpamReject {
		t.Fatalf("want reject got %v score=%v", v2.Action, v2.Score)
	}
}

func TestEngineFlagAt(t *testing.T) {
	e := antispam.New(nil, 20, 15, nil)
	e.SetConfig(antispam.Config{RejectAt: 20, QuarantineAt: 15, FlagAt: 2})
	v := e.Check(context.Background(), antispam.Input{From: ""}) // score 2
	if v.Action != domain.SpamFlag {
		t.Fatalf("want flag got %v score=%v", v.Action, v.Score)
	}
}

func TestEnginePhishSubject(t *testing.T) {
	e := antispam.New(nil, 20, 15, nil)
	e.SetConfig(antispam.Config{RejectAt: 20, QuarantineAt: 15, FlagAt: 3})
	v := e.Check(context.Background(), antispam.Input{
		From:    "ok@example.com",
		Headers: map[string]string{"Subject": "Urgent action required — verify your account"},
	})
	if v.Score < 2 {
		t.Fatalf("expected phish subject score, got %v %+v", v.Score, v.Reasons)
	}
}

func TestEngineSetConfigHotReload(t *testing.T) {
	e := antispam.New(nil, 10, 5, nil)
	e.SetConfig(antispam.Config{RejectAt: 1, QuarantineAt: 0.5, FlagAt: 0.25, RejectMessage: "nope"})
	if e.RejectText() != "nope" {
		t.Fatal(e.RejectText())
	}
	v := e.Check(context.Background(), antispam.Input{From: ""})
	if v.Action != domain.SpamReject {
		t.Fatalf("want reject after reload, got %v", v.Action)
	}
}

func TestEngineScoresRiskyURIs(t *testing.T) {
	e := antispam.New(nil, 20, 15, nil)
	e.SetConfig(antispam.Config{RejectAt: 20, QuarantineAt: 15, FlagAt: 3})
	v := e.Check(context.Background(), antispam.Input{
		From: "billing@example.test",
		Headers: map[string]string{
			"Message-ID": "<1@example.test>",
			"Date":       "Thu, 16 Jul 2026 00:00:00 +0000",
		},
		Raw: []byte("Please verify your password at http://accounts.example.com@192.0.2.5/login"),
	})
	got := map[string]bool{}
	for _, reason := range v.Reasons {
		got[reason.Code] = true
	}
	for _, code := range []string{"uri_ip", "uri_userinfo", "credential_lure"} {
		if !got[code] {
			t.Errorf("missing %s in %+v", code, v.Reasons)
		}
	}
	if v.Action != domain.SpamFlag {
		t.Fatalf("risky URI should at least be flagged, got %v score=%v", v.Action, v.Score)
	}
}

func TestEngineAppliesAndCapsLearnedSignals(t *testing.T) {
	keys := antispam.SignalKeys("Sender <mail@example.com>", "Quarterly project update")
	weights := signalStore{}
	for _, key := range keys {
		weights[key] = 3
	}
	e := antispam.New(nil, 20, 15, nil)
	e.Signals = weights
	e.SetConfig(antispam.Config{RejectAt: 20, QuarantineAt: 15, FlagAt: 3})
	v := e.Check(context.Background(), antispam.Input{
		From: "Sender <mail@example.com>",
		Headers: map[string]string{
			"Subject":    "Quarterly project update",
			"Message-ID": "<1@example.com>",
			"Date":       "Thu, 16 Jul 2026 00:00:00 +0000",
		},
	})
	if v.Score != 4 {
		t.Fatalf("learned score should be capped at 4, got %v (%+v)", v.Score, v.Reasons)
	}
	if v.Action != domain.SpamFlag {
		t.Fatalf("learned spam signals should flag, got %v", v.Action)
	}

	for key := range weights {
		weights[key] = -3
	}
	v = e.Check(context.Background(), antispam.Input{
		From: "mail@example.com",
		Headers: map[string]string{
			"Subject":    "Quarterly project update",
			"Message-ID": "<2@example.com>",
			"Date":       "Thu, 16 Jul 2026 00:00:00 +0000",
		},
	})
	if v.Score != -4 {
		t.Fatalf("learned ham score should be capped at -4, got %v (%+v)", v.Score, v.Reasons)
	}
}

func TestSignalKeysAreBounded(t *testing.T) {
	keys := antispam.SignalKeys("Sender <mail@Example.COM>", "One two three four five six seven eight nine")
	if len(keys) != 7 {
		t.Fatalf("want domain plus six unique tokens, got %d: %v", len(keys), keys)
	}
	if keys[0] != "from_domain:example.com" {
		t.Fatalf("unexpected domain key: %v", keys)
	}
}
