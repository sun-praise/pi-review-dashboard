// pi-review-dashboard: cross-repo statistics for the pi-review-agent fleet.
//
// One static binary serves everything: the ingest API (POST /api/events),
// the read API (GET /api/dashboard), and the embedded Vite frontend. State
// is a single SQLite file — back it up by copying it.
//
// Env:
//
//	PORT        listen port               (default 8787)
//	STATS_DB    sqlite path               (default data/stats.db)
//	STATS_TOKEN ingest bearer token; unset = open ingest (intranet default)
//
// Flags:
//
//	-seed N   write N demo events and exit (first-run eyeballing)
package main

import (
	"embed"
	"flag"
	"fmt"
	"io/fs"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/sun-praise/pi-review-dashboard/internal/api"
	"github.com/sun-praise/pi-review-dashboard/internal/store"
)

//go:embed all:web/dist
var distFS embed.FS

func main() {
	seed := flag.Int("seed", 0, "write N demo events into the database, then exit")
	flag.Parse()

	dbPath := envOr("STATS_DB", filepath.Join("data", "stats.db"))
	if err := os.MkdirAll(filepath.Dir(dbPath), 0o755); err != nil {
		log.Fatalf("create db dir: %v", err)
	}
	st, err := store.Open(dbPath)
	if err != nil {
		log.Fatalf("open store: %v", err)
	}
	defer st.Close()

	if *seed > 0 {
		if err := store.SeedDemo(st, *seed); err != nil {
			log.Fatalf("seed: %v", err)
		}
		fmt.Printf("seeded %d demo events into %s\n", *seed, dbPath)
		return
	}

	mux := http.NewServeMux()
	api.New(st, os.Getenv("STATS_TOKEN")).Register(mux)

	dist, err := fs.Sub(distFS, "web/dist")
	if err != nil {
		log.Fatalf("embed: %v", err)
	}
	mux.Handle("/", spaHandler(dist))

	addr := ":" + envOr("PORT", "8787")
	log.Printf("pi-review-dashboard listening on http://0.0.0.0%s (db=%s, ingest auth=%v)",
		addr, dbPath, os.Getenv("STATS_TOKEN") != "")
	srv := &http.Server{
		Addr:              addr,
		Handler:           logRequests(mux),
		ReadHeaderTimeout: 5 * time.Second,
	}
	log.Fatal(srv.ListenAndServe())
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

// spaHandler serves the embedded Vite build with SPA fallback: unknown paths
// get index.html so client-side routing keeps working on refresh. Note the
// /api/* routes are registered before "/" on the same mux and win by pattern
// precedence, so they never fall through to the static handler.
func spaHandler(dist fs.FS) http.Handler {
	files := http.FileServer(http.FS(dist))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p := strings.TrimPrefix(r.URL.Path, "/")
		if p == "" {
			p = "index.html"
		}
		if _, err := fs.Stat(dist, p); err != nil {
			r.URL.Path = "/"
		}
		files.ServeHTTP(w, r)
	})
}

func logRequests(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		rec := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(rec, r)
		log.Printf("%s %s -> %d (%s)", r.Method, r.URL.Path, rec.status, time.Since(start).Round(time.Millisecond))
	})
}

type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (r *statusRecorder) WriteHeader(code int) {
	r.status = code
	r.ResponseWriter.WriteHeader(code)
}
