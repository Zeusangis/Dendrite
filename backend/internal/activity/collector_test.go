package activity

import (
	"context"
	"database/sql"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/zeusangis/dendrite/internal/storage"
)

type fakeSampler struct{}

func (fakeSampler) Sample(context.Context, Config) (Sample, error) { return Sample{}, nil }
func setup(t *testing.T) (*Collector, *sql.DB) {
	t.Helper()
	db, err := storage.OpenDatabase(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	c, err := New(db, &sync.Mutex{}, fakeSampler{}, true)
	if err != nil {
		t.Fatal(err)
	}
	return c, db
}
func observe(t *testing.T, c *Collector, s Sample, at time.Time) {
	t.Helper()
	if err := c.Observe(s, at); err != nil {
		t.Fatal(err)
	}
}
func TestTimingPauseIdleAndSleep(t *testing.T) {
	c, db := setup(t)
	at := time.Now().Add(-time.Minute)
	sample := Sample{App: "Editor", AppID: "editor", WindowTitle: "Project"}
	observe(t, c, sample, at)
	observe(t, c, sample, at.Add(5*time.Second))
	sample.IdleSeconds = 65
	observe(t, c, sample, at.Add(10*time.Second))
	observe(t, c, sample, at.Add(15*time.Second))
	sessions, err := loadSessions(db, time.Time{})
	if err != nil {
		t.Fatal(err)
	}
	if len(sessions) != 1 || sessions[0].TotalSeconds != 15 || sessions[0].ActiveSeconds != 10 {
		t.Fatalf("timing %+v", sessions)
	}
	observe(t, c, sample, at.Add(time.Hour))
	sessions, _ = loadSessions(db, time.Time{})
	if sessions[0].TotalSeconds != 15 {
		t.Fatal("sleep inflated duration")
	}
	cfg := c.Status().Config
	cfg.Enabled = false
	if err = c.Configure(cfg); err != nil {
		t.Fatal(err)
	}
	observe(t, c, sample, at.Add(time.Hour+5*time.Second))
	sessions, _ = loadSessions(db, time.Time{})
	if len(sessions) != 1 {
		t.Fatal("paused recorded")
	}
	restored, err := New(db, &sync.Mutex{}, fakeSampler{}, true)
	if err != nil {
		t.Fatal(err)
	}
	if restored.Status().Config.Enabled || restored.Token() != c.Token() {
		t.Fatal("pause/token not persisted")
	}
}
func TestPrivacyAndBrowserHints(t *testing.T) {
	c, db := setup(t)
	cfg := c.Status().Config
	cfg.ExcludedDomains = []string{"private.example"}
	if err := c.Configure(cfg); err != nil {
		t.Fatal(err)
	}
	at := time.Now()
	for i, s := range []Sample{{App: "1Password"}, {App: "Editor", Locked: true}, {App: "Editor", Private: true}, {App: "Browser", URL: "https://sub.private.example/a"}} {
		observe(t, c, s, at.Add(time.Duration(i)*5*time.Second))
		observe(t, c, s, at.Add(time.Duration(i)*5*time.Second+time.Second))
	}
	sessions, _ := loadSessions(db, time.Time{})
	if len(sessions) != 0 {
		t.Fatal("private activity stored", sessions)
	}
	s := Sample{App: "Browser", AppID: "browser", WindowTitle: "Public page", URL: "https://user:pass@docs.example/a?secret=key#token"}
	observe(t, c, s, at.Add(30*time.Second))
	observe(t, c, s, at.Add(35*time.Second))
	sessions, _ = loadSessions(db, time.Time{})
	if len(sessions) != 1 || sessions[0].URL != "https://docs.example/a" || sessions[0].Domain != "docs.example" {
		t.Fatal("URL not sanitized", sessions)
	}
	cfg.WindowTitles = false
	cfg.BrowserPages = false
	if err := c.Configure(cfg); err != nil {
		t.Fatal(err)
	}
	observe(t, c, s, at.Add(40*time.Second))
	observe(t, c, s, at.Add(45*time.Second))
	sessions, _ = loadSessions(db, time.Time{})
	if sessions[len(sessions)-1].WindowTitle != "" || sessions[len(sessions)-1].URL != "" {
		t.Fatal("privacy config bypassed")
	}
	cfg.WindowTitles = true
	cfg.BrowserPages = true
	if err := c.Configure(cfg); err != nil {
		t.Fatal(err)
	}
	c.Browser(BrowserHint{App: "Browser", Private: true, Focused: true})
	observe(t, c, s, time.Now().Add(time.Second))
	observe(t, c, s, time.Now().Add(6*time.Second))
	sessions, _ = loadSessions(db, time.Time{})
	if len(sessions) != 2 {
		t.Fatal("private browser hint bypassed")
	}
}
func TestAutomaticGraphStrengthsAndRetention(t *testing.T) {
	c, db := setup(t)
	at := time.Now().Add(-time.Minute)
	editor := Sample{App: "Editor", AppID: "editor", WindowTitle: "Dendrite code"}
	browser := Sample{App: "Browser", AppID: "browser", WindowTitle: "Dendrite documentation", URL: "https://docs.example/dendrite"}
	observe(t, c, editor, at)
	observe(t, c, editor, at.Add(5*time.Second))
	observe(t, c, browser, at.Add(10*time.Second))
	observe(t, c, browser, at.Add(15*time.Second))
	observe(t, c, browser, at.Add(20*time.Second))
	noteID, err := storage.UpsertNode(db, "dendrite.md", "Dendrite", "note", at, at)
	if err != nil {
		t.Fatal(err)
	}
	if err = Rebuild(db); err != nil {
		t.Fatal(err)
	}
	snap, err := storage.GetGraph(db)
	if err != nil {
		t.Fatal(err)
	}
	if len(snap.Nodes) != 6 {
		t.Fatalf("automatic nodes %+v", snap.Nodes)
	}
	hasTransition, hasConcept := false, false
	ids := map[string]int64{}
	for _, e := range snap.Edges {
		if e.Source != "activity" || e.Strength <= 0 || e.Strength > 150 {
			t.Fatal("invalid score", e)
		}
		if e.RelationshipType == "context switch" {
			hasTransition = true
		}
		if e.RelationshipType == "activity concept" && (e.SourceID == noteID || e.TargetID == noteID) {
			hasConcept = true
		}
		if !strings.Contains(e.Reason, "active minutes") {
			t.Fatal("unexplained edge", e)
		}
	}
	for _, n := range snap.Nodes {
		ids[n.Path] = n.ID
	}
	if !hasTransition || !hasConcept {
		t.Fatal("missing automatic relations", snap.Edges)
	}
	if err = Rebuild(db); err != nil {
		t.Fatal(err)
	}
	again, _ := storage.GetGraph(db)
	for _, n := range again.Nodes {
		if ids[n.Path] != n.ID {
			t.Fatal("activity node IDs churn")
		}
	}
	summary, err := GetSummary(db, 1)
	if err != nil {
		t.Fatal(err)
	}
	if summary.TotalSeconds != 20 || summary.ActiveRatio != 1 || len(summary.Usage) != 2 {
		t.Fatal("summary duplicated time", summary)
	}
	cfg := c.Status().Config
	cfg.RetentionDays = 1
	if err = c.Configure(cfg); err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec(`UPDATE activity_sessions SET ended_at=?`, at.AddDate(0, 0, -2).Format(time.RFC3339Nano)); err != nil {
		t.Fatal(err)
	}
	if err = c.Configure(cfg); err != nil {
		t.Fatal(err)
	}
	if err = Rebuild(db); err != nil {
		t.Fatal(err)
	}
	snap, _ = storage.GetGraph(db)
	if len(snap.Nodes) != 1 || len(snap.Edges) != 0 {
		t.Fatal("expired graph remains", snap)
	}
}
func TestStrengthMonotonicAndBounded(t *testing.T) {
	for _, transition := range []bool{false, true} {
		low := Strength(60, 120, 1, transition)
		high := Strength(600, 1200, 10, transition)
		if high <= low || high > 150 {
			t.Fatal(low, high)
		}
		if Strength(1e20, 1e20, 1000000, transition) > 150 {
			t.Fatal("score overflow")
		}
	}
}
