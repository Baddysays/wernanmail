package api

import (
	"fmt"
	"net/http"
	"strings"
	"time"
)

func (h *Handler) MTAStsPolicy(w http.ResponseWriter, _ *http.Request) {
	mode := strings.ToLower(strings.TrimSpace(h.Cfg.MTAStsMode))
	if mode != "enforce" && mode != "testing" && mode != "none" {
		mode = "testing"
	}
	mx := strings.TrimSpace(h.Cfg.MTAStsMX)
	if mx == "" {
		mx = strings.TrimSpace(h.Cfg.DefaultIMAPHost)
	}
	maxAge := h.Cfg.MTAStsMaxAge
	if maxAge < 0 {
		maxAge = 0
	}
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Header().Set("Cache-Control", fmt.Sprintf("public, max-age=%d", maxAge))
	_, _ = fmt.Fprintf(w, "version: STSv1\nmode: %s\nmx: %s\nmax_age: %d\n", mode, mx, maxAge)
}

func (h *Handler) MTAStsDNS(w http.ResponseWriter, _ *http.Request) {
	writeData(w, http.StatusOK, map[string]string{
		"name":  "_mta-sts",
		"type":  "TXT",
		"value": "v=STSv1; id=" + time.Now().UTC().Format("20060102"),
		"host":  "Serve the policy at https://mta-sts.<domain>/.well-known/mta-sts.txt",
	})
}
