package api

import (
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
)

func (api *API) handleHealth(w http.ResponseWriter, r *http.Request) {
	respond(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (api *API) handleSearch(w http.ResponseWriter, r *http.Request) {
	tags := r.URL.Query().Get("tags")
	data, err := api.fetcher.SearchByTags("e621", strings.Split(tags, "+"), 1)
	if err != nil {
		respondErr(w, http.StatusInternalServerError, err)
	}
	respond(w, http.StatusOK, data)
}

func (api *API) handleGetPost(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	_ = id
	respond(w, http.StatusOK, nil)
}

func (api *API) handleCreateJob(w http.ResponseWriter, r *http.Request) {
	// decode body, validate, api.dispatcher.Submit(...)
	respond(w, http.StatusAccepted, nil)
}

func (api *API) handleListJobs(w http.ResponseWriter, r *http.Request) {
	respond(w, http.StatusOK, nil)
}
func (api *API) handleGetJob(w http.ResponseWriter, r *http.Request) { respond(w, http.StatusOK, nil) }
func (api *API) handleCancelJob(w http.ResponseWriter, r *http.Request) {
	respond(w, http.StatusOK, nil)
}
func (api *API) handleListSources(w http.ResponseWriter, r *http.Request) {
	respond(w, http.StatusOK, nil)
}
