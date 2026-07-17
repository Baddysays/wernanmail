package adminapi

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
	"unicode"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/go-chi/cors"
	"golang.org/x/crypto/bcrypt"

	"github.com/Baddysays/wernanmail/server/internal/antispam"
	"github.com/Baddysays/wernanmail/server/internal/dnsauth"
	"github.com/Baddysays/wernanmail/server/internal/domain"
	"github.com/Baddysays/wernanmail/server/internal/impersonate"
	"github.com/Baddysays/wernanmail/server/internal/mailcfg"
	"github.com/Baddysays/wernanmail/server/internal/mailfilter"
	"github.com/Baddysays/wernanmail/server/internal/metrics"
	"github.com/Baddysays/wernanmail/server/internal/settings"
	"github.com/Baddysays/wernanmail/server/internal/store"
	"github.com/Baddysays/wernanmail/server/internal/store/sqlite"
)

// Handler serves the admin REST API.
type Handler struct {
	Cfg      mailcfg.Config
	Store    *sqlite.Store
	Settings *settings.Manager
	Queue    store.QueueStore
	Tokens   *TokenStore
}

func NewRouter(h *Handler) http.Handler {
	if h.Tokens == nil {
		h.Tokens = NewTokenStore(12 * time.Hour)
	}
	origins := h.Cfg.AdminCORS
	if len(origins) == 0 {
		origins = []string{"http://localhost:5174"}
	}
	r := chi.NewRouter()
	r.Use(middleware.RequestID, middleware.RealIP, middleware.Recoverer)
	r.Use(cors.Handler(cors.Options{
		AllowedOrigins:   origins,
		AllowedMethods:   []string{"GET", "POST", "PUT", "PATCH", "DELETE", "OPTIONS"},
		AllowedHeaders:   []string{"Accept", "Authorization", "Content-Type"},
		AllowCredentials: true,
	}))
	r.Get("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, map[string]any{"status": "ok"})
	})
	r.Get("/readyz", h.readyz)
	// Unauthenticated scrape target — keep admin bind private / firewalled.
	r.Handle("/metrics", h.metricsHandler(metrics.New("admin")))
	r.Post("/api/admin/login", h.login)
	r.Post("/api/admin/logout", h.logout)
	r.Group(func(r chi.Router) {
		r.Use(h.auth)
		r.Get("/api/admin/dashboard", h.dashboard)
		r.Get("/api/admin/posture", h.posture)
		r.Get("/api/admin/domains", h.listDomains)
		r.Post("/api/admin/domains", h.createDomain)
		r.Patch("/api/admin/domains/{id}", h.updateDomain)
		r.Delete("/api/admin/domains/{id}", h.deleteDomain)
		r.Post("/api/admin/domains/{id}/dkim", h.genDKIM)
		r.Get("/api/admin/domains/{id}/mailboxes", h.listMailboxes)
		r.Post("/api/admin/domains/{id}/mailboxes", h.createMailbox)
		r.Patch("/api/admin/domains/{id}/mailboxes/{mid}", h.updateMailbox)
		r.Delete("/api/admin/domains/{id}/mailboxes/{mid}", h.deleteMailbox)
		r.Post("/api/admin/domains/{id}/mailboxes/{mid}/impersonate", h.impersonateMailbox)
		r.Get("/api/admin/domains/{id}/aliases", h.listAliases)
		r.Post("/api/admin/domains/{id}/aliases", h.createAlias)
		r.Delete("/api/admin/domains/{id}/aliases/{aid}", h.deleteAlias)
		r.Get("/api/admin/queue", h.listQueue)
		r.Post("/api/admin/queue/{id}/retry", h.retryQueue)
		r.Post("/api/admin/queue/{id}/delete", h.deleteQueue)
		r.Get("/api/admin/backup", h.backup)
		r.Get("/api/admin/backup/full", h.backupFull)
		r.Post("/api/admin/backup/restore", h.restoreBackup)
		r.Get("/api/admin/ops", h.opsStatus)
		r.Get("/api/admin/dns-status", h.dnsStatus)
		r.Get("/api/admin/host-stats", h.hostStats)
		r.Get("/api/admin/quarantine", h.listQuarantine)
		r.Post("/api/admin/quarantine/{id}/release", h.releaseQuarantine)
		r.Post("/api/admin/quarantine/{id}/delete", h.deleteQuarantine)
		r.Get("/api/admin/dmarc-reports", h.listDMARCReports)
		r.Get("/api/admin/mailboxes/{id}/filters", h.listMailboxFilters)
		r.Put("/api/admin/mailboxes/{id}/filters", h.putMailboxFilters)
		r.Get("/api/admin/settings", h.getSettings)
		r.Put("/api/admin/settings", h.putSettings)
		r.Get("/api/admin/audit", h.listAudit)
	})
	return r
}

func (h *Handler) auth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if tok := bearerToken(r); tok != "" {
			if user, ok := h.Tokens.Validate(tok); ok {
				ctx := context.WithValue(r.Context(), adminUserKey{}, user)
				next.ServeHTTP(w, r.WithContext(ctx))
				return
			}
		}
		user, pass, ok := r.BasicAuth()
		if ok && h.Cfg.CheckAdminPassword(user, pass) {
			ctx := context.WithValue(r.Context(), adminUserKey{}, user)
			next.ServeHTTP(w, r.WithContext(ctx))
			return
		}
		w.Header().Set("WWW-Authenticate", `Bearer realm="wernanmail-admin"`)
		writeErr(w, http.StatusUnauthorized, "admin.unauthorized", "unauthorized")
	})
}

type adminUserKey struct{}

func bearerToken(r *http.Request) string {
	h := r.Header.Get("Authorization")
	if len(h) > 7 && strings.EqualFold(h[:7], "Bearer ") {
		return strings.TrimSpace(h[7:])
	}
	return ""
}

func (h *Handler) actor(r *http.Request) string {
	if u, ok := r.Context().Value(adminUserKey{}).(string); ok && u != "" {
		return u
	}
	return h.Cfg.AdminUser
}

