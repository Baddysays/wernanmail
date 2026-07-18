package tlsrpt

import (
	"bytes"
	"compress/gzip"
	"encoding/base64"
	"testing"
	"time"
)

func TestParseMessageJSON(t *testing.T) {
	payload := `{
  "organization-name": "Example Org",
  "date-range": {"start-datetime":"2026-07-01T00:00:00Z","end-datetime":"2026-07-02T00:00:00Z"},
  "report-id": "rpt-1",
  "policies": [{
    "policy": {"policy-domain":"wernanmail.ru"},
    "summary": {"total-successful-session-count":10,"total-failure-session-count":1},
    "failure-details": [{"result-type":"starttls-not-supported"}]
  }]
}`
	var buf bytes.Buffer
	zw := gzip.NewWriter(&buf)
	_, _ = zw.Write([]byte(payload))
	_ = zw.Close()
	raw := "From: noreply@example.com\r\n" +
		"To: postmaster@wernanmail.ru\r\n" +
		"Subject: Report Domain: wernanmail.ru\r\n" +
		"MIME-Version: 1.0\r\n" +
		"Content-Type: application/tlsrpt+gzip; name=\"report.json.gz\"\r\n" +
		"Content-Transfer-Encoding: base64\r\n" +
		"Content-Disposition: attachment; filename=\"report.json.gz\"\r\n" +
		"\r\n" +
		base64.StdEncoding.EncodeToString(buf.Bytes()) + "\r\n"
	reports, err := ParseMessage([]byte(raw))
	if err != nil {
		t.Fatal(err)
	}
	if len(reports) != 1 {
		t.Fatalf("got %d", len(reports))
	}
	r := reports[0]
	if r.OrgName != "Example Org" || r.PolicyDomain != "wernanmail.ru" || r.SuccessCount != 10 || r.FailureCount != 1 {
		t.Fatalf("%+v", r)
	}
	if r.DateBegin.Year() != 2026 || r.DateEnd.Sub(r.DateBegin) < time.Hour {
		t.Fatalf("dates %+v %+v", r.DateBegin, r.DateEnd)
	}
}

func TestParseSkipsOrdinary(t *testing.T) {
	_, err := ParseMessage([]byte("From: a@b.c\r\nSubject: hi\r\n\r\nplain"))
	if err == nil {
		t.Fatal("expected error")
	}
}
