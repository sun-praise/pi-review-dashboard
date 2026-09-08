// Package store persists review-run stats events in SQLite and answers the
// dashboard's aggregate queries. One event == one completed review run; the
// emission contract lives in pi-review-agent's src/stats.ts (fields are
// additive-only, never renamed).
//
// Dedupe: (platform, repository, run_id, attempt) is UNIQUE and inserts are
// INSERT OR IGNORE, so CI re-runs and HTTP retries count once. A missing
// run_id is stored as NULL — SQLite treats NULLs as distinct in a UNIQUE
// index, so unidentifiable producers never false-dedupe against themselves.
package store

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	_ "modernc.org/sqlite" // pure-Go driver: CGO_ENABLED=0 builds, no libc dependency
)

type PersonaUsage struct {
	Name       string  `json:"name"`
	Input      int64   `json:"input"`
	Output     int64   `json:"output"`
	CacheRead  int64   `json:"cacheRead"`
	CacheWrite int64   `json:"cacheWrite"`
	Cost       float64 `json:"cost"`
	Resumed    bool    `json:"resumed"`
	Error      string  `json:"error,omitempty"`
}

type Usage struct {
	Input      int64 `json:"input"`
	Output     int64 `json:"output"`
	CacheRead  int64 `json:"cacheRead"`
	CacheWrite int64 `json:"cacheWrite"`
}

type SeverityInfo struct {
	Decision string `json:"decision"`
	Blocking int64  `json:"blocking"`
	Warning  int64  `json:"warning"`
	Fallback bool   `json:"fallback"`
}

// Event mirrors the agent's StatsEvent JSON one-to-one. Validation happens
// at insert time: ts must parse as RFC3339 and repository must be non-empty.
type Event struct {
	Schema     *int           `json:"schema"`
	Ts         string         `json:"ts"`
	Platform   string         `json:"platform"`
	Repository string         `json:"repository"`
	Pr         int64          `json:"pr"`
	RunID      string         `json:"runId"`
	Attempt    int64          `json:"attempt"`
	Mode       string         `json:"mode"`
	Personas   []PersonaUsage `json:"personas"`
	Verdict    *string        `json:"verdict"`
	Severity   SeverityInfo   `json:"severity"`
	Usage      Usage          `json:"usage"`
	CostTotal  float64        `json:"costTotal"`
	DurationMs *int64         `json:"durationMs"`
}

const schemaSQL = `
CREATE TABLE IF NOT EXISTS events (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	ts TEXT NOT NULL,
	platform TEXT NOT NULL DEFAULT '',
	repository TEXT NOT NULL,
	pr INTEGER NOT NULL DEFAULT 0,
	run_id TEXT,
	attempt INTEGER NOT NULL DEFAULT 1,
	mode TEXT NOT NULL DEFAULT '',
	personas TEXT NOT NULL DEFAULT '[]',
	verdict TEXT,
	severity_decision TEXT NOT NULL DEFAULT '',
	severity_blocking INTEGER NOT NULL DEFAULT 0,
	severity_warning INTEGER NOT NULL DEFAULT 0,
	input INTEGER NOT NULL DEFAULT 0,
	output INTEGER NOT NULL DEFAULT 0,
	cache_read INTEGER NOT NULL DEFAULT 0,
	cache_write INTEGER NOT NULL DEFAULT 0,
	cost_total REAL NOT NULL DEFAULT 0,
	duration_ms INTEGER,
	raw TEXT NOT NULL
);
CREATE UNIQUE INDEX IF NOT EXISTS idx_events_dedupe
	ON events(platform, repository, run_id, attempt);
CREATE INDEX IF NOT EXISTS idx_events_ts ON events(ts);
CREATE INDEX IF NOT EXISTS idx_events_repo ON events(repository);
`

type Store struct {
	db *sql.DB
}

func Open(path string) (*Store, error) {
	// WAL keeps concurrent dashboard reads unblocked while CI ingests;
	// busy_timeout rides out the rare writer contention window.
	dsn := "file:" + path + "?_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)"
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("open sqlite: %w", err)
	}
	// modernc/sqlite serializes writes internally; one connection avoids
	// SQLITE_BUSY between the migrate statements below.
	db.SetMaxOpenConns(1)
	if _, err := db.Exec(schemaSQL); err != nil {
		db.Close()
		return nil, fmt.Errorf("migrate: %w", err)
	}
	return &Store{db: db}, nil
}

func (s *Store) Close() error { return s.db.Close() }

type InvalidEvent struct {
	Repository string `json:"repository"`
	Reason     string `json:"reason"`
}

type InsertResult struct {
	Inserted int64          `json:"inserted"`
	Skipped  int64          `json:"skipped"`
	Invalid  []InvalidEvent `json:"invalid,omitempty"`
}

func validateEvent(e Event) string {
	if strings.TrimSpace(e.Repository) == "" {
		return "repository is required"
	}
	if _, err := time.Parse(time.RFC3339, e.Ts); err != nil {
		return "ts must be RFC3339"
	}
	return ""
}

