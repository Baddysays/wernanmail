package antispam_test

import (
	"testing"

	"github.com/Baddysays/wernanmail/server/internal/antispam"
)

func TestInterpretDNSBLAViaQueryEmptyZone(t *testing.T) {
	res := antispam.QueryDNSBL("1.2.3.4", "")
	if !res.Inconclusive {
		t.Fatalf("empty zone should be inconclusive: %+v", res)
	}
}

func TestListedOnRBLInvalidIP(t *testing.T) {
	if got := antispam.ListedOnRBL("not-an-ip", []string{"zen.spamhaus.org"}); got != "" {
		t.Fatalf("want empty, got %q", got)
	}
}
