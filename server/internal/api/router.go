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
	r.Use(middleware.Timeout(60 * time.Second))
	r.Use(cors.Handler(cors.Options{
		AllowedOrigins:   h.Cfg.CORSOrigins,
		AllowedMethods:   []string{"GET", "POST", "PUT", "PATCH", "DELETE", "OPTIONS"},
		AllowedHeaders:   []string{"Accept", "Authorization", "Content-Type", "X-Requested-With"},
		AllowCredentials: true,
		MaxAge:           300,
	}))

	// Container / probe health (no /api prefix)
	r.Get("/healthz", h.Health)

	r.Route("/api", func(r chi.Router) {
		r.Get("/health", h.Health)

		r.Post("/auth/login", h.Login)
		r.Post("/auth/logout", h.Logout)

		r.Group(func(r chi.Router) {
			r.Use(h.RequireSession)
			r.Get("/folders", h.ListFolders)
			r.Get("/mailboxes", h.ListFolders) // alias
			r.Get("/messages", h.ListMessages)
			r.Get("/messages/{id}", h.GetMessage)
			r.Post("/messages/send", h.SendMessage)
			r.Patch("/messages/{id}/flags", h.UpdateMessageFlags)
			r.Post("/messages/{id}/trash", h.TrashMessage)
			r.Delete("/messages/{id}", h.TrashMessage)
		})
	})

	return r
}
