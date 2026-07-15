package antivirus_test

import (
	"context"
	"strings"
	"testing"

	"github.com/Baddysays/wernanmail/server/internal/antivirus"
)

func TestLightAllowsHTMLBodyAndInlineImage(t *testing.T) {
	raw := []byte(strings.ReplaceAll(`From: a@example.com
To: b@example.com
Subject: hello
MIME-Version: 1.0
Content-Type: multipart/related; boundary="b1"

--b1
Content-Type: text/html; charset=utf-8
Content-Disposition: inline

<html><body><p>Hello .exe is just text</p><img src="cid:logo"></body></html>
--b1
Content-Type: image/png
Content-Disposition: inline; filename="logo.png"
Content-ID: <logo>

iVBORw0KGgo=
--b1--
`, "\n", "\r\n"))

	res, err := (antivirus.Light{}).Scan(context.Background(), raw, "")
	if err != nil {
		t.Fatal(err)
	}
	if !res.Clean {
		t.Fatalf("HTML+inline image must stay clean: %+v", res)
	}
}

func TestLightBlocksExeAttachment(t *testing.T) {
	raw := []byte(strings.ReplaceAll(`From: a@example.com
To: b@example.com
Subject: invoice
MIME-Version: 1.0
Content-Type: multipart/mixed; boundary="b1"

--b1
Content-Type: text/html; charset=utf-8

<html><body>See attached</body></html>
--b1
Content-Type: application/octet-stream
Content-Disposition: attachment; filename="invoice.pdf.exe"

MZ............
--b1--
`, "\n", "\r\n"))

	res, err := (antivirus.Light{}).Scan(context.Background(), raw, "")
	if err != nil {
		t.Fatal(err)
	}
	if res.Clean || res.Name != "blocked_attachment" {
		t.Fatalf("want blocked_attachment, got %+v", res)
	}
	if res.PreferQuarantine {
		t.Fatal("exe should hard-reject, not quarantine")
	}
}

func TestLightQuarantinesHTMLAttachment(t *testing.T) {
	raw := []byte(strings.ReplaceAll(`From: a@example.com
To: b@example.com
Subject: click
MIME-Version: 1.0
Content-Type: multipart/mixed; boundary="b1"

--b1
Content-Type: text/plain

see file
--b1
Content-Type: text/html
Content-Disposition: attachment; filename="login.html"

<html><body>phish</body></html>
--b1--
`, "\n", "\r\n"))

	res, err := (antivirus.Light{}).Scan(context.Background(), raw, "")
	if err != nil {
		t.Fatal(err)
	}
	if res.Clean || !res.PreferQuarantine {
		t.Fatalf("want quarantine html attachment, got %+v", res)
	}
}

func TestLightIgnoresExeMentionInBody(t *testing.T) {
	raw := []byte(strings.ReplaceAll(`From: a@example.com
To: b@example.com
Subject: docs
Content-Type: text/html; charset=utf-8

<html><body>Download setup.exe from our site — filename=".exe" mention only</body></html>
`, "\n", "\r\n"))

	res, err := (antivirus.Light{}).Scan(context.Background(), raw, "Subject with .exe")
	if err != nil {
		t.Fatal(err)
	}
	if !res.Clean {
		t.Fatalf("body text must not trigger: %+v", res)
	}
}
