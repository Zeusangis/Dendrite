package relationships

import (
	"database/sql"
	"fmt"
	"math"
	"sort"
	"strings"
	"time"

	"github.com/zeusangis/dendrite/internal/storage"
)

// Signal weights. Tuned by hand; see README "Relationship Engine" section.
const (
	WeightExplicitLink   = 40.0
	WeightSharedEntity   = 25.0
	WeightSharedProject  = 20.0
	WeightSharedTag      = 15.0
	WeightTextSimilarity = 15.0
	WeightCoMention      = 10.0
	WeightTemporal       = 10.0
	WeightFrequency      = 10.0
	WeightRecency        = 5.0

	// MaxScore is the theoretical sum of all signals (used for normalization).
	MaxScore = WeightExplicitLink + WeightSharedEntity + WeightSharedProject +
		WeightSharedTag + WeightTextSimilarity + WeightCoMention +
		WeightTemporal + WeightFrequency + WeightRecency
)

// EdgeBuilder accumulates raw signal points between two notes and turns them
// into a final strength plus an explanation.
type EdgeBuilder struct {
	points float64
	whys   []string
}

// Add awards points and records why.
func (b *EdgeBuilder) Add(weight float64, why string) {
	b.points += weight
	b.whys = append(b.whys, why)
}

// Empty reports whether any signal fired.
func (b *EdgeBuilder) Empty() bool { return len(b.whys) == 0 }

// Strength returns points capped at MaxScore (raw scale, 0..150).
func (b *EdgeBuilder) Strength() float64 { return math.Min(b.points, MaxScore) }

// Reason renders the explanation, e.g. "Explicit link; Shared tag: golang".
func (b *EdgeBuilder) Reason() string {
	return strings.Join(b.whys, "; ")
}

// noteData is the in-memory analysis unit for one note.
type noteData struct {
	id       int64
	title    string
	titleLC  string
	links    []string
	tags     []string
	entities []storage.Entity
	words    map[string]int
	created  string
	updated  string
}

// Engine computes automatic relationships between notes.
type Engine struct {
	db         *sql.DB
	similarity *similarityIndex
	coMentions map[[2]int64]int
	projects   map[int64]string // note_id -> front-matter project
	degrees    map[int64]int    // link degree per note, filled by Compute
	byTitle    map[string]int64 // lowercased title -> id
}

// NewEngine builds an engine over the given notes.
func NewEngine(db *sql.DB, notes []storage.Note) *Engine {
	e := &Engine{
		db:         db,
		coMentions: map[[2]int64]int{},
		projects:   map[int64]string{},
	}
	e.similarity = newSimilarityIndex(notes)
	return e
}

// AddCoMention records that note a and note b were mentioned together
// (e.g. in the same [[wikilink]] context) — used by the frequency signal.
func (e *Engine) AddCoMention(a, b int64) {
	if a == b {
		return
	}
	key := normPair(a, b)
	e.coMentions[key]++
}

// SetProject records the front-matter project for a note.
func (e *Engine) SetProject(noteID int64, project string) {
	if project != "" {
		e.projects[noteID] = strings.ToLower(project)
	}
}