func (h *Handler) login(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeErr(w, http.StatusBadRequest, "admin.bad_request", "bad json")
		return
	}
	if !h.Cfg.CheckAdminPassword(body.Username, body.Password) {
		writeErr(w, http.StatusUnauthorized, "admin.unauthorized", "invalid credentials")
		return
	}
	tok, err := h.Tokens.Issue(body.Username)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "admin.token", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "token": tok, "username": body.Username})
}

func (h *Handler) logout(w http.ResponseWriter, r *http.Request) {
	if tok := bearerToken(r); tok != "" {
		h.Tokens.Revoke(tok)
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (h *Handler) dashboard(w http.ResponseWriter, r *http.Request) {
	pending, dead, _ := h.Queue.Count(r.Context())
	qCount, _ := h.Store.CountQuarantine(r.Context())
	domains, _ := h.Store.ListDomains(r.Context())
	writeJSON(w, http.StatusOK, map[string]any{
		"queuePending": pending,
		"queueDead":    dead,
		"quarantine":   qCount,
		"domains":      len(domains),
	})
}

func (h *Handler) hostStats(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, collectHostStats(h.Cfg.DataDir))
}

func (h *Handler) listDomains(w http.ResponseWriter, r *http.Request) {
	list, err := h.Store.ListDomains(r.Context())
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "admin.store", err.Error())
		return
	}
	if list == nil {
		list = []domain.Domain{}
	}
	writeJSON(w, http.StatusOK, list)
}

func (h *Handler) createDomain(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Name              string `json:"name"`
		CatchAll          string `json:"catchAll"`
		DefaultQuotaBytes *int64 `json:"defaultQuotaBytes"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || strings.TrimSpace(body.Name) == "" {
		writeErr(w, http.StatusBadRequest, "admin.bad_request", "name required")
		return
	}
	d := &domain.Domain{Name: strings.ToLower(strings.TrimSpace(body.Name)), Enabled: true, CatchAll: body.CatchAll}
	if body.DefaultQuotaBytes != nil {
		d.DefaultQuotaBytes = *body.DefaultQuotaBytes
	}
	if err := h.Store.UpsertDomain(r.Context(), d); err != nil {
		writeErr(w, http.StatusInternalServerError, "admin.store", err.Error())
		return
	}
	_ = h.Store.AddAudit(r.Context(), &domain.AuditEntry{Actor: h.actor(r), Action: "domain.create", Target: d.Name})
	writeJSON(w, http.StatusCreated, d)
}

func (h *Handler) findDomain(ctx context.Context, id int64) (*domain.Domain, error) {
	domains, err := h.Store.ListDomains(ctx)
	if err != nil {
		return nil, err
	}
	for i := range domains {
		if domains[i].ID == id {
			return &domains[i], nil
		}
	}
	return nil, nil
}

func (h *Handler) updateDomain(w http.ResponseWriter, r *http.Request) {
	id, _ := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	d, err := h.findDomain(r.Context(), id)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "admin.store", err.Error())
		return
	}
	if d == nil {
		writeErr(w, http.StatusNotFound, "admin.not_found", "domain not found")
		return
	}
	var body struct {
		Enabled           *bool   `json:"enabled"`
		CatchAll          *string `json:"catchAll"`
		DefaultQuotaBytes *int64  `json:"defaultQuotaBytes"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeErr(w, http.StatusBadRequest, "admin.bad_request", "bad json")
		return
	}
	if body.Enabled != nil {
		d.Enabled = *body.Enabled
	}
	if body.CatchAll != nil {
		d.CatchAll = strings.TrimSpace(*body.CatchAll)
	}
	if body.DefaultQuotaBytes != nil {
		if *body.DefaultQuotaBytes < 0 {
			writeErr(w, http.StatusBadRequest, "admin.bad_request", "defaultQuotaBytes must be >= 0")
			return
		}
		d.DefaultQuotaBytes = *body.DefaultQuotaBytes
	}
	if err := h.Store.UpsertDomain(r.Context(), d); err != nil {
		writeErr(w, http.StatusInternalServerError, "admin.store", err.Error())
		return
	}
	_ = h.Store.AddAudit(r.Context(), &domain.AuditEntry{Actor: h.actor(r), Action: "domain.update", Target: d.Name})
	writeJSON(w, http.StatusOK, d)
}

func (h *Handler) deleteDomain(w http.ResponseWriter, r *http.Request) {
	id, _ := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	d, err := h.findDomain(r.Context(), id)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "admin.store", err.Error())
		return
	}
	if d == nil {
		writeErr(w, http.StatusNotFound, "admin.not_found", "domain not found")
		return
	}
	if err := h.Store.DeleteDomain(r.Context(), id); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			writeErr(w, http.StatusNotFound, "admin.not_found", "domain not found")
			return
		}
		writeErr(w, http.StatusInternalServerError, "admin.store", err.Error())
		return
	}
	_ = h.Store.AddAudit(r.Context(), &domain.AuditEntry{Actor: h.actor(r), Action: "domain.delete", Target: d.Name})
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (h *Handler) genDKIM(w http.ResponseWriter, r *http.Request) {
	id, _ := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	d, err := h.findDomain(r.Context(), id)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "admin.store", err.Error())
		return
	}
	if d == nil {
		writeErr(w, http.StatusNotFound, "admin.not_found", "domain not found")
		return
	}
	kp, err := dnsauth.GenerateDKIM(d.DKIMSelector)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "admin.dkim", err.Error())
		return
	}
	d.DKIMPrivate = kp.PrivatePEM
	d.DKIMPublic = kp.PublicDNS
	d.DKIMSelector = kp.Selector
	if err := h.Store.UpsertDomain(r.Context(), d); err != nil {
		writeErr(w, http.StatusInternalServerError, "admin.store", err.Error())
		return
	}
	_ = h.Store.AddAudit(r.Context(), &domain.AuditEntry{Actor: h.actor(r), Action: "domain.dkim", Target: d.Name})
	writeJSON(w, http.StatusOK, map[string]any{
		"selector": d.DKIMSelector,
		"dnsName":  d.DKIMSelector + "._domainkey." + d.Name,
		"dnsValue": d.DKIMPublic,
	})
}

