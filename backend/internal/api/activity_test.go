package api

import (
	"context"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/zeusangis/dendrite/internal/activity"
	"github.com/zeusangis/dendrite/internal/storage"
)

type noopSampler struct{}

func (noopSampler) Sample(context.Context, activity.Config) (activity.Sample, error) {
	return activity.Sample{}, nil
}
func TestWebsiteActivityAPI(t *testing.T) {
	h, _ := setup(t)
	collector, err := activity.New(h.DB, h.Service, noopSampler{}, true)
	if err != nil {
		t.Fatal(err)
	}
	h.Activity = collector
	at := time.Now().Add(-time.Minute)
	for i, raw := range []string{"https://en.wikipedia.org/wiki/Go?secret=x#token", "https://www.geeksforgeeks.org/go/", "https://github.com/golang/go"} {
		sample := activity.Sample{App: "Chrome", WindowTitle: "Go programming", URL: raw}
		if err = collector.Observe(sample, at.Add(time.Duration(i*10)*time.Second)); err != nil {
			t.Fatal(err)
		}
		if err = collector.Observe(sample, at.Add(time.Duration(i*10+5)*time.Second)); err != nil {
			t.Fatal(err)
		}
	}
	router := h.Router()
	call(t, router, "POST", "/api/sync", nil, 200, nil)
	var result activity.WebsiteResult
	call(t, router, "GET", "/api/activity/pages?limit=1&sort=active", nil, 200, &result)
	if result.Total != 3 || len(result.Pages) != 1 {
		t.Fatal(result)
	}
	id := result.Pages[0].NodeID
	var detail activity.NodeDetail
	body := call(t, router, "GET", "/api/activity/nodes/"+strconv.FormatInt(id, 10), nil, 200, &detail).Body.String()
	if strings.Contains(body, "?secret") || strings.Contains(body, "#token") || detail.Sessions < 1 || detail.Domain == "" {
		t.Fatal(body)
	}
	call(t, router, "GET", "/api/activity/domains?domain=wikipedia.org&limit=1000", nil, 200, &result)
	if result.Total != 1 || result.Limit != 100 || len(result.Domains) != 1 {
		t.Fatal(result)
	}
	var recent activity.RecentResult
	call(t, router, "GET", "/api/activity/recent?search=github&offset=0", nil, 200, &recent)
	if recent.Total != 1 || len(recent.Recent) != 1 {
		t.Fatal(recent)
	}
	call(t, router, "GET", "/api/activity/pages?sort=invalid", nil, 400, nil)
	call(t, router, "GET", "/api/activity/pages?limit=no", nil, 400, nil)
	call(t, router, "GET", "/api/activity/recent?offset=-1", nil, 400, nil)
	call(t, router, "POST", "/api/activity/domains", nil, 405, nil)
	// The extension is not trusted to pre-sanitize URLs or enforce exclusions.
	cfg := collector.Status().Config
	cfg.ExcludedDomains = []string{"github.com"}
	call(t, router, "PUT", "/api/activity/config", cfg, 200, nil)
	postHint := func(payload string) {
		req := httptest.NewRequest("POST", "/api/activity/browser", strings.NewReader(payload))
		req.Header.Set("Authorization", "Bearer "+collector.Token())
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)
		if w.Code != 200 {
			t.Fatal(w.Body.String())
		}
	}
	postHint(`{"app":"Chrome","url":"https://github.com/secret?token=x","focused":true}`)
	native := activity.Sample{App: "Google Chrome", AppID: "com.google.Chrome", URL: "https://public.example/fallback"}
	if err = collector.Observe(native, time.Now()); err != nil {
		t.Fatal(err)
	}
	if err = collector.Observe(native, time.Now().Add(5*time.Second)); err != nil {
		t.Fatal(err)
	}
	postHint(`{"app":"Chrome","url":"https://public.example/?secret=x","private":true,"focused":true}`)
	if err = collector.Observe(native, time.Now().Add(10*time.Second)); err != nil {
		t.Fatal(err)
	}
	if err = collector.Observe(native, time.Now().Add(15*time.Second)); err != nil {
		t.Fatal(err)
	}
	call(t, router, "GET", "/api/activity/pages?search=public.example", nil, 200, &result)
	if result.Total != 0 {
		t.Fatal("extension suppression bypassed", result)
	}
	call(t, router, "GET", "/api/activity/pages?search=github", nil, 200, &result)
	if result.Total != 0 {
		t.Fatal("exclusion bypass", result)
	}
	var keep storage.Note
	call(t, router, "POST", "/api/notes", map[string]string{"title": "Keep", "content": "Preserve this note"}, 201, &keep)
	noteID := keep.ID
	call(t, router, "DELETE", "/api/activity/data?confirm=true", nil, 200, nil)
	call(t, router, "GET", "/api/activity/nodes/"+strconv.FormatInt(id, 10), nil, 404, nil)
	call(t, router, "GET", "/api/nodes/"+strconv.FormatInt(noteID, 10), nil, 200, nil)
}

