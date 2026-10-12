package storage

import (
	"path/filepath"
	"testing"
	"time"
)

// TestMigrateUpgradesInstalledDatabase rebuilds the state an already-installed
// database would be in before the newest migration, so the upgrade path is
// exercised rather than only the from-scratch path.
func TestMigrateUpgradesInstalledDatabase(t *testing.T) {
	db, err := OpenDatabase(filepath.Join(t.TempDir(), "upgrade.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var latest int
	if err = db.QueryRow(`SELECT MAX(version) FROM schema_migrations`).Scan(&latest); err != nil {
		t.Fatal(err)
	}
	if latest < 2 {
		t.Fatal("expected several migrations", latest)
	}
	at := time.Now()
	if _, err = UpsertNode(db, "keep.md", "Keep me", "note", at, at); err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec(`INSERT INTO activity_sessions(app,app_id,window_title,domain,url,started_at,ended_at,total_seconds,active_seconds,samples,run_id) VALUES('Google Chrome','com.google.Chrome','','go.dev','https://go.dev/',?,?,5,5,2,'run')`, at.UTC().Format(time.RFC3339Nano), at.UTC().Format(time.RFC3339Nano)); err != nil {
		t.Fatal(err)
	}
	// Roll the newest migration back to simulate a database that predates it.
	if _, err = db.Exec(`DROP TABLE muted_connections`); err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec(`DELETE FROM schema_migrations WHERE version=?`, latest); err != nil {
		t.Fatal(err)
	}
	if err = Migrate(db); err != nil {
		t.Fatalf("upgrade failed: %v", err)
	}
	// Idempotent: running again must not fail or duplicate the migration.
	if err = Migrate(db); err != nil {
		t.Fatalf("repeat migrate failed: %v", err)
	}
	var applied int
	if err = db.QueryRow(`SELECT COUNT(*) FROM schema_migrations WHERE version=?`, latest).Scan(&applied); err != nil || applied != 1 {
		t.Fatalf("migration not recorded exactly once: %v %d", err, applied)
	}
	// Existing data and the new capability both work after the upgrade.
	note, err := GetNodeByPath(db, "keep.md")
	if err != nil || note == nil || note.Title != "Keep me" {
		t.Fatalf("existing note lost: %v %+v", err, note)
	}
	var sessions int
	if err = db.QueryRow(`SELECT COUNT(*) FROM activity_sessions`).Scan(&sessions); err != nil || sessions != 1 {
		t.Fatalf("existing sessions lost: %v %d", err, sessions)
	}
	if err = MuteConnection(db, "keep.md", "activity:page:abc"); err != nil {
		t.Fatal(err)
	}
	if muted, err := MutedPairs(db); err != nil || !muted[Pair("keep.md", "activity:page:abc")] {
		t.Fatalf("mutes unusable after upgrade: %v %+v", err, muted)
	}
}