func (h *Handler) listMailboxes(w http.ResponseWriter, r *http.Request) {
	id, _ := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	list, err := h.Store.ListMailboxes(r.Context(), id)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "admin.store", err.Error())
		return
	}
	// strip hashes
	out := make([]map[string]any, 0, len(list))
	for _, m := range list {
		used, _ := h.Store.UsageBytes(r.Context(), m.ID)
		out = append(out, map[string]any{
			"id": m.ID, "domainId": m.DomainID, "localPart": m.LocalPart,
			"displayName": m.DisplayName, "quotaBytes": m.QuotaBytes, "usedBytes": used, "enabled": m.Enabled,
			"createdAt": m.CreatedAt,
		})
	}
	writeJSON(w, http.StatusOK, out)
}

func (h *Handler) createMailbox(w http.ResponseWriter, r *http.Request) {
	domainID, _ := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	var body struct {
		LocalPart   string `json:"localPart"`
		Password    string `json:"password"`
		DisplayName string `json:"displayName"`
		QuotaBytes  *int64 `json:"quotaBytes"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.LocalPart == "" || body.Password == "" {
		writeErr(w, http.StatusBadRequest, "admin.bad_request", "localPart and password required")
		return
	}
	if msg := h.passwordPolicyError(body.Password); msg != "" {
		writeErr(w, http.StatusBadRequest, "admin.weak_password", msg)
		return
	}
	d, err := h.findDomain(r.Context(), domainID)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "admin.store", err.Error())
		return
	}
	if d == nil {
		writeErr(w, http.StatusNotFound, "admin.not_found", "domain not found")
		return
	}
	hash, err := sqlite.HashPassword(body.Password)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "admin.hash", err.Error())
		return
	}
	quota := int64(0)
	if body.QuotaBytes != nil {
		quota = *body.QuotaBytes
	} else if d.DefaultQuotaBytes > 0 {
		quota = d.DefaultQuotaBytes
	} else {
		quota = int64(h.Settings.GetInt(settings.KeyDefaultQuotaBytes, 200<<20))
	}
	if quota < 0 {
		writeErr(w, http.StatusBadRequest, "admin.bad_request", "quotaBytes must be >= 0")
		return
	}
	m := &domain.Mailbox{
		DomainID: domainID, LocalPart: strings.ToLower(body.LocalPart),
		PasswordHash: hash, DisplayName: body.DisplayName, QuotaBytes: quota, Enabled: true,
	}
	if err := h.Store.UpsertMailbox(r.Context(), m); err != nil {
		if isUniqueConstraint(err) {
			writeErr(w, http.StatusConflict, "admin.conflict", "mailbox already exists")
			return
		}
		writeErr(w, http.StatusInternalServerError, "admin.store", err.Error())
		return
	}
	_ = h.Store.AddAudit(r.Context(), &domain.AuditEntry{
		Actor: h.actor(r), Action: "mailbox.create", Target: m.Address(d.Name),
	})
	writeJSON(w, http.StatusCreated, map[string]any{
		"id": m.ID, "localPart": m.LocalPart, "quotaBytes": m.QuotaBytes, "displayName": m.DisplayName, "enabled": m.Enabled,
	})
}

func (h *Handler) updateMailbox(w http.ResponseWriter, r *http.Request) {
	domainID, _ := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	mid, _ := strconv.ParseInt(chi.URLParam(r, "mid"), 10, 64)
	d, err := h.findDomain(r.Context(), domainID)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "admin.store", err.Error())
		return
	}
	if d == nil {
		writeErr(w, http.StatusNotFound, "admin.not_found", "domain not found")
		return
	}
	m, err := h.Store.GetMailboxByID(r.Context(), mid)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "admin.store", err.Error())
		return
	}
	if m == nil || m.DomainID != domainID {
		writeErr(w, http.StatusNotFound, "admin.not_found", "mailbox not found")
		return
	}
	var body struct {
		DisplayName *string `json:"displayName"`
		QuotaBytes  *int64  `json:"quotaBytes"`
		Enabled     *bool   `json:"enabled"`
		Password    *string `json:"password"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeErr(w, http.StatusBadRequest, "admin.bad_request", "bad json")
		return
	}
	if body.DisplayName != nil {
		m.DisplayName = strings.TrimSpace(*body.DisplayName)
	}
	if body.QuotaBytes != nil {
		if *body.QuotaBytes < 0 {
			writeErr(w, http.StatusBadRequest, "admin.bad_request", "quotaBytes must be >= 0")
			return
		}
		m.QuotaBytes = *body.QuotaBytes
	}
	if body.Enabled != nil {
		m.Enabled = *body.Enabled
	}
	if body.Password != nil && strings.TrimSpace(*body.Password) != "" {
		pw := strings.TrimSpace(*body.Password)
		if msg := h.passwordPolicyError(pw); msg != "" {
			writeErr(w, http.StatusBadRequest, "admin.weak_password", msg)
			return
		}
		hash, err := sqlite.HashPassword(pw)
		if err != nil {
			writeErr(w, http.StatusInternalServerError, "admin.hash", err.Error())
			return
		}
		m.PasswordHash = hash
	}
	if err := h.Store.UpsertMailbox(r.Context(), m); err != nil {
		writeErr(w, http.StatusInternalServerError, "admin.store", err.Error())
		return
	}
	_ = h.Store.AddAudit(r.Context(), &domain.AuditEntry{
		Actor: h.actor(r), Action: "mailbox.update", Target: m.Address(d.Name),
	})
	used, _ := h.Store.UsageBytes(r.Context(), m.ID)
	writeJSON(w, http.StatusOK, map[string]any{
		"id": m.ID, "domainId": m.DomainID, "localPart": m.LocalPart,
		"displayName": m.DisplayName, "quotaBytes": m.QuotaBytes, "usedBytes": used, "enabled": m.Enabled,
		"createdAt": m.CreatedAt,
	})
}

