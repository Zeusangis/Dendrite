package api

import (
	"database/sql"
	"encoding/json"
	"errors"
	"io"
	"math"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/zeusangis/dendrite/internal/activity"
	"github.com/zeusangis/dendrite/internal/graph"
	"github.com/zeusangis/dendrite/internal/notes"
	"github.com/zeusangis/dendrite/internal/storage"
)

type Handler struct {
	Service  *graph.Service
	Vault    *notes.Vault
	DB       *sql.DB
	Activity *activity.Collector
}

func writeJSON(w http.ResponseWriter, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(v)
}
func writeErr(w http.ResponseWriter, status int, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(map[string]string{"error": msg})
}
func method(w http.ResponseWriter, r *http.Request, methods ...string) bool {
	for _, m := range methods {
		if r.Method == m {
			return true
		}
	}
	w.Header().Set("Allow", strings.Join(methods, ", "))
	writeErr(w, 405, "method not allowed")
	return false
}
func decode(w http.ResponseWriter, r *http.Request, v interface{}) bool {
	r.Body = http.MaxBytesReader(w, r.Body, 8<<20)
	d := json.NewDecoder(r.Body)
	d.DisallowUnknownFields()
	if err := d.Decode(v); err != nil {
		writeErr(w, 400, "invalid request: "+err.Error())
		return false
	}
	if err := d.Decode(new(interface{})); err != io.EOF {
		writeErr(w, 400, "request must contain one JSON object")
		return false
	}
	return true
}
func fail(w http.ResponseWriter, err error) {
	status := 500
	if errors.Is(err, notes.ErrConflict) {
		status = 409
	}
	if errors.Is(err, notes.ErrNotFound) || errors.Is(err, sql.ErrNoRows) {
		status = 404
	}
	writeErr(w, status, err.Error())
}
func (h *Handler) Router() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/health", h.HealthHandler)
	mux.HandleFunc("/api/notes", h.NotesHandler)
	mux.HandleFunc("/api/notes/", h.NotesHandler)
	mux.HandleFunc("/api/graph", h.GraphHandler)
	mux.HandleFunc("/api/nodes/", h.NodesHandler)
	mux.HandleFunc("/api/edges/", h.EdgesHandler)
	mux.HandleFunc("/api/search", h.SearchHandler)
	mux.HandleFunc("/api/sync", h.SyncHandler)
	mux.HandleFunc("/api/settings", h.SettingsHandler)
	mux.HandleFunc("/api/export", h.ExportHandler)
	mux.HandleFunc("/api/import", h.ImportHandler)
	mux.HandleFunc("/api/history", h.HistoryHandler)
	mux.HandleFunc("/api/history/", h.HistoryHandler)
	mux.HandleFunc("/api/activity", h.ActivityHandler)
	mux.HandleFunc("/api/activity/", h.ActivityHandler)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Browser requests from other sites must not mutate a local vault.
		origin := r.Header.Get("Origin")
		parsed, originErr := url.Parse(origin)
		trusted := originErr == nil && parsed.User == nil && (parsed.Scheme == "http" || parsed.Scheme == "https") && (parsed.Hostname() == "localhost" || parsed.Hostname() == "127.0.0.1" || parsed.Hostname() == "::1")
		extension := strings.HasPrefix(origin, "chrome-extension://") && r.URL.Path == "/api/activity/browser" && h.Activity != nil && r.Header.Get("Authorization") == "Bearer "+h.Activity.Token()
		if extension {
			w.Header().Set("Access-Control-Allow-Origin", origin)
			w.Header().Set("Access-Control-Allow-Methods", "POST, OPTIONS")
			w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")
		}
		if r.Method == "OPTIONS" && strings.HasPrefix(origin, "chrome-extension://") && r.URL.Path == "/api/activity/browser" {
			w.Header().Set("Access-Control-Allow-Origin", origin)
			w.Header().Set("Access-Control-Allow-Methods", "POST, OPTIONS")
			w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")
			w.WriteHeader(204)
			return
		}
		if origin != "" && !trusted && !extension {
			writeErr(w, 403, "untrusted origin")
			return
		}
		h.Service.Lock()
		defer h.Service.Unlock()
		mux.ServeHTTP(w, r)
	})
}
func (h *Handler) HealthHandler(w http.ResponseWriter, r *http.Request) {
	if method(w, r, "GET") {
		writeJSON(w, map[string]string{"status": "ok", "service": "dendrite"})
	}
}
func (h *Handler) NotesHandler(w http.ResponseWriter, r *http.Request) {
	path := strings.Trim(strings.TrimPrefix(r.URL.Path, "/api/notes"), "/")
	if path == "" {
		if !method(w, r, "GET", "POST") {
			return
		}
		if r.Method == "GET" {
			list, err := storage.ListNotes(h.DB)
			if err != nil {
				fail(w, err)
				return
			}
			writeJSON(w, list)
		} else {
			h.createNote(w, r)
		}
		return
	}
	if !method(w, r, "GET", "PUT", "DELETE") {
		return
	}
	switch r.Method {
	case "GET":
		h.getNote(w, r, path)
	case "PUT":
		h.updateNote(w, r, path)
	case "DELETE":
		h.deleteNote(w, r, path)
	}
}

