package outbound

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os"
	"strings"
	"time"
)

// EnsureRFCHeaders adds Date and Message-ID when clients omit them (common spam hit).
func EnsureRFCHeaders(raw []byte, idHost string) []byte {
	if len(raw) == 0 {
		return raw
	}
	s := string(raw)
	// Split header block (support both CRLF and LF).
	sep := "\r\n\r\n"
	idx := strings.Index(s, sep)
	if idx < 0 {
		sep = "\n\n"
		idx = strings.Index(s, sep)
	}
	var headers, body string
	if idx < 0 {
		headers = s
		body = ""
		sep = "\r\n\r\n"
	} else {
		headers = s[:idx]
		body = s[idx+len(sep):]
	}
	headers = strings.ReplaceAll(headers, "\r\n", "\n")
	headers = strings.ReplaceAll(headers, "\r", "\n")

	var inject []string
	if !hasHeader(headers, "date") {
		inject = append(inject, "Date: "+time.Now().Format(time.RFC1123Z))
	}
	if !hasHeader(headers, "message-id") {
		inject = append(inject, "Message-ID: <"+newMessageID(idHost)+">")
	}
	if len(inject) == 0 {
		return normalizeCRLF(raw)
	}
	out := strings.Join(inject, "\r\n") + "\r\n" + strings.ReplaceAll(headers, "\n", "\r\n") + "\r\n\r\n" + body
	return []byte(out)
}

func hasHeader(headers, name string) bool {
	name = strings.ToLower(name)
	for _, line := range strings.Split(headers, "\n") {
		if strings.HasPrefix(strings.ToLower(line), name+":") {
			return true
		}
	}
	return false
}

func newMessageID(host string) string {
	host = strings.TrimSpace(host)
	if host == "" {
		host = "localhost"
	}
	var b [8]byte
	_, _ = rand.Read(b[:])
	return fmt.Sprintf("%d.%s.%d@%s", time.Now().UnixNano(), hex.EncodeToString(b[:]), os.Getpid(), host)
}

func normalizeCRLF(raw []byte) []byte {
	s := strings.ReplaceAll(string(raw), "\r\n", "\n")
	s = strings.ReplaceAll(s, "\r", "\n")
	return []byte(strings.ReplaceAll(s, "\n", "\r\n"))
}
