package store

import (
	"path/filepath"
	"testing"
	"time"
)

func openTestStore(t *testing.T) *Store {
	t.Helper()
	st, err := Open(filepath.Join(t.TempDir(), "stats.db"))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { st.Close() })
	return st
}

func testEvent(repo, runID string, ts time.Time) Event {
	input := int64(100_000)
	cacheRead := int64(300_000)
	verdict := "CAN MERGE"
	return Event{
		Ts:         ts.UTC().Format(time.RFC3339),
		Platform:   "github",
		Repository: repo,
		Pr:         42,
		RunID:      runID,
		Attempt:    1,
		Mode:       "team",
		Personas: []PersonaUsage{
			{Name: "quality", Input: 60_000, Output: 1_000, CacheRead: 180_000, Cost: 0.01},
			{Name: "security", Input: 40_000, Output: 500, CacheRead: 120_000, Cost: 0.02},
		},
		Verdict:    &verdict,
		Severity:   SeverityInfo{Decision: verdict, Warning: 1},
		Usage:      Usage{Input: input, Output: 1_500, CacheRead: cacheRead},
		CostTotal:  0.03,
		DurationMs: ptr(int64(45_000)),
	}
}

func TestInsertDedupe(t *testing.T) {
	st := openTestStore(t)
	ev := testEvent("owner/repo", "run-1", time.Now())

	res, err := st.InsertEvents([]Event{ev})
	if err != nil {
		t.Fatalf("insert: %v", err)
	}
	if res.Inserted != 1 || res.Skipped != 0 {
		t.Fatalf("first insert = %+v, want inserted=1", res)
	}

	// Same (platform, repository, run_id, attempt) — HTTP retry or CI re-run.
	res, err = st.InsertEvents([]Event{ev})
	if err != nil {
		t.Fatalf("re-insert: %v", err)
	}
	if res.Inserted != 0 || res.Skipped != 1 {
		t.Fatalf("duplicate insert = %+v, want skipped=1", res)
	}

	// Same run re-attempted (attempt 2) is a NEW event.
	ev2 := ev
	ev2.Attempt = 2
	res, err = st.InsertEvents([]Event{ev2})
	if err != nil {
		t.Fatalf("attempt-2 insert: %v", err)
	}
	if res.Inserted != 1 {
		t.Fatalf("attempt-2 insert = %+v, want inserted=1", res)
	}
}

func TestInsertValidation(t *testing.T) {
	st := openTestStore(t)
	bad := []Event{
		{Ts: time.Now().Format(time.RFC3339)},      // no repository
		{Repository: "o/r", Ts: "not-a-timestamp"}, // bad ts
	}
	res, err := st.InsertEvents(bad)
	if err != nil {
		t.Fatalf("insert: %v", err)
	}
	if res.Inserted != 0 || len(res.Invalid) != 2 {
		t.Fatalf("res = %+v, want 2 invalid, 0 inserted", res)
	}

	// A missing run_id must NOT dedupe later inserts against itself.
	e1 := testEvent("o/r", "", time.Now())
	e2 := testEvent("o/r", "", time.Now())
	res, err = st.InsertEvents([]Event{e1, e2})
	if err != nil {
		t.Fatalf("insert null-runid: %v", err)
	}
	if res.Inserted != 2 {
		t.Fatalf("null run_id deduped: %+v", res)
	}
}

func TestDashboardAggregates(t *testing.T) {
	st := openTestStore(t)
	now := time.Now()
	events := []Event{
		testEvent("owner/alpha", "r1", now),
		testEvent("owner/alpha", "r2", now.Add(-2*time.Hour)),
		testEvent("owner/beta", "r3", now.Add(-3*time.Hour)),
		testEvent("owner/alpha", "r-old", now.Add(-30*24*time.Hour)), // outside 7d
	}
	if _, err := st.InsertEvents(events); err != nil {
		t.Fatalf("insert: %v", err)
	}

	// All time, all repos: input 4×100k, cacheRead 4×300k → hit rate 0.75.
	d, err := st.Dashboard(Window{})
	if err != nil {
		t.Fatalf("dashboard: %v", err)
	}
	if d.Summary.Reviews != 4 {
		t.Fatalf("reviews = %d, want 4", d.Summary.Reviews)
	}
	if d.Summary.Input != 400_000 || d.Summary.CacheRead != 1_200_000 {
		t.Fatalf("usage = %+v", d.Summary)
	}
	if d.Summary.CacheHitRate < 0.7499 || d.Summary.CacheHitRate > 0.7501 {
		t.Fatalf("cacheHitRate = %f, want 0.75", d.Summary.CacheHitRate)
	}
	if d.Summary.AvgDurationS != 45 {
		t.Fatalf("avgDurationS = %f, want 45", d.Summary.AvgDurationS)
	}
	if len(d.Repos) != 2 || d.Repos[0].Repository != "owner/alpha" {
		t.Fatalf("repos = %+v", d.Repos)
	}
	if d.Repos[0].Reviews != 3 || d.Repos[1].Reviews != 1 {
		t.Fatalf("repo reviews = %+v", d.Repos)
	}

	// 7-day window drops the 30-day-old event.
	d, err = st.Dashboard(Window{Days: 7})
	if err != nil {
		t.Fatalf("dashboard 7d: %v", err)
	}
	if d.Summary.Reviews != 3 {
		t.Fatalf("7d reviews = %d, want 3", d.Summary.Reviews)
	}

	// Repo filter.
	d, err = st.Dashboard(Window{Repo: "owner/beta"})
	if err != nil {
		t.Fatalf("dashboard repo: %v", err)
	}
	if d.Summary.Reviews != 1 || len(d.Repos) != 1 {
		t.Fatalf("beta window = %+v", d.Summary)
	}

	// Verdict rows and recent list carry the single-mode (empty) verdict too.
	single := testEvent("owner/alpha", "r-single", now)
	single.Mode = "single"
	single.Verdict = nil
	if _, err := st.InsertEvents([]Event{single}); err != nil {
		t.Fatalf("insert single: %v", err)
	}
	d, err = st.Dashboard(Window{Repo: "owner/alpha"})
	if err != nil {
		t.Fatalf("dashboard: %v", err)
	}
	var emptyVerdicts int64
	for _, v := range d.Verdicts {
		if v.Verdict == "" {
			emptyVerdicts = v.Count
		}
	}
	if emptyVerdicts != 1 {
		t.Fatalf("empty-verdict count = %d, want 1 (single-mode run)", emptyVerdicts)
	}
	if len(d.Recent) == 0 || d.Recent[0].Personas != 2 {
		t.Fatalf("recent = %+v", d.Recent)
	}
}