type BacklinkInfo struct {
	ID    int64  `json:"id"`
	Title string `json:"title"`
	Path  string `json:"path"`
}
type noteResponse struct {
	storage.Note
	Links     []string         `json:"links"`
	Backlinks []BacklinkInfo   `json:"backlinks"`
	Tags      []string         `json:"tags"`
	Entities  []storage.Entity `json:"entities"`
	Content   string           `json:"content"`
	Revision  string           `json:"revision"`
}

func (h *Handler) resolveNote(value string) (*storage.Note, error) {
	if id, err := strconv.ParseInt(value, 10, 64); err == nil {
		n := &storage.Note{}
		err = h.DB.QueryRow(`SELECT id,type,title,path,created_at,updated_at,importance,x,y FROM nodes WHERE id=?`, id).Scan(&n.ID, &n.Type, &n.Title, &n.Path, &n.CreatedAt, &n.UpdatedAt, &n.Importance, &n.X, &n.Y)
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return n, err
	}
	return storage.GetNodeByPath(h.DB, value)
}
func (h *Handler) getNote(w http.ResponseWriter, r *http.Request, value string) {
	n, err := h.resolveNote(value)
	if err != nil {
		fail(w, err)
		return
	}
	if n == nil || n.Type != "note" {
		fail(w, notes.ErrNotFound)
		return
	}
	raw, err := h.Vault.Raw(n.Path)
	if err != nil {
		fail(w, err)
		return
	}
	pn, err := notes.ParseContent(raw, n.Path)
	if err != nil {
		fail(w, err)
		return
	}
	resp := noteResponse{Note: *n, Links: []string{}, Tags: []string{}, Backlinks: []BacklinkInfo{}, Entities: []storage.Entity{}, Content: raw, Revision: notes.Revision(raw)}
	resp.Links = append(resp.Links, pn.Links...)
	resp.Tags = append(resp.Tags, pn.Tags...)
	for _, e := range pn.Entities {
		resp.Entities = append(resp.Entities, storage.Entity{NoteID: n.ID, Name: e.Name, Kind: e.Kind, Weight: e.Weight})
	}
	rows, err := h.DB.Query(`SELECT DISTINCT n.id,n.title,n.path FROM node_links l JOIN nodes n ON n.id=l.note_id WHERE l.target_title=? COLLATE NOCASE AND n.id<>? ORDER BY n.title`, n.Title, n.ID)
	if err != nil {
		fail(w, err)
		return
	}
	for rows.Next() {
		var b BacklinkInfo
		if err = rows.Scan(&b.ID, &b.Title, &b.Path); err != nil {
			rows.Close()
			fail(w, err)
			return
		}
		resp.Backlinks = append(resp.Backlinks, b)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		fail(w, err)
		return
	}
	writeJSON(w, resp)
}

