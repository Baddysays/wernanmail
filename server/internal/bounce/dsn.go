package bounce

import (
	"fmt"
	"strings"
	"time"
)

// BuildDSN creates a simple RFC 3464-ish delivery status notification.
func BuildDSN(originalFrom, failedTo, hostname, reason string, original []byte) []byte {
	if hostname == "" {
		hostname = "localhost"
	}
	now := time.Now().Format(time.RFC1123Z)
	id := fmt.Sprintf("%d@%s", time.Now().UnixNano(), hostname)
	boundary := "wernan-dsn-boundary"

	var b strings.Builder
	b.WriteString("From: Mail Delivery System <MAILER-DAEMON@" + hostname + ">\r\n")
	b.WriteString("To: " + originalFrom + "\r\n")
	b.WriteString("Subject: Undelivered Mail Returned to Sender\r\n")
	b.WriteString("Date: " + now + "\r\n")
	b.WriteString("Message-ID: <" + id + ">\r\n")
	b.WriteString("MIME-Version: 1.0\r\n")
	b.WriteString("Content-Type: multipart/report; report-type=delivery-status; boundary=\"" + boundary + "\"\r\n")
	b.WriteString("Auto-Submitted: auto-replied\r\n")
	b.WriteString("\r\n")

	b.WriteString("--" + boundary + "\r\n")
	b.WriteString("Content-Type: text/plain; charset=UTF-8\r\n\r\n")
	b.WriteString("This is the mail system at " + hostname + ".\r\n\r\n")
	b.WriteString("Your message could not be delivered to " + failedTo + ".\r\n")
	b.WriteString("Reason: " + sanitize(reason) + "\r\n\r\n")

	b.WriteString("--" + boundary + "\r\n")
	b.WriteString("Content-Type: message/delivery-status\r\n\r\n")
	b.WriteString("Reporting-MTA: dns; " + hostname + "\r\n")
	b.WriteString("Arrival-Date: " + now + "\r\n\r\n")
	b.WriteString("Final-Recipient: rfc822; " + failedTo + "\r\n")
	b.WriteString("Action: failed\r\n")
	b.WriteString("Status: 5.0.0\r\n")
	b.WriteString("Diagnostic-Code: smtp; " + sanitize(reason) + "\r\n\r\n")

	b.WriteString("--" + boundary + "\r\n")
	b.WriteString("Content-Type: message/rfc822\r\n\r\n")
	if len(original) > 0 {
		// Cap attached original to keep bounce small.
		const max = 64 << 10
		if len(original) > max {
			b.Write(original[:max])
			b.WriteString("\r\n\r\n[truncated]\r\n")
		} else {
			b.Write(original)
		}
	}
	b.WriteString("\r\n--" + boundary + "--\r\n")
	return []byte(b.String())
}

func sanitize(s string) string {
	s = strings.ReplaceAll(s, "\r", " ")
	s = strings.ReplaceAll(s, "\n", " ")
	if len(s) > 400 {
		s = s[:400] + "…"
	}
	return s
}
