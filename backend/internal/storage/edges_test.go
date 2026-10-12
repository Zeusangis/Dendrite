package storage

import (
	"path/filepath"
	"testing"
	"time"
)

func TestEdgeOwnershipRestoresLosingEvidence(t *testing.T) {
	db, err := OpenDatabase(filepath.Join(t.TempDir(), "edges.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	a, err := UpsertNode(db, "a.md", "A", "note", time.Now(), time.Now())
	if err != nil {
		t.Fatal(err)
	}
	b, err := UpsertNode(db, "b.md", "B", "note", time.Now(), time.Now())
	if err != nil {
		t.Fatal(err)
	}
	makeEdge := func(source string, score float64) Edge {
		return Edge{SourceID: b, TargetID: a, Source: source, Strength: score, Reason: source, RelationshipType: source}
	}
	check := func(source string, id int64) int64 {
		t.Helper()
		g, e := GetGraph(db)
		if e != nil || len(g.Edges) != 1 {
			t.Fatal(e, g)
		}
		v := g.Edges[0]
		if v.Source != source || v.SourceID != a || v.TargetID != b || (id != 0 && id != v.ID) {
			t.Fatal(v)
		}
		return v.ID
	}
	if err = ReplaceAutoEdges(db, []Edge{makeEdge("automatic", 120)}); err != nil {
		t.Fatal(err)
	}
	id := check("automatic", 0)
	if err = ReplaceEdgeEvidence(db, []string{"activity"}, []Edge{makeEdge("activity", 40)}); err != nil {
		t.Fatal(err)
	}
	check("activity", id)
	if err = ReplaceAutoEdges(db, []Edge{makeEdge("explicit", 150)}); err != nil {
		t.Fatal(err)
	}
	check("explicit", id)
	if err = ReplaceEdgeEvidence(db, []string{"activity"}, []Edge{makeEdge("activity", 90)}); err != nil {
		t.Fatal(err)
	}
	check("explicit", id)
	if err = ReplaceAutoEdges(db, []Edge{makeEdge("automatic", 80)}); err != nil {
		t.Fatal(err)
	}
	check("activity", id)
	if err = ReplaceEdgeEvidence(db, []string{"activity"}, nil); err != nil {
		t.Fatal(err)
	}
	check("automatic", id)
	if err = ReplaceAutoEdges(db, nil); err != nil {
		t.Fatal(err)
	}
	g, err := GetGraph(db)
	if err != nil || len(g.Edges) != 0 {
		t.Fatal(err, g)
	}
}