// Compute scores unordered pairs and retains weak edges for user-controlled visibility.
func (e *Engine) Compute(notes []storage.Note) ([]storage.Edge, error) {
	data := make([]*noteData, 0, len(notes))
	byTitleIndex := map[string]*noteData{}

	for i := range notes {
		n := &notes[i]
		tags, err := e.loadTags(n.ID)
		if err != nil {
			return nil, err
		}
		ents, err := e.loadEntities(n.ID)
		if err != nil {
			return nil, err
		}
		links, err := e.loadLinks(n.ID)
		if err != nil {
			return nil, err
		}
		d := &noteData{
			id:       n.ID,
			title:    n.Title,
			titleLC:  strings.ToLower(n.Title),
			links:    links,
			tags:     tags,
			entities: ents,
			words:    e.similarity.docWords(n.ID),
			created:  n.CreatedAt,
			updated:  n.UpdatedAt,
		}
		data = append(data, d)
		byTitleIndex[d.titleLC] = d
	}

	linkSet := map[[2]int64]bool{}
	for _, d := range data {
		for _, target := range d.links {
			if td, ok := byTitleIndex[strings.ToLower(target)]; ok && td.id != d.id {
				linkSet[normPair(d.id, td.id)] = true
			}
		}
	}

	tagIndex := map[string][]*noteData{}
	for _, d := range data {
		for _, t := range d.tags {
			tagIndex[t] = append(tagIndex[t], d)
		}
	}
	entIndex := map[string][]*noteData{}
	for _, d := range data {
		for _, ent := range d.entities {
			entIndex[strings.ToLower(ent.Name)] = append(entIndex[strings.ToLower(ent.Name)], d)
		}
	}

	// Importance feed: raw out/in link counts per note.
	deg := map[int64]int{}

	var edges []storage.Edge
	for i := 0; i < len(data); i++ {
		for j := i + 1; j < len(data); j++ {
			a, b := data[i], data[j]
			bldr := &EdgeBuilder{}
			pair := normPair(a.id, b.id)

			// 1. Explicit [[link]] in either direction.
			if linkSet[pair] {
				bldr.Add(WeightExplicitLink, "Explicit link")

			}

			// 2. Shared entities (weighted, up to weight cap).
			if pts, names := sharedEntities(a, b, entIndex); pts > 0 {
				bldr.Add(pts, names)

			}

			// 3. Shared front-matter project.
			if pa, pb := e.projects[a.id], e.projects[b.id]; pa != "" && pa == pb {
				bldr.Add(WeightSharedProject, "Same project: "+pa)

			}

			// 4. Shared tags (scaled by count).
			if pts, names := sharedTags(a, b, tagIndex); pts > 0 {
				bldr.Add(pts, names)

			}

			// 5. Text similarity (cosine over content tokens).
			if sim := e.similarity.Similarity(a.id, b.id); sim > 0.08 {
				bldr.Add(sim*WeightTextSimilarity, fmt.Sprintf("Similar content (%d%%)", int(math.Round(sim*100))))
			}

			// 6. Co-mention frequency (both referenced in another note).
			if c := e.coMentions[pair]; c > 1 {
				bldr.Add(math.Min(float64(c-1)*2.5, WeightCoMention),
					fmt.Sprintf("Mentioned together %d×", c))
			}

			// 7. Temporal proximity of creation/edit times.
			if pts, why := temporalPoints(a, b); pts > 0 {
				bldr.Add(pts, why)
			}

			// 8. Recency bonus (both touched within the last week).
			if pts, why := recencyPoints(a, b); pts > 0 {
				bldr.Add(pts, why)
			}

			if bldr.Empty() {
				continue
			}
			strength := bldr.Strength()
			source, typ := "automatic", "related"
			if linkSet[pair] {
				source, typ = "explicit", "wikilink"
			}
			if c := e.coMentions[pair]; c > 3 {
				bldr.Add(WeightFrequency*0.5, "Repeated co-mentions")
				strength = bldr.Strength()
			}

			edge := storage.Edge{
				SourceID:         pair[0],
				TargetID:         pair[1],
				Strength:         round2(strength),
				RelationshipType: typ,
				Source:           source,
				Reason:           bldr.Reason(),
			}

			if linkSet[pair] {
				deg[a.id]++
				deg[b.id]++
			}
			edges = append(edges, edge)
		}
	}

	// Store degrees so importance can consume them later.
	e.degrees = deg

	sort.Slice(edges, func(i, j int) bool { return edges[i].Strength > edges[j].Strength })
	return edges, nil
}

// MinAutoEdgeStrength is the raw-score threshold below which edges stay hidden.
const MinAutoEdgeStrength = 25.0

// Degrees exposes link degrees for the importance calculation.
func (e *Engine) Degrees() map[int64]int { return e.degrees }

// Similarity exposes the text-similarity index for content loading.
func (e *Engine) Similarity() *similarityIndex { return e.similarity }

// IDForTitle resolves a note title to its id (0 when unknown).
func (e *Engine) IDForTitle(title string) int64 {
	if e.byTitle == nil {
		return 0
	}
	return e.byTitle[strings.ToLower(title)]
}