func (h *Handler) deleteMailbox(w http.ResponseWriter, r *http.Request) {
	domainID, _ := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	mid, _ := strconv.ParseInt(chi.URLParam(r, "mid"), 10, 64)
	d, err := h.findDomain(r.Context(), domainID)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "admin.store", err.Error())
		return
	}
	if d == nil {
		writeErr(w, http.StatusNotFound, "admin.not_found", "domain not found")
		return
	}
	m, err := h.Store.GetMailboxByID(r.Context(), mid)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "admin.store", err.Error())
		return
	}
	if m == nil || m.DomainID != domainID {
		writeErr(w, http.StatusNotFound, "admin.not_found", "mailbox not found")
		return
	}
	target := m.Address(d.Name)
	if err := h.Store.DeleteMailbox(r.Context(), domainID, mid); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			writeErr(w, http.StatusNotFound, "admin.not_found", "mailbox not found")
			return
		}
		writeErr(w, http.StatusInternalServerError, "admin.store", err.Error())
		return
	}
	_ = h.Store.AddAudit(r.Context(), &domain.AuditEntry{Actor: h.actor(r), Action: "mailbox.delete", Target: target})
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (h *Handler) impersonateMailbox(w http.ResponseWriter, r *http.Request) {
	_ = h.Settings.Reload(r.Context())
	if !h.Settings.GetBool(settings.KeySuperuserEnabled, false) {
		writeErr(w, http.StatusForbidden, "admin.superuser_disabled", "superuser mode is disabled")
		return
	}
	if strings.TrimSpace(h.Cfg.MasterPassword) == "" {
		writeErr(w, http.StatusServiceUnavailable, "admin.master_unset", "MAIL_MASTER_PASSWORD is not configured")
		return
	}
	domainID, _ := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	mid, _ := strconv.ParseInt(chi.URLParam(r, "mid"), 10, 64)
	d, err := h.findDomain(r.Context(), domainID)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "admin.store", err.Error())
		return
	}
	if d == nil {
		writeErr(w, http.StatusNotFound, "admin.not_found", "domain not found")
		return
	}
	m, err := h.Store.GetMailboxByID(r.Context(), mid)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "admin.store", err.Error())
		return
	}
	if m == nil || m.DomainID != domainID {
		writeErr(w, http.StatusNotFound, "admin.not_found", "mailbox not found")
		return
	}
	if !m.Enabled || !d.Enabled {
		writeErr(w, http.StatusBadRequest, "admin.mailbox_disabled", "mailbox or domain is disabled")
		return
	}
	username := m.Address(d.Name)
	actor := h.actor(r)
	tok, err := impersonate.Issue(h.Cfg.SessionSecret, username, actor, 2*time.Minute)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "admin.impersonate", err.Error())
		return
	}
	base := strings.TrimSpace(h.Settings.Get(settings.KeyWebmailURL))
	if base == "" {
		base = strings.TrimSpace(h.Cfg.WebmailURL)
	}
	loginURL := ""
	if base != "" {
		u, err := url.Parse(strings.TrimRight(base, "/"))
		if err == nil {
			u.Path = strings.TrimRight(u.Path, "/") + "/login"
			q := u.Query()
			q.Set("impersonate", tok)
			u.RawQuery = q.Encode()
			loginURL = u.String()
		}
	}
	_ = h.Store.AddAudit(r.Context(), &domain.AuditEntry{
		Actor: actor, Action: "mailbox.impersonate", Target: username,
	})
	writeJSON(w, http.StatusOK, map[string]any{
		"token":     tok,
		"username":  username,
		"url":       loginURL,
		"expiresIn": 120,
	})
}

func (h *Handler) listAliases(w http.ResponseWriter, r *http.Request) {
	id, _ := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	list, err := h.Store.ListAliases(r.Context(), id)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "admin.store", err.Error())
		return
	}
	out := make([]map[string]any, 0, len(list))
	for _, a := range list {
		out = append(out, map[string]any{
			"id": a.ID, "domainId": a.DomainID, "localPart": a.LocalPart,
			"mailboxId": a.MailboxID, "enabled": a.Enabled,
		})
	}
	writeJSON(w, http.StatusOK, out)
}

