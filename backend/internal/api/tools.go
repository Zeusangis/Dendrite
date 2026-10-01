package api

import (
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"net/http"
	"sort"

	"github.com/zeusangis/dendrite/internal/notes"
	"github.com/zeusangis/dendrite/internal/storage"
)

type Settings struct {
	MinAutoEdgeStrength float64 `json:"min_auto_edge_strength"`
}

func (h *Handler) settings() (Settings, error) {
	cfg := Settings{25}
	v, err := storage.GetSetting(h.DB, "min_auto_edge_strength")
	if err != nil {
		return cfg, err
	}
	if v != "" {
		_, err = fmt.Sscan(v, &cfg.MinAutoEdgeStrength)
	}
	return cfg, err
}
func (h *Handler) SettingsHandler(w http.ResponseWriter, r *http.Request) {
	if !method(w, r, "GET", "PUT") {
		return
	}
	if r.Method == "GET" {
		cfg, err := h.settings()
		if err != nil {
			fail(w, err)
			return
		}
		writeJSON(w, cfg)
		return
	}
	var cfg Settings
	if !decode(w, r, &cfg) {
		return
	}
	if cfg.MinAutoEdgeStrength < 0 || cfg.MinAutoEdgeStrength > 150 {
		writeErr(w, 400, "threshold must be between 0 and 150")
		return
	}
	previous, err := h.settings()
	if err != nil {
		fail(w, err)
		return
	}
	if err = storage.SetSetting(h.DB, "min_auto_edge_strength", fmt.Sprint(cfg.MinAutoEdgeStrength)); err == nil {
		err = h.Service.RebuildLocked()
	}
	if err != nil {
		storage.SetSetting(h.DB, "min_auto_edge_strength", fmt.Sprint(previous.MinAutoEdgeStrength))
		h.Service.RebuildLocked()
		fail(w, err)
		return
	}
	writeJSON(w, cfg)
}

type BackupFile struct {
	Path    string   `json:"path"`
	Content string   `json:"content"`
	X       *float64 `json:"x"`
	Y       *float64 `json:"y"`
}
type Backup struct {
	Version  int          `json:"version"`
	Files    []BackupFile `json:"files"`
	Settings *Settings    `json:"settings,omitempty"`
}

func (h *Handler) ExportHandler(w http.ResponseWriter, r *http.Request) {
	if !method(w, r, "GET") {
		return
	}
	list, err := h.Vault.List()
	if err != nil {
		fail(w, err)
		return
	}
	cfg, err := h.settings()
	if err != nil {
		fail(w, err)
		return
	}
	backup := Backup{Version: 1, Files: []BackupFile{}, Settings: &cfg}
	for _, f := range list {
		raw, err := h.Vault.Raw(f.RelPath)
		if err != nil {
			fail(w, err)
			return
		}
		n, err := storage.GetNodeByPath(h.DB, f.RelPath)
		if err != nil {
			fail(w, err)
			return
		}
		entry := BackupFile{Path: f.RelPath, Content: raw}
		if n != nil {
			entry.X, entry.Y = n.X, n.Y
		}
		backup.Files = append(backup.Files, entry)
	}
	w.Header().Set("Content-Disposition", `attachment; filename="dendrite-backup.json"`)
	writeJSON(w, backup)
}
func (h *Handler) ImportHandler(w http.ResponseWriter, r *http.Request) {
	if !method(w, r, "POST") {
		return
	}
	var backup Backup
	if !decode(w, r, &backup) {
		return
	}
	if backup.Version != 1 || len(backup.Files) == 0 || len(backup.Files) > 2000 {
		writeErr(w, 400, "expected a version 1 backup with 1–2000 files")
		return
	}
	before, after := map[string]*string{}, map[string]*string{}
	for _, f := range backup.Files {
		if _, err := h.Vault.AbsPath(f.Path); err != nil {
			writeErr(w, 400, err.Error())
			return
		}
		if _, ok := after[f.Path]; ok {
			writeErr(w, 400, "duplicate import path")
			return
		}
		if (f.X == nil) != (f.Y == nil) || (f.X != nil && (math.Abs(*f.X) > 1e7 || math.Abs(*f.Y) > 1e7)) {
			writeErr(w, 400, "invalid backup position")
			return
		}
		raw, err := h.Vault.Raw(f.Path)
		if err != nil && !errors.Is(err, notes.ErrNotFound) {
			fail(w, err)
			return
		}
		if err == nil {
			if r.URL.Query().Get("overwrite") != "true" {
				writeErr(w, 409, "import would overwrite "+f.Path+"; explicitly allow overwrite or choose another vault")
				return
			}
			before[f.Path] = ptr(raw)
		} else {
			before[f.Path] = nil
		}
		after[f.Path] = ptr(f.Content)
	}
	// Validate metadata before touching any files.
	if backup.Settings != nil && (backup.Settings.MinAutoEdgeStrength < 0 || backup.Settings.MinAutoEdgeStrength > 150) {
		writeErr(w, 400, "invalid backup threshold")
		return
	}
	if err := h.applyChange("Import notes", before, after); err != nil {
		fail(w, err)
		return
	}
	for _, f := range backup.Files {
		n, err := storage.GetNodeByPath(h.DB, f.Path)
		if err != nil {
			fail(w, err)
			return
		}
		if n != nil {
			if err = storage.UpdateNodePosition(h.DB, n.ID, f.X, f.Y); err != nil {
				fail(w, err)
				return
			}
		}
	}
	if backup.Settings != nil {
		if err := storage.SetSetting(h.DB, "min_auto_edge_strength", fmt.Sprint(backup.Settings.MinAutoEdgeStrength)); err != nil {
			fail(w, err)
			return
		}
		if err := h.Service.RebuildLocked(); err != nil {
			fail(w, err)
			return
		}
	}
	writeJSON(w, map[string]int{"imported": len(backup.Files)})
}

