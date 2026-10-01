package graph

import (
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/zeusangis/dendrite/internal/activity"
	"github.com/zeusangis/dendrite/internal/notes"
	"github.com/zeusangis/dendrite/internal/relationships"
	"github.com/zeusangis/dendrite/internal/storage"
)

type Service struct {
	vault     *notes.Vault
	db        *sql.DB
	mu        sync.Mutex
	lastBuild time.Time
}

func NewService(v *notes.Vault, db *sql.DB) *Service { return &Service{vault: v, db: db} }
func (s *Service) Lock()                             { s.mu.Lock() }
func (s *Service) Unlock()                           { s.mu.Unlock() }

type SyncResult struct {
	Scanned int      `json:"scanned"`
	Created int      `json:"created"`
	Updated int      `json:"updated"`
	Deleted int      `json:"deleted"`
	Edges   int      `json:"edges"`
	Errors  []string `json:"errors,omitempty"`
}

func (s *Service) Sync() (*SyncResult, error) { s.Lock(); defer s.Unlock(); return s.SyncLocked() }
func (s *Service) SyncLocked() (*SyncResult, error) {
	res := &SyncResult{}
	files, err := s.vault.List()
	if err != nil {
		return res, err
	}
	existing, err := storage.ListNotes(s.db)
	if err != nil {
		return res, err
	}
	old := map[string]storage.Note{}
	for _, n := range existing {
		old[n.Path] = n
	}
	seen := map[string]bool{}
	changed := false
	for _, file := range files {
		res.Scanned++
		seen[file.RelPath] = true
		raw, err := s.vault.Raw(file.RelPath)
		if err != nil {
			res.Errors = append(res.Errors, err.Error())
			continue
		}
		revision := notes.Revision(raw)
		var previous string
		err = s.db.QueryRow(`SELECT revision FROM note_revisions WHERE path=?`, file.RelPath).Scan(&previous)
		if err != nil && err != sql.ErrNoRows {
			return res, err
		}
		if previous == revision {
			continue
		}
		pn, err := notes.ParseContent(raw, file.RelPath)
		if err != nil {
			res.Errors = append(res.Errors, err.Error())
			continue
		}
		mod := time.Unix(file.ModTime, 0)
		created := mod
		if n, ok := old[file.RelPath]; ok {
			created, _ = time.Parse(time.RFC3339, n.CreatedAt)
		}
		id, err := storage.UpsertNode(s.db, file.RelPath, pn.Title, "note", created, mod)
		if err != nil {
			return res, err
		}
		if _, ok := old[file.RelPath]; ok {
			res.Updated++
		} else {
			res.Created++
		}
		if err = storage.ReplaceLinks(s.db, id, pn.Links); err != nil {
			return res, err
		}
		if err = storage.ReplaceTags(s.db, id, pn.Tags); err != nil {
			return res, err
		}
		ents := []storage.Entity{}
		names := []string{}
		for _, e := range pn.Entities {
			ents = append(ents, storage.Entity{NoteID: id, Name: e.Name, Kind: e.Kind, Weight: e.Weight})
			names = append(names, e.Name)
		}
		if err = storage.ReplaceEntities(s.db, id, ents); err != nil {
			return res, err
		}
		tx, err := s.db.Begin()
		if err != nil {
			return res, err
		}
		_, err = tx.Exec(`DELETE FROM note_search WHERE rowid=?`, id)
		if err == nil {
			_, err = tx.Exec(`INSERT INTO note_search(rowid,title,content,tags,entities,project,path) VALUES(?,?,?,?,?,?,?)`, id, pn.Title, pn.Body, strings.Join(pn.Tags, " "), strings.Join(names, " "), pn.Project, pn.RelPath)
		}
		if err == nil {
			_, err = tx.Exec(`INSERT INTO note_revisions(path,revision) VALUES(?,?) ON CONFLICT(path) DO UPDATE SET revision=excluded.revision`, file.RelPath, revision)
		}
		if err != nil {
			tx.Rollback()
			return res, err
		}
		if err = tx.Commit(); err != nil {
			return res, err
		}
		changed = true
	}
	for _, n := range existing {
		if !seen[n.Path] {
			if _, err = s.db.Exec(`DELETE FROM note_search WHERE rowid=?`, n.ID); err != nil {
				return res, err
			}
			if err = storage.DeleteNodeByPath(s.db, n.Path); err != nil {
				return res, err
			}
			res.Deleted++
			changed = true
		}
	}
	if changed || time.Since(s.lastBuild) > time.Minute {
		if err = s.rebuild(res); err != nil {
			s.lastBuild = time.Time{}
			return res, err
		}
		s.lastBuild = time.Now()
	} else {
		if err = s.db.QueryRow(`SELECT COUNT(*) FROM edges`).Scan(&res.Edges); err != nil {
			return res, err
		}
	}
	if len(res.Errors) > 0 {
		return res, errors.New(strings.Join(res.Errors, "; "))
	}
	if err = activity.Rebuild(s.db); err != nil {
		return res, err
	}
	if err = s.db.QueryRow(`SELECT COUNT(*) FROM edges`).Scan(&res.Edges); err != nil {
		return res, err
	}
	return res, nil
}
func (s *Service) rebuild(res *SyncResult) error {
	all, err := storage.ListNotes(s.db)
	if err != nil {
		return err
	}
	engine := relationships.NewEngine(s.db, all)
	engine.SetTitleIndex(all)
	for _, n := range all {
		pn, err := s.vault.Read(n.Path)
		if err != nil {
			return fmt.Errorf("read %s: %w", n.Path, err)
		}
		engine.Similarity().LoadContent(n.ID, pn.Body)
		engine.SetProject(n.ID, pn.Project)
		for i := 0; i < len(pn.Links); i++ {
			for j := i + 1; j < len(pn.Links); j++ {
				a, b := engine.IDForTitle(pn.Links[i]), engine.IDForTitle(pn.Links[j])
				if a != 0 && b != 0 {
					engine.AddCoMention(a, b)
				}
			}
		}
	}
	edges, err := engine.Compute(all)
	if err != nil {
		return err
	}
	if err = storage.ReplaceAutoEdges(s.db, edges); err != nil {
		return err
	}
	res.Edges = len(edges)
	// Weak edges are retained, but don't inflate importance until made visible.
	threshold := 25.0
	value, _ := storage.GetSetting(s.db, "min_auto_edge_strength")
	fmt.Sscan(value, &threshold)
	visible := []storage.Edge{}
	for _, e := range edges {
		if e.Source == "explicit" || e.Strength >= threshold {
			visible = append(visible, e)
		}
	}
	for id, score := range relationships.ComputeImportance(all, visible, nil) {
		if err = storage.UpdateImportance(s.db, id, score); err != nil {
			return err
		}
	}
	return nil
}
func (s *Service) RebuildLocked() error {
	if err := s.rebuild(&SyncResult{}); err != nil {
		return err
	}
	return activity.Rebuild(s.db)
}
