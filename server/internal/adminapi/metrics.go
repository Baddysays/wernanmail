package adminapi

import (
	"net/http"

	"github.com/Baddysays/wernanmail/server/internal/metrics"
)

// metricsHandler exposes Prometheus text metrics from the admin process + store gauges.
// Unauthenticated; restricted to loopback and SCRAPE_ALLOW.
func (h *Handler) metricsHandler(reg *metrics.Registry) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !scrapeAllowed(r) {
			denyScrape(w)
			return
		}
		ctx := r.Context()
		if h.Queue != nil {
			pending, dead, err := h.Queue.Count(ctx)
			if err == nil {
				reg.Set("queue_pending", int64(pending))
				reg.Set("queue_dead", int64(dead))
			}
		}
		if h.Store != nil {
			if n, err := h.Store.CountQuarantine(ctx); err == nil {
				reg.Set("quarantine_open", int64(n))
			}
			if domains, err := h.Store.ListDomains(ctx); err == nil {
				reg.Set("domains", int64(len(domains)))
				var mailboxes int64
				for _, d := range domains {
					mbs, err := h.Store.ListMailboxes(ctx, d.ID)
					if err == nil {
						mailboxes += int64(len(mbs))
					}
				}
				reg.Set("mailboxes", mailboxes)
			}
		}
		hs := collectHostStats(h.Cfg.DataDir)
		reg.Set("host_mail_rss_bytes", int64(hs.MailRSS))
		reg.Set("host_data_bytes", int64(hs.DataBytes))
		reg.Set("host_mail_procs", int64(len(hs.Processes)))

		w.Header().Set("Content-Type", "text/plain; version=0.0.4; charset=utf-8")
		reg.WritePrometheus(w)
	})
}
