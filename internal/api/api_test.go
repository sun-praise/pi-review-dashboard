package api

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"github.com/sun-praise/pi-review-dashboard/internal/store"
)

func newTestServer(t *testing.T, token string) (*Server, *httptest.Server) {
	t.Helper()
	st, err := store.Open(filepath.Join(t.TempDir(), "stats.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { st.Close() })
	srv := New(st, token)
	ts := httptest.NewServer(srv.router())
	t.Cleanup(ts.Close)
	return srv, ts
}

// router builds the mux exactly like main() does, so tests exercise the
// same method+pattern registrations.
func (s *Server) router() http.Handler {
	mux := http.NewServeMux()
	s.Register(mux)
	return mux
}

func eventJSON(repo, runID string) string {
	return fmt.Sprintf(`{"schema":1,"ts":%q,"platform":"github","repository":%q,"pr":7,`+
		`"runId":%q,"attempt":1,"mode":"team","personas":[{"name":"quality","input":1000,"output":100,`+
		`"cacheRead":5000,"cacheWrite":0,"cost":0.001,"resumed":true}],`+
		`"verdict":"CAN MERGE","severity":{"decision":"CAN MERGE","blocking":0,"warning":0,"fallback":false},`+
		`"usage":{"input":1000,"output":100,"cacheRead":5000,"cacheWrite":0},"costTotal":0.001,"durationMs":12000}`,
		time.Now().UTC().Format(time.RFC3339), repo, runID)
}

func TestIngestAuth(t *testing.T) {
	_, ts := newTestServer(t, "sekrit")

	resp, err := http.Post(ts.URL+"/api/events", "application/json", bytes.NewBufferString(eventJSON("o/r", "r1")))
	if err != nil {
		t.Fatalf("post: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("no token = %d, want 401", resp.StatusCode)
	}

	req, _ := http.NewRequest("POST", ts.URL+"/api/events", bytes.NewBufferString(eventJSON("o/r", "r1")))
	req.Header.Set("Authorization", "Bearer sekrit")
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("post: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusAccepted {
		t.Fatalf("with token = %d, want 202", resp.StatusCode)
	}
}

func TestIngestSingleArrayAndDedupe(t *testing.T) {
	_, ts := newTestServer(t, "")

	// Single object.
	resp, err := http.Post(ts.URL+"/api/events", "application/json", bytes.NewBufferString(eventJSON("o/r", "r1")))
	if err != nil {
		t.Fatalf("post: %v", err)
	}
	defer resp.Body.Close()
	var res store.InsertResult
	if err := json.NewDecoder(resp.Body).Decode(&res); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if res.Inserted != 1 {
		t.Fatalf("single = %+v", res)
	}

	// Batch with a duplicate + an invalid entry.
	batch := fmt.Sprintf("[%s,%s,%s]",
		eventJSON("o/r", "r1"), eventJSON("o/other", "r2"),
		`{"ts":"2026-09-08T00:00:00Z","repository":""}`)
	resp2, err := http.Post(ts.URL+"/api/events", "application/json", bytes.NewBufferString(batch))
	if err != nil {
		t.Fatalf("post batch: %v", err)
	}
	defer resp2.Body.Close()
	if err := json.NewDecoder(resp2.Body).Decode(&res); err != nil {
		t.Fatalf("decode batch: %v", err)
	}
	if res.Inserted != 1 || res.Skipped != 1 || len(res.Invalid) != 1 {
		t.Fatalf("batch = %+v, want 1 inserted / 1 skipped / 1 invalid", res)
	}

	// Garbage payloads are 400s, not 500s.
	for _, body := range []string{"{", "null", "[]"} {
		resp, err := http.Post(ts.URL+"/api/events", "application/json", bytes.NewBufferString(body))
		if err != nil {
			t.Fatalf("post %q: %v", body, err)
		}
		resp.Body.Close()
		if resp.StatusCode != http.StatusBadRequest {
			t.Fatalf("body %q = %d, want 400", body, resp.StatusCode)
		}
	}
}

func TestDashboardEndpoint(t *testing.T) {
	_, ts := newTestServer(t, "")
	if _, err := http.Post(ts.URL+"/api/events", "application/json", bytes.NewBufferString(eventJSON("o/r", "r1"))); err != nil {
		t.Fatalf("ingest: %v", err)
	}

	resp, err := http.Get(ts.URL + "/api/dashboard?days=30")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d", resp.StatusCode)
	}
	var d store.Dashboard
	if err := json.NewDecoder(resp.Body).Decode(&d); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if d.Summary.Reviews != 1 || d.Summary.Input != 1000 || d.Summary.CacheRead != 5000 {
		t.Fatalf("summary = %+v", d.Summary)
	}
	// cacheRead 5000 / (1000 + 5000) = 0.8333
	if d.Summary.CacheHitRate < 0.83 || d.Summary.CacheHitRate > 0.84 {
		t.Fatalf("cacheHitRate = %f", d.Summary.CacheHitRate)
	}
	if len(d.Recent) != 1 || d.Recent[0].Verdict != "CAN MERGE" {
		t.Fatalf("recent = %+v", d.Recent)
	}
}