func (s *Store) InsertEvents(events []Event) (InsertResult, error) {
	var res InsertResult
	tx, err := s.db.Begin()
	if err != nil {
		return res, err
	}
	defer tx.Rollback()
	stmt, err := tx.Prepare(`INSERT OR IGNORE INTO events
		(ts, platform, repository, pr, run_id, attempt, mode, personas, verdict,
		 severity_decision, severity_blocking, severity_warning,
		 input, output, cache_read, cache_write, cost_total, duration_ms, raw)
		VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`)
	if err != nil {
		return res, err
	}
	defer stmt.Close()
	for _, e := range events {
		if reason := validateEvent(e); reason != "" {
			res.Invalid = append(res.Invalid, InvalidEvent{Repository: e.Repository, Reason: reason})
			continue
		}
		personas, err := json.Marshal(e.Personas)
		if err != nil || e.Personas == nil {
			personas = []byte("[]")
		}
		attempt := e.Attempt
		if attempt <= 0 {
			attempt = 1
		}
		// Interface-typed NULLable columns: empty run_id → NULL (see package
		// comment), absent verdict/duration → NULL.
		var runID, verdict, duration any
		if e.RunID != "" {
			runID = e.RunID
		}
		if e.Verdict != nil {
			verdict = *e.Verdict
		}
		if e.DurationMs != nil {
			duration = *e.DurationMs
		}
		raw, err := json.Marshal(e)
		if err != nil {
			res.Invalid = append(res.Invalid, InvalidEvent{Repository: e.Repository, Reason: err.Error()})
			continue
		}
		out, err := stmt.Exec(
			e.Ts, e.Platform, strings.TrimSpace(e.Repository), e.Pr, runID, attempt, e.Mode,
			string(personas), verdict, e.Severity.Decision, e.Severity.Blocking, e.Severity.Warning,
			e.Usage.Input, e.Usage.Output, e.Usage.CacheRead, e.Usage.CacheWrite,
			e.CostTotal, duration, string(raw),
		)
		if err != nil {
			return res, fmt.Errorf("insert %s/%s: %w", e.Repository, e.RunID, err)
		}
		if n, _ := out.RowsAffected(); n == 1 {
			res.Inserted++
		} else {
			res.Skipped++
		}
	}
	return res, tx.Commit()
}

// Window scopes every dashboard query. Days <= 0 means all time; Repo == ""
// means all repositories. The repo filter in WHERE uses (? = ” OR repository
// = ?) rather than string-building the predicate — parameters only, always.
type Window struct {
	Days int
	Repo string
}

const windowWhere = "WHERE (? = '' OR repository = ?) AND (? = '' OR ts >= ?)"

func (w Window) args(cutoff string) []any { return []any{w.Repo, w.Repo, cutoff, cutoff} }

func cutoffUTC(days int) string {
	if days <= 0 {
		return ""
	}
	return time.Now().AddDate(0, 0, -days).UTC().Format(time.RFC3339)
}

type Summary struct {
	Reviews      int64   `json:"reviews"`
	Input        int64   `json:"input"`
	Output       int64   `json:"output"`
	CacheRead    int64   `json:"cacheRead"`
	CostTotal    float64 `json:"costTotal"`
	AvgDurationS float64 `json:"avgDurationS"`
	// Cache hit rate: share of prompt tokens served from cache =
	// cacheRead / (input + cacheRead); 0 when nothing was prompted.
	CacheHitRate float64 `json:"cacheHitRate"`
}

type RepoRow struct {
	Repository string  `json:"repository"`
	Reviews    int64   `json:"reviews"`
	Input      int64   `json:"input"`
	Output     int64   `json:"output"`
	CacheRead  int64   `json:"cacheRead"`
	CostTotal  float64 `json:"costTotal"`
	LastTs     string  `json:"lastTs"`
}

type TrendPoint struct {
	Day        string  `json:"day"`
	Repository string  `json:"repository"`
	Cost       float64 `json:"cost"`
	Tokens     int64   `json:"tokens"` // input + output + cacheRead
}

type VerdictRow struct {
	Verdict string `json:"verdict"` // "" = single-persona run (no verdict)
	Count   int64  `json:"count"`
}

type RunRow struct {
	Ts               string   `json:"ts"`
	Platform         string   `json:"platform"`
	Repository       string   `json:"repository"`
	Pr               int64    `json:"pr"`
	Mode             string   `json:"mode"`
	Verdict          string   `json:"verdict"`
	SeverityDecision string   `json:"severityDecision"`
	Blocking         int64    `json:"blocking"`
	Warning          int64    `json:"warning"`
	Input            int64    `json:"input"`
	Output           int64    `json:"output"`
	CacheRead        int64    `json:"cacheRead"`
	CostTotal        float64  `json:"costTotal"`
	DurationS        *float64 `json:"durationS"`
	Personas         int      `json:"personas"`
}

