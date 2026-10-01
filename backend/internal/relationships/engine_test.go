package relationships

import (
	"math"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/zeusangis/dendrite/internal/storage"
)

func TestEdgeBuilderScoring(t *testing.T) {
	b := &EdgeBuilder{}
	if !b.Empty() {
		t.Fatal("fresh builder should be empty")
	}
	b.Add(WeightExplicitLink, "Explicit link")
	b.Add(WeightSharedTag, "Shared tags: go")
	if b.Empty() {
		t.Fatal("builder should not be empty")
	}
	if got := b.Strength(); got != 55.0 {
		t.Errorf("strength = %v, want 55", got)
	}
	if got := b.Reason(); got != "Explicit link; Shared tags: go" {
		t.Errorf("reason = %q", got)
	}
}

func TestEdgeBuilderCaps(t *testing.T) {
	b := &EdgeBuilder{}
	for i := 0; i < 100; i++ {
		b.Add(WeightExplicitLink, "Explicit link")
	}
	if b.Strength() > MaxScore {
		t.Errorf("strength %v exceeds max %v", b.Strength(), MaxScore)
	}
}

func TestTokenizerDropsStopwords(t *testing.T) {
	tf := tokenize("The quick brown fox jumps over the lazy dog")
	if _, ok := tf["the"]; ok {
		t.Error("stopword 'the' should be dropped")
	}
	if tf["quick"] != 1 {
		t.Errorf("quick freq = %v", tf["quick"])
	}
}

func TestSimilarityIdenticalAndDisjoint(t *testing.T) {
	idx := newSimilarityIndex(nil)
	idx.LoadContent(1, "go interfaces define behavior contracts")
	idx.LoadContent(2, "go interfaces define behavior contracts")
	idx.LoadContent(3, "gardening tips for tomatoes and peppers")

	if sim := idx.Similarity(1, 2); sim < 0.99 {
		t.Errorf("identical docs similarity = %v, want ~1", sim)
	}
	if sim := idx.Similarity(1, 3); sim != 0 {
		t.Errorf("disjoint docs similarity = %v, want 0", sim)
	}
}

func TestImportanceCountsEachPairOnce(t *testing.T) {
	nodes := []storage.Note{{ID: 1}, {ID: 2}, {ID: 3}, {ID: 4}}
	edges := []storage.Edge{{SourceID: 1, TargetID: 2, Strength: 40}, {SourceID: 1, TargetID: 3, Strength: 10}}
	scores := ComputeImportance(nodes, edges, map[int64]int{1: 100})
	if scores[1] != 1 || scores[4] != 0 {
		t.Fatal(scores)
	}
	want := 0.55*0.5 + 0.45*math.Log1p(40)/(math.Log1p(40)+math.Log1p(10))
	if math.Abs(scores[2]-want) > 1e-9 {
		t.Fatalf("score %v want %v", scores[2], want)
	}
}

func TestEngineSourcesScoringAndCoMentions(t *testing.T) {
	db, err := storage.OpenDatabase(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	stamp := time.Now().Add(-30 * 24 * time.Hour)
	a, err := storage.UpsertNode(db, "a.md", "Alpha", "note", stamp, stamp)
	if err != nil {
		t.Fatal(err)
	}
	b, err := storage.UpsertNode(db, "b.md", "Beta", "note", stamp, stamp)
	if err != nil {
		t.Fatal(err)
	}
	if err = storage.ReplaceLinks(db, a, []string{"Beta"}); err != nil {
		t.Fatal(err)
	}
	if err = storage.ReplaceTags(db, a, []string{"go"}); err != nil {
		t.Fatal(err)
	}
	if err = storage.ReplaceTags(db, b, []string{"go"}); err != nil {
		t.Fatal(err)
	}
	list, err := storage.ListNotes(db)
	if err != nil {
		t.Fatal(err)
	}
	engine := NewEngine(db, list)
	engine.SetProject(a, "Lab")
	engine.SetProject(b, "lab")
	engine.Similarity().LoadContent(a, "contracts behavior")
	engine.Similarity().LoadContent(b, "contracts behavior")
	for i := 0; i < 4; i++ {
		engine.AddCoMention(a, b)
	}
	edges, err := engine.Compute(list)
	if err != nil {
		t.Fatal(err)
	}
	if len(edges) != 1 || edges[0].Source != "explicit" || edges[0].Strength != 105 {
		t.Fatalf("scoring %+v", edges)
	}
	for _, reason := range []string{"Same project: lab", "Shared tags: go", "Similar content (100%)", "Mentioned together 4×", "Repeated co-mentions"} {
		if !strings.Contains(edges[0].Reason, reason) {
			t.Fatal("missing reason", reason, edges)
		}
	}
}

func TestDescribe(t *testing.T) {
	e := storage.Edge{Reason: "Explicit link; Shared tags: go, backend", Strength: 55}
	out := Describe(e, "A", "B")
	for _, want := range []string{"A — B", "Explicit link", "Shared tags: go, backend"} {
		if !strings.Contains(out, want) {
			t.Errorf("Describe output missing %q:\n%s", want, out)
		}
	}
}
