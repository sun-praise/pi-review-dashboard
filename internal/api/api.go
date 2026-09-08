// Package api exposes the dashboard's HTTP surface: one ingest endpoint the
// pi-review-agents POST to (bearer-token auth, idempotent) and one read
// endpoint the embedded frontend polls. Everything is same-origin in
// production (the frontend is served from this binary), so no CORS.
package api

import (
	"encoding/json"
	"io"
	"net/http"
	"strconv"

	"github.com/sun-praise/pi-review-dashboard/internal/store"
)

const maxIngestBytes = 1 << 20 // one event ≈ 1KB; 1MiB covers large CI batches

type Server struct {
	store *store.Store
	token string
}

func New(st *store.Store, token string) *Server {
	return &Server{store: st, token: token}
}

func (s *Server) Register(mux *http.ServeMux) {
	mux.HandleFunc("POST /api/events", s.handleIngest)
	mux.HandleFunc("GET /api/dashboard", s.handleDashboard)
	mux.HandleFunc("GET /api/healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})
}

func (s *Server) authorized(r *http.Request) bool {
	if s.token == "" {
		return true
	}
	return r.Header.Get("Authorization") == "Bearer "+s.token
}

func (s *Server) handleIngest(w http.ResponseWriter, r *http.Request) {
	if !s.authorized(r) {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	body := http.MaxBytesReader(w, r.Body, maxIngestBytes)
	raw, err := io.ReadAll(body)
	if err != nil {
		http.Error(w, "body too large or unreadable", http.StatusRequestEntityTooLarge)
		return
	}
	// The agent POSTs a single event object; batches (arrays) are accepted
	// for forward compatibility. A literal "null" decodes to a nil slice
	// without error, so it is rejected explicitly.
	var events []store.Event
	if err := json.Unmarshal(raw, &events); err != nil {
		var one store.Event
		if errOne := json.Unmarshal(raw, &one); errOne != nil {
			http.Error(w, "body must be a stats event object or array", http.StatusBadRequest)
			return
		}
		events = []store.Event{one}
	}
	// "null" decodes to a nil slice and "[]" to an empty one — both carry
	// no events, reject instead of silently 202-ing nothing.
	if len(events) == 0 {
		http.Error(w, "empty payload", http.StatusBadRequest)
		return
	}
	res, err := s.store.InsertEvents(events)
	if err != nil {
		http.Error(w, "insert failed: "+err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	// 202: the event is accepted but aggregation is read-side; partial
	// invalid items ride along in the response instead of failing the batch.
	w.WriteHeader(http.StatusAccepted)
	_ = json.NewEncoder(w).Encode(res)
}

func (s *Server) handleDashboard(w http.ResponseWriter, r *http.Request) {
	days := 30
	if raw := r.URL.Query().Get("days"); raw != "" {
		if n, err := strconv.Atoi(raw); err == nil {
			days = n
		}
	}
	d, err := s.store.Dashboard(store.Window{Days: days, Repo: r.URL.Query().Get("repo")})
	if err != nil {
		http.Error(w, "query failed: "+err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(d)
}