func (h *Handler) createAlias(w http.ResponseWriter, r *http.Request) {
	domainID, _ := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	var body struct {
		LocalPart string `json:"localPart"`
		MailboxID int64  `json:"mailboxId"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.LocalPart == "" || body.MailboxID == 0 {
		writeErr(w, http.StatusBadRequest, "admin.bad_request", "localPart and mailboxId required")
		return
	}
	local := strings.ToLower(strings.TrimSpace(body.LocalPart))
	mb, err := h.Store.GetMailboxByID(r.Context(), body.MailboxID)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "admin.store", err.Error())
		return
	}
	if mb == nil || mb.DomainID != domainID {
		writeErr(w, http.StatusBadRequest, "admin.bad_request", "mailbox must belong to this domain")
		return
	}
	mboxes, err := h.Store.ListMailboxes(r.Context(), domainID)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "admin.store", err.Error())
		return
	}
	for _, m := range mboxes {
		if strings.EqualFold(m.LocalPart, local) {
			writeErr(w, http.StatusConflict, "admin.conflict", "alias conflicts with mailbox local part")
			return
		}
	}
	a := &domain.Alias{
		DomainID: domainID, LocalPart: local,
		MailboxID: body.MailboxID, Enabled: true,
	}
	if err := h.Store.UpsertAlias(r.Context(), a); err != nil {
		if isUniqueConstraint(err) {
			writeErr(w, http.StatusConflict, "admin.conflict", "alias already exists")
			return
		}
		writeErr(w, http.StatusInternalServerError, "admin.store", err.Error())
		return
	}
	_ = h.Store.AddAudit(r.Context(), &domain.AuditEntry{
		Actor: h.actor(r), Action: "alias.create", Target: local,
	})
	writeJSON(w, http.StatusCreated, map[string]any{"id": a.ID, "localPart": a.LocalPart, "mailboxId": a.MailboxID})
}

func (h *Handler) deleteAlias(w http.ResponseWriter, r *http.Request) {
	domainID, _ := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	aid, _ := strconv.ParseInt(chi.URLParam(r, "aid"), 10, 64)
	if domainID == 0 || aid == 0 {
		writeErr(w, http.StatusBadRequest, "admin.bad_request", "ids required")
		return
	}
	if err := h.Store.DeleteAlias(r.Context(), domainID, aid); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			writeErr(w, http.StatusNotFound, "admin.not_found", "alias not found")
			return
		}
		writeErr(w, http.StatusInternalServerError, "admin.store", err.Error())
		return
	}
	_ = h.Store.AddAudit(r.Context(), &domain.AuditEntry{
		Actor: h.actor(r), Action: "alias.delete", Target: strconv.FormatInt(aid, 10),
	})
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (h *Handler) backup(w http.ResponseWriter, r *http.Request) {
	_ = h.Settings.Reload(r.Context())
	domains, err := h.Store.ListDomains(r.Context())
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "admin.store", err.Error())
		return
	}
	type mbOut struct {
		ID          int64  `json:"id"`
		LocalPart   string `json:"localPart"`
		DisplayName string `json:"displayName"`
		QuotaBytes  int64  `json:"quotaBytes"`
		Enabled     bool   `json:"enabled"`
	}
	type aliasOut struct {
		LocalPart        string `json:"localPart"`
		MailboxID        int64  `json:"mailboxId"`
		MailboxLocalPart string `json:"mailboxLocalPart"`
		Enabled          bool   `json:"enabled"`
	}
	type domOut struct {
		Name              string     `json:"name"`
		Enabled           bool       `json:"enabled"`
		CatchAll          string     `json:"catchAll"`
		DKIMSelector      string     `json:"dkimSelector"`
		DefaultQuotaBytes int64      `json:"defaultQuotaBytes"`
		HasDKIM           bool       `json:"hasDkim"`
		Mailboxes         []mbOut    `json:"mailboxes"`
		Aliases           []aliasOut `json:"aliases"`
	}
	outDomains := make([]domOut, 0, len(domains))
	for _, d := range domains {
		mboxes, _ := h.Store.ListMailboxes(r.Context(), d.ID)
		aliases, _ := h.Store.ListAliases(r.Context(), d.ID)
		mbByID := map[int64]string{}
		mb := make([]mbOut, 0, len(mboxes))
		for _, m := range mboxes {
			mbByID[m.ID] = m.LocalPart
			mb = append(mb, mbOut{ID: m.ID, LocalPart: m.LocalPart, DisplayName: m.DisplayName, QuotaBytes: m.QuotaBytes, Enabled: m.Enabled})
		}
		al := make([]aliasOut, 0, len(aliases))
		for _, a := range aliases {
			al = append(al, aliasOut{
				LocalPart: a.LocalPart, MailboxID: a.MailboxID, MailboxLocalPart: mbByID[a.MailboxID], Enabled: a.Enabled,
			})
		}
		outDomains = append(outDomains, domOut{
			Name: d.Name, Enabled: d.Enabled, CatchAll: d.CatchAll, DKIMSelector: d.DKIMSelector,
			DefaultQuotaBytes: d.DefaultQuotaBytes, HasDKIM: d.DKIMPublic != "", Mailboxes: mb, Aliases: al,
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"exportedAt": time.Now().UTC(),
		"settings":   redactSettings(h.Settings.All()),
		"domains":    outDomains,
	})
}

func redactSettings(in map[string]string) map[string]string {
	out := make(map[string]string, len(in))
	for k, v := range in {
		lk := strings.ToLower(k)
		if strings.Contains(lk, "password") || strings.Contains(lk, "secret") || strings.Contains(lk, "private") || strings.Contains(lk, "key") {
			if v != "" {
				out[k] = "[redacted]"
				continue
			}
		}
		out[k] = v
	}
	return out
}

func (h *Handler) restoreBackup(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Settings map[string]string `json:"settings"`
		Domains  []struct {
			Name              string `json:"name"`
			Enabled           bool   `json:"enabled"`
			CatchAll          string `json:"catchAll"`
			DKIMSelector      string `json:"dkimSelector"`
			DefaultQuotaBytes int64  `json:"defaultQuotaBytes"`
			Mailboxes         []struct {
				ID          int64  `json:"id"`
				LocalPart   string `json:"localPart"`
				DisplayName string `json:"displayName"`
				QuotaBytes  int64  `json:"quotaBytes"`
				Enabled     bool   `json:"enabled"`
			} `json:"mailboxes"`
			Aliases []struct {
				LocalPart        string `json:"localPart"`
				MailboxID        int64  `json:"mailboxId"`
				MailboxLocalPart string `json:"mailboxLocalPart"`
				Enabled          bool   `json:"enabled"`
			} `json:"aliases"`
		} `json:"domains"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeErr(w, http.StatusBadRequest, "admin.bad_request", "bad json")
		return
	}
	nSettings, nDomains, nMailboxes, nAliases := 0, 0, 0, 0
	for k, v := range body.Settings {
		if v == "[redacted]" || k == "" {
			continue
		}
		if err := h.Settings.Set(r.Context(), k, v); err != nil {
			writeErr(w, http.StatusInternalServerError, "admin.store", err.Error())
			return
		}
		nSettings++
	}
	for _, d := range body.Domains {
		name := strings.TrimSpace(strings.ToLower(d.Name))
		if name == "" {
			continue
		}
		existing, _ := h.Store.GetDomainByName(r.Context(), name)
		dom := &domain.Domain{
			Name: name, Enabled: d.Enabled, CatchAll: d.CatchAll, DKIMSelector: d.DKIMSelector,
			DefaultQuotaBytes: d.DefaultQuotaBytes,
		}
		if dom.DKIMSelector == "" {
			dom.DKIMSelector = "wernan"
		}
		if existing != nil {
			dom.ID = existing.ID
			dom.DKIMPrivate = existing.DKIMPrivate
			dom.DKIMPublic = existing.DKIMPublic
			if dom.DKIMSelector == "" {
				dom.DKIMSelector = existing.DKIMSelector
			}
			if d.DefaultQuotaBytes == 0 {
				dom.DefaultQuotaBytes = existing.DefaultQuotaBytes
			}
		}
		if err := h.Store.UpsertDomain(r.Context(), dom); err != nil {
			writeErr(w, http.StatusInternalServerError, "admin.store", err.Error())
			return
		}
		nDomains++

		localToID := map[string]int64{}
		existingMBs, _ := h.Store.ListMailboxes(r.Context(), dom.ID)
		for _, m := range existingMBs {
			localToID[strings.ToLower(m.LocalPart)] = m.ID
		}
		exportIDToLocal := map[int64]string{}
		for _, mb := range d.Mailboxes {
			lp := strings.ToLower(strings.TrimSpace(mb.LocalPart))
			if lp == "" {
				continue
			}
			if mb.ID > 0 {
				exportIDToLocal[mb.ID] = lp
			}
			if id, ok := localToID[lp]; ok {
				cur, _ := h.Store.GetMailboxByID(r.Context(), id)
				if cur != nil {
					cur.DisplayName = mb.DisplayName
					if mb.QuotaBytes >= 0 {
						cur.QuotaBytes = mb.QuotaBytes
					}
					_ = h.Store.UpsertMailbox(r.Context(), cur)
				}
				continue
			}
			hash, err := randomPasswordHash()
			if err != nil {
				writeErr(w, http.StatusInternalServerError, "admin.hash", err.Error())
				return
			}
			m := &domain.Mailbox{
				DomainID: dom.ID, LocalPart: lp, PasswordHash: hash,
				DisplayName: mb.DisplayName, QuotaBytes: mb.QuotaBytes, Enabled: false,
			}
			if err := h.Store.UpsertMailbox(r.Context(), m); err != nil {
				if isUniqueConstraint(err) {
					continue
				}
				writeErr(w, http.StatusInternalServerError, "admin.store", err.Error())
				return
			}
			localToID[lp] = m.ID
			nMailboxes++
		}

		for _, a := range d.Aliases {
			lp := strings.ToLower(strings.TrimSpace(a.LocalPart))
			if lp == "" {
				continue
			}
			targetLocal := strings.ToLower(strings.TrimSpace(a.MailboxLocalPart))
			if targetLocal == "" && a.MailboxID > 0 {
				targetLocal = exportIDToLocal[a.MailboxID]
			}
			if targetLocal == "" {
				continue
			}
			targetID, ok := localToID[targetLocal]
			if !ok {
				continue
			}
			if _, exists := localToID[lp]; exists {
				continue
			}
			al := &domain.Alias{DomainID: dom.ID, LocalPart: lp, MailboxID: targetID, Enabled: true}
			if !a.Enabled {
				al.Enabled = false
			}
			if err := h.Store.UpsertAlias(r.Context(), al); err != nil {
				if isUniqueConstraint(err) {
					continue
				}
				writeErr(w, http.StatusInternalServerError, "admin.store", err.Error())
				return
			}
			nAliases++
		}
	}
	_ = h.Store.AddAudit(r.Context(), &domain.AuditEntry{
		Actor: h.actor(r), Action: "backup.restore",
		Detail: "settings=" + strconv.Itoa(nSettings) + " domains=" + strconv.Itoa(nDomains) +
			" mailboxes=" + strconv.Itoa(nMailboxes) + " aliases=" + strconv.Itoa(nAliases),
	})
	writeJSON(w, http.StatusOK, map[string]any{
		"ok": true, "settings": nSettings, "domains": nDomains,
		"mailboxes": nMailboxes, "aliases": nAliases,
	})
}

