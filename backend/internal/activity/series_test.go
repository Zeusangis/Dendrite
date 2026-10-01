package activity

import (
	"testing"
	"time"
)

func insertSeriesSession(t *testing.T, c *Collector, app, appID, runID string, start, end time.Time, total, active float64) {
	t.Helper()
	_, err := c.db.Exec(`INSERT INTO activity_sessions(app,app_id,window_title,domain,url,started_at,ended_at,total_seconds,active_seconds,samples,run_id) VALUES(?,?, '', '', '', ?, ?, ?, ?, 1, ?)`, app, appID, start.UTC().Format(time.RFC3339Nano), end.UTC().Format(time.RFC3339Nano), total, active, runID)
	if err != nil {
		t.Fatal(err)
	}
}

func TestGetSeriesSplitsSessionsAndCountsObservedSwitches(t *testing.T) {
	c, db := setup(t)
	now := time.Date(2026, time.October, 10, 12, 0, 0, 0, time.UTC)
	insertSeriesSession(t, c, "Editor", "editor", "run-1", time.Date(2026, 10, 8, 23, 50, 0, 0, time.UTC), time.Date(2026, 10, 9, 0, 10, 0, 0, time.UTC), 1200, 600)
	insertSeriesSession(t, c, "Browser", "browser", "run-1", time.Date(2026, 10, 9, 0, 10, 0, 0, time.UTC), time.Date(2026, 10, 9, 0, 20, 0, 0, time.UTC), 600, 600)
	insertSeriesSession(t, c, "Idle app", "idle", "run-1", time.Date(2026, 10, 9, 0, 20, 0, 0, time.UTC), time.Date(2026, 10, 9, 0, 30, 0, 0, time.UTC), 600, 0)
	insertSeriesSession(t, c, "Editor", "editor", "run-1", time.Date(2026, 10, 9, 0, 30, 0, 0, time.UTC), time.Date(2026, 10, 9, 0, 40, 0, 0, time.UTC), 600, 600)

	series, err := GetSeries(db, 3, "day", now)
	if err != nil {
		t.Fatal(err)
	}
	if len(series.Buckets) != 3 || series.Buckets[0].TotalSeconds != 600 || series.Buckets[0].ActiveSeconds != 300 || series.Buckets[0].IdleSeconds != 300 {
		t.Fatalf("first day should contain the clipped half-session: %+v", series.Buckets)
	}
	second := series.Buckets[1]
	if second.TotalSeconds != 2400 || second.ActiveSeconds != 1500 || second.IdleSeconds != 900 || second.ContextSwitches != 1 {
		t.Fatalf("unexpected next-day aggregate: %+v", second)
	}
	if series.Buckets[2].TotalSeconds != 0 || len(series.Buckets[2].Apps) != 0 {
		t.Fatalf("future activity leaked into current day: %+v", series.Buckets[2])
	}
}

func TestGetSeriesWeeklyTopAppsAndOtherBucket(t *testing.T) {
	c, db := setup(t)
	now := time.Date(2026, time.October, 7, 12, 0, 0, 0, time.UTC)
	for i := 0; i < 6; i++ {
		start := time.Date(2026, time.October, 1, i, 0, 0, 0, time.UTC)
		insertSeriesSession(t, c, string(rune('A'+i)), string(rune('a'+i)), "", start, start.Add(time.Hour), 3600, 1800)
	}
	start := time.Date(2026, time.October, 6, 10, 0, 0, 0, time.UTC)
	insertSeriesSession(t, c, "G", "g", "", start, start.Add(2*time.Minute), 120, 60)

	series, err := GetSeries(db, 10, "week", now)
	if err != nil {
		t.Fatal(err)
	}
	if len(series.Buckets) != 2 || series.Buckets[0].Start[:10] != "2026-09-28" || series.Buckets[1].Start[:10] != "2026-10-05" {
		t.Fatalf("weeks should start on Monday: %+v", series.Buckets)
	}
	if len(series.Apps) != TopSeriesApps || series.Buckets[0].TotalSeconds != 6*3600 || series.Buckets[1].TotalSeconds != 120 {
		t.Fatalf("unexpected weekly totals or app limit: %+v", series)
	}
	first := series.Buckets[0]
	if len(first.Apps) != TopSeriesApps+1 || first.Apps[len(first.Apps)-1].App != "Other" || first.Apps[len(first.Apps)-1].TotalSeconds != 3600 {
		t.Fatalf("non-top apps should be combined into Other: %+v", first.Apps)
	}
	if _, err = GetSeries(db, 7, "month", now); err == nil {
		t.Fatal("invalid granularity should be rejected")
	}
}
