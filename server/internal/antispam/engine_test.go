package antispam_test

import (
	"context"
	"testing"

	"github.com/Baddysays/wernanmail/server/internal/antispam"
	"github.com/Baddysays/wernanmail/server/internal/domain"
)

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
