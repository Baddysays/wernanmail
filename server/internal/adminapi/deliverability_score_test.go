package adminapi

import "testing"

func TestBuildDeliverabilityRatingPerfect(t *testing.T) {
	dns := dnsSnapshot{
		MX:    checkResult("ok", "mail.example.com"),
		SPF:   checkResult("ok", "v=spf1"),
		DKIM:  checkResult("ok", "published"),
		DMARC: checkResult("ok", "v=DMARC1"),
	}
	ptr := checkResult("ok", "mail.example.com")
	rbl := checkResult("ok", "not listed")
	spam := map[string]any{"state": "ok", "detail": "ready"}
	r := buildDeliverabilityRating(dns, ptr, rbl, spam)
	if r.Score != 10 || r.Verdict != "perfect" {
		t.Fatalf("got score=%v verdict=%s", r.Score, r.Verdict)
	}
}

func TestBuildDeliverabilityRatingMissingDKIM(t *testing.T) {
	dns := dnsSnapshot{
		MX:    checkResult("ok", "x"),
		SPF:   checkResult("ok", "x"),
		DKIM:  checkResult("missing", "no"),
		DMARC: checkResult("ok", "x"),
	}
	ptr := checkResult("ok", "x")
	rbl := checkResult("ok", "x")
	spam := map[string]any{"state": "ok", "detail": "x"}
	r := buildDeliverabilityRating(dns, ptr, rbl, spam)
	if r.Score >= 10 || r.Verdict == "perfect" {
		t.Fatalf("expected less than perfect, got %v %s", r.Score, r.Verdict)
	}
	if r.Score != 8 { // lost 2 DKIM points → 8/10
		t.Fatalf("score=%v want 8", r.Score)
	}
}
