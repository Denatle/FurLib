package api

import (
	"FurLibrarer/internal/fetcher"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"go.uber.org/zap"
)

type API struct {
	router *chi.Mux
	log    *zap.Logger

	fetcher *fetcher.Fetcher
}

func NewAPI(log *zap.Logger, f *fetcher.Fetcher) *API {
	a := &API{log: log, fetcher: f}
	a.router = a.buildRouter()
	return a
}

func (api *API) buildRouter() *chi.Mux {
	r := chi.NewRouter()

	r.Use(middleware.RequestID)
	r.Use(middleware.RealIP)
	r.Use(api.zapLogger())
	r.Use(middleware.Recoverer)
	r.Use(middleware.Timeout(30 * time.Second))

	r.Get("/health", api.handleHealth)

	r.Route("/api/v1", func(r chi.Router) {
		r.Route("/library", func(r chi.Router) {
			r.Get("/", api.handleSearch)
			r.Get("/{id}", api.handleGetPost)
		})

		r.Route("/jobs", func(r chi.Router) {
			r.Post("/", api.handleCreateJob)
			r.Get("/", api.handleListJobs)
			r.Get("/{id}", api.handleGetJob)
			r.Delete("/{id}", api.handleCancelJob)
		})
	})

	return r
}