type Dashboard struct {
	Summary  Summary      `json:"summary"`
	Repos    []RepoRow    `json:"repos"`
	Trend    []TrendPoint `json:"trend"`
	Verdicts []VerdictRow `json:"verdicts"`
	Recent   []RunRow     `json:"recent"`
}

func (s *Store) Dashboard(w Window) (*Dashboard, error) {
	d := &Dashboard{}
	cut := cutoffUTC(w.Days)

	var avgMs sql.NullFloat64
	err := s.db.QueryRow(
		`SELECT COUNT(*), COALESCE(SUM(input),0), COALESCE(SUM(output),0),
		        COALESCE(SUM(cache_read),0), COALESCE(SUM(cost_total),0), AVG(duration_ms)
		 FROM events `+windowWhere, w.args(cut)...,
	).Scan(&d.Summary.Reviews, &d.Summary.Input, &d.Summary.Output,
		&d.Summary.CacheRead, &d.Summary.CostTotal, &avgMs)
	if err != nil {
		return nil, err
	}
	if prompted := d.Summary.Input + d.Summary.CacheRead; prompted > 0 {
		d.Summary.CacheHitRate = float64(d.Summary.CacheRead) / float64(prompted)
	}
	if avgMs.Valid && avgMs.Float64 > 0 {
		d.Summary.AvgDurationS = avgMs.Float64 / 1000
	}

	repoRows, err := s.db.Query(
		`SELECT repository, COUNT(*), COALESCE(SUM(input),0), COALESCE(SUM(output),0),
		        COALESCE(SUM(cache_read),0), COALESCE(SUM(cost_total),0), MAX(ts)
		 FROM events `+windowWhere+`
		 GROUP BY repository ORDER BY SUM(cost_total) DESC`, w.args(cut)...)
	if err != nil {
		return nil, err
	}
	defer repoRows.Close()
	for repoRows.Next() {
		var r RepoRow
		if err := repoRows.Scan(&r.Repository, &r.Reviews, &r.Input, &r.Output,
			&r.CacheRead, &r.CostTotal, &r.LastTs); err != nil {
			return nil, err
		}
		d.Repos = append(d.Repos, r)
	}
	if err := repoRows.Err(); err != nil {
		return nil, err
	}

	// Day buckets are substr(ts,1,10) — ISO-8601 UTC dates. Local-timezone
	// bucketing would need a tz-aware group; a review fleet spanning tz's
	// has no meaningful "local day" anyway, so UTC is the honest bucket.
	trendRows, err := s.db.Query(
		`SELECT substr(ts,1,10), repository, COALESCE(SUM(cost_total),0),
		        COALESCE(SUM(input+output+cache_read),0)
		 FROM events `+windowWhere+`
		 GROUP BY 1, 2 ORDER BY 1`, w.args(cut)...)
	if err != nil {
		return nil, err
	}
	defer trendRows.Close()
	for trendRows.Next() {
		var p TrendPoint
		if err := trendRows.Scan(&p.Day, &p.Repository, &p.Cost, &p.Tokens); err != nil {
			return nil, err
		}
		d.Trend = append(d.Trend, p)
	}
	if err := trendRows.Err(); err != nil {
		return nil, err
	}

	verdictRows, err := s.db.Query(
		`SELECT COALESCE(verdict,''), COUNT(*)
		 FROM events `+windowWhere+` GROUP BY verdict ORDER BY 2 DESC`, w.args(cut)...)
	if err != nil {
		return nil, err
	}
	defer verdictRows.Close()
	for verdictRows.Next() {
		var v VerdictRow
		if err := verdictRows.Scan(&v.Verdict, &v.Count); err != nil {
			return nil, err
		}
		d.Verdicts = append(d.Verdicts, v)
	}
	if err := verdictRows.Err(); err != nil {
		return nil, err
	}

	recentRows, err := s.db.Query(
		`SELECT ts, platform, repository, pr, mode, COALESCE(verdict,''),
		        severity_decision, severity_blocking, severity_warning,
		        input, output, cache_read, cost_total, duration_ms,
		        json_array_length(personas)
		 FROM events `+windowWhere+`
		 ORDER BY ts DESC LIMIT 100`, w.args(cut)...)
	if err != nil {
		return nil, err
	}
	defer recentRows.Close()
	for recentRows.Next() {
		var r RunRow
		var duration sql.NullInt64
		if err := recentRows.Scan(&r.Ts, &r.Platform, &r.Repository, &r.Pr, &r.Mode,
			&r.Verdict, &r.SeverityDecision, &r.Blocking, &r.Warning,
			&r.Input, &r.Output, &r.CacheRead, &r.CostTotal, &duration, &r.Personas); err != nil {
			return nil, err
		}
		if duration.Valid {
			secs := float64(duration.Int64) / 1000
			r.DurationS = &secs
		}
		d.Recent = append(d.Recent, r)
	}
	if err := recentRows.Err(); err != nil {
		return nil, err
	}
	return d, nil
}
