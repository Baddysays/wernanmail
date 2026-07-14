package api

import (
	"context"
	"net/http"

	"github.com/Baddysays/wernanmail/server/internal/config"
	"github.com/Baddysays/wernanmail/server/internal/session"
)

type ctxKey int

const sessionKey ctxKey = 1

type Handler struct {
	Cfg   config.Config
	Store *session.Store
}

func (h *Handler) RequireSession(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		cookie, err := r.Cookie(h.Cfg.SessionCookie)
		if err != nil || cookie.Value == "" {
			writeError(w, http.StatusUnauthorized, CodeSessionRequired)
			return
		}
		sess, ok := h.Store.Get(cookie.Value)
		if !ok {
			writeError(w, http.StatusUnauthorized, CodeSessionInvalid)
			return
		}
		ctx := context.WithValue(r.Context(), sessionKey, sess)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

func sessionFrom(r *http.Request) *session.Session {
	sess, _ := r.Context().Value(sessionKey).(*session.Session)
	return sess
}

func (h *Handler) setSessionCookie(w http.ResponseWriter, id string) {
	http.SetCookie(w, &http.Cookie{
		Name:     h.Cfg.SessionCookie,
		Value:    id,
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		Secure:   h.Cfg.CookieSecure,
		MaxAge:   h.Cfg.SessionTTLHours * 3600,
	})
}

func (h *Handler) clearSessionCookie(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{
		Name:     h.Cfg.SessionCookie,
		Value:    "",
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		Secure:   h.Cfg.CookieSecure,
		MaxAge:   -1,
	})
}
