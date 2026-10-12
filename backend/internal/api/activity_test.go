package api

import (
	"context"
	"net/http/httptest"
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
