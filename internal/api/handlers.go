package api

import (
	"FurLib/internal/dispatcher"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
)

func (api *API) handleHealth(w http.ResponseWriter, r *http.Request) {
	respond(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (api *API) handleSearch(w http.ResponseWriter, r *http.Request) {
	var tags []string
	if raw := r.URL.Query().Get("tags"); raw != "" {
		tags = strings.Split(raw, "+")
	}
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	if limit <= 0 {
		limit = 20
	}
	sort := r.URL.Query().Get("sort")
	data, err := api.librarian.Search(tags, limit, sort)
	if err != nil {
		respondErr(w, http.StatusInternalServerError, err)
		return
	}
	respond(w, http.StatusOK, data)
}

func (api *API) handleGetPost(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseUint(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		respondErr(w, http.StatusBadRequest, fmt.Errorf("invalid id"))
		return
	}
	post, err := api.librarian.GetPost(uint(id))
	if err != nil {
		respondErr(w, http.StatusNotFound, err)
		return
	}
	respond(w, http.StatusOK, post)
}

func (api *API) handleGetFile(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseUint(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		respondErr(w, http.StatusBadRequest, fmt.Errorf("invalid id"))
		return
	}
	post, err := api.librarian.GetPost(uint(id))
	if err != nil {
		respondErr(w, http.StatusNotFound, err)
		return
	}
	http.ServeFile(w, r, post.FilePath)
}

type createJobRequest struct {
	Tags      []string   `json:"tags"`
	Limit     int        `json:"limit"`
	Sources   []string   `json:"sources,omitempty"`
	NewerThan *time.Time `json:"newer_than,omitempty"`
	OlderThan *time.Time `json:"older_than,omitempty"`
}

func (api *API) handleCreateJob(w http.ResponseWriter, r *http.Request) {
	var req createJobRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondErr(w, http.StatusBadRequest, err)
		return
	}

	opts := dispatcher.Options{
		Sources:   req.Sources,
		NewerThan: req.NewerThan,
		OlderThan: req.OlderThan,
	}

	id, err := api.dispatcher.Submit(req.Tags, req.Limit, opts)
	if err != nil {
		respondErr(w, http.StatusInternalServerError, err)
		return
	}

	respond(w, http.StatusAccepted, map[string]string{"id": id})
}

func (api *API) handleListJobs(w http.ResponseWriter, r *http.Request) {
	respond(w, http.StatusOK, api.dispatcher.ListJobs())
}

func (api *API) handleGetJob(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	job, err := api.dispatcher.GetJob(id)
	if err != nil {
		respondErr(w, http.StatusNotFound, err)
		return
	}
	respond(w, http.StatusOK, job)
}

func (api *API) handleCancelJob(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if err := api.dispatcher.CancelJob(id); err != nil {
		respondErr(w, http.StatusNotFound, err)
		return
	}
	respond(w, http.StatusOK, nil)
}

func (api *API) handleLibraryHealth(w http.ResponseWriter, r *http.Request) {
	report, err := api.librarian.Check()
	if err != nil {
		respondErr(w, http.StatusInternalServerError, err)
		return
	}
	respond(w, http.StatusOK, report)
}

func (api *API) handleLibraryHeal(w http.ResponseWriter, r *http.Request) {
	report, err := api.librarian.Heal(r.Context())
	if err != nil {
		respondErr(w, http.StatusInternalServerError, err)
		return
	}
	respond(w, http.StatusOK, report)
}

func (api *API) handleListSources(w http.ResponseWriter, r *http.Request) {
	respond(w, http.StatusOK, nil)
}

func (api *API) handleStream(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		respondErr(w, http.StatusInternalServerError, fmt.Errorf("streaming not supported"))
		return
	}

	tags := strings.Split(r.URL.Query().Get("tags"), "+")
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	if limit <= 0 {
		limit = 10
	}

	opts := dispatcher.Options{
		Sources: parseCommaSeparated(r.URL.Query().Get("sources")),
	}
	if s := r.URL.Query().Get("newer_than"); s != "" {
		if t, err := time.Parse(time.RFC3339, s); err == nil {
			opts.NewerThan = &t
		}
	}
	if s := r.URL.Query().Get("older_than"); s != "" {
		if t, err := time.Parse(time.RFC3339, s); err == nil {
			opts.OlderThan = &t
		}
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	for event := range api.dispatcher.Stream(r.Context(), tags, limit, opts) {
		data, err := json.Marshal(event)
		if err != nil {
			continue
		}
		fmt.Fprintf(w, "data: %s\n\n", data)
		flusher.Flush()
	}
}

func parseCommaSeparated(s string) []string {
	if s == "" {
		return nil
	}
	parts := strings.Split(s, ",")
	out := parts[:0]
	for _, p := range parts {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}
