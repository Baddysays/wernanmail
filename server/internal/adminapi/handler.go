package adminapi

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/go-chi/cors"
	"golang.org/x/crypto/bcrypt"

	"github.com/Baddysays/wernanmail/server/internal/dnsauth"
	"github.com/Baddysays/wernanmail/server/internal/domain"
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
}

func NewRouter(h *Handler) http.Handler {
	r := chi.NewRouter()
	r.Use(middleware.RequestID, middleware.RealIP, middleware.Recoverer)
	r.Use(cors.Handler(cors.Options{
		AllowedOrigins:   []string{"*"},
		AllowedMethods:   []string{"GET", "POST", "PUT", "PATCH", "DELETE", "OPTIONS"},
		AllowedHeaders:   []string{"Accept", "Authorization", "Content-Type"},
		AllowCredentials: true,
	}))
	r.Get("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, map[string]any{"status": "ok"})
	})
	r.Post("/api/admin/login", h.login)
	r.Group(func(r chi.Router) {
		r.Use(h.auth)
		r.Get("/api/admin/dashboard", h.dashboard)
		r.Get("/api/admin/domains", h.listDomains)
		r.Post("/api/admin/domains", h.createDomain)
		r.Post("/api/admin/domains/{id}/dkim", h.genDKIM)
		r.Get("/api/admin/domains/{id}/mailboxes", h.listMailboxes)
		r.Post("/api/admin/domains/{id}/mailboxes", h.createMailbox)
		r.Get("/api/admin/queue", h.listQueue)
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
		user, pass, ok := r.BasicAuth()
		if !ok || user != h.Cfg.AdminUser || pass != h.Cfg.AdminPassword {
			w.Header().Set("WWW-Authenticate", `Basic realm="wernanmail-admin"`)
			writeErr(w, http.StatusUnauthorized, "admin.unauthorized", "unauthorized")
			return
		}
		next.ServeHTTP(w, r)
	})
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
	if body.Username != h.Cfg.AdminUser || body.Password != h.Cfg.AdminPassword {
		writeErr(w, http.StatusUnauthorized, "admin.unauthorized", "invalid credentials")
		return
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
		Name     string `json:"name"`
		CatchAll string `json:"catchAll"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || strings.TrimSpace(body.Name) == "" {
		writeErr(w, http.StatusBadRequest, "admin.bad_request", "name required")
		return
	}
	d := &domain.Domain{Name: strings.ToLower(strings.TrimSpace(body.Name)), Enabled: true, CatchAll: body.CatchAll}
	if err := h.Store.UpsertDomain(r.Context(), d); err != nil {
		writeErr(w, http.StatusInternalServerError, "admin.store", err.Error())
		return
	}
	_ = h.Store.AddAudit(r.Context(), &domain.AuditEntry{Actor: h.Cfg.AdminUser, Action: "domain.create", Target: d.Name})
	writeJSON(w, http.StatusCreated, d)
}

func (h *Handler) genDKIM(w http.ResponseWriter, r *http.Request) {
	id, _ := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	domains, err := h.Store.ListDomains(r.Context())
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "admin.store", err.Error())
		return
	}
	var d *domain.Domain
	for i := range domains {
		if domains[i].ID == id {
			d = &domains[i]
			break
		}
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
	_ = h.Store.AddAudit(r.Context(), &domain.AuditEntry{Actor: h.Cfg.AdminUser, Action: "domain.dkim", Target: d.Name})
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
		QuotaBytes  int64  `json:"quotaBytes"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.LocalPart == "" || body.Password == "" {
		writeErr(w, http.StatusBadRequest, "admin.bad_request", "localPart and password required")
		return
	}
	hash, err := sqlite.HashPassword(body.Password)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "admin.hash", err.Error())
		return
	}
	if body.QuotaBytes == 0 {
		body.QuotaBytes = int64(h.Settings.GetInt(settings.KeyDefaultQuotaBytes, 200<<20))
	}
	m := &domain.Mailbox{
		DomainID: domainID, LocalPart: strings.ToLower(body.LocalPart),
		PasswordHash: hash, DisplayName: body.DisplayName, QuotaBytes: body.QuotaBytes, Enabled: true,
	}
	if err := h.Store.UpsertMailbox(r.Context(), m); err != nil {
		writeErr(w, http.StatusInternalServerError, "admin.store", err.Error())
		return
	}
	_ = h.Store.AddAudit(r.Context(), &domain.AuditEntry{
		Actor: h.Cfg.AdminUser, Action: "mailbox.create", Target: body.LocalPart,
	})
	writeJSON(w, http.StatusCreated, map[string]any{"id": m.ID, "localPart": m.LocalPart})
}

func (h *Handler) listQueue(w http.ResponseWriter, r *http.Request) {
	list, err := h.Queue.List(r.Context(), 100)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "admin.store", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, list)
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
	_ = h.Store.ResolveQuarantine(r.Context(), id, "release")
	_ = h.Store.AddAudit(r.Context(), &domain.AuditEntry{Actor: h.Cfg.AdminUser, Action: "quarantine.release", Target: strconv.FormatInt(id, 10)})
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (h *Handler) deleteQuarantine(w http.ResponseWriter, r *http.Request) {
	id, _ := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	_ = h.Store.ResolveQuarantine(r.Context(), id, "delete")
	_ = h.Store.AddAudit(r.Context(), &domain.AuditEntry{Actor: h.Cfg.AdminUser, Action: "quarantine.delete", Target: strconv.FormatInt(id, 10)})
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
	_ = h.Store.AddAudit(r.Context(), &domain.AuditEntry{Actor: h.Cfg.AdminUser, Action: "settings.update", Detail: strconv.Itoa(len(body))})
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

// Ensure bcrypt linked.
var _ = bcrypt.ErrMismatchedHashAndPassword
