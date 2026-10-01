package relationships

import (
	"math"
	"strings"

	"github.com/zeusangis/dendrite/internal/storage"
)

// similarityIndex builds a TF vector per note and answers cosine queries.
type similarityIndex struct {
	byID   map[int64]map[string]float64
	length map[int64]float64
}

var stopwords = map[string]bool{
	"the": true, "and": true, "for": true, "are": true, "but": true, "not": true,
	"you": true, "all": true, "can": true, "her": true, "was": true, "one": true,
	"our": true, "out": true, "day": true, "get": true, "has": true, "him": true,
	"his": true, "how": true, "its": true, "new": true, "now": true, "old": true,
	"see": true, "two": true, "way": true, "who": true, "did": true, "with": true,
	"this": true, "that": true, "from": true, "they": true, "have": true,
	"will": true, "your": true, "what": true, "when": true, "into": true,
	"while": true, "also": true, "a": true, "an": true, "of": true,
	"in": true, "on": true, "to": true, "is": true, "it": true, "as": true,
	"be": true, "we": true, "or": true, "by": true, "at": true, "am": true,
}

func newSimilarityIndex(notes []storage.Note) *similarityIndex {
	idx := &similarityIndex{
		byID:   make(map[int64]map[string]float64, len(notes)),
		length: make(map[int64]float64, len(notes)),
	}
	for i := range notes {
		// Content words are loaded lazily per note via docWords below; here we
		// only seed the map so Lookups never miss structurally.
		idx.byID[notes[i].ID] = map[string]float64{}
	}
	return idx
}

// LoadContent feeds a note's raw text into the index.
func (s *similarityIndex) LoadContent(noteID int64, text string) {
	tf := tokenize(text)
	s.byID[noteID] = tf
	var sum float64
	for _, v := range tf {
		sum += v * v
	}
	s.length[noteID] = math.Sqrt(sum)
}

// docWords returns the TF vector (used by the engine for debugging/inspection).
func (s *similarityIndex) docWords(noteID int64) map[string]int {
	out := map[string]int{}
	for w := range s.byID[noteID] {
		out[w]++
	}
	return out
}

// Similarity returns cosine similarity between two notes (0..1).
func (s *similarityIndex) Similarity(a, b int64) float64 {
	va, vb := s.byID[a], s.byID[b]
	if len(va) == 0 || len(vb) == 0 {
		return 0
	}
	la, lb := s.length[a], s.length[b]
	if la == 0 || lb == 0 {
		return 0
	}
	var dot float64
	// Iterate over the smaller vector.
	if len(va) > len(vb) {
		va, vb = vb, va
	}
	for w, fa := range va {
		if fb, ok := vb[w]; ok {
			dot += fa * fb
		}
	}
	return dot / (la * lb)
}

// tokenize lowercases, splits on non-letters and returns term frequencies.
func tokenize(text string) map[string]float64 {
	tf := map[string]float64{}
	var b strings.Builder
	flush := func() {
		w := b.String()
		b.Reset()
		if len(w) < 3 || stopwords[w] {
			return
		}
		tf[w]++
	}
	for _, r := range strings.ToLower(text) {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '_' {
			b.WriteRune(r)
		} else {
			flush()
		}
	}
	flush()
	return tf
}
