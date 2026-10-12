package activity

import (
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/zeusangis/dendrite/internal/storage"
)

func TestWebsiteGraphLifecycle(t *testing.T) {
	c, db := setup(t)
	at := time.Now().Add(-time.Hour)
	note, err := storage.UpsertNode(db, "go.md", "Go resources", "note", at, at)
	if err != nil {
		t.Fatal(err)
	}
	if err = storage.ReplaceTags(db, note, []string{"golang"}); err != nil {
		t.Fatal(err)
	}
	home, err := storage.UpsertNode(db, "home.md", "Home", "note", at, at)
	if err != nil {
		t.Fatal(err)
	}
	for i, raw := range []string{"https://GO.DEV:443/?secret=1#fragment", "https://go.dev/", "https://en.wikipedia.org/wiki/Go_(programming_language)", "https://www.geeksforgeeks.org/go-language/", "https://github.com/golang/go"} {
		title := []string{"Go tutorial", "Go tutorial", "Go programming language", "Go tutorial", "Go source"}[i]
		s := Sample{App: "Chrome", WindowTitle: title, URL: raw}
		observe(t, c, s, at.Add(time.Duration(i*20)*time.Second))
		observe(t, c, s, at.Add(time.Duration(i*20+5)*time.Second))
	}
	if err = Rebuild(db); err != nil {
		t.Fatal(err)
	}
	g, err := storage.GetGraph(db)
	if err != nil {
		t.Fatal(err)
	}
	var pageID int64
	count := 0
	for _, n := range g.Nodes {
		if n.Path == nodeKey(NodePage, "https://go.dev/") {
			count++
			pageID = n.ID
		}
	}
	if count != 1 {
		t.Fatal("URL identity not canonical", g.Nodes)
	}
	x, y := 123.0, 456.0
	if err = storage.UpdateNodePosition(db, pageID, &x, &y); err != nil {
		t.Fatal(err)
	}
	for _, e := range g.Edges {
		if (e.SourceID == home || e.TargetID == home) && e.Source == "activity" {
			t.Fatal("generic note matched", e)
		}
	}
	detail, err := GetNodeDetail(db, pageID)
	if err != nil || detail.URL != "https://go.dev/" || len(detail.RelatedNotes) != 1 || detail.RelatedNotes[0].Node.ID != note {
		t.Fatal(err, detail)
	}
	if !strings.Contains(detail.RelatedNotes[0].Edge.Reason, "alias") {
		t.Fatal(detail.RelatedNotes)
	}
	if err = Rebuild(db); err != nil {
		t.Fatal(err)
	}
	again, err := storage.GetGraph(db)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(g.Edges, again.Edges) {
		t.Fatal("rebuild changed edges", g.Edges, again.Edges)
	}
	n, err := storage.GetNodeByPath(db, nodeKey(NodePage, "https://go.dev/"))
	if err != nil || n.ID != pageID || n.X == nil || *n.X != x {
		t.Fatal(err, n)
	}
	cfg := c.Status().Config
	cfg.Enabled = false
	cfg.ExcludedDomains = []string{"go.dev"}
	if err = c.Configure(cfg); err != nil {
		t.Fatal(err)
	}
	if err = Rebuild(db); err != nil {
		t.Fatal(err)
	}
	if _, err = GetNodeDetail(db, pageID); err == nil {
		t.Fatal("excluded detail still visible")
	}
	if _, err = db.Exec(`UPDATE activity_sessions SET ended_at=?`, time.Now().AddDate(0, 0, -40).UTC().Format(time.RFC3339Nano)); err != nil {
		t.Fatal(err)
	}
	if err = Rebuild(db); err != nil {
		t.Fatal(err)
	}
	g, err = storage.GetGraph(db)
	if err != nil || len(g.Nodes) != 2 {
		t.Fatal(err, g)
	}
	if err = c.Clear(); err != nil {
		t.Fatal(err)
	}
	g, err = storage.GetGraph(db)
	if err != nil || len(g.Nodes) != 2 {
		t.Fatal("notes deleted", err, g)
	}
}
func TestObservedSwitchBoundaries(t *testing.T) {
	now := time.Now()
	a := Session{AppID: "a", RunID: "run", TotalSeconds: 5, ActiveSeconds: 5, EndedAt: now.Format(time.RFC3339Nano)}
	b := Session{AppID: "b", RunID: "run", TotalSeconds: 5, ActiveSeconds: 5, StartedAt: now.Add(time.Second).Format(time.RFC3339Nano)}
	if !observedSwitch(a, b) {
		t.Fatal("continuous switch missed")
	}
	for _, change := range []func(*Session){func(s *Session) { s.RunID = "other" }, func(s *Session) { s.RunID = "" }, func(s *Session) { s.ActiveSeconds = 0 }, func(s *Session) { s.StartedAt = now.Add(time.Minute).Format(time.RFC3339Nano) }, func(s *Session) { s.StartedAt = now.Add(-time.Second).Format(time.RFC3339Nano) }} {
		copy := b
		change(&copy)
		if observedSwitch(a, copy) {
			t.Fatal("false switch", copy)
		}
	}
}