func randomPasswordHash() (string, error) {
	var b [24]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	return sqlite.HashPassword(hex.EncodeToString(b[:]))
}

func (h *Handler) dnsStatus(w http.ResponseWriter, r *http.Request) {
	name := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("domain")))
	snap := h.collectDNSStatus(r.Context(), name)
	writeJSON(w, http.StatusOK, map[string]any{
		"domain":    snap.Domain,
		"mailHost":  snap.MailHost,
		"mx":        snap.MX,
		"spf":       snap.SPF,
		"dkim":      snap.DKIM,
		"dmarc":     snap.DMARC,
		"checkedAt": time.Now().UTC(),
	})
}

func publicDNSResolver() *net.Resolver {
	return &net.Resolver{
		PreferGo: true,
		Dial: func(ctx context.Context, network, _ string) (net.Conn, error) {
			var d net.Dialer
			// Public resolvers вЂ” VPS stub may still cache pre-delegation NXDOMAIN.
			for _, server := range []string{"8.8.8.8:53", "1.1.1.1:53"} {
				c, err := d.DialContext(ctx, "udp", server)
				if err == nil {
					return c, nil
				}
			}
			return d.DialContext(ctx, network, "8.8.8.8:53")
		},
	}
}

func checkResult(state, detail string) map[string]string {
	return map[string]string{"state": state, "detail": detail}
}

func extractDKIMP(s string) string {
	s = strings.ReplaceAll(s, " ", "")
	s = strings.ReplaceAll(s, "\"", "")
	idx := strings.Index(s, "p=")
	if idx < 0 {
		return ""
	}
	p := s[idx+2:]
	if i := strings.IndexAny(p, ";"); i >= 0 {
		p = p[:i]
	}
	return p
}

func (h *Handler) listQueue(w http.ResponseWriter, r *http.Request) {
	list, err := h.Queue.List(r.Context(), 100)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "admin.store", err.Error())
		return
	}
	if list == nil {
		list = []domain.QueueJob{}
	}
	writeJSON(w, http.StatusOK, list)
}