func nodePathOf(snap storage.GraphSnapshot, id int64) string {
	for _, n := range snap.Nodes {
		if n.ID == id {
			return n.Path
		}
	}
	return ""
}

func TestMuteConnectionAPI(t *testing.T) {
	h, _ := setup(t)
	collector, err := activity.New(h.DB, h.Service, noopSampler{}, true)
	if err != nil {
		t.Fatal(err)
	}
	h.Activity = collector
	router := h.Router()
	at := time.Now().Add(-time.Minute)
	sample := activity.Sample{App: "Chrome", WindowTitle: "Go tutorial", URL: "https://go.dev/"}
	if err = collector.Observe(sample, at); err != nil {
		t.Fatal(err)
	}
	if err = collector.Observe(sample, at.Add(5*time.Second)); err != nil {
		t.Fatal(err)
	}
	var note storage.Note
	call(t, router, "POST", "/api/notes", map[string]string{"title": "Go resources", "content": "# Go resources\n\n#golang"}, 201, &note)
	call(t, router, "POST", "/api/sync", nil, 200, nil)
	var pages activity.WebsiteResult
	call(t, router, "GET", "/api/activity/pages?search=go.dev", nil, 200, &pages)
	if len(pages.Pages) != 1 || pages.Pages[0].NodeID == 0 {
		t.Fatal("page fixture missing", pages)
	}
	pageID := pages.Pages[0].NodeID
	call(t, router, "POST", "/api/activity/muted", map[string]int64{"source_id": pageID, "target_id": note.ID}, 200, nil)
	var muted struct {
		Muted []storage.MutedConnection `json:"muted"`
	}
	call(t, router, "GET", "/api/activity/muted", nil, 200, &muted)
	if len(muted.Muted) != 1 || muted.Muted[0].A == nil || muted.Muted[0].B == nil {
		t.Fatal("muted pair not resolved", muted)
	}
	var detail activity.NodeDetail
	call(t, router, "GET", "/api/activity/nodes/"+strconv.FormatInt(pageID, 10), nil, 200, &detail)
	if len(detail.RelatedNotes) != 0 || detail.Muted != 1 {
		t.Fatal("muted connection still visible in node detail", detail)
	}
	var snap storage.GraphSnapshot
	call(t, router, "GET", "/api/graph", nil, 200, &snap)
	mutedPair := storage.Pair(detail.Node.Path, note.Path)
	for _, e := range snap.Edges {
		if e.Source == "activity" && storage.Pair(nodePathOf(snap, e.SourceID), nodePathOf(snap, e.TargetID)) == mutedPair {
			t.Fatal("muted connection still projected", e)
		}
	}
	// Only that pair is hidden: the same note's go.dev domain connection survives.
	sibling := false
	for _, e := range snap.Edges {
		if e.Source == "activity" && storage.Pair(nodePathOf(snap, e.SourceID), nodePathOf(snap, e.TargetID)) != mutedPair && (e.SourceID == note.ID || e.TargetID == note.ID) {
			sibling = true
		}
	}
	if !sibling {
		t.Fatal("muting hid unrelated connections", snap.Edges)
	}
	// Validation and method handling.
	call(t, router, "POST", "/api/activity/muted", map[string]int64{"source_id": pageID, "target_id": pageID}, 400, nil)
	call(t, router, "POST", "/api/activity/muted", map[string]int64{"source_id": pageID, "target_id": 999999}, 404, nil)
	call(t, router, "POST", "/api/activity/muted", map[string]interface{}{"source_id": pageID}, 400, nil)
	call(t, router, "DELETE", "/api/activity/muted", nil, 400, nil)
	call(t, router, "PUT", "/api/activity/muted", nil, 405, nil)
	call(t, router, "DELETE", "/api/activity/muted?a_path=only-one", nil, 400, nil)
	// A mute can also be lifted by path once its activity node is long pruned.
	call(t, router, "DELETE", "/api/activity/muted?a_path="+url.QueryEscape(detail.Node.Path)+"&b_path="+url.QueryEscape(note.Path), nil, 200, nil)
	call(t, router, "POST", "/api/activity/muted", map[string]int64{"source_id": pageID, "target_id": note.ID}, 200, nil)
	// Restoring brings the connection back and empties the list.
	call(t, router, "DELETE", "/api/activity/muted?source_id="+strconv.FormatInt(pageID, 10)+"&target_id="+strconv.FormatInt(note.ID, 10), nil, 200, nil)
	call(t, router, "GET", "/api/activity/muted", nil, 200, &muted)
	if len(muted.Muted) != 0 {
		t.Fatal("unmute did not clear the pair", muted)
	}
	call(t, router, "GET", "/api/graph", nil, 200, &snap)
	restored := false
	for _, e := range snap.Edges {
		if e.Source == "activity" && e.RelationshipType == activity.EdgeConcept {
			restored = true
		}
	}
	if !restored {
		t.Fatal("restored connection missing", snap.Edges)
	}
	call(t, router, "GET", "/api/activity/nodes/"+strconv.FormatInt(pageID, 10), nil, 200, &detail)
	if len(detail.RelatedNotes) != 1 || detail.Muted != 0 {
		t.Fatal("restore did not update node detail", detail)
	}
	// Clearing activity leaves no stale mutes behind for recreated nodes.
	call(t, router, "POST", "/api/activity/muted", map[string]int64{"source_id": pageID, "target_id": note.ID}, 200, nil)
	call(t, router, "DELETE", "/api/activity/data?confirm=true", nil, 200, nil)
	call(t, router, "GET", "/api/activity/muted", nil, 200, &muted)
	if len(muted.Muted) != 1 {
		t.Fatal("clear should keep the mute so a revisit stays hidden", muted)
	}
}

