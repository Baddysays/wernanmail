package adminapi

import (
	"context"
	"encoding/json"
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

	"github.com/Baddysays/wernanmail/server/internal/dnsauth"
	"github.com/Baddysays/wernanmail/server/internal/domain"
	"github.com/Baddysays/wernanmail/server/internal/impersonate"
	"github.com/Baddysays/wernanmail/server/internal/mailcfg"
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
	r.Post("/api/admin/login", h.login)
	r.Post("/api/admin/logout", h.logout)
	r.Group(func(r chi.Router) {
		r.Use(h.auth)
		r.Get("/api/admin/dashboard", h.dashboard)
		r.Get("/api/admin/domains", h.listDomains)
		r.Post("/api/admin/domains", h.createDomain)
		r.Patch("/api/admin/domains/{id}", h.updateDomain)
		r.Post("/api/admin/domains/{id}/dkim", h.genDKIM)
		r.Get("/api/admin/domains/{id}/mailboxes", h.listMailboxes)
		r.Post("/api/admin/domains/{id}/mailboxes", h.createMailbox)
		r.Patch("/api/admin/domains/{id}/mailboxes/{mid}", h.updateMailbox)
		r.Post("/api/admin/domains/{id}/mailboxes/{mid}/impersonate", h.impersonateMailbox)
		r.Get("/api/admin/domains/{id}/aliases", h.listAliases)
		r.Post("/api/admin/domains/{id}/aliases", h.createAlias)
		r.Get("/api/admin/queue", h.listQueue)
		r.Post("/api/admin/queue/{id}/retry", h.retryQueue)
		r.Post("/api/admin/queue/{id}/delete", h.deleteQueue)
		r.Get("/api/admin/backup", h.backup)
		r.Post("/api/admin/backup/restore", h.restoreBackup)
		r.Get("/api/admin/ops", h.opsStatus)
		r.Get("/api/admin/dns-status", h.dnsStatus)
		r.Get("/api/admin/host-stats", h.hostStats)
		r.Get("/api/admin/quarantine", h.listQuarantine)
		r.Post("/api/admin/quarantine/{id}/release", h.releaseQuarantine)
		r.Post("/api/admin/quarantine/{id}/delete", h.deleteQuarantine)
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
	q, _ := h.Store.ListQuarantine(r.Context(), 5)
	domains, _ := h.Store.ListDomains(r.Context())
	writeJSON(w, http.StatusOK, map[string]any{
		"queuePending": pending,
		"queueDead":    dead,
		"quarantine":   len(q),
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
		out = append(out, map[string]any{
			"id": m.ID, "domainId": m.DomainID, "localPart": m.LocalPart,
			"displayName": m.DisplayName, "quotaBytes": m.QuotaBytes, "enabled": m.Enabled,
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
	writeJSON(w, http.StatusOK, map[string]any{
		"id": m.ID, "domainId": m.DomainID, "localPart": m.LocalPart,
		"displayName": m.DisplayName, "quotaBytes": m.QuotaBytes, "enabled": m.Enabled,
		"createdAt": m.CreatedAt,
	})
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
		"token":    tok,
		"username": username,
		"url":      loginURL,
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
	a := &domain.Alias{
		DomainID: domainID, LocalPart: strings.ToLower(strings.TrimSpace(body.LocalPart)),
		MailboxID: body.MailboxID, Enabled: true,
	}
	if err := h.Store.UpsertAlias(r.Context(), a); err != nil {
		writeErr(w, http.StatusInternalServerError, "admin.store", err.Error())
		return
	}
	_ = h.Store.AddAudit(r.Context(), &domain.AuditEntry{
		Actor: h.actor(r), Action: "alias.create", Target: body.LocalPart,
	})
	writeJSON(w, http.StatusCreated, map[string]any{"id": a.ID, "localPart": a.LocalPart, "mailboxId": a.MailboxID})
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
		LocalPart string `json:"localPart"`
		MailboxID int64  `json:"mailboxId"`
		Enabled   bool   `json:"enabled"`
	}
	type domOut struct {
		Name         string     `json:"name"`
		Enabled      bool       `json:"enabled"`
		CatchAll     string     `json:"catchAll"`
		DKIMSelector string     `json:"dkimSelector"`
		HasDKIM      bool       `json:"hasDkim"`
		Mailboxes    []mbOut    `json:"mailboxes"`
		Aliases      []aliasOut `json:"aliases"`
	}
	outDomains := make([]domOut, 0, len(domains))
	for _, d := range domains {
		mboxes, _ := h.Store.ListMailboxes(r.Context(), d.ID)
		aliases, _ := h.Store.ListAliases(r.Context(), d.ID)
		mb := make([]mbOut, 0, len(mboxes))
		for _, m := range mboxes {
			mb = append(mb, mbOut{ID: m.ID, LocalPart: m.LocalPart, DisplayName: m.DisplayName, QuotaBytes: m.QuotaBytes, Enabled: m.Enabled})
		}
		al := make([]aliasOut, 0, len(aliases))
		for _, a := range aliases {
			al = append(al, aliasOut{LocalPart: a.LocalPart, MailboxID: a.MailboxID, Enabled: a.Enabled})
		}
		outDomains = append(outDomains, domOut{
			Name: d.Name, Enabled: d.Enabled, CatchAll: d.CatchAll, DKIMSelector: d.DKIMSelector,
			HasDKIM: d.DKIMPublic != "", Mailboxes: mb, Aliases: al,
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
			Name         string `json:"name"`
			Enabled      bool   `json:"enabled"`
			CatchAll     string `json:"catchAll"`
			DKIMSelector string `json:"dkimSelector"`
		} `json:"domains"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeErr(w, http.StatusBadRequest, "admin.bad_request", "bad json")
		return
	}
	nSettings, nDomains := 0, 0
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
		}
		if err := h.Store.UpsertDomain(r.Context(), dom); err != nil {
			writeErr(w, http.StatusInternalServerError, "admin.store", err.Error())
			return
		}
		nDomains++
	}
	_ = h.Store.AddAudit(r.Context(), &domain.AuditEntry{
		Actor: h.actor(r), Action: "backup.restore",
		Detail: "settings=" + strconv.Itoa(nSettings) + " domains=" + strconv.Itoa(nDomains),
	})
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "settings": nSettings, "domains": nDomains})
}

func (h *Handler) dnsStatus(w http.ResponseWriter, r *http.Request) {
	name := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("domain")))
	domains, err := h.Store.ListDomains(r.Context())
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "admin.store", err.Error())
		return
	}
	var d *domain.Domain
	for i := range domains {
		if name != "" && domains[i].Name == name {
			d = &domains[i]
			break
		}
	}
	if d == nil && len(domains) > 0 {
		d = &domains[0]
		name = d.Name
	}
	if d == nil || name == "" {
		writeJSON(w, http.StatusOK, map[string]any{
			"domain": "",
			"mx":     checkResult("missing", "no domain"),
			"spf":    checkResult("missing", "no domain"),
			"dkim":   checkResult("missing", "no domain"),
			"dmarc":  checkResult("missing", "no domain"),
		})
		return
	}

	mailHost := strings.TrimSpace(h.Cfg.Hostname)
	if mailHost == "" {
		mailHost = "mail." + name
	}
	res := publicDNSResolver()
	ctx := r.Context()

	mxState, mxDetail := "missing", "no MX"
	if mxs, err := res.LookupMX(ctx, name); err == nil && len(mxs) > 0 {
		found := false
		parts := make([]string, 0, len(mxs))
		want := strings.TrimSuffix(strings.ToLower(mailHost), ".")
		for _, mx := range mxs {
			host := strings.TrimSuffix(strings.ToLower(mx.Host), ".")
			parts = append(parts, host)
			if host == want {
				found = true
			}
		}
		if found {
			mxState, mxDetail = "ok", strings.Join(parts, ", ")
		} else {
			mxState, mxDetail = "warn", "MX: "+strings.Join(parts, ", ")
		}
	}

	spfState, spfDetail := "missing", "no TXT"
	if txts, err := res.LookupTXT(ctx, name); err == nil {
		for _, t := range txts {
			tt := strings.TrimSpace(t)
			if strings.HasPrefix(tt, "v=spf1") {
				spfState, spfDetail = "ok", tt
				break
			}
		}
	}

	selector := d.DKIMSelector
	if selector == "" {
		selector = "wernan"
	}
	dkimHost := selector + "._domainkey." + name
	dkimState, dkimDetail := "missing", "not published"
	if d.DKIMPublic == "" {
		dkimState, dkimDetail = "warn", "no local key"
	} else if txts, err := res.LookupTXT(ctx, dkimHost); err == nil && len(txts) > 0 {
		pub := strings.Join(txts, "")
		want := extractDKIMP(d.DKIMPublic)
		got := extractDKIMP(pub)
		if want != "" && got != "" && want == got {
			dkimState, dkimDetail = "ok", "published"
		} else if strings.Contains(pub, "v=DKIM1") || strings.Contains(pub, "p=") {
			dkimState, dkimDetail = "warn", "TXT found, key mismatch"
		} else {
			dkimState, dkimDetail = "warn", "unexpected TXT"
		}
	} else if d.DKIMPublic != "" {
		dkimState, dkimDetail = "warn", "key ready, not in DNS"
	}

	dmarcState, dmarcDetail := "missing", "no _dmarc"
	if txts, err := res.LookupTXT(ctx, "_dmarc."+name); err == nil {
		for _, t := range txts {
			if strings.Contains(strings.ToLower(t), "v=dmarc1") {
				dmarcState, dmarcDetail = "ok", t
				break
			}
		}
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"domain":    name,
		"mailHost":  mailHost,
		"mx":        checkResult(mxState, mxDetail),
		"spf":       checkResult(spfState, spfDetail),
		"dkim":      checkResult(dkimState, dkimDetail),
		"dmarc":     checkResult(dmarcState, dmarcDetail),
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
	writeJSON(w, http.StatusOK, map[string]any{
		"hostname":       h.Cfg.Hostname,
		"ehlo":           h.Cfg.EHLOHost,
		"tlsConfigured":  tlsOK,
		"greylistSeconds": h.Settings.GetInt(settings.KeyGreylistSeconds, 0),
		"bounceEnabled":  h.Settings.GetBool(settings.KeyBounceEnabled, true),
		"rateSendPerHour": h.Settings.GetInt(settings.KeyRateSendPerHour, 200),
		"rateSubmitPerMin": h.Settings.GetInt(settings.KeyRateSubmitPerMin, 60),
		"relayHost":      h.Settings.Get(settings.KeyRelayHost),
	})
}

func (h *Handler) listQuarantine(w http.ResponseWriter, r *http.Request) {
	list, err := h.Store.ListQuarantine(r.Context(), 100)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "admin.store", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, list)
}

func (h *Handler) releaseQuarantine(w http.ResponseWriter, r *http.Request) {
	id, _ := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	q, raw, err := h.Store.GetQuarantineRaw(r.Context(), id)
	if err != nil || q == nil {
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
	if err := h.Store.ResolveQuarantine(r.Context(), id, "release"); err != nil {
		writeErr(w, http.StatusInternalServerError, "admin.store", err.Error())
		return
	}
	_ = h.Store.AddAudit(r.Context(), &domain.AuditEntry{Actor: h.actor(r), Action: "quarantine.release", Target: strconv.FormatInt(id, 10)})
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (h *Handler) deleteQuarantine(w http.ResponseWriter, r *http.Request) {
	id, _ := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err := h.Store.ResolveQuarantine(r.Context(), id, "delete"); err != nil {
		writeErr(w, http.StatusInternalServerError, "admin.store", err.Error())
		return
	}
	_ = h.Store.AddAudit(r.Context(), &domain.AuditEntry{Actor: h.actor(r), Action: "quarantine.delete", Target: strconv.FormatInt(id, 10)})
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
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
