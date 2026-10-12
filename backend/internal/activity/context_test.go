package activity

import (
	"github.com/zeusangis/dendrite/internal/storage"
	"strings"
	"testing"
	"time"
)

func TestContextNormalization(t *testing.T) {
	cfg := DefaultConfig()
	cfg.ExcludedDomains = []string{"private.example"}
	for _, tc := range []struct{ raw, want, domain string }{
		{"HTTPS://user:password@DOCS.Example.:443?token=1#secret", "https://docs.example/", "docs.example"},
		{"https://docs.example/a/", "https://docs.example/a/", "docs.example"},
		{"https://docs.example/a", "https://docs.example/a", "docs.example"},
		{"http://[::1]:80/a", "http://[::1]/a", "::1"},
		{"file:///secret", "", ""}, {"javascript:alert(1)", "", ""}, {"https://x:99999/", "", ""}, {"https://%zz/", "", ""},
	} {
		raw, domain := SanitizeURL(tc.raw)
		if raw != tc.want || domain != tc.domain {
			t.Fatalf("%s: %q %q", tc.raw, raw, domain)
		}
	}
	for _, ctx := range []ActivityContext{
		{App: "Chrome", URL: "https://sub.private.example/x", Focused: true},
		{App: "Chrome", Private: true, Focused: true}, {App: "Chrome", Focused: false},
		{App: "Chrome", WindowTitle: "Incognito", Focused: true}, {App: "1Password", Focused: true},
	} {
		if _, ok := NormalizeContext(ctx, cfg); ok {
			t.Fatal("private context accepted", ctx)
		}
	}
	a, ok := NormalizeContext(ActivityContext{App: "chrome", URL: "https://GO.DEV", Domain: "spoofed", Focused: true}, cfg)
	b, _ := NormalizeContext(ActivityContext{App: "Google Chrome", AppID: "com.google.Chrome", URL: "https://go.dev/", Focused: true}, cfg)
	if !ok || a.AppID != b.AppID || a.URL != b.URL || a.Domain != "go.dev" {
		t.Fatal(a, b)
	}
}
func TestPrivacyReconcilesRetainedHistory(t *testing.T) {
	c, db := setup(t)
	at := time.Now().Add(-time.Minute)
	s := Sample{App: "Chrome", URL: "https://user:password@private.example/x?q=1#secret", WindowTitle: "Sensitive"}
	observe(t, c, s, at)
	observe(t, c, s, at.Add(5*time.Second))
	cfg := c.Status().Config
	cfg.ExcludedDomains = []string{"private.example"}
	if err := c.Configure(cfg); err != nil {
		t.Fatal(err)
	}
	sessions, err := loadSessions(db, time.Time{})
	if err != nil || len(sessions) != 0 {
		t.Fatal(err, sessions)
	}
}
func TestExcludedSessionCannotBridgeContextSwitch(t *testing.T) {
	c, db := setup(t)
	at := time.Now().Add(-time.Minute)
	for i, app := range []string{"Editor", "Sensitive", "Terminal"} {
		s := Sample{App: app}
		observe(t, c, s, at.Add(time.Duration(i*10)*time.Second))
		observe(t, c, s, at.Add(time.Duration(i*10+5)*time.Second))
	}
	cfg := c.Status().Config
	cfg.ExcludedApps = []string{"Sensitive"}
	if err := c.Configure(cfg); err != nil {
		t.Fatal(err)
	}
	if err := Rebuild(db); err != nil {
		t.Fatal(err)
	}
	graph, err := storage.GetGraph(db)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range graph.Edges {
		if e.RelationshipType == EdgeContextSwitch {
			t.Fatal("suppressed context bridged", e)
		}
	}
}

func TestWeightedMatching(t *testing.T) {
	for _, tc := range []struct {
		concept, kind, title, domain string
		want                         bool
	}{
		{"golang", "tag", "Go tutorial", "go.dev", true}, {"go_lang", "entity", "The Go programming language", "go.dev", true},
		{"docs.example", "note title", "Unrelated", "docs.example", true}, {"Rust programming", "note title", "RUST-programming guide", "example.com", true},
		{"Home", "note title", "Home page", "example.com", false},
		{"Home page", "note title", "Home page", "example.com", false},
		{"1234", "tag", "Page 1234", "example.com", false}, {"Google", "tag", "Google search", "google.com", false},
		{"Go", "tag", "Go shopping", "example.com", false}, {"Rust", "tag", "Trust me", "example.com", false}, {"Nothing", "entity", "Go tutorial", "go.dev", false},
	} {
		m := strongestMatch([]Concept{{tc.concept, tc.kind}}, &derived{Type: NodePage, Title: tc.title, Domain: tc.domain})
		if (m != nil) != tc.want {
			t.Fatalf("%+v: %+v", tc, m)
		}
		if m != nil && (!strings.Contains(m.Kind, "phrase") && !strings.Contains(m.Kind, "domain") && !strings.Contains(m.Kind, "alias")) {
			t.Fatal("missing explanation", m)
		}
	}
}
