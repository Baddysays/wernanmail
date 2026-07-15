package received

import (
	"fmt"
	"strings"
	"time"
)

// Prepend adds a Received header for our MTA.
func Prepend(raw []byte, helo, remoteIP, hostname, byAuth string) []byte {
	if hostname == "" {
		hostname = "localhost"
	}
	from := "unknown"
	if helo != "" {
		from = helo
	}
	line := fmt.Sprintf(
		"Received: from %s (%s)\r\n\tby %s with ESMTP%s\r\n\tfor local delivery; %s\r\n",
		from,
		remoteIP,
		hostname,
		authNote(byAuth),
		time.Now().Format(time.RFC1123Z),
	)
	return append([]byte(line), normalize(raw)...)
}

func authNote(user string) string {
	user = strings.TrimSpace(user)
	if user == "" {
		return ""
	}
	return " (authenticated as " + user + ")"
}

func normalize(raw []byte) []byte {
	s := string(raw)
	if strings.HasPrefix(s, "\r\n") || strings.HasPrefix(s, "\n") {
		return raw
	}
	return raw
}
