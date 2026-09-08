package store

import (
	"fmt"
	"math/rand/v2"
	"time"
)

// SeedDemo fills the database with plausible fake events so a fresh
// dashboard deployment has something to look at. Values mimic the real
// agent's shape: a team review = 2-4 reviewer personas + coordinator,
// DeepSeek-ish token volumes with a 60-85% cache-hit share on resume runs.
func SeedDemo(s *Store, n int) error {
	repos := []string{"sun-praise/pi-review-agent", "sun-praise/hugo-blog", "ceramic-lims/backend", "spde-fullstack/api"}
	personaPool := []string{"quality", "security", "performance", "architecture", "regression-test"}
	verdicts := []string{"CAN MERGE", "CAN MERGE", "CAN MERGE", "CONDITIONAL MERGE", "CONDITIONAL MERGE", "CANNOT MERGE", "UNKNOWN"}
	events := make([]Event, 0, n)
	now := time.Now().UTC()
	for i := range n {
		repo := repos[rand.IntN(len(repos))]
		ts := now.Add(-time.Duration(rand.IntN(60*24)) * time.Hour).Add(-time.Duration(rand.IntN(3600)) * time.Second)
		verdict := verdicts[rand.IntN(len(verdicts))]
		event := Event{
			Ts:         ts.Format(time.RFC3339Nano),
			Platform:   "github",
			Repository: repo,
			Pr:         int64(20 + rand.IntN(180)),
			RunID:      fmt.Sprintf("seed-%d", i),
			Attempt:    1,
			Mode:       "team",
			Severity:   SeverityInfo{Decision: verdict},
			DurationMs: ptr(int64(20_000 + rand.IntN(90_000))),
		}
		if verdict == "CANNOT MERGE" {
			event.Severity.Blocking = int64(1 + rand.IntN(2))
			event.Severity.Warning = int64(rand.IntN(3))
		} else if verdict == "CONDITIONAL MERGE" {
			event.Severity.Warning = int64(1 + rand.IntN(3))
		}
		team := personaPool[:2+rand.IntN(3)]
		event.Personas = make([]PersonaUsage, 0, len(team)+1)
		for _, name := range team {
			input := int64(30_000 + rand.IntN(120_000))
			cacheRead := input * int64(55+rand.IntN(30)) / 100
			input -= cacheRead
			cost := float64(input)*0.14/1e6 + float64(cacheRead)*0.0028/1e6
			event.Personas = append(event.Personas, PersonaUsage{
				Name: name, Input: input, Output: int64(800 + rand.IntN(3200)),
				CacheRead: cacheRead, Cost: cost, Resumed: rand.IntN(2) == 1,
			})
			event.Usage.Input += input
			event.Usage.Output += event.Personas[len(event.Personas)-1].Output
			event.Usage.CacheRead += cacheRead
			event.CostTotal += cost
		}
		coordIn := int64(20_000 + rand.IntN(60_000))
		coordCache := coordIn * 70 / 100
		coordIn -= coordCache
		coordCost := float64(coordIn)*0.14/1e6 + float64(coordCache)*0.0028/1e6
		event.Personas = append(event.Personas, PersonaUsage{
			Name: "coordinator", Input: coordIn, Output: int64(1000 + rand.IntN(2500)),
			CacheRead: coordCache, Cost: coordCost,
		})
		event.Usage.Input += coordIn
		event.Usage.Output += event.Personas[len(event.Personas)-1].Output
		event.Usage.CacheRead += coordCache
		event.CostTotal += coordCost
		event.Verdict = &verdict
		events = append(events, event)
	}
	_, err := s.InsertEvents(events)
	return err
}

func ptr[T any](v T) *T { return &v }
