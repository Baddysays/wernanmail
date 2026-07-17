package antispam

import "testing"

func TestInterpretDNSBLAPrefersListing(t *testing.T) {
	listed, inconcl, detail := interpretDNSBLA([]string{"127.255.255.252", "127.0.0.2"})
	if !listed || inconcl || detail != "127.0.0.2" {
		t.Fatalf("listed=%v inconcl=%v detail=%q", listed, inconcl, detail)
	}
}

func TestInterpretDNSBLAInconclusiveOnly(t *testing.T) {
	listed, inconcl, _ := interpretDNSBLA([]string{"127.255.255.254"})
	if listed || !inconcl {
		t.Fatalf("listed=%v inconcl=%v", listed, inconcl)
	}
}
