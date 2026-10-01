package api

import (
	"context"
	"net/http/httptest"
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
	call(t, router, "DELETE", "/api/activity/data", nil, 400, nil)
	call(t, router, "DELETE", "/api/activity/data?confirm=true", nil, 200, nil)
	call(t, router, "GET", "/api/graph", nil, 200, &snap)
	if len(snap.Nodes) != 0 {
		t.Fatal("activity clear failed")
	}
	cfg.IdleSeconds = 0
	call(t, router, "PUT", "/api/activity/config", cfg, 400, nil)
}