// History is local-only and stores full Markdown snapshots, including deletions.
// Only application changes are undoable; external edits always win conflicts.
func (h *Handler) checkState(expected map[string]*string) error {
	for path, value := range expected {
		raw, err := h.Vault.Raw(path)
		if value == nil {
			if err == nil {
				return fmt.Errorf("%s: %w", path, notes.ErrConflict)
			}
			if !errors.Is(err, notes.ErrNotFound) {
				return err
			}
		} else {
			if err != nil {
				return fmt.Errorf("%s: %w", path, notes.ErrConflict)
			}
			if raw != *value {
				return fmt.Errorf("%s: %w", path, notes.ErrConflict)
			}
		}
	}
	return nil
}
func (h *Handler) writeStates(expected, target map[string]*string) error {
	if err := h.checkState(expected); err != nil {
		return err
	}
	paths := []string{}
	for path := range target {
		paths = append(paths, path)
	}
	sort.Strings(paths)
	written := []string{}
	for _, path := range paths {
		var err error
		if target[path] == nil {
			err = h.Vault.Delete(path)
		} else {
			err = h.Vault.WriteRaw(path, *target[path], expected[path] == nil)
		}
		if err != nil {
			var rollback error
			for i := len(written) - 1; i >= 0; i-- {
				p := written[i]
				var e error
				if expected[p] == nil {
					e = h.Vault.Delete(p)
				} else {
					e = h.Vault.WriteRaw(p, *expected[p], false)
				}
				rollback = errors.Join(rollback, e)
			}
			return errors.Join(err, rollback)
		}
		written = append(written, path)
	}
	return nil
}
func (h *Handler) applyChange(label string, before, after map[string]*string) error {
	if err := h.writeStates(before, after); err != nil {
		return err
	}
	if _, err := h.Service.SyncLocked(); err != nil {
		rollback := h.writeStates(after, before)
		_, syncErr := h.Service.SyncLocked()
		return errors.Join(err, rollback, syncErr)
	}
	a, _ := json.Marshal(before)
	b, _ := json.Marshal(after)
	tx, err := h.DB.Begin()
	if err != nil {
		rollback := h.writeStates(after, before)
		_, syncErr := h.Service.SyncLocked()
		return errors.Join(err, rollback, syncErr)
	}
	defer tx.Rollback()
	_, err = tx.Exec(`DELETE FROM history WHERE applied=0`)
	if err == nil {
		_, err = tx.Exec(`INSERT INTO history(label,before_json,after_json) VALUES(?,?,?)`, label, string(a), string(b))
	}
	if err == nil {
		_, err = tx.Exec(`DELETE FROM history WHERE id NOT IN(SELECT id FROM history ORDER BY id DESC LIMIT 100)`)
	}
	if err == nil {
		err = tx.Commit()
	}
	if err != nil {
		tx.Rollback()
		rollback := h.writeStates(after, before)
		_, syncErr := h.Service.SyncLocked()
		return errors.Join(err, rollback, syncErr)
	}
	return nil
}
func (h *Handler) HistoryHandler(w http.ResponseWriter, r *http.Request) {
	action := r.URL.Path[len("/api/history"):]
	if action == "" {
		if !method(w, r, "GET") {
			return
		}
		var undo, redo string
		err := h.DB.QueryRow(`SELECT COALESCE((SELECT label FROM history WHERE applied=1 ORDER BY id DESC LIMIT 1),''),COALESCE((SELECT label FROM history WHERE applied=0 ORDER BY id ASC LIMIT 1),'')`).Scan(&undo, &redo)
		if err != nil {
			fail(w, err)
			return
		}
		writeJSON(w, map[string]string{"undo": undo, "redo": redo})
		return
	}
	if action != "/undo" && action != "/redo" {
		writeErr(w, 404, "unknown history action")
		return
	}
	if !method(w, r, "POST") {
		return
	}
	undo := action == "/undo"
	query := `SELECT id,before_json,after_json FROM history WHERE applied=0 ORDER BY id ASC LIMIT 1`
	if undo {
		query = `SELECT id,before_json,after_json FROM history WHERE applied=1 ORDER BY id DESC LIMIT 1`
	}
	var id int64
	var a, b string
	if err := h.DB.QueryRow(query).Scan(&id, &a, &b); err != nil {
		fail(w, err)
		return
	}
	var before, after map[string]*string
	if err := json.Unmarshal([]byte(a), &before); err != nil {
		fail(w, err)
		return
	}
	if err := json.Unmarshal([]byte(b), &after); err != nil {
		fail(w, err)
		return
	}
	expected, target := before, after
	applied := 1
	if undo {
		expected, target = after, before
		applied = 0
	}
	if err := h.writeStates(expected, target); err != nil {
		fail(w, err)
		return
	}
	if _, err := h.Service.SyncLocked(); err != nil {
		rollback := h.writeStates(target, expected)
		_, syncErr := h.Service.SyncLocked()
		fail(w, errors.Join(err, rollback, syncErr))
		return
	}
	if _, err := h.DB.Exec(`UPDATE history SET applied=? WHERE id=?`, applied, id); err != nil {
		rollback := h.writeStates(target, expected)
		_, syncErr := h.Service.SyncLocked()
		fail(w, errors.Join(err, rollback, syncErr))
		return
	}
	writeJSON(w, map[string]bool{"ok": true})
}
