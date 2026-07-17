package adminapi

import (
	"net"
	"net/http"
	"os"
	"strings"
)

// scrapeAllowed reports whether r may hit unauthenticated scrape endpoints
// (/metrics, detailed /readyz). Loopback is always allowed. Extra CIDRs/IPs come
// from SCRAPE_ALLOW (comma-separated), e.g. "10.0.0.0/8,192.168.1.50".
func scrapeAllowed(r *http.Request) bool {
	ip := net.ParseIP(strings.TrimSpace(r.RemoteAddr))
	if host, _, err := net.SplitHostPort(strings.TrimSpace(r.RemoteAddr)); err == nil {
		ip = net.ParseIP(host)
	}
	if ip == nil {
		return false
	}
	if ip.IsLoopback() {
		return true
	}
	for _, part := range strings.Split(os.Getenv("SCRAPE_ALLOW"), ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		if strings.Contains(part, "/") {
			_, n, err := net.ParseCIDR(part)
			if err == nil && n.Contains(ip) {
				return true
			}
			continue
		}
		if other := net.ParseIP(part); other != nil && other.Equal(ip) {
			return true
		}
	}
	return false
}

func denyScrape(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.WriteHeader(http.StatusForbidden)
	_, _ = w.Write([]byte("forbidden: scrape only from loopback or SCRAPE_ALLOW\n"))
}
