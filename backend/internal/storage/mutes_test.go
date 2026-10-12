package storage

import (
	"path/filepath"
	"testing"
	"time"
)

func TestMutedConnectionLifecycle(t *testing.T) {
	db, err := OpenDatabase(filepath.Join(t.TempDir(), "mutes.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	at := time.Now()
	note, err := UpsertNode(db, "go.md", "Go resources", "note", at, at)
	if err != nil {
		t.Fatal(err)
	}
	page, err := UpsertNode(db, "activity:page:abc", "Go tutorial", "page", at, at)
	if err != nil {
		t.Fatal(err)
	}
	// Order must not matter, and repeating a mute must stay harmless.
	for i := 0; i < 2; i++ {
		if err = MuteConnection(db, "activity:page:abc", "go.md"); err != nil {
			t.Fatal(err)
		}
	}
	if err = MuteConnection(db, "activity:page:abc", "go.md"); err != nil {
		t.Fatal(err)
	}
	muted, err := ListMutedConnections(db)
	if err != nil || len(muted) != 1 {
		t.Fatal(err, muted)
	}
	if muted[0].APath != "activity:page:abc" || muted[0].BPath != "go.md" {
		t.Fatalf("pair is not canonically ordered: %+v", muted[0])
	}
	if muted[0].A == nil || muted[0].A.ID != page || muted[0].B == nil || muted[0].B.ID != note {
		t.Fatalf("nodes not resolved: %+v", muted[0])
	}
	pairs, err := MutedPairs(db)
	if err != nil || !pairs[Pair("go.md", "activity:page:abc")] || !pairs[Pair("activity:page:abc", "go.md")] {
		t.Fatal(err, pairs)
	}
	if err = MuteConnection(db, "go.md", "go.md"); err == nil {
		t.Fatal("a self connection was muted")
	}
	if err = UnmuteConnection(db, "go.md", "activity:page:abc"); err != nil {
		t.Fatal(err)
	}
	if muted, err = ListMutedConnections(db); err != nil || len(muted) != 0 {
		t.Fatal(err, muted)
	}
	// A mute recorded against a node that later disappears still lists its path.
	if err = MuteConnection(db, "activity:page:abc", "go.md"); err != nil {
		t.Fatal(err)
	}
	if err = DeleteNodeByPath(db, "activity:page:abc"); err != nil {
		t.Fatal(err)
	}
	muted, err = ListMutedConnections(db)
	if err != nil || len(muted) != 1 || muted[0].A != nil || muted[0].APath != "activity:page:abc" {
		t.Fatalf("expired mute lost its path: %v %+v", err, muted)
	}
}
