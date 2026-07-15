package dmarc

import (
	"bytes"
	"compress/gzip"
	"encoding/base64"
	"strings"
	"testing"
)

const aggregateXML = `<?xml version="1.0"?>
<feedback>
  <report_metadata>
    <org_name>Example Receiver</org_name>
    <report_id>report-42</report_id>
    <date_range><begin>1700000000</begin><end>1700086400</end></date_range>
  </report_metadata>
  <record>
    <row>
      <source_ip>192.0.2.10</source_ip>
      <count>7</count>
      <policy_evaluated><disposition>quarantine</disposition><dkim>pass</dkim><spf>fail</spf></policy_evaluated>
    </row>
  </record>
</feedback>`

func TestParseMessageGzipAggregate(t *testing.T) {
	var compressed bytes.Buffer
	zw := gzip.NewWriter(&compressed)
	if _, err := zw.Write([]byte(aggregateXML)); err != nil {
		t.Fatal(err)
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	encoded := base64.StdEncoding.EncodeToString(compressed.Bytes())
	raw := "From: reports@example.net\r\n" +
		"To: postmaster@example.com\r\n" +
		"Subject: DMARC aggregate report\r\n" +
		"MIME-Version: 1.0\r\n" +
		"Content-Type: application/gzip; name=\"report.xml.gz\"\r\n" +
		"Content-Transfer-Encoding: base64\r\n\r\n" +
		wrapBase64(encoded) + "\r\n"

	reports, err := ParseMessage([]byte(raw))
	if err != nil {
		t.Fatal(err)
	}
	if len(reports) != 1 {
		t.Fatalf("got %d records, want 1", len(reports))
	}
	got := reports[0]
	if got.OrgName != "Example Receiver" || got.ReportID != "report-42" ||
		got.SourceIP != "192.0.2.10" || got.Count != 7 ||
		got.DKIMResult != "pass" || got.SPFResult != "fail" || got.Disposition != "quarantine" {
		t.Fatalf("unexpected report: %+v", got)
	}
	if got.DateEnd.Unix()-got.DateBegin.Unix() != 86400 {
		t.Fatalf("unexpected date range: %v - %v", got.DateBegin, got.DateEnd)
	}
}

func TestParseMessageSkipsOrdinaryMail(t *testing.T) {
	_, err := ParseMessage([]byte("From: a@example.com\r\nSubject: hello\r\n\r\nplain text"))
	if err == nil {
		t.Fatal("expected non-report error")
	}
}

func wrapBase64(s string) string {
	var lines []string
	for len(s) > 76 {
		lines = append(lines, s[:76])
		s = s[76:]
	}
	lines = append(lines, s)
	return strings.Join(lines, "\r\n")
}