type noteInput struct {
	Title    string  `json:"title"`
	Content  *string `json:"content"`
	Path     string  `json:"path"`
	Revision string  `json:"revision"`
}

func validTitle(title string) bool {
	return strings.TrimSpace(title) != "" && len(title) <= 240 && !strings.ContainsAny(title, "\r\n")
}
func (h *Handler) createNote(w http.ResponseWriter, r *http.Request) {
	var in noteInput
	if !decode(w, r, &in) {
		return
	}
	if !validTitle(in.Title) {
		writeErr(w, 400, "a single-line title (up to 240 bytes) is required")
		return
	}
	path := in.Path
	if path == "" {
		slug := notes.TitleToFilename(in.Title)
		if slug == "" {
			writeErr(w, 400, "title needs letters or digits")
			return
		}
		path = slug + ".md"
	}
	if _, err := h.Vault.AbsPath(path); err != nil {
		writeErr(w, 400, err.Error())
		return
	}
	body := ""
	if in.Content != nil {
		body = *in.Content
	}
	after := map[string]*string{path: ptr(notes.WithTitle(body, in.Title))}
	if err := h.applyChange("Create "+in.Title, map[string]*string{path: nil}, after); err != nil {
		fail(w, err)
		return
	}
	n, err := storage.GetNodeByPath(h.DB, path)
	if err != nil {
		fail(w, err)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(201)
	writeJSON(w, n)
}
func (h *Handler) updateNote(w http.ResponseWriter, r *http.Request, value string) {
	n, err := h.resolveNote(value)
	if err != nil {
		fail(w, err)
		return
	}
	if n == nil || n.Type != "note" {
		fail(w, notes.ErrNotFound)
		return
	}
	var in noteInput
	if !decode(w, r, &in) {
		return
	}
	raw, err := h.Vault.Raw(n.Path)
	if err != nil {
		fail(w, err)
		return
	}
	if in.Revision != "" && in.Revision != notes.Revision(raw) {
		fail(w, notes.ErrConflict)
		return
	}
	title := in.Title
	if title == "" {
		title = n.Title
	}
	if !validTitle(title) {
		writeErr(w, 400, "invalid title")
		return
	}
	body := raw
	if in.Content != nil {
		body = *in.Content
	}
	if err = h.applyChange("Edit "+n.Title, map[string]*string{n.Path: ptr(raw)}, map[string]*string{n.Path: ptr(notes.WithTitle(body, title))}); err != nil {
		fail(w, err)
		return
	}
	updated, err := storage.GetNodeByPath(h.DB, n.Path)
	if err != nil {
		fail(w, err)
		return
	}
	writeJSON(w, updated)
}
func (h *Handler) deleteNote(w http.ResponseWriter, r *http.Request, value string) {
	n, err := h.resolveNote(value)
	if err != nil {
		fail(w, err)
		return
	}
	if n == nil || n.Type != "note" {
		fail(w, notes.ErrNotFound)
		return
	}
	raw, err := h.Vault.Raw(n.Path)
	if err != nil {
		fail(w, err)
		return
	}
	if revision := r.URL.Query().Get("revision"); revision != "" && revision != notes.Revision(raw) {
		fail(w, notes.ErrConflict)
		return
	}
	if err = h.applyChange("Delete "+n.Title, map[string]*string{n.Path: ptr(raw)}, map[string]*string{n.Path: nil}); err != nil {
		fail(w, err)
		return
	}
	writeJSON(w, map[string]bool{"deleted": true})
}
func (h *Handler) GraphHandler(w http.ResponseWriter, r *http.Request) {
	if !method(w, r, "GET") {
		return
	}
	snap, err := storage.GetGraph(h.DB)
	if err != nil {
		fail(w, err)
		return
	}
	writeJSON(w, snap)
}
func (h *Handler) NodesHandler(w http.ResponseWriter, r *http.Request) {
	parts := strings.Split(strings.Trim(strings.TrimPrefix(r.URL.Path, "/api/nodes/"), "/"), "/")
	if len(parts) > 2 {
		writeErr(w, 404, "unknown endpoint")
		return
	}
	n, err := h.resolveNote(parts[0])
	if err != nil {
		fail(w, err)
		return
	}
	if n == nil {
		fail(w, notes.ErrNotFound)
		return
	}
	if len(parts) == 1 {
		if method(w, r, "GET") {
			writeJSON(w, n)
		}
		return
	}
	switch parts[1] {
	case "related":
		if method(w, r, "GET") {
			h.getRelated(w, r, n.ID)
		}
	case "activity":
		if method(w, r, "GET") {
			usage, err := activity.NodeUsage(h.DB, n.ID)
			if err != nil {
				fail(w, err)
				return
			}
			if usage == nil {
				writeErr(w, 404, "activity stats not found")
				return
			}
			writeJSON(w, usage)
		}
	case "position":
		if method(w, r, "PUT") {
			h.setPosition(w, r, n.ID)
		}
	default:
		writeErr(w, 404, "unknown endpoint")
	}
}

type RelatedNode struct {
	Node      storage.Note `json:"node"`
	Edge      storage.Edge `json:"edge"`
	Direction string       `json:"direction"`
}

func (h *Handler) getRelated(w http.ResponseWriter, r *http.Request, id int64) {
	snap, err := storage.GetGraph(h.DB)
	if err != nil {
		fail(w, err)
		return
	}
	byID := map[int64]storage.Note{}
	for _, n := range snap.Nodes {
		byID[n.ID] = n
	}
	related := []RelatedNode{}
	for _, e := range snap.Edges {
		other := int64(0)
		if e.SourceID == id {
			other = e.TargetID
		} else if e.TargetID == id {
			other = e.SourceID
		}
		if other != 0 {
			related = append(related, RelatedNode{byID[other], e, "undirected"})
		}
	}
	writeJSON(w, related)
}
func (h *Handler) setPosition(w http.ResponseWriter, r *http.Request, id int64) {
	var pos struct {
		X *float64 `json:"x"`
		Y *float64 `json:"y"`
	}
	if !decode(w, r, &pos) {
		return
	}
	if (pos.X == nil) != (pos.Y == nil) || (pos.X != nil && (math.Abs(*pos.X) > 1e7 || math.Abs(*pos.Y) > 1e7)) {
		writeErr(w, 400, "positions must both be numbers or both null")
		return
	}
	if err := storage.UpdateNodePosition(h.DB, id, pos.X, pos.Y); err != nil {
		fail(w, err)
		return
	}
	writeJSON(w, map[string]bool{"ok": true})
}
func (h *Handler) EdgesHandler(w http.ResponseWriter, r *http.Request) {
	if !method(w, r, "GET") {
		return
	}
	id, err := strconv.ParseInt(strings.TrimPrefix(r.URL.Path, "/api/edges/"), 10, 64)
	if err != nil {
		writeErr(w, 400, "invalid edge id")
		return
	}
	snap, err := storage.GetGraph(h.DB)
	if err != nil {
		fail(w, err)
		return
	}
	for _, e := range snap.Edges {
		if e.ID == id {
			writeJSON(w, e)
			return
		}
	}
	writeErr(w, 404, "edge not found")
}
func (h *Handler) SearchHandler(w http.ResponseWriter, r *http.Request) {
	if !method(w, r, "GET") {
		return
	}
	q := r.URL.Query()
	limit, _ := strconv.Atoi(q.Get("limit"))
	hits, err := h.Service.SearchFiltered(q.Get("q"), limit, graph.SearchFilters{Tag: q.Get("tag"), Project: q.Get("project"), Path: q.Get("path")})
	if err != nil {
		fail(w, err)
		return
	}
	writeJSON(w, hits)
}
func (h *Handler) SyncHandler(w http.ResponseWriter, r *http.Request) {
	if !method(w, r, "POST") {
		return
	}
	res, err := h.Service.SyncLocked()
	if err != nil {
		fail(w, err)
		return
	}
	writeJSON(w, res)
}
func ptr(s string) *string { return &s }
