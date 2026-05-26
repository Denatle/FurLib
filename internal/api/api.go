package api

import (
	"FurLib/internal/dispatcher"
	"FurLib/internal/librarian"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"go.uber.org/zap"
)

type API struct {
	router     *chi.Mux
	log        *zap.Logger
	dispatcher *dispatcher.Dispatcher
	librarian  *librarian.Librarian
}

func NewAPI(log *zap.Logger, d *dispatcher.Dispatcher, l *librarian.Librarian) *API {
	a := &API{log: log, dispatcher: d, librarian: l}
	a.router = a.buildRouter()
	return a
}

func (api *API) buildRouter() *chi.Mux {
	r := chi.NewRouter()

	r.Use(middleware.RequestID)
	r.Use(middleware.RealIP)
	r.Use(api.zapLogger())
	r.Use(middleware.Recoverer)

	r.Get("/health", api.handleHealth)

	r.Get("/api/v1/stream", api.handleStream)

	r.Group(func(r chi.Router) {
		r.Use(middleware.Timeout(30 * time.Second))

		r.Route("/api/v1", func(r chi.Router) {
			r.Route("/library", func(r chi.Router) {
				r.Get("/", api.handleSearch)
				r.Get("/health", api.handleLibraryHealth)
				r.Post("/heal", api.handleLibraryHeal)
				// /deleted must be registered before /{id} to avoid shadowing
				r.Get("/deleted", api.handleListDeleted)
				r.Delete("/deleted", api.handleClearDeleted)
				r.Get("/{id}", api.handleGetPost)
				r.Get("/{id}/file", api.handleGetFile)
				r.Delete("/{id}", api.handleDeletePost)
			})

			r.Route("/jobs", func(r chi.Router) {
				r.Post("/", api.handleCreateJob)
				r.Get("/", api.handleListJobs)
				r.Get("/{id}", api.handleGetJob)
				r.Delete("/{id}", api.handleCancelJob)
			})
		})
	})

	return r
}