func TestActivityControlsGraphAndPrivacy(t *testing.T) {
	h, _ := setup(t)
	collector, err := activity.New(h.DB, h.Service, noopSampler{}, true)
	if err != nil {
		t.Fatal(err)
	}
	h.Activity = collector
	router := h.Router()
	var status activity.Status
	call(t, router, "GET", "/api/activity", nil, 200, &status)
	if !status.Config.Enabled {
		t.Fatal("autostart not enabled")
	}
	if strings.Contains(call(t, router, "GET", "/api/activity", nil, 200, nil).Body.String(), collector.Token()) {
		t.Fatal("token exposed in status")
	}
	sample := activity.Sample{App: "Editor", AppID: "editor", WindowTitle: "Project code"}
	at := time.Now().Add(-time.Minute)
	if err = collector.Observe(sample, at); err != nil {
		t.Fatal(err)
	}
	if err = collector.Observe(sample, at.Add(5*time.Second)); err != nil {
		t.Fatal(err)
	}
	call(t, router, "POST", "/api/sync", nil, 200, nil)
	var snap storage.GraphSnapshot
	call(t, router, "GET", "/api/graph", nil, 200, &snap)
	if len(snap.Nodes) != 2 || len(snap.Edges) != 1 {
		t.Fatal("automatic graph", snap)
	}
	for _, n := range snap.Nodes {
		call(t, router, "GET", "/api/nodes/"+strings.TrimPrefix(notePath(n.ID), "/api/notes/"), nil, 200, nil)
		call(t, router, "GET", notePath(n.ID), nil, 404, nil)
		call(t, router, "DELETE", notePath(n.ID), nil, 404, nil)
	}
	req := httptest.NewRequest("POST", "/api/activity/browser", strings.NewReader(`{"app":"Browser","url":"https://example.com","title":"Example","focused":true}`))
	req.Header.Set("Origin", "chrome-extension://test")
	req.Header.Set("Authorization", "Bearer "+collector.Token())
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code != 200 {
		t.Fatal("paired extension rejected", w.Body.String())
	}
	call(t, router, "POST", "/api/activity/browser", map[string]string{"app": "Browser"}, 401, nil)
	cfg := status.Config
	cfg.Enabled = false
	call(t, router, "PUT", "/api/activity/config", cfg, 200, &status)
	if status.Config.Enabled {
		t.Fatal("pause failed")
	}
	if err = collector.Observe(sample, at.Add(10*time.Second)); err != nil {
		t.Fatal(err)
	}
	var summary activity.Summary
	call(t, router, "GET", "/api/activity/summary", nil, 200, &summary)
	if summary.TotalSeconds != 5 {
		t.Fatal("paused recording", summary)
	}
	var series activity.Series
	call(t, router, "GET", "/api/activity/series?days=7&granularity=day", nil, 200, &series)
	if series.Granularity != "day" || len(series.Buckets) != 7 {
		t.Fatal("unexpected daily series", series)
	}
	call(t, router, "GET", "/api/activity/series?granularity=month", nil, 400, nil)
	call(t, router, "POST", "/api/activity/series", nil, 405, nil)
	call(t, router, "DELETE", "/api/activity/data", nil, 400, nil)
	call(t, router, "DELETE", "/api/activity/data?confirm=true", nil, 200, nil)
	call(t, router, "GET", "/api/graph", nil, 200, &snap)
	if len(snap.Nodes) != 0 {
		t.Fatal("activity clear failed")
	}
	cfg.IdleSeconds = 0
	call(t, router, "PUT", "/api/activity/config", cfg, 400, nil)
}

