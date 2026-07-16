package antispam

import (
	"fmt"
	"net"
	"strings"
)

// DNSBLResult is one zone lookup for an IPv4 address.
type DNSBLResult struct {
	Zone         string
	Listed       bool
	Inconclusive bool
	Detail       string
}

// QueryDNSBL looks up ip against a single DNSBL zone.
// Spamhaus-style 127.255.255.* answers are treated as inconclusive (open-resolver / policy),
// not as a listing — so public resolvers do not false-positive outbound checks.
func QueryDNSBL(ip, zone string) DNSBLResult {
	zone = strings.TrimSpace(strings.TrimSuffix(zone, "."))
	out := DNSBLResult{Zone: zone}
	if zone == "" {
		out.Inconclusive = true
		out.Detail = "empty zone"
		return out
	}
	parsed := net.ParseIP(ip)
	if parsed == nil {
		out.Inconclusive = true
		out.Detail = "invalid IP"
		return out
	}
	b := parsed.To4()
	if b == nil {
		out.Inconclusive = true
		out.Detail = "IPv6 DNSBL not supported"
		return out
	}
	name := fmt.Sprintf("%d.%d.%d.%d.%s", b[3], b[2], b[1], b[0], zone)
	hosts, err := net.LookupHost(name)
	if err != nil {
		// NXDOMAIN / no answer → clean.
		out.Detail = "not listed"
		return out
	}
	listed, inconclusive, detail := interpretDNSBLA(hosts)
	out.Listed = listed
	out.Inconclusive = inconclusive
	out.Detail = detail
	if listed && detail == "" {
		out.Detail = "listed"
	}
	if !listed && !inconclusive {
		out.Detail = "not listed"
	}
	return out
}

// ListedOnRBL returns the first zone that lists ip, or "" if clean / inconclusive.
func ListedOnRBL(ip string, zones []string) string {
	for _, zone := range zones {
		res := QueryDNSBL(ip, zone)
		if res.Listed {
			return res.Zone
		}
	}
	return ""
}

func interpretDNSBLA(hosts []string) (listed, inconclusive bool, detail string) {
	for _, h := range hosts {
		ip := net.ParseIP(h)
		if ip == nil {
			continue
		}
		v4 := ip.To4()
		if v4 == nil || v4[0] != 127 {
			continue
		}
		// Spamhaus / similar: 127.255.255.x = query error / open resolver / rate limit.
		if v4[1] == 255 && v4[2] == 255 {
			return false, true, fmt.Sprintf("DNSBL query inconclusive (%s)", h)
		}
		// Conventional listing codes live in 127.0.0.2–127.0.0.255.
		if v4[1] == 0 && v4[2] == 0 && v4[3] >= 2 {
			return true, false, h
		}
	}
	return false, false, ""
}