func (h *Handler) retryQueue(w http.ResponseWriter, r *http.Request) {
	id, _ := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if id <= 0 {
		writeErr(w, http.StatusBadRequest, "admin.bad_request", "bad id")
		return
	}
	if err := h.Queue.Retry(r.Context(), id); err != nil {
		writeErr(w, http.StatusInternalServerError, "admin.store", err.Error())
		return
	}
	_ = h.Store.AddAudit(r.Context(), &domain.AuditEntry{Actor: h.actor(r), Action: "queue.retry", Target: strconv.FormatInt(id, 10)})
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (h *Handler) deleteQueue(w http.ResponseWriter, r *http.Request) {
	id, _ := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if id <= 0 {
		writeErr(w, http.StatusBadRequest, "admin.bad_request", "bad id")
		return
	}
	if err := h.Queue.DeleteJob(r.Context(), id); err != nil {
		writeErr(w, http.StatusInternalServerError, "admin.store", err.Error())
		return
	}
	_ = h.Store.AddAudit(r.Context(), &domain.AuditEntry{Actor: h.actor(r), Action: "queue.delete", Target: strconv.FormatInt(id, 10)})
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (h *Handler) opsStatus(w http.ResponseWriter, r *http.Request) {
	_ = h.Settings.Reload(r.Context())
	tlsOK := h.Cfg.TLSCertFile != "" && h.Cfg.TLSKeyFile != ""
	schemaVer := 0
	if h.Store != nil {
		if v, err := h.Store.SchemaVersion(); err == nil {
			schemaVer = v
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"hostname":         h.Cfg.Hostname,
		"ehlo":             h.Cfg.EHLOHost,
		"tlsConfigured":    tlsOK,
		"greylistSeconds":  h.Settings.GetInt(settings.KeyGreylistSeconds, 0),
		"bounceEnabled":    h.Settings.GetBool(settings.KeyBounceEnabled, true),
		"rateSendPerHour":  h.Settings.GetInt(settings.KeyRateSendPerHour, 200),
		"rateSubmitPerMin": h.Settings.GetInt(settings.KeyRateSubmitPerMin, 60),
		"relayHost":        h.Settings.Get(settings.KeyRelayHost),
		"schemaVersion":    schemaVer,
	})
}

func (h *Handler) listQuarantine(w http.ResponseWriter, r *http.Request) {
	list, err := h.Store.ListQuarantine(r.Context(), 100)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "admin.store", err.Error())
		return
	}
	if list == nil {
		list = []domain.QuarantineItem{}
	}
	domains, _ := h.Store.ListDomains(r.Context())
	domName := map[int64]string{}
	for _, d := range domains {
		domName[d.ID] = d.Name
	}
	type qOut struct {
		ID          int64     `json:"id"`
		MailboxID   int64     `json:"mailboxId"`
		MailboxAddr string    `json:"mailboxAddr,omitempty"`
		Subject     string    `json:"subject"`
		FromAddr    string    `json:"fromAddr"`
		VerdictJSON string    `json:"verdictJson"`
		CreatedAt   time.Time `json:"createdAt"`
	}
	out := make([]qOut, 0, len(list))
	for _, q := range list {
		row := qOut{
			ID: q.ID, MailboxID: q.MailboxID, Subject: q.Subject,
			FromAddr: q.FromAddr, VerdictJSON: q.VerdictJSON, CreatedAt: q.CreatedAt,
		}
		if m, err := h.Store.GetMailboxByID(r.Context(), q.MailboxID); err == nil && m != nil {
			if name := domName[m.DomainID]; name != "" {
				row.MailboxAddr = m.Address(name)
			} else {
				row.MailboxAddr = m.LocalPart
			}
		}
		out = append(out, row)
	}
	writeJSON(w, http.StatusOK, out)
}

func (h *Handler) releaseQuarantine(w http.ResponseWriter, r *http.Request) {
	id, _ := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	q, raw, err := h.Store.GetQuarantineRaw(r.Context(), id)
	if err != nil || q == nil || q.Resolution != "" {
		writeErr(w, http.StatusNotFound, "admin.not_found", "not found")
		return
	}
	msg := &domain.Message{
		MailboxID: q.MailboxID, Folder: domain.FolderInbox,
		Subject: q.Subject, FromAddr: q.FromAddr, Date: time.Now().UTC(),
	}
	if err := h.Store.AppendMessage(r.Context(), msg, raw); err != nil {
		writeErr(w, http.StatusInternalServerError, "admin.store", err.Error())
		return
	}
	if err := h.Store.LearnSpamSignals(r.Context(), antispam.SignalKeys(q.FromAddr, q.Subject), -0.5); err != nil {
		writeErr(w, http.StatusInternalServerError, "admin.store", err.Error())
		return
	}
	if err := h.Store.ResolveQuarantine(r.Context(), id, "release"); err != nil {
		writeErr(w, http.StatusInternalServerError, "admin.store", err.Error())
		return
	}
	_ = h.Store.AddAudit(r.Context(), &domain.AuditEntry{Actor: h.actor(r), Action: "quarantine.release", Target: strconv.FormatInt(id, 10)})
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (h *Handler) deleteQuarantine(w http.ResponseWriter, r *http.Request) {
	id, _ := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	q, _, err := h.Store.GetQuarantineRaw(r.Context(), id)
	if err != nil || q == nil || q.Resolution != "" {
		writeErr(w, http.StatusNotFound, "admin.not_found", "not found")
		return
	}
	if err := h.Store.LearnSpamSignals(r.Context(), antispam.SignalKeys(q.FromAddr, q.Subject), 0.5); err != nil {
		writeErr(w, http.StatusInternalServerError, "admin.store", err.Error())
		return
	}
	if err := h.Store.ResolveQuarantine(r.Context(), id, "delete"); err != nil {
		writeErr(w, http.StatusInternalServerError, "admin.store", err.Error())
		return
	}
	_ = h.Store.AddAudit(r.Context(), &domain.AuditEntry{Actor: h.actor(r), Action: "quarantine.delete", Target: strconv.FormatInt(id, 10)})
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (h *Handler) listDMARCReports(w http.ResponseWriter, r *http.Request) {
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	list, err := h.Store.ListDMARCReports(r.Context(), limit)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "admin.store", err.Error())
		return
	}
	if list == nil {
		list = []domain.DMARCReport{}
	}
	writeJSON(w, http.StatusOK, list)
}

