package api

import (
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/go-chi/cors"
)

func NewRouter(h *Handler) http.Handler {
	r := chi.NewRouter()
	r.Use(middleware.RequestID)
	r.Use(middleware.RealIP)
	r.Use(middleware.Logger)
	r.Use(middleware.Recoverer)
	r.Use(middleware.Timeout(180 * time.Second))
	r.Use(cors.Handler(cors.Options{
		AllowedOrigins:   h.Cfg.CORSOrigins,
		AllowedMethods:   []string{"GET", "POST", "PUT", "PATCH", "DELETE", "OPTIONS"},
		AllowedHeaders:   []string{"Accept", "Authorization", "Content-Type", "X-Requested-With"},
		AllowCredentials: true,
		MaxAge:           300,
	}))

	// Container / probe health (no /api prefix)
	r.Get("/healthz", h.Health)
	r.Get("/.well-known/mta-sts.txt", h.MTAStsPolicy)

	r.Route("/api", func(r chi.Router) {
		r.Get("/health", h.Health)

		r.Post("/auth/login", h.Login)
		r.Post("/auth/impersonate", h.Impersonate)
		r.Post("/auth/logout", h.Logout)

		r.Group(func(r chi.Router) {
			r.Use(h.RequireSession)
			r.Get("/auth/me", h.Me)
			r.Get("/mta-sts/dns", h.MTAStsDNS)
			r.Get("/folders", h.ListFolders)
			r.Get("/mailboxes", h.ListFolders) // alias
			r.Get("/messages", h.ListMessages)
			r.Get("/messages/search", h.SearchMessages)
			r.Get("/messages/{id}", h.GetMessage)
			r.Get("/messages/{id}/attachments/{part}", h.GetAttachment)
			r.Post("/messages/send", h.SendMessage)
			r.Post("/messages/drafts", h.SaveDraft)
			r.Patch("/messages/{id}/flags", h.UpdateMessageFlags)
			r.Post("/messages/{id}/trash", h.TrashMessage)
			r.Post("/messages/{id}/move", h.MoveMessage)
			r.Delete("/messages/{id}", h.TrashMessage)
		})
	})

	return r
}
