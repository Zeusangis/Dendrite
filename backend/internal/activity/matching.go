package activity

import (
	"database/sql"
	"sort"
	"strings"
	"unicode"
)

type Concept struct{ Text, Kind string }
type MatchEvidence struct {
	Concept, Kind string
	Weight        float64
}

func normalizeConcept(s string) string {
	return strings.Join(strings.Fields(strings.Map(func(r rune) rune {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			return unicode.ToLower(r)
		}
		return ' '
	}, s)), " ")
}

var genericConcepts = map[string]bool{"app": true, "home": true, "page": true, "test": true, "new": true, "new tab": true, "login": true, "log in": true, "google": true, "browser": true, "docs": true, "documentation": true, "github": true, "website": true, "welcome": true, "index": true, "untitled": true}

func usableConcept(s string) bool {
	n := normalizeConcept(s)
	if len([]rune(n)) < 3 || genericConcepts[n] {
		return false
	}
	meaningful := false
	for _, token := range strings.Fields(n) {
		if !genericConcepts[token] && strings.IndexFunc(token, unicode.IsLetter) >= 0 {
			meaningful = true
		}
	}
	return meaningful
}
func phraseMatch(text, phrase string) bool {
	return strings.Contains(" "+normalizeConcept(text)+" ", " "+normalizeConcept(phrase)+" ")
}
func noteConcepts(db *sql.DB, id int64, title string) ([]Concept, error) {
	concepts := []Concept{{title, "note title"}}
	rows, err := db.Query(`SELECT tag,'tag' FROM note_tags WHERE note_id=? UNION SELECT name,'entity' FROM entities WHERE note_id=? ORDER BY 2,1`, id, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var c Concept
		if err = rows.Scan(&c.Text, &c.Kind); err != nil {
			return nil, err
		}
		concepts = append(concepts, c)
	}
	return concepts, rows.Err()
}
func strongestMatch(concepts []Concept, node *derived) *MatchEvidence {
	matches := []MatchEvidence{}
	for _, c := range concepts {
		if !usableConcept(c.Text) {
			continue
		}
		text := normalizeConcept(c.Text)
		kind, weight := "", 0.0
		// Exact hosts are intentional evidence, not generic provider token overlap.
		raw := strings.TrimSuffix(strings.ToLower(strings.TrimSpace(c.Text)), "/")
		if strings.Contains(raw, "://") {
			_, raw = SanitizeURL(raw)
		}
		if node.Domain != "" && raw == node.Domain {
			kind, weight = "exact domain", 80
		}
		if kind == "" && phraseMatch(node.Title, c.Text) {
			switch c.Kind {
			case "note title":
				kind, weight = "note title phrase", 60
			case "entity":
				kind, weight = "entity phrase", 50
			case "tag":
				kind, weight = "tag phrase", 40
			}
			if node.Type == NodePage && normalizeConcept(node.Title) == text {
				kind, weight = "exact page title", 70
			}
		}
		// Go is too short/ambiguous alone. Explicit Go-language concepts may match
		// Go titles and the language's own domain, never arbitrary 'go' substrings.
		if (text == "golang" || text == "go lang" || text == "go language" || text == "go programming") && (node.Domain == "go.dev" || node.Domain == "golang.org" || phraseMatch(node.Title, "Go programming") || phraseMatch(node.Title, "Go tutorial") || phraseMatch(node.Title, "Golang")) {
			kind, weight = "Go language alias", 60
		}
		if weight >= 40 {
			matches = append(matches, MatchEvidence{c.Text, kind, weight})
		}
	}
	if len(matches) == 0 {
		return nil
	}
	sort.Slice(matches, func(i, j int) bool {
		if matches[i].Weight == matches[j].Weight {
			if matches[i].Concept == matches[j].Concept {
				return matches[i].Kind < matches[j].Kind
			}
			return matches[i].Concept < matches[j].Concept
		}
		return matches[i].Weight > matches[j].Weight
	})
	return &matches[0]
}