func (h *Handler) listMailboxFilters(w http.ResponseWriter, r *http.Request) {
	mailboxID, _ := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	mailbox, err := h.Store.GetMailboxByID(r.Context(), mailboxID)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "admin.store", err.Error())
		return
	}
	if mailbox == nil {
		writeErr(w, http.StatusNotFound, "admin.not_found", "mailbox not found")
		return
	}
	list, err := h.Store.ListMailFilters(r.Context(), mailboxID)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "admin.store", err.Error())
		return
	}
	if list == nil {
		list = []domain.MailFilter{}
	}
	writeJSON(w, http.StatusOK, list)
}

func (h *Handler) putMailboxFilters(w http.ResponseWriter, r *http.Request) {
	mailboxID, _ := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	mailbox, err := h.Store.GetMailboxByID(r.Context(), mailboxID)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "admin.store", err.Error())
		return
	}
	if mailbox == nil {
		writeErr(w, http.StatusNotFound, "admin.not_found", "mailbox not found")
		return
	}
	var filters []domain.MailFilter
	if err := json.NewDecoder(r.Body).Decode(&filters); err != nil {
		writeErr(w, http.StatusBadRequest, "admin.bad_request", "expected a JSON array of filters")
		return
	}
	if len(filters) > 100 {
		writeErr(w, http.StatusBadRequest, "admin.bad_request", "at most 100 filters are allowed")
		return
	}
	for i := range filters {
		filters[i].ID = 0
		filters[i].MailboxID = mailboxID
		filters[i].MatchField = strings.ToLower(strings.TrimSpace(filters[i].MatchField))
		filters[i].MatchOp = strings.ToLower(strings.TrimSpace(filters[i].MatchOp))
		filters[i].MatchValue = strings.TrimSpace(filters[i].MatchValue)
		filters[i].Action = strings.ToLower(strings.TrimSpace(filters[i].Action))
		filters[i].ActionArg = strings.TrimSpace(filters[i].ActionArg)
		if err := mailfilter.Validate(filters[i]); err != nil {
			writeErr(w, http.StatusBadRequest, "admin.bad_request", "filter "+strconv.Itoa(i)+": "+err.Error())
			return
		}
	}
	if err := h.Store.ReplaceMailFilters(r.Context(), mailboxID, filters); err != nil {
		writeErr(w, http.StatusInternalServerError, "admin.store", err.Error())
		return
	}
	_ = h.Store.AddAudit(r.Context(), &domain.AuditEntry{
		Actor: h.actor(r), Action: "mailbox.filters.update",
		Target: strconv.FormatInt(mailboxID, 10), Detail: strconv.Itoa(len(filters)),
	})
	list, err := h.Store.ListMailFilters(r.Context(), mailboxID)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "admin.store", err.Error())
		return
	}
	if list == nil {
		list = []domain.MailFilter{}
	}
	writeJSON(w, http.StatusOK, list)
}

func (h *Handler) getSettings(w http.ResponseWriter, r *http.Request) {
	_ = h.Settings.Reload(r.Context())
	writeJSON(w, http.StatusOK, h.Settings.All())
}

func (h *Handler) putSettings(w http.ResponseWriter, r *http.Request) {
	var body map[string]string
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeErr(w, http.StatusBadRequest, "admin.bad_request", "bad json")
		return
	}
	for k, v := range body {
		if err := h.Settings.Set(r.Context(), k, v); err != nil {
			writeErr(w, http.StatusInternalServerError, "admin.store", err.Error())
			return
		}
	}
	_ = h.Store.AddAudit(r.Context(), &domain.AuditEntry{Actor: h.actor(r), Action: "settings.update", Detail: strconv.Itoa(len(body))})
	writeJSON(w, http.StatusOK, h.Settings.All())
}

func (h *Handler) listAudit(w http.ResponseWriter, r *http.Request) {
	list, err := h.Store.ListAudit(r.Context(), 200)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "admin.store", err.Error())
		return
	}
	if list == nil {
		list = []domain.AuditEntry{}
	}
	writeJSON(w, http.StatusOK, list)
}

func (h *Handler) passwordPolicyError(password string) string {
	minLen := h.Settings.GetInt(settings.KeyPasswordMinLength, 8)
	if minLen < 1 {
		minLen = 1
	}
	if len([]rune(password)) < minLen {
		return "password must be at least " + strconv.Itoa(minLen) + " characters"
	}
	if h.Settings.GetBool(settings.KeyPasswordRequireDigit, false) {
		ok := false
		for _, r := range password {
			if unicode.IsDigit(r) {
				ok = true
				break
			}
		}
		if !ok {
			return "password must contain a digit"
		}
	}
	if h.Settings.GetBool(settings.KeyPasswordRequireUpper, false) {
		ok := false
		for _, r := range password {
			if unicode.IsUpper(r) {
				ok = true
				break
			}
		}
		if !ok {
			return "password must contain an uppercase letter"
		}
	}
	return ""
}

func isUniqueConstraint(err error) bool {
	if err == nil {
		return false
	}
	s := strings.ToLower(err.Error())
	return strings.Contains(s, "unique") || strings.Contains(s, "constraint failed")
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(map[string]any{"data": v})
}

func writeErr(w http.ResponseWriter, code int, errCode, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(map[string]any{"error": map[string]string{"code": errCode, "message": msg}})
}

// Ensure bcrypt linked for mailbox hashing helpers.
var _ = bcrypt.ErrMismatchedHashAndPassword