// The extension is the only source that can name a Firefox domain: Firefox does
// not expose a window document URL to the native sampler, so a paired Firefox
// extension must be able to reach the browser endpoint. Each browser reports a
// different extension origin, and accepting only chrome-extension:// silently
// rejected a pairing that the user had just completed.
func TestBrowserHintOrigins(t *testing.T) {
	h, _ := setup(t)
	collector, err := activity.New(h.DB, h.Service, noopSampler{}, true)
	if err != nil {
		t.Fatal(err)
	}
	h.Activity = collector
	router := h.Router()
	post := func(origin, token, payload string) (int, string) {
		req := httptest.NewRequest("POST", "/api/activity/browser", strings.NewReader(payload))
		if origin != "" {
			req.Header.Set("Origin", origin)
		}
		if token != "" {
			req.Header.Set("Authorization", "Bearer "+token)
		}
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)
		return w.Code, w.Body.String()
	}
	hint := `{"app":"Firefox","url":"https://mozilla.org/firefox/notes?secret=x#y","title":"Firefox notes","focused":true}`
	for _, origin := range []string{"chrome-extension://abcdef", "moz-extension://0f1e2d3c-firefox", "safari-web-extension://abcdef"} {
		if code, body := post(origin, collector.Token(), hint); code != 200 {
			t.Fatalf("%s rejected with a valid token: %d %s", origin, code, body)
		}
	}
	// The origin is defence in depth; the pairing token stays the real gate, and
	// an ordinary website is never trusted even when it guesses the token.
	if code, _ := post("moz-extension://0f1e2d3c-firefox", "wrong", hint); code == 200 {
		t.Fatal("a Firefox origin bypassed the pairing token")
	}
	if code, body := post("https://evil.example", collector.Token(), hint); code != 403 {
		t.Fatalf("an untrusted website origin was accepted: %d %s", code, body)
	}
	// Firefox preflights the JSON POST, so the preflight must be answered too.
	req := httptest.NewRequest("OPTIONS", "/api/activity/browser", nil)
	req.Header.Set("Origin", "moz-extension://0f1e2d3c-firefox")
	req.Header.Set("Access-Control-Request-Method", "POST")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code != 204 || w.Header().Get("Access-Control-Allow-Origin") != "moz-extension://0f1e2d3c-firefox" {
		t.Fatalf("Firefox preflight refused: %d %v", w.Code, w.Header())
	}
	// End to end: the hint supplies the domain the native sampler cannot read for
	// Firefox, and the session is attributed to Firefox rather than a Chromium app.
	if code, body := post("moz-extension://0f1e2d3c-firefox", collector.Token(), hint); code != 200 {
		t.Fatalf("Firefox hint rejected: %d %s", code, body)
	}
	// The hint is time-boxed, so the native samples must follow it.
	at := time.Now()
	native := activity.Sample{App: "Firefox", AppID: "org.mozilla.firefox", WindowTitle: "Native Firefox title"}
	if err = collector.Observe(native, at); err != nil {
		t.Fatal(err)
	}
	if err = collector.Observe(native, at.Add(5*time.Second)); err != nil {
		t.Fatal(err)
	}
	call(t, router, "POST", "/api/sync", nil, 200, nil)
	var sites activity.WebsiteResult
	call(t, router, "GET", "/api/activity/domains?search=mozilla", nil, 200, &sites)
	if sites.Total != 1 || sites.Domains[0].Domain != "mozilla.org" {
		t.Fatalf("Firefox domain not tracked: %+v", sites)
	}
	if len(sites.Domains[0].Browsers) != 1 || sites.Domains[0].Browsers[0] != "Firefox" {
		t.Fatalf("Firefox domain attributed to the wrong browser: %+v", sites.Domains[0].Browsers)
	}
	var detail activity.NodeDetail
	call(t, router, "GET", "/api/activity/nodes/"+strconv.FormatInt(sites.Domains[0].NodeID, 10), nil, 200, &detail)
	if detail.Domain != "mozilla.org" || detail.Sessions != 1 {
		t.Fatalf("unexpected Firefox node detail: %+v", detail)
	}
	// The URL is still sanitized on ingestion, whatever the origin claimed.
	if strings.Contains(call(t, router, "GET", "/api/activity/recent?search=mozilla", nil, 200, nil).Body.String(), "?secret") {
		t.Fatal("Firefox hint leaked a query string")
	}
}
