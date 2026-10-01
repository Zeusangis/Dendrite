package storage

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"time"
)

// Note is the graph-facing projection of a Markdown file.
type Note struct {
	ID         int64    `json:"id"`
	Type       string   `json:"type"`
	Title      string   `json:"title"`
	Path       string   `json:"path"`
	CreatedAt  string   `json:"created_at"`
	UpdatedAt  string   `json:"updated_at"`
	Importance float64  `json:"importance"`
	X          *float64 `json:"x"`
	Y          *float64 `json:"y"`
}

// Edge is a directed relationship between two nodes.
type Edge struct {
	ID               int64   `json:"id"`
	SourceID         int64   `json:"source_id"`
	TargetID         int64   `json:"target_id"`
	Strength         float64 `json:"strength"`
	RelationshipType string  `json:"relationship_type"`
	Source           string  `json:"source"` // explicit | automatic | activity | ai
	Reason           string  `json:"reason,omitempty"`
	CreatedAt        string  `json:"created_at"`
}

// Link is a parsed [[wikilink]] occurrence in a note.
type Link struct {
	NoteID      int64  `json:"note_id"`
	TargetTitle string `json:"target_title"`
}

// Tag is a #tag found on a note.
type Tag struct {
	NoteID int64  `json:"note_id"`
	Tag    string `json:"tag"`
}

// Entity is an extracted named concept (code identifier, tech term, ...).
type Entity struct {
	NoteID int64   `json:"note_id"`
	Name   string  `json:"name"`
	Kind   string  `json:"kind"`
	Weight float64 `json:"weight"`
}

func nowISO() string { return time.Now().UTC().Format(time.RFC3339) }

// UpsertNode creates or updates a node identified by its path. The title,
// timestamps and importance are refreshed; persisted x/y coordinates are kept.
func UpsertNode(db *sql.DB, path, title, typ string, createdAt, updatedAt time.Time) (int64, error) {
	_, err := db.Exec(`
		INSERT INTO nodes (type, title, path, created_at, updated_at, importance)
		VALUES (?, ?, ?, ?, ?,
			COALESCE((SELECT importance FROM nodes WHERE path = ?), 0.5))
		ON CONFLICT(path) DO UPDATE SET
			type = excluded.type,
			title = excluded.title,
			updated_at = excluded.updated_at`,
		typ, title, path, createdAt.UTC().Format(time.RFC3339), updatedAt.UTC().Format(time.RFC3339), path)
	if err != nil {
		return 0, fmt.Errorf("upsert node %s: %w", path, err)
	}
	var id int64
	err = db.QueryRow(`SELECT id FROM nodes WHERE path = ?`, path).Scan(&id)
	return id, err
}

// GetNodeByPath returns the node row for a vault-relative path.
func GetNodeByPath(db *sql.DB, path string) (*Note, error) {
	n := &Note{}
	err := db.QueryRow(`SELECT id, type, title, path, created_at, updated_at, importance, x, y FROM nodes WHERE path = ?`, path).
		Scan(&n.ID, &n.Type, &n.Title, &n.Path, &n.CreatedAt, &n.UpdatedAt, &n.Importance, &n.X, &n.Y)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return n, nil
}

// DeleteNodeByPath removes a node (edges cascade via FK ON DELETE CASCADE).
func DeleteNodeByPath(db *sql.DB, path string) error {
	_, err := db.Exec(`DELETE FROM nodes WHERE path = ?`, path)
	return err
}

