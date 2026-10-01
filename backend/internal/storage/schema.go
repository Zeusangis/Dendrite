package storage

import (
	"database/sql"
	"fmt"
)

// Schema holds all DDL migrations in order. Each entry runs inside a
// transaction on startup; the applied set is tracked in schema_migrations.
var Schema = []string{
	// 1: core graph tables
	`CREATE TABLE IF NOT EXISTS nodes (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		type TEXT NOT NULL DEFAULT 'note',
		title TEXT NOT NULL,
		path TEXT NOT NULL UNIQUE,
		created_at TEXT NOT NULL,
		updated_at TEXT NOT NULL,
		importance REAL NOT NULL DEFAULT 0.5,
		x REAL,
		y REAL
	);
	CREATE INDEX IF NOT EXISTS idx_nodes_title ON nodes(title);
	CREATE INDEX IF NOT EXISTS idx_nodes_type ON nodes(type);`,

	`CREATE TABLE IF NOT EXISTS edges (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		source_id INTEGER NOT NULL REFERENCES nodes(id) ON DELETE CASCADE,
		target_id INTEGER NOT NULL REFERENCES nodes(id) ON DELETE CASCADE,
		strength REAL NOT NULL DEFAULT 0,
		relationship_type TEXT NOT NULL DEFAULT 'wikilink',
		source TEXT NOT NULL DEFAULT 'automatic',
		reason TEXT NOT NULL DEFAULT '',
		created_at TEXT NOT NULL,
		UNIQUE(source_id, target_id)
	);
	CREATE INDEX IF NOT EXISTS idx_edges_source ON edges(source_id);
	CREATE INDEX IF NOT EXISTS idx_edges_target ON edges(target_id);
	CREATE INDEX IF NOT EXISTS idx_edges_strength ON edges(strength);`,

	`CREATE TABLE IF NOT EXISTS node_links (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		note_id INTEGER NOT NULL REFERENCES nodes(id) ON DELETE CASCADE,
		target_title TEXT NOT NULL,
		UNIQUE(note_id, target_title)
	);
	CREATE INDEX IF NOT EXISTS idx_node_links_target ON node_links(target_title);`,

	`CREATE TABLE IF NOT EXISTS note_tags (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		note_id INTEGER NOT NULL REFERENCES nodes(id) ON DELETE CASCADE,
		tag TEXT NOT NULL,
		UNIQUE(note_id, tag)
	);
	CREATE INDEX IF NOT EXISTS idx_note_tags_tag ON note_tags(tag);`,

	`CREATE TABLE IF NOT EXISTS entities (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		note_id INTEGER NOT NULL REFERENCES nodes(id) ON DELETE CASCADE,
		name TEXT NOT NULL,
		kind TEXT NOT NULL,
		weight REAL NOT NULL DEFAULT 1.0,
		UNIQUE(note_id, name)
	);
	CREATE INDEX IF NOT EXISTS idx_entities_name ON entities(name);`,

	`CREATE TABLE IF NOT EXISTS metadata (
		key TEXT PRIMARY KEY,
		value TEXT NOT NULL
	);`,

	// 2: seed default settings
	`INSERT OR IGNORE INTO metadata (key, value) VALUES
		('min_auto_edge_strength', '25.0'),
		('graph_scale', '1.0'),
		('graph_tx', '0'),
		('graph_ty', '0');`,
	`CREATE VIRTUAL TABLE note_search USING fts5(title, content, tags, entities, project, path UNINDEXED, tokenize = 'unicode61');
 CREATE TABLE note_revisions (path TEXT PRIMARY KEY REFERENCES nodes(path) ON DELETE CASCADE, revision TEXT NOT NULL);
 CREATE TABLE history (id INTEGER PRIMARY KEY AUTOINCREMENT, label TEXT NOT NULL, before_json TEXT NOT NULL, after_json TEXT NOT NULL, applied INTEGER NOT NULL DEFAULT 1, created_at TEXT NOT NULL DEFAULT (datetime('now')));`,
	`CREATE TABLE activity_sessions (
 id INTEGER PRIMARY KEY AUTOINCREMENT,
 app TEXT NOT NULL, app_id TEXT NOT NULL, window_title TEXT NOT NULL DEFAULT '', domain TEXT NOT NULL DEFAULT '', url TEXT NOT NULL DEFAULT '',
 started_at TEXT NOT NULL, ended_at TEXT NOT NULL, total_seconds REAL NOT NULL DEFAULT 0, active_seconds REAL NOT NULL DEFAULT 0,
 samples INTEGER NOT NULL DEFAULT 0, run_id TEXT NOT NULL
 );
 CREATE INDEX activity_sessions_time ON activity_sessions(started_at);
 CREATE INDEX activity_sessions_app ON activity_sessions(app_id);
 CREATE TABLE activity_settings (id INTEGER PRIMARY KEY CHECK(id=1), config_json TEXT NOT NULL);
 CREATE TABLE activity_node_stats (node_id INTEGER PRIMARY KEY REFERENCES nodes(id) ON DELETE CASCADE, total_seconds REAL NOT NULL, active_seconds REAL NOT NULL, sessions INTEGER NOT NULL, last_seen TEXT NOT NULL);`,
}

// Migrate applies pending schema migrations. Safe to call on every startup.
func Migrate(db *sql.DB) error {
	if _, err := db.Exec(`CREATE TABLE IF NOT EXISTS schema_migrations (
		version INTEGER PRIMARY KEY,
		name TEXT NOT NULL,
		applied_at TEXT NOT NULL DEFAULT (datetime('now'))
	)`); err != nil {
		return fmt.Errorf("create schema_migrations: %w", err)
	}

	var current int
	if err := db.QueryRow(`SELECT COALESCE(MAX(version), 0) FROM schema_migrations`).Scan(&current); err != nil {
		return fmt.Errorf("read schema version: %w", err)
	}

	for i, stmt := range Schema {
		version := i + 1
		if version <= current {
			continue
		}
		tx, err := db.Begin()
		if err != nil {
			return err
		}
		if _, err := tx.Exec(stmt); err != nil {
			tx.Rollback()
			return fmt.Errorf("migration %d: %w", version, err)
		}
		if _, err := tx.Exec(`INSERT INTO schema_migrations (version, name) VALUES (?, ?)`, version, fmt.Sprintf("migration_%d", version)); err != nil {
			tx.Rollback()
			return fmt.Errorf("migration %d record: %w", version, err)
		}
		if err := tx.Commit(); err != nil {
			return err
		}
	}
	return nil
}
