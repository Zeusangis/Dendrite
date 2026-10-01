package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/zeusangis/dendrite/internal/graph"
	"github.com/zeusangis/dendrite/internal/notes"
	"github.com/zeusangis/dendrite/internal/storage"
)

func setup(t *testing.T) (*Handler, http.Handler) {
	t.Helper()
	root := t.TempDir()
	vault := &notes.Vault{Root: filepath.Join(root, "notes")}
	if err := os.MkdirAll(vault.Root, 0755); err != nil {
		t.Fatal(err)
	}
	db, err := storage.OpenDatabase(filepath.Join(root, "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	h := &Handler{Vault: vault, DB: db, Service: graph.NewService(vault, db)}
	return h, h.Router()
}
func call(t *testing.T, router http.Handler, method, path string, input interface{}, status int, out interface{}) *httptest.ResponseRecorder {
	t.Helper()
	body := ""
	if input != nil {
		b, err := json.Marshal(input)
		if err != nil {
			t.Fatal(err)
		}
		body = string(b)
	}
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code != status {
		t.Fatalf("%s %s: status %d, want %d: %s", method, path, w.Code, status, w.Body.String())
	}
	if out != nil {
		if err := json.Unmarshal(w.Body.Bytes(), out); err != nil {
			t.Fatal(err)
		}
	}
	return w
}
func notePath(id int64) string { return "/api/notes/" + strconv.FormatInt(id, 10) }
func TestFirstMilestoneAndStableSync(t *testing.T) {
	h, router := setup(t)
	var a, b storage.Note
	call(t, router, "POST", "/api/notes", map[string]string{"title": "Alpha", "content": "---\nproject: lab\n---\n# Alpha\n\n[[Beta|friend]] #test\n"}, 201, &a)
	call(t, router, "POST", "/api/notes", map[string]string{"title": "Beta", "content": "# Beta\n\nOther content"}, 201, &b)
	var snap storage.GraphSnapshot
	call(t, router, "GET", "/api/graph", nil, 200, &snap)
	if len(snap.Nodes) != 2 || len(snap.Edges) != 1 {
		t.Fatalf("graph %+v", snap)
	}
	edge := snap.Edges[0]
	if edge.Source != "explicit" || edge.RelationshipType != "wikilink" || !strings.Contains(edge.Reason, "Explicit link") {
		t.Fatalf("edge %+v", edge)
	}
	var detail noteResponse
	call(t, router, "GET", notePath(b.ID), nil, 200, &detail)
	if len(detail.Backlinks) != 1 || detail.Backlinks[0].ID != a.ID {
		t.Fatalf("backlinks %+v", detail.Backlinks)
	}
	call(t, router, "GET", notePath(a.ID), nil, 200, &detail)
	if !strings.HasPrefix(detail.Content, "---\n") || strings.Count(detail.Content, "# Alpha") != 1 {
		t.Fatalf("raw lost: %q", detail.Content)
	}
	for i := 0; i < 3; i++ {
		call(t, router, "POST", "/api/sync", nil, 200, nil)
	}
	call(t, router, "GET", "/api/graph", nil, 200, &snap)
	if snap.Edges[0].ID != edge.ID || snap.Edges[0].CreatedAt != edge.CreatedAt {
		t.Fatal("edge identity churn")
	}
	call(t, router, "GET", notePath(a.ID), nil, 200, &detail)
	if len(detail.Links) != 1 || len(detail.Tags) != 1 {
		t.Fatalf("metadata attached to wrong note: %+v", detail)
	}
	call(t, router, "GET", "/api/edges/"+strconv.FormatInt(edge.ID, 10), nil, 200, nil)
	pos := map[string]interface{}{"x": 120.5, "y": -80.0}
	call(t, router, "PUT", "/api/nodes/"+strconv.FormatInt(a.ID, 10)+"/position", pos, 200, nil)
	call(t, router, "POST", "/api/sync", nil, 200, nil)
	call(t, router, "GET", notePath(a.ID), nil, 200, &detail)
	if detail.X == nil || *detail.X != 120.5 {
		t.Fatal("position not persisted")
	}
	call(t, router, "PUT", "/api/nodes/"+strconv.FormatInt(a.ID, 10)+"/position", map[string]interface{}{"x": nil, "y": nil}, 200, nil)
	call(t, router, "GET", notePath(a.ID), nil, 200, &detail)
	if detail.X != nil || detail.Y != nil {
		t.Fatal("unpin not persisted")
	}
	call(t, router, "PUT", notePath(a.ID), map[string]interface{}{"title": "Renamed", "content": detail.Content, "revision": detail.Revision}, 200, &a)
	call(t, router, "GET", notePath(a.ID), nil, 200, &detail)
	if detail.Title != "Renamed" || strings.Contains(detail.Content, "# Alpha") || !strings.Contains(detail.Content, "# Renamed") || !strings.HasPrefix(detail.Content, "---\n") {
		t.Fatal("bad rename", detail.Content)
	}
	var hits []graph.SearchHit
	call(t, router, "GET", "/api/search?q=Renam&tag=test&project=lab", nil, 200, &hits)
	if len(hits) != 1 || hits[0].Node.ID != a.ID {
		t.Fatalf("search %+v", hits)
	}
	call(t, router, "PUT", notePath(a.ID), map[string]string{"title": "Renamed", "content": "# Renamed\n\nNo link now"}, 200, nil)
	call(t, router, "GET", "/api/graph", nil, 200, &snap)
	if snap.Edges[0].Source != "automatic" || snap.Edges[0].ID != edge.ID {
		t.Fatal("explicit edge source stale", snap.Edges)
	}
	if err := h.Vault.WriteRaw(b.Path, "# External Beta\n\nUnique externalwords", false); err != nil {
		t.Fatal(err)
	}
	call(t, router, "POST", "/api/sync", nil, 200, nil)
	call(t, router, "GET", notePath(b.ID), nil, 200, &detail)
	if detail.Title != "External Beta" {
		t.Fatal("external edit not synced")
	}
	call(t, router, "GET", "/api/search?q=externalwords", nil, 200, &hits)
	if len(hits) != 1 {
		t.Fatal("external content not indexed")
	}
	call(t, router, "DELETE", notePath(b.ID), nil, 200, nil)
	call(t, router, "GET", "/api/graph", nil, 200, &snap)
	if len(snap.Nodes) != 1 || len(snap.Edges) != 0 {
		t.Fatal("delete not cascaded")
	}
	if _, err := h.Vault.Raw(b.Path); err != notes.ErrNotFound {
		t.Fatal("file remains", err)
	}
}
func TestConflictsHistoryAndBackup(t *testing.T) {
	h, router := setup(t)
	var n storage.Note
	call(t, router, "POST", "/api/notes", map[string]string{"title": "Note", "content": "# Note\n\nOriginal"}, 201, &n)
	call(t, router, "POST", "/api/notes", map[string]string{"title": "Note", "content": "overwritten"}, 409, nil)
	call(t, router, "POST", "/api/notes", map[string]string{"title": "Bad", "path": "../outside.md"}, 400, nil)
	var detail noteResponse
	call(t, router, "GET", notePath(n.ID), nil, 200, &detail)
	call(t, router, "PUT", notePath(n.ID), map[string]interface{}{"content": "# Note\n\nChanged", "revision": detail.Revision}, 200, nil)
	call(t, router, "PUT", notePath(n.ID), map[string]interface{}{"content": "stale", "revision": detail.Revision}, 409, nil)
	call(t, router, "POST", "/api/history/undo", nil, 200, nil)
	call(t, router, "GET", notePath(n.ID), nil, 200, &detail)
	if !strings.Contains(detail.Content, "Original") {
		t.Fatal("undo failed")
	}
	call(t, router, "POST", "/api/history/redo", nil, 200, nil)
	call(t, router, "GET", notePath(n.ID), nil, 200, &detail)
	if !strings.Contains(detail.Content, "Changed") {
		t.Fatal("redo failed")
	}
	if err := h.Vault.WriteRaw(n.Path, "# Note\n\nExternal change", false); err != nil {
		t.Fatal(err)
	}
	call(t, router, "POST", "/api/history/undo", nil, 409, nil)
	var backup Backup
	call(t, router, "GET", "/api/export", nil, 200, &backup)
	if len(backup.Files) != 1 || !strings.Contains(backup.Files[0].Content, "External change") {
		t.Fatal("backup lost source")
	}
	_, target := setup(t)
	call(t, target, "POST", "/api/import", backup, 200, nil)
	var snap storage.GraphSnapshot
	call(t, target, "GET", "/api/graph", nil, 200, &snap)
	if len(snap.Nodes) != 1 {
		t.Fatal("import failed")
	}
	call(t, target, "POST", "/api/import", backup, 409, nil)
	call(t, target, "DELETE", notePath(snap.Nodes[0].ID), nil, 200, nil)
	call(t, target, "POST", "/api/history/undo", nil, 200, nil)
	call(t, target, "GET", "/api/graph", nil, 200, &snap)
	if len(snap.Nodes) != 1 {
		t.Fatal("delete undo failed")
	}
}
func TestConcurrentSyncAndRequests(t *testing.T) {
	h, router := setup(t)
	var wg sync.WaitGroup
	failures := make(chan string, 20)
	for i := 0; i < 10; i++ {
		wg.Add(2)
		go func(i int) {
			defer wg.Done()
			body := fmt.Sprintf(`{"title":"Concurrent %d","content":"shared words #parallel"}`, i)
			w := httptest.NewRecorder()
			router.ServeHTTP(w, httptest.NewRequest("POST", "/api/notes", strings.NewReader(body)))
			if w.Code != 201 {
				failures <- w.Body.String()
			}
		}(i)
		go func() {
			defer wg.Done()
			if _, err := h.Service.Sync(); err != nil {
				failures <- err.Error()
			}
		}()
	}
	wg.Wait()
	close(failures)
	for err := range failures {
		t.Error(err)
	}
	var snap storage.GraphSnapshot
	call(t, router, "GET", "/api/graph", nil, 200, &snap)
	if len(snap.Nodes) != 10 {
		t.Fatal("concurrent writes lost", len(snap.Nodes))
	}
	var hits []graph.SearchHit
	call(t, router, "GET", "/api/search?q=%22shared%20words%22&tag=parallel", nil, 200, &hits)
	if len(hits) != 10 {
		t.Fatal("phrase search", hits)
	}
}

func TestWeakEdgesSettingsAndMethods(t *testing.T) {
	_, router := setup(t)
	for _, title := range []string{"A", "B"} {
		call(t, router, "POST", "/api/notes", map[string]string{"title": title, "content": ""}, 201, nil)
	}
	var snap storage.GraphSnapshot
	call(t, router, "GET", "/api/graph", nil, 200, &snap)
	if len(snap.Edges) != 1 || snap.Edges[0].Strength >= 25 || snap.Edges[0].Source != "automatic" {
		t.Fatal("weak edge discarded", snap)
	}
	if snap.Nodes[0].Importance != 0 {
		t.Fatal("hidden edge inflated importance")
	}
	call(t, router, "PUT", "/api/settings", Settings{0}, 200, nil)
	call(t, router, "GET", "/api/graph", nil, 200, &snap)
	if snap.Nodes[0].Importance == 0 {
		t.Fatal("threshold not applied")
	}
	call(t, router, "PUT", "/api/settings", Settings{151}, 400, nil)
	for _, path := range []string{"/api/health", "/api/graph", "/api/search", "/api/settings", "/api/export", "/api/history"} {
		call(t, router, "DELETE", path, nil, 405, nil)
	}
	call(t, router, "GET", "/api/sync", nil, 405, nil)
	req := httptest.NewRequest("POST", "/api/notes", strings.NewReader(`{"title":"Bad origin"}`))
	req.Header.Set("Origin", "https://attacker.example")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code != 403 {
		t.Fatal("cross-origin request accepted")
	}
}
