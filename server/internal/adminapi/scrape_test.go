package adminapi

import (
	"net/http/httptest"
	"testing"
)

func TestScrapeAllowedLoopback(t *testing.T) {
	r := httptest.NewRequest("GET", "/metrics", nil)
	r.RemoteAddr = "127.0.0.1:12345"
	if !scrapeAllowed(r) {
		t.Fatal("loopback should be allowed")
	}
}

func TestScrapeAllowedDenied(t *testing.T) {
	t.Setenv("SCRAPE_ALLOW", "")
	r := httptest.NewRequest("GET", "/metrics", nil)
	r.RemoteAddr = "8.8.8.8:9999"
	if scrapeAllowed(r) {
		t.Fatal("public IP should be denied by default")
	}
}

func TestScrapeAllowedCIDR(t *testing.T) {
	t.Setenv("SCRAPE_ALLOW", "10.0.0.0/8")
	r := httptest.NewRequest("GET", "/metrics", nil)
	r.RemoteAddr = "10.1.2.3:80"
	if !scrapeAllowed(r) {
		t.Fatal("CIDR should allow")
	}
}
