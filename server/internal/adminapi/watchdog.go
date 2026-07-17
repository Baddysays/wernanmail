package adminapi

import (
	"context"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/Baddysays/wernanmail/server/internal/alerts"
	"github.com/Baddysays/wernanmail/server/internal/domain"
	"github.com/Baddysays/wernanmail/server/internal/settings"
)

// StartWatchdog runs health checks and notifies channels configured in Settings.
// Safe to call once from admin main; exits when ctx is done.
func (h *Handler) StartWatchdog(ctx context.Context) {
	if h.Alerts == nil {
		h.Alerts = alerts.NewWatcher()
	}
	go h.watchdogLoop(ctx)
}

func (h *Handler) watchdogLoop(ctx context.Context) {
	ticker := time.NewTicker(60 * time.Second)
	defer ticker.Stop()
	timer := time.NewTimer(15 * time.Second)
	defer timer.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-timer.C:
			h.watchdogTick(ctx)
		case <-ticker.C:
			h.watchdogTick(ctx)
		}
	}
}

func (h *Handler) watchdogTick(ctx context.Context) {
	if h.Settings == nil {
		return
	}
	_ = h.Settings.Reload(ctx)
	cfg := h.alertConfig()
	if !cfg.Enabled || !cfg.HasChannel() {
		return
	}
	pending, dead, qErr := h.queueCounts(ctx)
	_, missing, stackMode := h.stackProcs()
	qErrStr := ""
	if qErr != nil {
		qErrStr = qErr.Error()
	}
	issues := alerts.CollectIssues(alerts.Snapshot{
		StackMode:    stackMode,
		StackMissing: missing,
		QueuePending: pending,
		QueueDead:    dead,
		QueueErr:     qErrStr,
		Host:         cfg.EHLO,
	}, cfg.PendingWarn)
	if len(issues) == 0 {
		return
	}
	tickCtx, cancel := context.WithTimeout(ctx, 25*time.Second)
	defer cancel()
	for _, err := range h.Alerts.Notify(tickCtx, cfg, issues) {
		log.Printf("alerts: %v", err)
	}
}

func (h *Handler) alertConfig() alerts.Config {
	host := strings.TrimSpace(h.Cfg.Hostname)
	from := "alerts@localhost"
	if host != "" && !strings.Contains(host, " ") {
		from = "alerts@" + host
	}
	ehlo := strings.TrimSpace(h.Cfg.EHLOHost)
	if ehlo == "" {
		ehlo = host
	}
	relay := strings.TrimSpace(h.Cfg.RelayHost)
	if relay == "" && h.Settings != nil {
		relay = h.Settings.Get(settings.KeyRelayHost)
	}
	return alerts.ConfigFromSettings(h.Settings, from, ehlo, relay)
}

func (h *Handler) testAlerts(w http.ResponseWriter, r *http.Request) {
	_ = h.Settings.Reload(r.Context())
	cfg := h.alertConfig()
	if !cfg.HasChannel() {
		writeErr(w, http.StatusBadRequest, "alerts.no_channel", "configure email, Telegram, or webhook first")
		return
	}
	if h.Alerts == nil {
		h.Alerts = alerts.NewWatcher()
	}
	ctx, cancel := context.WithTimeout(r.Context(), 25*time.Second)
	defer cancel()
	if err := h.Alerts.NotifyTest(ctx, cfg); err != nil {
		writeErr(w, http.StatusBadGateway, "alerts.send_failed", err.Error())
		return
	}
	_ = h.Store.AddAudit(r.Context(), &domain.AuditEntry{
		Actor: h.actor(r), Action: "alerts.test", Detail: "ok",
	})
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}
