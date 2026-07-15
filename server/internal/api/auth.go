package api

import (
	"net/http"
	"strings"

	"github.com/Baddysays/wernanmail/server/internal/impersonate"
	"github.com/Baddysays/wernanmail/server/internal/mail"
	"github.com/Baddysays/wernanmail/server/internal/session"
)

type loginRequest struct {
	IMAPHost string `json:"imapHost"`
	IMAPPort int    `json:"imapPort"`
	SMTPHost string `json:"smtpHost"`
	SMTPPort int    `json:"smtpPort"`
	Username string `json:"username"`
	Password string `json:"password"`
	TLS      *bool  `json:"tls"`
}

type impersonateRequest struct {
	Token    string `json:"token"`
	IMAPHost string `json:"imapHost"`
	IMAPPort int    `json:"imapPort"`
	SMTPHost string `json:"smtpHost"`
	SMTPPort int    `json:"smtpPort"`
	TLS      *bool  `json:"tls"`
}

func (h *Handler) Health(w http.ResponseWriter, r *http.Request) {
	writeData(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (h *Handler) Login(w http.ResponseWriter, r *http.Request) {
	if h.loginGuard == nil {
		h.loginGuard = newLoginGuard()
	}
	if !h.loginGuard.allow(r) {
		writeError(w, http.StatusTooManyRequests, CodeAuthFailed)
		return
	}
	var req loginRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, CodeInvalidRequest)
		return
	}
	req.IMAPHost = strings.TrimSpace(req.IMAPHost)
	req.SMTPHost = strings.TrimSpace(req.SMTPHost)
	req.Username = strings.TrimSpace(req.Username)
	if req.IMAPHost == "" || req.SMTPHost == "" || req.Username == "" || req.Password == "" {
		writeError(w, http.StatusBadRequest, CodeInvalidRequest)
		return
	}
	if req.IMAPPort == 0 {
		req.IMAPPort = 993
	}
	if req.SMTPPort == 0 {
		req.SMTPPort = 587
	}
	useTLS := true
	if req.TLS != nil {
		useTLS = *req.TLS
	}

	creds := session.Credentials{
		IMAPHost: req.IMAPHost,
		IMAPPort: req.IMAPPort,
		SMTPHost: req.SMTPHost,
		SMTPPort: req.SMTPPort,
		Username: req.Username,
		Password: req.Password,
		TLS:      useTLS,
	}

	if err := mail.VerifyLogin(creds); err != nil {
		if h.loginGuard != nil {
			h.loginGuard.fail(r)
		}
		writeError(w, http.StatusUnauthorized, CodeAuthFailed)
		return
	}
	h.loginGuard.succeed(r)

	sess, err := h.Store.Create(creds)
	if err != nil {
		writeError(w, http.StatusInternalServerError, CodeInternal)
		return
	}
	h.setSessionCookie(w, sess.ID)
	writeData(w, http.StatusOK, map[string]any{
		"username":     creds.Username,
		"impersonated": false,
	})
}

func (h *Handler) Impersonate(w http.ResponseWriter, r *http.Request) {
	var req impersonateRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, CodeInvalidRequest)
		return
	}
	req.Token = strings.TrimSpace(req.Token)
	if req.Token == "" {
		writeError(w, http.StatusBadRequest, CodeInvalidRequest)
		return
	}
	if h.Cfg.SessionSecret == "" || h.Cfg.MasterPassword == "" {
		writeError(w, http.StatusServiceUnavailable, CodeImpersonateUnavailable)
		return
	}
	payload, err := impersonate.Parse(h.Cfg.SessionSecret, req.Token)
	if err != nil {
		writeError(w, http.StatusUnauthorized, CodeImpersonateInvalid)
		return
	}

	imapHost := strings.TrimSpace(req.IMAPHost)
	smtpHost := strings.TrimSpace(req.SMTPHost)
	if imapHost == "" {
		imapHost = h.Cfg.DefaultIMAPHost
	}
	if smtpHost == "" {
		smtpHost = h.Cfg.DefaultSMTPHost
	}
	imapPort := req.IMAPPort
	if imapPort == 0 {
		imapPort = h.Cfg.DefaultIMAPPort
	}
	smtpPort := req.SMTPPort
	if smtpPort == 0 {
		smtpPort = h.Cfg.DefaultSMTPPort
	}
	useTLS := h.Cfg.DefaultTLS
	if req.TLS != nil {
		useTLS = *req.TLS
	}

	creds := session.Credentials{
		IMAPHost: imapHost,
		IMAPPort: imapPort,
		SMTPHost: smtpHost,
		SMTPPort: smtpPort,
		Username: payload.Username,
		Password: h.Cfg.MasterPassword,
		TLS:      useTLS,
	}
	if err := mail.VerifyLogin(creds); err != nil {
		writeError(w, http.StatusUnauthorized, CodeAuthFailed)
		return
	}
	sess, err := h.Store.CreateWith(creds, session.CreateOpts{
		Impersonated:   true,
		ImpersonatedBy: payload.Actor,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, CodeInternal)
		return
	}
	h.setSessionCookie(w, sess.ID)
	writeData(w, http.StatusOK, map[string]any{
		"username":       creds.Username,
		"impersonated":   true,
		"impersonatedBy": payload.Actor,
	})
}

func (h *Handler) Me(w http.ResponseWriter, r *http.Request) {
	sess := sessionFrom(r)
	writeData(w, http.StatusOK, map[string]any{
		"username":       sess.Creds.Username,
		"impersonated":   sess.Impersonated,
		"impersonatedBy": sess.ImpersonatedBy,
	})
}

func (h *Handler) Logout(w http.ResponseWriter, r *http.Request) {
	if cookie, err := r.Cookie(h.Cfg.SessionCookie); err == nil && cookie.Value != "" {
		h.Store.Delete(cookie.Value)
	}
	h.clearSessionCookie(w)
	writeData(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (h *Handler) ListFolders(w http.ResponseWriter, r *http.Request) {
	sess := sessionFrom(r)
	folders, err := mail.ListFolders(sess.Creds)
	if err != nil {
		writeError(w, http.StatusBadGateway, CodeFolderListFailed)
		return
	}
	writeData(w, http.StatusOK, folders)
}