// SetTitleIndex seeds the title→id map used by IDForTitle.
func (e *Engine) SetTitleIndex(notes []storage.Note) {
	m := make(map[string]int64, len(notes))
	for i := range notes {
		m[strings.ToLower(notes[i].Title)] = notes[i].ID
	}
	e.byTitle = m
}

func sharedEntities(a, b *noteData, index map[string][]*noteData) (float64, string) {
	const entCap = WeightSharedEntity // max points from this signal
	var matched []string
	var pts float64
	bLower := strings.ToLower(b.title)
	for _, ea := range a.entities {
		lc := strings.ToLower(ea.Name)
		// Both notes mention the same entity, and it is not just the other's title.
		for _, eb := range b.entities {
			if strings.ToLower(eb.Name) == lc && lc != bLower && lc != a.titleLC {
				pts += ea.Weight * 10.0
				matched = append(matched, ea.Name)
				break
			}
		}
	}
	if pts > entCap {
		pts = entCap
	}
	if len(matched) == 0 {
		return 0, ""
	}
	sort.Strings(matched)
	if len(matched) > 3 {
		matched = append(matched[:3], fmt.Sprintf("+%d more", len(matched)-3))
	}
	return pts, "Shared concept: " + strings.Join(matched, ", ")
}

func sharedTags(a, b *noteData, index map[string][]*noteData) (float64, string) {
	var shared []string
	for _, ta := range a.tags {
		for _, tb := range b.tags {
			if ta == tb {
				shared = append(shared, ta)
				break
			}
		}
	}
	if len(shared) == 0 {
		return 0, ""
	}
	pts := math.Min(float64(len(shared))/2.0*WeightSharedTag, WeightSharedTag*1.5)
	if len(shared) > 3 {
		shared = append(shared[:3], fmt.Sprintf("+%d more", len(shared)-3))
	}
	return pts, "Shared tags: " + strings.Join(shared, ", ")
}

func temporalPoints(a, b *noteData) (float64, string) {
	ta, ok1 := parseTime(a.updated)
	tb, ok2 := parseTime(b.updated)
	if !ok1 || !ok2 {
		return 0, ""
	}
	diff := ta.Sub(tb)
	if diff < 0 {
		diff = -diff
	}
	const day = 24 * time.Hour
	switch {
	case diff < 2*day:
		return WeightTemporal, "Edited within 2 days of each other"
	case diff < 7*day:
		return WeightTemporal * 0.6, "Edited within a week of each other"
	default:
		return 0, ""
	}
}

func recencyPoints(a, b *noteData) (float64, string) {
	for _, d := range []*noteData{a, b} {
		t, ok := parseTime(d.updated)
		if !ok || time.Since(t) < 0 || time.Since(t) >= 7*24*time.Hour {
			return 0, ""
		}
	}
	return WeightRecency, "Both recently active"
}

func parseTime(s string) (time.Time, bool) {
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		return time.Time{}, false
	}
	return t, true
}

func normPair(a, b int64) [2]int64 {
	if a < b {
		return [2]int64{a, b}
	}
	return [2]int64{b, a}
}

func byTitle(data []*noteData, title string) (*noteData, bool) {
	for _, d := range data {
		if strings.EqualFold(d.title, title) {
			return d, true
		}
	}
	return nil, false
}

func round2(f float64) float64 { return math.Round(f*100) / 100 }

func (e *Engine) loadTags(id int64) ([]string, error) {
	rows, err := e.db.Query(`SELECT tag FROM note_tags WHERE note_id = ?`, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var t string
		if err := rows.Scan(&t); err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

func (e *Engine) loadEntities(id int64) ([]storage.Entity, error) {
	rows, err := e.db.Query(`SELECT name, kind, weight FROM entities WHERE note_id = ?`, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []storage.Entity
	for rows.Next() {
		var en storage.Entity
		if err := rows.Scan(&en.Name, &en.Kind, &en.Weight); err != nil {
			return nil, err
		}
		out = append(out, en)
	}
	return out, rows.Err()
}

func (e *Engine) loadLinks(id int64) ([]string, error) {
	rows, err := e.db.Query(`SELECT target_title FROM node_links WHERE note_id = ?`, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var t string
		if err := rows.Scan(&t); err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}
