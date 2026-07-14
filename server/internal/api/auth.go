package api

import (
	"net/http"
	"strings"

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

func (h *Handler) Health(w http.ResponseWriter, r *http.Request) {
	writeData(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (h *Handler) Login(w http.ResponseWriter, r *http.Request) {
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
		writeError(w, http.StatusUnauthorized, CodeAuthFailed)
		return
	}

	sess, err := h.Store.Create(creds)
	if err != nil {
		writeError(w, http.StatusInternalServerError, CodeInternal)
		return
	}
	h.setSessionCookie(w, sess.ID)
	writeData(w, http.StatusOK, map[string]string{
		"username": creds.Username,
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
