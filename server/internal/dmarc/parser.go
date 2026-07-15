// Package dmarc parses aggregate feedback attachments.
package dmarc

import (
	"archive/zip"
	"bytes"
	"compress/gzip"
	"encoding/base64"
	"encoding/xml"
	"fmt"
	"io"
	"mime"
	"mime/multipart"
	"mime/quotedprintable"
	"net/mail"
	"path/filepath"
	"strings"
	"time"

	"github.com/Baddysays/wernanmail/server/internal/domain"
)

const maxPartBytes = 10 << 20

// ParseMessage returns aggregate records found in XML, gzip, or zip MIME parts.
func ParseMessage(raw []byte) ([]domain.DMARCReport, error) {
	msg, err := mail.ReadMessage(bytes.NewReader(raw))
	if err != nil {
		return nil, err
	}
	var out []domain.DMARCReport
	if err := walkPart(msg.Header, msg.Body, &out); err != nil {
		return nil, err
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("no DMARC aggregate report")
	}
	return out, nil
}

func walkPart(header mail.Header, body io.Reader, out *[]domain.DMARCReport) error {
	mediaType, params, _ := mime.ParseMediaType(header.Get("Content-Type"))
	if strings.HasPrefix(strings.ToLower(mediaType), "multipart/") {
		mr := multipart.NewReader(body, params["boundary"])
		for {
			part, err := mr.NextPart()
			if err == io.EOF {
				return nil
			}
			if err != nil {
				return err
			}
			if err := walkPart(mail.Header(part.Header), part, out); err != nil {
				// Non-report or malformed sibling attachments must not hide a valid report.
				continue
			}
		}
	}

	name := strings.ToLower(params["name"])
	if _, disp, err := mime.ParseMediaType(header.Get("Content-Disposition")); err == nil && name == "" {
		name = strings.ToLower(disp["filename"])
	}
	kind := strings.ToLower(mediaType + " " + filepath.Ext(name))
	if !strings.Contains(kind, "gzip") && !strings.Contains(kind, ".gz") &&
		!strings.Contains(kind, "zip") && !strings.Contains(kind, "xml") && !strings.Contains(kind, ".xml") {
		return fmt.Errorf("not a DMARC report part")
	}
	r := decodeTransfer(header.Get("Content-Transfer-Encoding"), body)
	data, err := io.ReadAll(io.LimitReader(r, maxPartBytes+1))
	if err != nil || len(data) > maxPartBytes {
		return fmt.Errorf("DMARC part too large")
	}
	switch {
	case strings.Contains(kind, "gzip") || strings.Contains(kind, ".gz"):
		return parseGzip(data, out)
	case strings.Contains(kind, "zip"):
		return parseZip(data, out)
	case strings.Contains(kind, "xml") || strings.Contains(kind, ".xml"):
		return appendXML(data, out)
	}
	return fmt.Errorf("not a DMARC report part")
}

func decodeTransfer(encoding string, r io.Reader) io.Reader {
	switch strings.ToLower(strings.TrimSpace(encoding)) {
	case "base64":
		return base64.NewDecoder(base64.StdEncoding, r)
	case "quoted-printable":
		return quotedprintable.NewReader(r)
	default:
		return r
	}
}

func parseZip(data []byte, out *[]domain.DMARCReport) error {
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return err
	}
	found := false
	for _, f := range zr.File {
		if !strings.HasSuffix(strings.ToLower(f.Name), ".xml") || f.UncompressedSize64 > maxPartBytes {
			continue
		}
		rc, err := f.Open()
		if err != nil {
			continue
		}
		b, readErr := io.ReadAll(io.LimitReader(rc, maxPartBytes+1))
		_ = rc.Close()
		if readErr == nil && len(b) <= maxPartBytes && appendXML(b, out) == nil {
			found = true
		}
	}
	if !found {
		return fmt.Errorf("zip has no aggregate XML")
	}
	return nil
}

func parseGzip(data []byte, out *[]domain.DMARCReport) error {
	gr, err := gzip.NewReader(bytes.NewReader(data))
	if err != nil {
		return err
	}
	defer gr.Close()
	b, err := io.ReadAll(io.LimitReader(gr, maxPartBytes+1))
	if err != nil || len(b) > maxPartBytes {
		return fmt.Errorf("gzip DMARC report too large")
	}
	return appendXML(b, out)
}

type feedback struct {
	ReportMetadata struct {
		OrgName   string `xml:"org_name"`
		ReportID  string `xml:"report_id"`
		DateRange struct {
			Begin int64 `xml:"begin"`
			End   int64 `xml:"end"`
		} `xml:"date_range"`
	} `xml:"report_metadata"`
	Records []struct {
		Row struct {
			SourceIP   string `xml:"source_ip"`
			Count      int    `xml:"count"`
			PolicyEval struct {
				Disposition string `xml:"disposition"`
				DKIM        string `xml:"dkim"`
				SPF         string `xml:"spf"`
			} `xml:"policy_evaluated"`
		} `xml:"row"`
	} `xml:"record"`
}

func appendXML(data []byte, out *[]domain.DMARCReport) error {
	var f feedback
	if err := xml.Unmarshal(data, &f); err != nil {
		return err
	}
	if strings.TrimSpace(f.ReportMetadata.OrgName) == "" || len(f.Records) == 0 {
		return fmt.Errorf("not aggregate DMARC feedback")
	}
	for _, rec := range f.Records {
		*out = append(*out, domain.DMARCReport{
			OrgName: strings.TrimSpace(f.ReportMetadata.OrgName), ReportID: strings.TrimSpace(f.ReportMetadata.ReportID),
			DateBegin: time.Unix(f.ReportMetadata.DateRange.Begin, 0).UTC(),
			DateEnd:   time.Unix(f.ReportMetadata.DateRange.End, 0).UTC(),
			SourceIP:  strings.TrimSpace(rec.Row.SourceIP), Count: rec.Row.Count,
			DKIMResult:  strings.TrimSpace(rec.Row.PolicyEval.DKIM),
			SPFResult:   strings.TrimSpace(rec.Row.PolicyEval.SPF),
			Disposition: strings.TrimSpace(rec.Row.PolicyEval.Disposition),
		})
	}
	return nil
}
