// Package tlsrpt parses SMTP TLS Reporting (RFC 8460) JSON aggregates.
package tlsrpt

import (
	"archive/zip"
	"bytes"
	"compress/gzip"
	"encoding/base64"
	"encoding/json"
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

type reportDoc struct {
	OrganizationName string `json:"organization-name"`
	DateRange        struct {
		StartDatetime string `json:"start-datetime"`
		EndDatetime   string `json:"end-datetime"`
	} `json:"date-range"`
	ReportID string `json:"report-id"`
	Policies []struct {
		Policy struct {
			PolicyDomain string `json:"policy-domain"`
		} `json:"policy"`
		Summary struct {
			TotalSuccessfulSessionCount int `json:"total-successful-session-count"`
			TotalFailureSessionCount    int `json:"total-failure-session-count"`
		} `json:"summary"`
		FailureDetails []struct {
			ResultType string `json:"result-type"`
		} `json:"failure-details"`
	} `json:"policies"`
}

// ParseMessage extracts TLS-RPT aggregates from JSON / gzip / zip MIME parts.
func ParseMessage(raw []byte) ([]domain.TLSRPTReport, error) {
	msg, err := mail.ReadMessage(bytes.NewReader(raw))
	if err != nil {
		return nil, err
	}
	var out []domain.TLSRPTReport
	if err := walkPart(msg.Header, msg.Body, &out); err != nil {
		return nil, err
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("no TLS-RPT aggregate report")
	}
	return out, nil
}

func walkPart(header mail.Header, body io.Reader, out *[]domain.TLSRPTReport) error {
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
			_ = walkPart(mail.Header(part.Header), part, out)
		}
	}

	name := strings.ToLower(params["name"])
	if _, disp, err := mime.ParseMediaType(header.Get("Content-Disposition")); err == nil && name == "" {
		name = strings.ToLower(disp["filename"])
	}
	kind := strings.ToLower(mediaType + " " + filepath.Ext(name))
	if !strings.Contains(kind, "gzip") && !strings.Contains(kind, ".gz") &&
		!strings.Contains(kind, "zip") && !strings.Contains(kind, "json") && !strings.Contains(kind, ".json") &&
		!strings.Contains(kind, "tlsrpt") {
		return fmt.Errorf("not a TLS-RPT report part")
	}
	r := decodeTransfer(header.Get("Content-Transfer-Encoding"), body)
	data, err := io.ReadAll(io.LimitReader(r, maxPartBytes+1))
	if err != nil || len(data) > maxPartBytes {
		return fmt.Errorf("TLS-RPT part too large")
	}
	switch {
	case strings.Contains(kind, "gzip") || strings.Contains(kind, ".gz"):
		return parseGzip(data, out)
	case strings.Contains(kind, "zip"):
		return parseZip(data, out)
	default:
		return appendJSON(data, out)
	}
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

func parseGzip(data []byte, out *[]domain.TLSRPTReport) error {
	gr, err := gzip.NewReader(bytes.NewReader(data))
	if err != nil {
		return err
	}
	defer gr.Close()
	body, err := io.ReadAll(io.LimitReader(gr, maxPartBytes+1))
	if err != nil || len(body) > maxPartBytes {
		return fmt.Errorf("gzip too large")
	}
	return appendJSON(body, out)
}

func parseZip(data []byte, out *[]domain.TLSRPTReport) error {
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return err
	}
	found := false
	for _, f := range zr.File {
		lower := strings.ToLower(f.Name)
		if (!strings.HasSuffix(lower, ".json") && !strings.Contains(lower, "tlsrpt")) || f.UncompressedSize64 > maxPartBytes {
			continue
		}
		rc, err := f.Open()
		if err != nil {
			continue
		}
		body, readErr := io.ReadAll(io.LimitReader(rc, maxPartBytes+1))
		_ = rc.Close()
		if readErr == nil && len(body) <= maxPartBytes && appendJSON(body, out) == nil {
			found = true
		}
	}
	if !found {
		return fmt.Errorf("zip has no TLS-RPT JSON")
	}
	return nil
}

func appendJSON(data []byte, out *[]domain.TLSRPTReport) error {
	var doc reportDoc
	if err := json.Unmarshal(data, &doc); err != nil {
		return err
	}
	if doc.OrganizationName == "" && doc.ReportID == "" && len(doc.Policies) == 0 {
		return fmt.Errorf("empty TLS-RPT")
	}
	begin := parseTime(doc.DateRange.StartDatetime)
	end := parseTime(doc.DateRange.EndDatetime)
	if len(doc.Policies) == 0 {
		*out = append(*out, domain.TLSRPTReport{
			OrgName: doc.OrganizationName, ReportID: doc.ReportID,
			DateBegin: begin, DateEnd: end,
		})
		return nil
	}
	for _, p := range doc.Policies {
		resultType := ""
		if len(p.FailureDetails) > 0 {
			resultType = p.FailureDetails[0].ResultType
		}
		*out = append(*out, domain.TLSRPTReport{
			OrgName:      doc.OrganizationName,
			ReportID:     doc.ReportID,
			DateBegin:    begin,
			DateEnd:      end,
			PolicyDomain: p.Policy.PolicyDomain,
			SuccessCount: p.Summary.TotalSuccessfulSessionCount,
			FailureCount: p.Summary.TotalFailureSessionCount,
			ResultType:   resultType,
		})
	}
	return nil
}

func parseTime(s string) time.Time {
	s = strings.TrimSpace(s)
	if s == "" {
		return time.Time{}
	}
	for _, layout := range []string{time.RFC3339, time.RFC3339Nano, "2006-01-02T15:04:05Z"} {
		if t, err := time.Parse(layout, s); err == nil {
			return t.UTC()
		}
	}
	return time.Time{}
}