// ListNotes returns all note-type nodes ordered by title.
func ListNotes(db *sql.DB) ([]Note, error) { return listNodes(db, true) }
func ListNodes(db *sql.DB) ([]Note, error) { return listNodes(db, false) }
func listNodes(db *sql.DB, notesOnly bool) ([]Note, error) {
	query := `SELECT id, type, title, path, created_at, updated_at, importance, x, y FROM nodes`
	if notesOnly {
		query += ` WHERE type = 'note'`
	}
	query += ` ORDER BY title`
	rows, err := db.Query(query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	notes := []Note{}
	for rows.Next() {
		var n Note
		if err := rows.Scan(&n.ID, &n.Type, &n.Title, &n.Path, &n.CreatedAt, &n.UpdatedAt, &n.Importance, &n.X, &n.Y); err != nil {
			return nil, err
		}
		notes = append(notes, n)
	}
	return notes, rows.Err()
}

// ReplaceLinks swaps out the parsed wikilinks for one note.
func ReplaceLinks(db *sql.DB, noteID int64, links []string) error {
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	if _, err := tx.Exec(`DELETE FROM node_links WHERE note_id = ?`, noteID); err != nil {
		return err
	}
	for _, t := range links {
		if _, err := tx.Exec(`INSERT OR IGNORE INTO node_links (note_id, target_title) VALUES (?, ?)`, noteID, t); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// ReplaceTags swaps out the parsed tags for one note.
func ReplaceTags(db *sql.DB, noteID int64, tags []string) error {
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	if _, err := tx.Exec(`DELETE FROM note_tags WHERE note_id = ?`, noteID); err != nil {
		return err
	}
	for _, t := range tags {
		if _, err := tx.Exec(`INSERT OR IGNORE INTO note_tags (note_id, tag) VALUES (?, ?)`, noteID, t); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// ReplaceEntities swaps out the extracted entities for one note.
func ReplaceEntities(db *sql.DB, noteID int64, ents []Entity) error {
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	if _, err := tx.Exec(`DELETE FROM entities WHERE note_id = ?`, noteID); err != nil {
		return err
	}
	for _, e := range ents {
		if _, err := tx.Exec(`INSERT OR IGNORE INTO entities (note_id, name, kind, weight) VALUES (?, ?, ?, ?)`, noteID, e.Name, e.Kind, e.Weight); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// ReplaceAutoEdges deletes all automatic edges and inserts the given set,
// as one transaction so the graph is never partially updated.
func ReplaceAutoEdges(db *sql.DB, edges []Edge) error {
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	if _, err := tx.Exec(`CREATE TEMP TABLE IF NOT EXISTS rebuilt_pairs (source_id INTEGER, target_id INTEGER, PRIMARY KEY(source_id, target_id)); DELETE FROM rebuilt_pairs;`); err != nil {
		return err
	}
	for _, e := range edges {
		if _, err := tx.Exec(`INSERT INTO rebuilt_pairs VALUES (?, ?)`, e.SourceID, e.TargetID); err != nil {
			return err
		}
		if _, err := tx.Exec(`
			INSERT INTO edges (source_id, target_id, strength, relationship_type, source, reason, created_at)
			VALUES (?, ?, ?, ?, ?, ?, ?)
			ON CONFLICT(source_id, target_id) DO UPDATE SET
				strength = excluded.strength,
				relationship_type = excluded.relationship_type,
				reason = excluded.reason,
				source = excluded.source`,
			e.SourceID, e.TargetID, e.Strength, e.RelationshipType, e.Source, e.Reason, nowISO()); err != nil {
			return err
		}
	}
	if _, err := tx.Exec(`DELETE FROM edges WHERE source IN ('automatic','explicit') AND NOT EXISTS (SELECT 1 FROM rebuilt_pairs p WHERE p.source_id = edges.source_id AND p.target_id = edges.target_id)`); err != nil {
		return err
	}
	return tx.Commit()
}

// GraphSnapshot is the full node+edge payload served to the frontend.
type GraphSnapshot struct {
	Nodes []Note `json:"nodes"`
	Edges []Edge `json:"edges"`
}

// GetGraph loads every node and edge.
func GetGraph(db *sql.DB) (*GraphSnapshot, error) {
	notes, err := ListNodes(db)
	if err != nil {
		return nil, err
	}
	if notes == nil {
		notes = []Note{}
	}

	rows, err := db.Query(`SELECT id, source_id, target_id, strength, relationship_type, source, reason, created_at FROM edges ORDER BY strength DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	edges := []Edge{}
	for rows.Next() {
		var e Edge
		if err := rows.Scan(&e.ID, &e.SourceID, &e.TargetID, &e.Strength, &e.RelationshipType, &e.Source, &e.Reason, &e.CreatedAt); err != nil {
			return nil, err
		}
		edges = append(edges, e)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return &GraphSnapshot{Nodes: notes, Edges: edges}, nil
}

// SetSetting stores a metadata key/value pair.
func SetSetting(db *sql.DB, key, value string) error {
	_, err := db.Exec(`INSERT INTO metadata (key, value) VALUES (?, ?)
		ON CONFLICT(key) DO UPDATE SET value = excluded.value`, key, value)
	return err
}

// GetSetting reads a metadata value; empty string when missing.
func GetSetting(db *sql.DB, key string) (string, error) {
	var v string
	err := db.QueryRow(`SELECT value FROM metadata WHERE key = ?`, key).Scan(&v)
	if err == sql.ErrNoRows {
		return "", nil
	}
	return v, err
}

// UpdateNodePosition persists a manually dragged node position.
func UpdateNodePosition(db *sql.DB, id int64, x, y *float64) error {
	res, err := db.Exec(`UPDATE nodes SET x = ?, y = ? WHERE id = ?`, x, y, id)
	if err != nil {
		return err
	}
	count, err := res.RowsAffected()
	if err == nil && count == 0 {
		return sql.ErrNoRows
	}
	return err
}

// UpdateImportance writes the normalized importance score.
func UpdateImportance(db *sql.DB, id int64, importance float64) error {
	_, err := db.Exec(`UPDATE nodes SET importance = ? WHERE id = ?`, importance, id)
	return err
}

// GetSettingJSON decodes a JSON-encoded setting; ok=false when missing.
func GetSettingJSON(db *sql.DB, key string, out interface{}) (bool, error) {
	v, err := GetSetting(db, key)
	if err != nil || v == "" {
		return false, err
	}
	return true, json.Unmarshal([]byte(v), out)
}
